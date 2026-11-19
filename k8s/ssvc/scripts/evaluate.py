#!/usr/bin/env python3
"""k8s VulnerabilityReport と repo-scan の脆弱性を SSVC で継続的に評価し、メトリクスとログを出す。

標準ライブラリだけで動く。k8s/ssvc の CronJob で毎日実行する。

入力:
  - k8s API: VulnerabilityReport (aquasecurity.github.io/v1alpha1)・Ingress (networking.k8s.io/v1)・Pod (v1)・Job (batch/v1)
  - VictoriaLogs: repo-scan Namespace の最新 run_id の vulnerability 指摘
  - 資産の宣言 (assets.yaml) と例外ファイル (overrides.yaml)
  - SSVC Deployer 判定表 (deployer_patch_application_priority_nuage.csv、上流の deployer_patch_application_priority_1_0_0.csv との差分を判定出所に反映)
  - CISA KEV / CISA Vulnrichment / FIRST EPSS (参考値)

出力:
  - 標準出力: 1 件 1 行の JSON (vlagent が VictoriaLogs に入れる)
      event="ssvc_finding"     評価した CVE ごとの結果
      event="no_scan_result"   VulnerabilityReport が無い稼働中ワークロードの一覧
      event="unscanned_asset"  未スキャン資産 (hosts) の一覧
      event="expired_override" 期限切れの例外の一覧
      event="eval_done"        実行完了のサマリ (最新 run_id の識別に使用)
  - メトリクス (Prometheus テキスト形式): vmagent の /api/v1/import/prometheus に送る
"""

import argparse
import concurrent.futures
import csv
import datetime
import json
import os
import re
import ssl
import sys
import time
import urllib.error
import urllib.parse
import urllib.request
from zoneinfo import ZoneInfo

JST = ZoneInfo("Asia/Tokyo")

CVE_RE = re.compile(r"^CVE-(\d{4})-(\d{4,})$")
PR_NAMESPACE_RE = re.compile(r".+-pr-[0-9]+$")
REPLICASET_HASH_RE = re.compile(r"^(.+)-[bcdfghjklmnpqrstvwxz2456789]{6,10}$")
ARGO_POD_RE = re.compile(r"^(.+)-[0-9]{10}(-.+)?-[0-9]+$")

EXPLOITATION_ORDER = ("none", "public poc", "active")
EXPOSURE_ORDER = ("small", "controlled", "open")
AUTOMATABLE_ORDER = ("no", "yes")
HUMAN_IMPACT_ORDER = ("low", "medium", "high", "very high")

SSVC_DECISIONS = ("immediate", "out-of-cycle", "scheduled", "defer")
REGISTERED_METRIC_DECISIONS = (
    "immediate",
    "out-of-cycle",
    "scheduled",
    "defer",
    "unknown",
    "not-affected",
)
OVERRIDE_DECISIONS = frozenset(SSVC_DECISIONS + ("not-affected",))

VULNRICHMENT_EXPLOITATION_MAP = {
    "none": "none",
    "poc": "public poc",
    "public poc": "public poc",
    "active": "active",
}
VULNRICHMENT_AUTOMATABLE_MAP = {
    "no": "no",
    "yes": "yes",
}

DEFAULT_KEV_URL = (
    "https://www.cisa.gov/sites/default/files/feeds/known_exploited_vulnerabilities.json"
)
DEFAULT_VULNRICHMENT_BASE_URL = (
    "https://raw.githubusercontent.com/cisagov/vulnrichment/develop"
)
DEFAULT_EPSS_URL = "https://api.first.org/data/v1/epss"


# ---------------------------------------------------------------------------
# 簡易 YAML パーサー (標準ライブラリのみ。未対応構文は黙らず ValueError にする)
# ---------------------------------------------------------------------------


def _strip_yaml_comment(line):
    """クォートの外側にある # コメントを落とす。"""
    in_single = False
    in_double = False
    escaped = False
    for idx, ch in enumerate(line):
        if escaped:
            escaped = False
            continue
        if in_double and ch == "\\":
            escaped = True
            continue
        if ch == "'" and not in_double:
            in_single = not in_single
        elif ch == '"' and not in_single:
            in_double = not in_double
        elif ch == "#" and not in_single and not in_double:
            if idx == 0 or line[idx - 1].isspace():
                return line[:idx].rstrip()
    return line.rstrip()


def _split_inline_items(text):
    """カンマ区切りのインライン要素をクォート・括弧の入れ子を考慮して分割する。"""
    items = []
    buf = []
    depth = 0
    in_single = False
    in_double = False
    escaped = False
    for ch in text:
        if escaped:
            buf.append(ch)
            escaped = False
            continue
        if in_double and ch == "\\":
            buf.append(ch)
            escaped = True
            continue
        if ch == "'" and not in_double:
            in_single = not in_single
        elif ch == '"' and not in_single:
            in_double = not in_double
        elif not in_single and not in_double:
            if ch in "[{":
                depth += 1
            elif ch in "]}":
                depth -= 1
                if depth < 0:
                    raise ValueError(f"インライン括弧の対応が不正である: {text}")
            elif ch == "," and depth == 0:
                items.append("".join(buf).strip())
                buf = []
                continue
        buf.append(ch)
    if in_single or in_double or depth != 0:
        raise ValueError(f"閉じられていないクォートまたは括弧がある: {text}")
    tail = "".join(buf).strip()
    if tail:
        items.append(tail)
    return items


def _find_colon_outside_quotes(text):
    """クォート・括弧外のキー区切り ':' の位置を返す。無ければ -1。"""
    depth = 0
    in_single = False
    in_double = False
    escaped = False
    for idx, ch in enumerate(text):
        if escaped:
            escaped = False
            continue
        if in_double and ch == "\\":
            escaped = True
            continue
        if ch == "'" and not in_double:
            in_single = not in_single
        elif ch == '"' and not in_single:
            in_double = not in_double
        elif not in_single and not in_double:
            if ch in "[{":
                depth += 1
            elif ch in "]}":
                depth -= 1
            elif ch == ":" and depth == 0:
                if idx + 1 == len(text) or text[idx + 1].isspace():
                    return idx
    return -1


def _parse_scalar(raw):
    """YAML のスカラーおよびインライン配列/マップを Python 値に変換する。"""
    text = raw.strip()
    if not text:
        return None
    if text[0] in ("&", "*", "!"):
        raise ValueError(f"未対応の YAML 構文 (アンカー/タグ): {text}")
    if text in ("|", ">", "|-", ">-", "|+", ">+"):
        raise ValueError(f"未対応の YAML 構文 (複数行スカラー): {text}")

    if text.startswith("["):
        if not text.endswith("]"):
            raise ValueError(f"閉じられていないインライン配列: {text}")
        inner = text[1:-1].strip()
        if not inner:
            return []
        return [_parse_scalar(item) for item in _split_inline_items(inner)]

    if text.startswith("{"):
        if not text.endswith("}"):
            raise ValueError(f"閉じられていないインラインマップ: {text}")
        inner = text[1:-1].strip()
        if not inner:
            return {}
        result = {}
        for item in _split_inline_items(inner):
            colon = _find_colon_outside_quotes(item)
            if colon < 0:
                raise ValueError(f"インラインマップの形式が不正である: {item}")
            key = _parse_scalar(item[:colon])
            val = _parse_scalar(item[colon + 1 :])
            result[str(key)] = val
        return result

    if text.startswith('"'):
        if len(text) < 2 or not text.endswith('"'):
            raise ValueError(f"閉じられていないダブルクォート文字列: {text}")
        return json.loads(text)

    if text.startswith("'"):
        if len(text) < 2 or not text.endswith("'"):
            raise ValueError(f"閉じられていないシングルクォート文字列: {text}")
        return text[1:-1].replace("''", "'")

    lower = text.lower()
    if lower in ("null", "~"):
        return None
    if lower == "true":
        return True
    if lower == "false":
        return False
    if re.fullmatch(r"-?[0-9]+", text):
        return int(text)
    if re.fullmatch(r"-?[0-9]+\.[0-9]+", text):
        return float(text)
    return text


def _tokenize_yaml(text):
    """YAML テキストを (行番号, インデント幅, 内容) のリストに分解する。"""
    tokens = []
    doc_markers = 0
    for lineno, raw_line in enumerate(text.splitlines(), start=1):
        if "\t" in raw_line[: len(raw_line) - len(raw_line.lstrip(" \t"))]:
            raise ValueError(f"{lineno} 行目: インデントにタブ文字は使えない")
        stripped = _strip_yaml_comment(raw_line)
        if not stripped:
            continue
        content = stripped.lstrip(" ")
        if content == "---":
            doc_markers += 1
            if doc_markers > 1:
                raise ValueError(f"{lineno} 行目: 複数ドキュメント (---) は未対応である")
            continue
        if content == "...":
            break
        indent = len(stripped) - len(content)
        tokens.append((lineno, indent, content))
    return tokens


def _parse_yaml_block(tokens, idx, min_indent):
    """tokens[idx] から min_indent 以上のブロックをパースし (値, 次の idx) を返す。"""
    if idx >= len(tokens):
        return None, idx

    lineno, indent, content = tokens[idx]
    if indent < min_indent:
        return None, idx

    if content == "-" or content.startswith("- "):
        items = []
        list_indent = indent
        while idx < len(tokens):
            t_line, t_indent, t_content = tokens[idx]
            if t_indent < list_indent:
                break
            if t_indent > list_indent:
                raise ValueError(f"{t_line} 行目: 配列要素のインデントが深すぎる")
            if not (t_content == "-" or t_content.startswith("- ")):
                break

            rest = t_content[1:].lstrip(" ") if len(t_content) > 1 else ""
            idx += 1
            if not rest:
                if idx < len(tokens) and tokens[idx][1] > list_indent:
                    val, idx = _parse_yaml_block(tokens, idx, tokens[idx][1])
                    items.append(val)
                else:
                    items.append(None)
                continue

            colon = _find_colon_outside_quotes(rest)
            if colon >= 0:
                # "- key: val" 形式のマップ要素
                item_indent = t_indent + (len(t_content) - len(rest))
                key = str(_parse_scalar(rest[:colon]))
                val_raw = rest[colon + 1 :].strip()
                item_dict = {}
                if val_raw:
                    item_dict[key] = _parse_scalar(val_raw)
                elif idx < len(tokens) and tokens[idx][1] > item_indent:
                    nested, idx = _parse_yaml_block(tokens, idx, tokens[idx][1])
                    item_dict[key] = nested
                else:
                    item_dict[key] = None

                if idx < len(tokens) and tokens[idx][1] == item_indent:
                    cont, idx = _parse_yaml_block(tokens, idx, item_indent)
                    if not isinstance(cont, dict):
                        raise ValueError(
                            f"{tokens[idx - 1][0]} 行目: リスト内マップの続きが不正である"
                        )
                    item_dict.update(cont)
                items.append(item_dict)
            else:
                items.append(_parse_scalar(rest))
        return items, idx

    colon = _find_colon_outside_quotes(content)
    if colon >= 0:
        mapping = {}
        map_indent = indent
        while idx < len(tokens):
            t_line, t_indent, t_content = tokens[idx]
            if t_indent < map_indent:
                break
            if t_indent > map_indent:
                raise ValueError(f"{t_line} 行目: マップのインデントが揃っていない")
            if t_content == "-" or t_content.startswith("- "):
                break
            c_pos = _find_colon_outside_quotes(t_content)
            if c_pos < 0:
                raise ValueError(f"{t_line} 行目: key: value 形式ではない: {t_content}")
            key = str(_parse_scalar(t_content[:c_pos]))
            val_raw = t_content[c_pos + 1 :].strip()
            idx += 1
            if val_raw:
                mapping[key] = _parse_scalar(val_raw)
            elif idx < len(tokens) and tokens[idx][1] > map_indent:
                nested, idx = _parse_yaml_block(tokens, idx, tokens[idx][1])
                mapping[key] = nested
            else:
                mapping[key] = {}
        return mapping, idx

    idx += 1
    return _parse_scalar(content), idx


def parse_yaml(text):
    """YAML テキストをパースして Python のオブジェクト (通常 dict) を返す。"""
    tokens = _tokenize_yaml(text)
    if not tokens:
        return {}
    value, next_idx = _parse_yaml_block(tokens, 0, tokens[0][1])
    if next_idx != len(tokens):
        lineno, _, content = tokens[next_idx]
        raise ValueError(f"{lineno} 行目: 解釈できない行が残っている: {content}")
    return value


# ---------------------------------------------------------------------------
# 資産宣言 (assets.yaml) と例外ファイル (overrides.yaml) の読み込み・検証
# ---------------------------------------------------------------------------


def _require_choice(value, allowed, label):
    if not isinstance(value, str) or value not in allowed:
        raise ValueError(f"{label} は {allowed} のいずれかでなければならない: {value!r}")
    return value


def _require_nonempty_str(value, label):
    if not isinstance(value, str) or not value.strip():
        raise ValueError(f"{label} は空でない文字列でなければならない: {value!r}")
    return value.strip()


def load_assets(path):
    """assets.yaml を読み、構造と値を検証して正規化する。"""
    with open(path, encoding="utf-8") as f:
        raw = parse_yaml(f.read())
    if raw is None:
        raw = {}
    if not isinstance(raw, dict):
        raise ValueError(f"assets.yaml のトップレベルはマップでなければならない: {path}")

    unknown_top = set(raw) - {"namespaces", "repositories", "hosts"}
    if unknown_top:
        raise ValueError(f"assets.yaml に未知のキーがある: {sorted(unknown_top)}")

    namespaces = {}
    raw_ns = raw.get("namespaces") or {}
    if not isinstance(raw_ns, dict):
        raise ValueError("assets.yaml の namespaces はマップでなければならない")
    for ns_name, ns_cfg in raw_ns.items():
        if not isinstance(ns_cfg, dict):
            raise ValueError(f"namespaces.{ns_name} はマップでなければならない")
        unknown = set(ns_cfg) - {
            "human_impact",
            "reason",
            "exposure",
            "exposure_reason",
            "workloads",
        }
        if unknown:
            raise ValueError(f"namespaces.{ns_name} に未知のキーがある: {sorted(unknown)}")
        human_impact = _require_choice(
            ns_cfg.get("human_impact"), HUMAN_IMPACT_ORDER, f"namespaces.{ns_name}.human_impact"
        )
        reason = ns_cfg.get("reason")
        exposure = ns_cfg.get("exposure")
        exposure_reason = ns_cfg.get("exposure_reason")
        if exposure is not None:
            exposure = _require_choice(
                exposure, EXPOSURE_ORDER, f"namespaces.{ns_name}.exposure"
            )
            exposure_reason = _require_nonempty_str(
                exposure_reason, f"namespaces.{ns_name}.exposure_reason"
            )
        workloads = {}
        raw_wls = ns_cfg.get("workloads") or {}
        if not isinstance(raw_wls, dict):
            raise ValueError(f"namespaces.{ns_name}.workloads はマップでなければならない")
        for wl_name, wl_cfg in raw_wls.items():
            if not isinstance(wl_cfg, dict):
                raise ValueError(
                    f"namespaces.{ns_name}.workloads.{wl_name} はマップでなければならない"
                )
            wl_unknown = set(wl_cfg) - {"human_impact", "exposure", "reason"}
            if wl_unknown:
                raise ValueError(
                    f"namespaces.{ns_name}.workloads.{wl_name} に未知のキーがある: {sorted(wl_unknown)}"
                )
            wl_hi = wl_cfg.get("human_impact")
            if wl_hi is not None:
                wl_hi = _require_choice(
                    wl_hi,
                    HUMAN_IMPACT_ORDER,
                    f"namespaces.{ns_name}.workloads.{wl_name}.human_impact",
                )
            wl_exp = wl_cfg.get("exposure")
            wl_reason = wl_cfg.get("reason")
            if wl_exp is not None:
                wl_exp = _require_choice(
                    wl_exp,
                    EXPOSURE_ORDER,
                    f"namespaces.{ns_name}.workloads.{wl_name}.exposure",
                )
                wl_reason = _require_nonempty_str(
                    wl_reason, f"namespaces.{ns_name}.workloads.{wl_name}.reason"
                )
            workloads[str(wl_name)] = {
                "human_impact": wl_hi,
                "exposure": wl_exp,
                "reason": wl_reason,
            }
        namespaces[str(ns_name)] = {
            "human_impact": human_impact,
            "reason": reason,
            "exposure": exposure,
            "exposure_reason": exposure_reason,
            "workloads": workloads,
        }

    repositories = {}
    raw_repos = raw.get("repositories") or {}
    if not isinstance(raw_repos, dict):
        raise ValueError("assets.yaml の repositories はマップでなければならない")
    for repo_name, repo_cfg in raw_repos.items():
        if not isinstance(repo_cfg, dict):
            raise ValueError(f"repositories.{repo_name} はマップでなければならない")
        unknown = set(repo_cfg) - {"namespaces", "human_impact", "exposure", "reason"}
        if unknown:
            raise ValueError(f"repositories.{repo_name} に未知のキーがある: {sorted(unknown)}")
        if "namespaces" in repo_cfg and repo_cfg["namespaces"] is not None:
            if "human_impact" in repo_cfg or "exposure" in repo_cfg:
                raise ValueError(
                    f"repositories.{repo_name}: namespaces と human_impact/exposure は同時に指定できない"
                )
            ns_list = repo_cfg["namespaces"]
            if not isinstance(ns_list, list) or not ns_list:
                raise ValueError(
                    f"repositories.{repo_name}.namespaces は 1 件以上の配列でなければならない"
                )
            resolved_ns = []
            for ns_ref in ns_list:
                ns_str = _require_nonempty_str(
                    ns_ref, f"repositories.{repo_name}.namespaces[]"
                )
                if ns_str not in namespaces:
                    raise ValueError(
                        f"repositories.{repo_name} が参照する Namespace '{ns_str}' が namespaces に無い"
                    )
                resolved_ns.append(ns_str)
            repositories[str(repo_name)] = {
                "namespaces": resolved_ns,
                "human_impact": None,
                "exposure": None,
                "reason": repo_cfg.get("reason") or "",
            }
        else:
            hi = _require_choice(
                repo_cfg.get("human_impact"),
                HUMAN_IMPACT_ORDER,
                f"repositories.{repo_name}.human_impact",
            )
            exp = _require_choice(
                repo_cfg.get("exposure"),
                EXPOSURE_ORDER,
                f"repositories.{repo_name}.exposure",
            )
            repositories[str(repo_name)] = {
                "namespaces": None,
                "human_impact": hi,
                "exposure": exp,
                "reason": str(repo_cfg.get("reason") or ""),
            }

    hosts = {}
    raw_hosts = raw.get("hosts") or {}
    if not isinstance(raw_hosts, dict):
        raise ValueError("assets.yaml の hosts はマップでなければならない")
    for host_name, host_cfg in raw_hosts.items():
        if not isinstance(host_cfg, dict):
            raise ValueError(f"hosts.{host_name} はマップでなければならない")
        unknown = set(host_cfg) - {"kind", "human_impact", "exposure", "scan", "reason"}
        if unknown:
            raise ValueError(f"hosts.{host_name} に未知のキーがある: {sorted(unknown)}")
        hosts[str(host_name)] = {
            "kind": _require_nonempty_str(host_cfg.get("kind"), f"hosts.{host_name}.kind"),
            "human_impact": _require_choice(
                host_cfg.get("human_impact"),
                HUMAN_IMPACT_ORDER,
                f"hosts.{host_name}.human_impact",
            ),
            "exposure": _require_choice(
                host_cfg.get("exposure"), EXPOSURE_ORDER, f"hosts.{host_name}.exposure"
            ),
            "scan": _require_nonempty_str(host_cfg.get("scan"), f"hosts.{host_name}.scan"),
            "reason": str(host_cfg.get("reason") or ""),
        }

    return {"namespaces": namespaces, "repositories": repositories, "hosts": hosts}


def load_overrides(path):
    """overrides.yaml を読み、構造・必須項目・日付形式を検証する。"""
    with open(path, encoding="utf-8") as f:
        raw = parse_yaml(f.read())
    if raw is None or raw == {}:
        return []
    if not isinstance(raw, dict):
        raise ValueError(f"overrides.yaml のトップレベルはマップでなければならない: {path}")
    unknown_top = set(raw) - {"overrides"}
    if unknown_top:
        raise ValueError(f"overrides.yaml に未知のキーがある: {sorted(unknown_top)}")
    items = raw.get("overrides")
    if items is None:
        return []
    if not isinstance(items, list):
        raise ValueError("overrides.yaml の overrides は配列でなければならない")

    entries = []
    for idx, entry in enumerate(items):
        label = f"overrides[{idx}]"
        if not isinstance(entry, dict):
            raise ValueError(f"{label} はマップでなければならない")
        unknown = set(entry) - {
            "cve",
            "namespace",
            "workload",
            "repository",
            "decision",
            "reason",
            "expires",
        }
        if unknown:
            raise ValueError(f"{label} に未知のキーがある: {sorted(unknown)}")

        cve = _require_nonempty_str(entry.get("cve"), f"{label}.cve").upper()
        decision = _require_choice(
            entry.get("decision"), tuple(sorted(OVERRIDE_DECISIONS)), f"{label}.decision"
        )
        reason = _require_nonempty_str(entry.get("reason"), f"{label}.reason")
        expires_raw = _require_nonempty_str(entry.get("expires"), f"{label}.expires")
        try:
            expires_date = datetime.date.fromisoformat(expires_raw)
        except ValueError as e:
            raise ValueError(f"{label}.expires は YYYY-MM-DD 形式でなければならない: {expires_raw}") from e

        has_ns = entry.get("namespace") is not None
        has_wl = entry.get("workload") is not None
        has_repo = entry.get("repository") is not None
        if has_repo and (has_ns or has_wl):
            raise ValueError(f"{label}: namespace/workload と repository は同時に指定できない")
        if not has_repo and not (has_ns and has_wl):
            raise ValueError(f"{label}: namespace + workload、または repository の指定が要る")

        if has_repo:
            repo = _require_nonempty_str(entry.get("repository"), f"{label}.repository")
            target_type = "repo"
            namespace = ""
            workload = ""
            target = repo
        else:
            namespace = _require_nonempty_str(entry.get("namespace"), f"{label}.namespace")
            workload = _require_nonempty_str(entry.get("workload"), f"{label}.workload")
            repo = ""
            target_type = "k8s"
            target = f"{namespace}/{workload}"

        entries.append(
            {
                "cve": cve,
                "target_type": target_type,
                "namespace": namespace,
                "workload": workload,
                "repository": repo,
                "target": target,
                "decision": decision,
                "reason": reason,
                "expires": expires_raw,
                "expires_date": expires_date,
            }
        )
    return entries


def split_overrides(entries, today):
    """例外エントリを「有効」と「期限切れ」に分ける。有効な重複はエラーにする。"""
    active = {}
    expired = []
    for entry in entries:
        if today > entry["expires_date"]:
            expired.append(entry)
            continue
        key = (entry["cve"], entry["target_type"], entry["namespace"], entry["workload"], entry["repository"])
        if key in active:
            raise ValueError(f"有効な例外が重複している: {entry['cve']} ({entry['target']})")
        active[key] = entry
    return active, expired


# ---------------------------------------------------------------------------
# CERT/CC SSVC Deployer 判定表
# ---------------------------------------------------------------------------


def load_decision_table(path):
    """CERT/CC SSVC Deployer 表 (CSV) を読み込み、72 通り揃っていることを確かめる。"""
    table = {}
    with open(path, encoding="utf-8", newline="") as f:
        reader = csv.DictReader(f)
        for row in reader:
            cleaned = {k.strip(): v.strip() for k, v in row.items()}
            key = (
                cleaned["Exploitation v1.1.0"],
                cleaned["System Exposure v1.0.1"],
                cleaned["Automatable v2.0.0"],
                cleaned["Human Impact v2.0.2"],
            )
            val = cleaned["Defer, Scheduled, Out-of-Cycle, Immediate v1.0.0"]
            if (
                key[0] not in EXPLOITATION_ORDER
                or key[1] not in EXPOSURE_ORDER
                or key[2] not in AUTOMATABLE_ORDER
                or key[3] not in HUMAN_IMPACT_ORDER
                or val not in SSVC_DECISIONS
            ):
                raise ValueError(f"SSVC 判定表に想定外の値がある: {key} -> {val}")
            table[key] = val

    expected_count = (
        len(EXPLOITATION_ORDER)
        * len(EXPOSURE_ORDER)
        * len(AUTOMATABLE_ORDER)
        * len(HUMAN_IMPACT_ORDER)
    )
    if len(table) != expected_count:
        raise ValueError(
            f"SSVC 判定表の組み合わせ数が {expected_count} ではない: {len(table)}"
        )
    return table


def diff_decision_tables(base_table, custom_table):
    """基準の判定表と判定結果が異なる 4 決定点のキー集合を返す。"""
    if base_table is None:
        return frozenset()
    return frozenset(
        key for key, val in custom_table.items() if base_table.get(key) != val
    )


def evaluate_ssvc(exploitation, exposure, automatable, human_impact, table):
    """4 つの決定点から SSVC 判定を返す。いずれかが欠けていれば 'unknown' にする。"""
    if (
        exploitation not in EXPLOITATION_ORDER
        or exposure not in EXPOSURE_ORDER
        or automatable not in AUTOMATABLE_ORDER
        or human_impact not in HUMAN_IMPACT_ORDER
    ):
        return "unknown"
    return table[(exploitation, exposure, automatable, human_impact)]


# ---------------------------------------------------------------------------
# 対象正規化と資産解決
# ---------------------------------------------------------------------------


def normalize_workload(resource_kind, resource_name):
    """既存の Grafana / VMRule と同じ規則で ReplicaSet・Argo Pod をワークロード名にまとめる。"""
    if resource_kind == "ReplicaSet":
        return REPLICASET_HASH_RE.sub(r"\1", resource_name)
    if resource_kind == "Pod":
        m = ARGO_POD_RE.match(resource_name)
        if m:
            return m.group(1) + (m.group(2) or "")
    return resource_name


def _resolve_namespace_asset(ns_name, assets, ingress_namespaces):
    """Namespace 単体の (human_impact, human_impact_source, exposure, exposure_source) を解決する。"""
    ns_cfg = assets["namespaces"][ns_name]
    hi = ns_cfg["human_impact"]
    hi_src = "declared_namespace"
    if ns_cfg["exposure"] is not None:
        exp = ns_cfg["exposure"]
        exp_src = "declared_namespace"
    else:
        exp = "open" if ns_name in ingress_namespaces else "controlled"
        exp_src = "auto_ingress"
    return hi, hi_src, exp, exp_src


def resolve_asset_for_k8s(namespace, workload, assets, ingress_namespaces):
    """k8s ワークロードの重要度・露出を解決する。未登録なら registered=False。"""
    if namespace not in assets["namespaces"]:
        return {"registered": False}
    ns_cfg = assets["namespaces"][namespace]
    hi, hi_src, exp, exp_src = _resolve_namespace_asset(
        namespace, assets, ingress_namespaces
    )
    wl_cfg = ns_cfg["workloads"].get(workload)
    if wl_cfg:
        if wl_cfg["human_impact"] is not None:
            hi = wl_cfg["human_impact"]
            hi_src = "declared_workload"
        if wl_cfg["exposure"] is not None:
            exp = wl_cfg["exposure"]
            exp_src = "declared_workload"
    return {
        "registered": True,
        "human_impact": hi,
        "human_impact_source": hi_src,
        "exposure": exp,
        "exposure_source": exp_src,
    }


def resolve_asset_for_repo(repo_name, assets, ingress_namespaces):
    """リポジトリの重要度・露出を解決する。複数 Namespace なら最も重い値を使う。"""
    if repo_name not in assets["repositories"]:
        return {"registered": False}
    repo_cfg = assets["repositories"][repo_name]
    if repo_cfg["namespaces"]:
        hi_candidates = []
        exp_candidates = []
        for ns_name in repo_cfg["namespaces"]:
            hi, _, exp, exp_src = _resolve_namespace_asset(
                ns_name, assets, ingress_namespaces
            )
            hi_candidates.append((HUMAN_IMPACT_ORDER.index(hi), hi, f"declared_repo_namespaces({ns_name})"))
            exp_src_label = (
                f"auto_ingress_repo_namespaces({ns_name})"
                if exp_src == "auto_ingress"
                else f"declared_repo_namespaces({ns_name})"
            )
            exp_candidates.append((EXPOSURE_ORDER.index(exp), exp, exp_src_label))
        _, best_hi, best_hi_src = max(hi_candidates, key=lambda x: x[0])
        _, best_exp, best_exp_src = max(exp_candidates, key=lambda x: x[0])
        return {
            "registered": True,
            "human_impact": best_hi,
            "human_impact_source": best_hi_src,
            "exposure": best_exp,
            "exposure_source": best_exp_src,
        }
    return {
        "registered": True,
        "human_impact": repo_cfg["human_impact"],
        "human_impact_source": "declared_repository",
        "exposure": repo_cfg["exposure"],
        "exposure_source": "declared_repository",
    }


# ---------------------------------------------------------------------------
# 外部データ取得 (k8s API / VictoriaLogs / KEV / Vulnrichment / EPSS)
# ---------------------------------------------------------------------------


def parse_cve_id(cve_id):
    """CVE-YYYY-NNNN... に一致すれば (year, number) を返す。CVE でなければ None。"""
    if not cve_id:
        return None
    m = CVE_RE.match(cve_id.strip().upper())
    if not m:
        return None
    return int(m.group(1)), int(m.group(2))


def vulnrichment_path(cve_id):
    """CVE ID から cisagov/vulnrichment 内の相対パスを返す。"""
    parsed = parse_cve_id(cve_id)
    if not parsed:
        return None
    year, num = parsed
    return f"{year}/{num // 1000}xxx/{cve_id.strip().upper()}.json"


def extract_vulnrichment_ssvc(doc):
    """CVE JSON 5 の containers.adp[].metrics[].other (type=='ssvc') から Exploitation と Automatable を取る。"""
    adp_list = (doc.get("containers") or {}).get("adp") or []
    exploitation = None
    automatable = None
    for adp in adp_list:
        if not isinstance(adp, dict):
            continue
        for metric in adp.get("metrics") or []:
            if not isinstance(metric, dict):
                continue
            other = metric.get("other")
            if not isinstance(other, dict) or other.get("type") != "ssvc":
                continue
            content = other.get("content") or {}
            for opt in content.get("options") or []:
                if not isinstance(opt, dict):
                    continue
                if "Exploitation" in opt:
                    raw_exp = str(opt["Exploitation"]).strip().lower()
                    exploitation = VULNRICHMENT_EXPLOITATION_MAP.get(raw_exp)
                if "Automatable" in opt:
                    raw_auto = str(opt["Automatable"]).strip().lower()
                    automatable = VULNRICHMENT_AUTOMATABLE_MAP.get(raw_auto)
            if exploitation is not None or automatable is not None:
                return exploitation, automatable
    return exploitation, automatable


def _http_get_json(url, headers=None, ssl_context=None, timeout=30, attempts=3, wait_seconds=2, allow_404=False):
    """JSON を HTTP GET する。404 が許容される場合は None を返す。"""
    last_error = None
    for attempt in range(1, attempts + 1):
        req = urllib.request.Request(url, headers=headers or {"User-Agent": "nuage-ssvc/1.0"})
        try:
            with urllib.request.urlopen(req, timeout=timeout, context=ssl_context) as resp:
                return json.loads(resp.read().decode("utf-8"))
        except urllib.error.HTTPError as e:
            if allow_404 and e.code == 404:
                return None
            last_error = f"HTTP {e.code}"
            if 400 <= e.code < 500 and e.code != 429:
                raise RuntimeError(f"GET {url}: {last_error}") from e
        except (urllib.error.URLError, OSError, ValueError) as e:
            last_error = repr(e)
        if attempt < attempts:
            time.sleep(wait_seconds * attempt)
    raise RuntimeError(f"GET {url} に失敗した: {last_error}")


def fetch_kev_set(url=DEFAULT_KEV_URL):
    """CISA KEV カタログから CVE ID の集合を返す。"""
    doc = _http_get_json(url, timeout=60, attempts=3)
    return {
        str(v["cveID"]).strip().upper()
        for v in (doc.get("vulnerabilities") or [])
        if isinstance(v, dict) and v.get("cveID")
    }


def fetch_vulnrichment_map(cve_ids, base_url=DEFAULT_VULNRICHMENT_BASE_URL, max_workers=12):
    """ユニークな CVE ID 群について Vulnrichment から (exploitation, automatable) を並列取得する。"""
    unique_cves = sorted({c.strip().upper() for c in cve_ids if parse_cve_id(c)})
    results = {}
    errors = {}
    base = base_url.rstrip("/")

    def _one(cve):
        rel = vulnrichment_path(cve)
        doc = _http_get_json(f"{base}/{rel}", timeout=20, attempts=3, wait_seconds=1, allow_404=True)
        if doc is None:
            return None, None
        return extract_vulnrichment_ssvc(doc)

    with concurrent.futures.ThreadPoolExecutor(max_workers=max_workers) as pool:
        futures = {pool.submit(_one, cve): cve for cve in unique_cves}
        for fut in concurrent.futures.as_completed(futures):
            cve = futures[fut]
            try:
                results[cve] = fut.result()
            except Exception as e:  # noqa: BLE001
                errors[cve] = repr(e)
    return results, errors


def fetch_epss_map(cve_ids, base_url=DEFAULT_EPSS_URL, batch_size=50):
    """EPSS API から CVE ごとの (epss, percentile) をバッチ取得する。失敗時はエラー内容を返す。"""
    unique_cves = sorted({c.strip().upper() for c in cve_ids if parse_cve_id(c)})
    results = {}
    errors = []
    for start in range(0, len(unique_cves), batch_size):
        chunk = unique_cves[start : start + batch_size]
        url = f"{base_url}?cve={urllib.parse.quote(','.join(chunk), safe=',')}"
        try:
            doc = _http_get_json(url, timeout=30, attempts=3, wait_seconds=2)
            for row in doc.get("data") or []:
                cve = str(row.get("cve") or "").strip().upper()
                if not cve:
                    continue
                try:
                    epss_val = float(row["epss"])
                except (KeyError, TypeError, ValueError):
                    epss_val = None
                try:
                    pct_val = float(row["percentile"])
                except (KeyError, TypeError, ValueError):
                    pct_val = None
                results[cve] = (epss_val, pct_val)
        except Exception as e:  # noqa: BLE001
            errors.append(f"EPSS batch {chunk[0]}..{chunk[-1]}: {e!r}")
    return results, errors


class K8sClient:
    """ServiceAccount トークンを使って Kubernetes API からページネーション付きで読む最小クライアント。"""

    DEFAULT_CA_FILE = "/var/run/secrets/kubernetes.io/serviceaccount/ca.crt"

    def __init__(self, api_server=None, token=None, ca_file=DEFAULT_CA_FILE):
        if api_server is None:
            host = os.environ["KUBERNETES_SERVICE_HOST"]
            port = os.environ.get("KUBERNETES_SERVICE_PORT", "443")
            api_server = f"https://{host}:{port}"
        if token is None:
            with open("/var/run/secrets/kubernetes.io/serviceaccount/token", encoding="utf-8") as f:
                token = f.read().strip()
        self.api_server = api_server.rstrip("/")
        self.headers = {"Authorization": f"Bearer {token}", "Accept": "application/json"}
        self.ssl_context = ssl.create_default_context(cafile=ca_file) if ca_file else None

    def iter_items(self, path, page_size=50):
        """ページごとにアイテムを順次返し、巨大な一覧を一度にメモリに溜めない。"""
        cont = None
        while True:
            params = {"limit": str(page_size)}
            if cont:
                params["continue"] = cont
            url = f"{self.api_server}{path}?{urllib.parse.urlencode(params)}"
            doc = _http_get_json(url, headers=self.headers, ssl_context=self.ssl_context)
            for item in doc.get("items") or []:
                yield item
            cont = (doc.get("metadata") or {}).get("continue")
            if not cont:
                break

    def list_all(self, path, page_size=50):
        return list(self.iter_items(path, page_size=page_size))


def _iter_k8s_resources(k8s_client, path):
    if hasattr(k8s_client, "iter_items"):
        return k8s_client.iter_items(path)
    return k8s_client.list_all(path)


def _resolve_pod_workload(pod, job_owners):
    """Pod のメタデータからスキャン対象の (namespace, resource_kind, resource_name) を返す。対象外なら None。"""
    meta = pod.get("metadata") or {}
    ns = meta.get("namespace") or ""
    if not ns or PR_NAMESPACE_RE.match(ns):
        return None
    if meta.get("deletionTimestamp"):
        return None
    phase = (pod.get("status") or {}).get("phase") or ""
    if phase in ("Succeeded", "Failed"):
        return None

    pod_name = meta.get("name") or ""
    owner_refs = meta.get("ownerReferences") or []
    if not owner_refs:
        return (ns, "Pod", pod_name) if pod_name else None

    owner = owner_refs[0]
    owner_kind = owner.get("kind") or ""
    owner_name = owner.get("name") or ""
    if owner_kind in ("ReplicaSet", "StatefulSet", "DaemonSet", "ReplicationController"):
        return (ns, owner_kind, owner_name) if owner_name else None
    if owner_kind == "Job":
        parent = job_owners.get((ns, owner_name))
        if parent and parent[0] == "CronJob" and parent[1]:
            return ns, "CronJob", parent[1]
        if ns == "trivy-operator":
            return None
        return (ns, "Job", owner_name) if owner_name else None
    return (ns, "Pod", pod_name) if pod_name else None


def collect_running_workloads(k8s_client):
    """稼働中の Pod から PR 環境や完了済み Job を除いたスキャン対象ワークロード集合を返す。"""
    job_owners = {}
    for job in _iter_k8s_resources(k8s_client, "/apis/batch/v1/jobs"):
        meta = job.get("metadata") or {}
        ns = meta.get("namespace") or ""
        name = meta.get("name") or ""
        owner_refs = meta.get("ownerReferences") or []
        if ns and name and owner_refs:
            job_owners[(ns, name)] = (
                owner_refs[0].get("kind") or "",
                owner_refs[0].get("name") or "",
            )

    workloads = set()
    for pod in _iter_k8s_resources(k8s_client, "/api/v1/pods"):
        resolved = _resolve_pod_workload(pod, job_owners)
        if resolved is not None:
            workloads.add(resolved)
    return workloads


def collect_k8s_data(k8s_client):
    """Ingress・VulnerabilityReport と、レポートが無い稼働中ワークロード一覧を返す。"""
    ingress_namespaces = {
        (ing.get("metadata") or {}).get("namespace")
        for ing in _iter_k8s_resources(k8s_client, "/apis/networking.k8s.io/v1/ingresses")
        if (ing.get("metadata") or {}).get("namespace")
    }

    findings = []
    targets = set()
    scanned_workloads = set()
    seen_keys = set()

    for rep in _iter_k8s_resources(
        k8s_client, "/apis/aquasecurity.github.io/v1alpha1/vulnerabilityreports"
    ):
        meta = rep.get("metadata") or {}
        ns = meta.get("namespace") or ""
        if not ns or PR_NAMESPACE_RE.match(ns):
            continue
        labels = meta.get("labels") or {}
        owner_refs = meta.get("ownerReferences") or []
        resource_kind = labels.get("trivy-operator.resource.kind") or (
            owner_refs[0].get("kind") if owner_refs else ""
        )
        resource_name = labels.get("trivy-operator.resource.name") or (
            owner_refs[0].get("name") if owner_refs else meta.get("name") or ""
        )
        container = labels.get("trivy-operator.container.name") or ""
        workload = normalize_workload(resource_kind, resource_name)
        target = f"{ns}/{workload}"
        targets.add(("k8s", ns, workload))
        if resource_kind and resource_name:
            scanned_workloads.add((ns, resource_kind, resource_name))

        report = rep.get("report") or {}
        artifact = report.get("artifact") or {}
        registry = (report.get("registry") or {}).get("server") or ""
        repo_img = artifact.get("repository") or ""
        tag = artifact.get("tag") or ""
        image_ref = "/".join(p for p in (registry, repo_img) if p)
        if tag:
            image_ref = f"{image_ref}:{tag}"

        for vuln in report.get("vulnerabilities") or []:
            cve_id = str(vuln.get("vulnerabilityID") or "").strip().upper()
            pkg_name = vuln.get("resource") or ""
            installed_ver = vuln.get("installedVersion") or ""
            dedup_key = (ns, workload, container, cve_id, pkg_name, installed_ver)
            if dedup_key in seen_keys:
                continue
            seen_keys.add(dedup_key)

            findings.append(
                {
                    "source": "k8s",
                    "namespace": ns,
                    "workload": workload,
                    "container": container,
                    "repository": "",
                    "target": target,
                    "cve_id": cve_id,
                    "pkg_name": pkg_name,
                    "installed_version": installed_ver,
                    "fixed_version": vuln.get("fixedVersion") or "",
                    "trivy_severity": str(vuln.get("severity") or "UNKNOWN").upper(),
                    "title": vuln.get("title") or cve_id,
                    "url": vuln.get("primaryLink") or "",
                    "image": image_ref,
                }
            )

    running_workloads = collect_running_workloads(k8s_client)
    no_scan_result_targets = []
    for ns, resource_kind, resource_name in sorted(running_workloads - scanned_workloads):
        workload = normalize_workload(resource_kind, resource_name)
        no_scan_result_targets.append(
            {
                "namespace": ns,
                "resource_kind": resource_kind,
                "resource_name": resource_name,
                "workload": workload,
                "target": f"{ns}/{workload}",
            }
        )

    return ingress_namespaces, findings, targets, no_scan_result_targets


def query_victorialogs(endpoint, expr, timeout=60):
    """VictoriaLogs の /select/logsql/query に POST して JSON 行のリストを返す。"""
    body = urllib.parse.urlencode({"query": expr}).encode("utf-8")
    last_error = None
    for attempt in range(1, 4):
        req = urllib.request.Request(endpoint, data=body, method="POST")
        try:
            with urllib.request.urlopen(req, timeout=timeout) as resp:
                text = resp.read().decode("utf-8")
                return [json.loads(line) for line in text.splitlines() if line.strip()]
        except (urllib.error.URLError, OSError, ValueError) as e:
            last_error = repr(e)
        if attempt < 3:
            time.sleep(2 * attempt)
    raise RuntimeError(f"VictoriaLogs の問い合わせに失敗した: {endpoint}: {last_error}")


def collect_repo_scan_data(vlogs_url, lookback="7d"):
    """VictoriaLogs から repo ごとの最新 trivy run_id の vulnerability 指摘を取る。"""
    done_rows = query_victorialogs(
        vlogs_url,
        f'_time:{lookback} kubernetes.pod_namespace:="repo-scan" event:="scan_done" scanner:="trivy" '
        "| fields _time, repo, run_id",
    )
    latest_by_repo = {}
    for row in done_rows:
        repo = row.get("repo") or ""
        run_id = row.get("run_id") or ""
        ts = row.get("_time") or ""
        if not repo or not run_id:
            continue
        prev = latest_by_repo.get(repo)
        if prev is None or ts > prev[0]:
            latest_by_repo[repo] = (ts, run_id)

    targets = {("repo", "", repo) for repo in latest_by_repo}
    if not latest_by_repo:
        return [], targets, {}

    run_ids = sorted({run_id for _, run_id in latest_by_repo.values()})
    quoted_ids = ", ".join(json.dumps(rid) for rid in run_ids)
    finding_rows = query_victorialogs(
        vlogs_url,
        f'_time:{lookback} kubernetes.pod_namespace:="repo-scan" event:="finding" '
        f'category:="vulnerability" run_id:in({quoted_ids})',
    )

    findings = []
    for row in finding_rows:
        repo = row.get("repo") or ""
        run_id = row.get("run_id") or ""
        expected = latest_by_repo.get(repo)
        if not expected or run_id != expected[1]:
            continue
        cve_id = str(row.get("rule_id") or "").strip().upper()
        findings.append(
            {
                "source": "repo",
                "namespace": "",
                "workload": "",
                "container": "",
                "repository": repo,
                "target": repo,
                "cve_id": cve_id,
                "pkg_name": row.get("pkg_name") or "",
                "installed_version": row.get("installed_version") or "",
                "fixed_version": row.get("fixed_version") or "",
                "trivy_severity": str(row.get("severity") or "Unknown").upper(),
                "title": row.get("title") or cve_id,
                "url": row.get("url") or "",
                "file": row.get("file") or "",
            }
        )
    return findings, targets, {r: rid for r, (_, rid) in latest_by_repo.items()}


# ---------------------------------------------------------------------------
# 評価・ログ・メトリクス生成
# ---------------------------------------------------------------------------


ALL_METRIC_DECISIONS = REGISTERED_METRIC_DECISIONS + ("unregistered",)
ALL_METRIC_EXPLOITATIONS = ("active", "public poc", "none", "missing", "error")

_SCRIPTS_DIR = os.path.dirname(os.path.abspath(__file__))
DEFAULT_TABLE_PATH = os.path.join(_SCRIPTS_DIR, "deployer_patch_application_priority_nuage.csv")
DEFAULT_BASE_TABLE_PATH = os.path.join(_SCRIPTS_DIR, "deployer_patch_application_priority_1_0_0.csv")
_DEFAULT_BASE_TABLE = object()


def evaluate_findings(
    findings,
    targets,
    assets,
    ingress_namespaces,
    active_overrides,
    kev_set,
    vulnrichment_map,
    epss_map,
    decision_table,
    vulnrichment_errors=None,
    base_decision_table=None,
):
    """全脆弱性指摘を SSVC で評価し、評価済みレコード群と対象別集計を返す。"""
    vulnrichment_errors = vulnrichment_errors or {}
    customized_keys = diff_decision_tables(base_decision_table, decision_table)
    evaluated = []
    target_registration = {}
    counts = {}

    for source, namespace, target in targets:
        if source == "k8s":
            asset_info = resolve_asset_for_k8s(namespace, target, assets, ingress_namespaces)
        else:
            asset_info = resolve_asset_for_repo(target, assets, ingress_namespaces)
        target_registration[(source, namespace, target)] = asset_info["registered"]
        for dec in ALL_METRIC_DECISIONS:
            counts[(source, namespace, target, dec)] = 0

    for item in findings:
        source = item["source"]
        namespace = item["namespace"]
        target_label = item["workload"] if source == "k8s" else item["repository"]
        target_key = (source, namespace, target_label)

        if source == "k8s":
            asset_info = resolve_asset_for_k8s(namespace, target_label, assets, ingress_namespaces)
            ovr_key = (item["cve_id"], "k8s", namespace, target_label, "")
        else:
            asset_info = resolve_asset_for_repo(target_label, assets, ingress_namespaces)
            ovr_key = (item["cve_id"], "repo", "", "", target_label)

        target_registration[target_key] = asset_info["registered"]
        for dec in ALL_METRIC_DECISIONS:
            counts.setdefault((source, namespace, target_label, dec), 0)

        cve_id = item["cve_id"]
        epss_val, epss_pct = epss_map.get(cve_id, (None, None))
        has_vulnrichment_error = cve_id in vulnrichment_errors

        if cve_id in kev_set:
            exploitation = "active"
            exploitation_source = "KEV"
        elif cve_id in vulnrichment_map and vulnrichment_map[cve_id][0] is not None:
            exploitation = vulnrichment_map[cve_id][0]
            exploitation_source = "Vulnrichment"
        elif has_vulnrichment_error:
            exploitation = None
            exploitation_source = "error"
        else:
            exploitation = None
            exploitation_source = "missing"

        if cve_id in vulnrichment_map and vulnrichment_map[cve_id][1] is not None:
            automatable = vulnrichment_map[cve_id][1]
            automatable_source = "Vulnrichment"
        elif has_vulnrichment_error:
            automatable = None
            automatable_source = "error"
        else:
            automatable = None
            automatable_source = "missing"

        override = active_overrides.get(ovr_key)

        if not asset_info["registered"]:
            decision = "unregistered"
            decision_source = "unregistered"
            human_impact = None
            human_impact_source = "unregistered"
            exposure = None
            exposure_source = "unregistered"
            override_reason = ""
            override_expires = ""
        elif override is not None:
            decision = override["decision"]
            decision_source = "override"
            human_impact = asset_info["human_impact"]
            human_impact_source = asset_info["human_impact_source"]
            exposure = asset_info["exposure"]
            exposure_source = asset_info["exposure_source"]
            override_reason = override["reason"]
            override_expires = override["expires"]
        else:
            human_impact = asset_info["human_impact"]
            human_impact_source = asset_info["human_impact_source"]
            exposure = asset_info["exposure"]
            exposure_source = asset_info["exposure_source"]
            decision = evaluate_ssvc(
                exploitation, exposure, automatable, human_impact, decision_table
            )
            if decision == "unknown":
                decision_source = "error" if has_vulnrichment_error else "missing_data"
            elif (exploitation, exposure, automatable, human_impact) in customized_keys:
                decision_source = "ssvc_table_custom"
            else:
                decision_source = "ssvc_table"
            override_reason = ""
            override_expires = ""

        counts[(source, namespace, target_label, decision)] = (
            counts.get((source, namespace, target_label, decision), 0) + 1
        )

        record = dict(item)
        record.update(
            {
                "decision": decision,
                "decision_source": decision_source,
                "exploitation": exploitation,
                "exploitation_source": exploitation_source,
                "exposure": exposure,
                "exposure_source": exposure_source,
                "automatable": automatable,
                "automatable_source": automatable_source,
                "human_impact": human_impact,
                "human_impact_source": human_impact_source,
                "epss": epss_val,
                "epss_percentile": epss_pct,
                "override_reason": override_reason,
                "override_expires": override_expires,
            }
        )
        evaluated.append(record)

    return evaluated, counts, target_registration


def _label_value(value):
    return str(value).replace("\\", "\\\\").replace("\n", "\\n").replace('"', '\\"')


def render_metrics(
    counts,
    target_registration,
    assets,
    expired_overrides,
    now,
    include_last_success=True,
    evaluated=None,
    no_scan_result_targets=None,
):
    """Prometheus テキスト形式を生成する。CVE ID はラベルに含めない。"""
    no_scan_result_targets = no_scan_result_targets or []
    lines = [
        "# TYPE nuage_ssvc_findings gauge",
        "# TYPE nuage_ssvc_unregistered_targets_total gauge",
        "# TYPE nuage_ssvc_no_scan_result_targets_total gauge",
        "# TYPE nuage_ssvc_unscanned_assets gauge",
        "# TYPE nuage_ssvc_unscanned_assets_total gauge",
        "# TYPE nuage_ssvc_expired_overrides_total gauge",
        "# TYPE nuage_ssvc_last_run_timestamp_seconds gauge",
    ]
    if evaluated is not None:
        lines.append("# TYPE nuage_ssvc_exploitation_findings gauge")
        lines.append("# TYPE nuage_ssvc_active_exploitation_total gauge")
    if include_last_success:
        lines.append("# TYPE nuage_ssvc_last_success_timestamp_seconds gauge")

    for (source, namespace, target, decision), value in sorted(counts.items()):
        lines.append(
            "nuage_ssvc_findings{"
            f'source="{_label_value(source)}",'
            f'namespace="{_label_value(namespace)}",'
            f'target="{_label_value(target)}",'
            f'decision="{_label_value(decision)}"'
            f"}} {value}"
        )

    if evaluated is not None:
        exp_counts = {}
        for source, namespace, target in target_registration:
            for exp in ALL_METRIC_EXPLOITATIONS:
                exp_counts[(source, namespace, target, exp)] = 0
        active_exploitation_total = 0
        for item in evaluated:
            source = item["source"]
            namespace = item["namespace"]
            target_label = item["workload"] if source == "k8s" else item["repository"]
            exp_key = (
                item["exploitation"]
                if item["exploitation"] is not None
                else item["exploitation_source"]
            )
            for exp in ALL_METRIC_EXPLOITATIONS:
                exp_counts.setdefault((source, namespace, target_label, exp), 0)
            exp_counts[(source, namespace, target_label, exp_key)] = (
                exp_counts.get((source, namespace, target_label, exp_key), 0) + 1
            )
            if item["exploitation"] == "active":
                active_exploitation_total += 1
        for (source, namespace, target, exp), value in sorted(exp_counts.items()):
            lines.append(
                "nuage_ssvc_exploitation_findings{"
                f'source="{_label_value(source)}",'
                f'namespace="{_label_value(namespace)}",'
                f'target="{_label_value(target)}",'
                f'exploitation="{_label_value(exp)}"'
                f"}} {value}"
            )
        lines.append(f"nuage_ssvc_active_exploitation_total {active_exploitation_total}")

    unregistered_count = sum(1 for reg in target_registration.values() if not reg)
    lines.append(f"nuage_ssvc_unregistered_targets_total {unregistered_count}")
    lines.append(f"nuage_ssvc_no_scan_result_targets_total {len(no_scan_result_targets)}")

    hosts = assets.get("hosts") or {}
    unscanned_count = 0
    for host_name, host_cfg in sorted(hosts.items()):
        if host_cfg["scan"] == "none":
            unscanned_count += 1
            lines.append(
                "nuage_ssvc_unscanned_assets{"
                f'host="{_label_value(host_name)}",'
                f'kind="{_label_value(host_cfg["kind"])}",'
                f'human_impact="{_label_value(host_cfg["human_impact"])}",'
                f'exposure="{_label_value(host_cfg["exposure"])}"'
                "} 1"
            )
    lines.append(f"nuage_ssvc_unscanned_assets_total {unscanned_count}")
    lines.append(f"nuage_ssvc_expired_overrides_total {len(expired_overrides)}")
    lines.append(f"nuage_ssvc_last_run_timestamp_seconds {int(now)}")
    if include_last_success:
        lines.append(f"nuage_ssvc_last_success_timestamp_seconds {int(now)}")
    return "\n".join(lines) + "\n"


def build_log_records(
    evaluated,
    target_registration,
    assets,
    expired_overrides,
    run_id,
    no_scan_result_targets=None,
):
    """標準出力用の JSON 行 (ssvc_finding / no_scan_result / unscanned_asset / expired_override / eval_done) を組む。"""
    no_scan_result_targets = no_scan_result_targets or []
    records = []
    decision_totals = {dec: 0 for dec in REGISTERED_METRIC_DECISIONS + ("unregistered",)}

    for item in evaluated:
        dec = item["decision"]
        decision_totals[dec] = decision_totals.get(dec, 0) + 1
        rec = {
            "event": "ssvc_finding",
            "msg": f"{item['target']}: [{dec}] {item['cve_id']} ({item['pkg_name']})",
            "run_id": run_id,
        }
        rec.update(item)
        records.append(rec)

    for item in no_scan_result_targets:
        records.append(
            {
                "event": "no_scan_result",
                "msg": f"スキャン結果なし: {item['namespace']}/{item['resource_kind']}/{item['resource_name']}",
                "run_id": run_id,
                "namespace": item["namespace"],
                "resource_kind": item["resource_kind"],
                "resource_name": item["resource_name"],
                "workload": item["workload"],
                "target": item["target"],
            }
        )

    unscanned_count = 0
    for host_name, host_cfg in sorted((assets.get("hosts") or {}).items()):
        if host_cfg["scan"] == "none":
            unscanned_count += 1
            records.append(
                {
                    "event": "unscanned_asset",
                    "msg": f"未スキャン資産: {host_name} ({host_cfg['kind']})",
                    "run_id": run_id,
                    "host": host_name,
                    "kind": host_cfg["kind"],
                    "human_impact": host_cfg["human_impact"],
                    "exposure": host_cfg["exposure"],
                    "scan": host_cfg["scan"],
                    "reason": host_cfg["reason"],
                }
            )

    for ovr in expired_overrides:
        records.append(
            {
                "event": "expired_override",
                "msg": f"期限切れ例外: {ovr['target']} {ovr['cve']} (期限 {ovr['expires']})",
                "run_id": run_id,
                "cve_id": ovr["cve"],
                "target_type": ovr["target_type"],
                "namespace": ovr["namespace"],
                "workload": ovr["workload"],
                "repository": ovr["repository"],
                "target": ovr["target"],
                "decision": ovr["decision"],
                "expires": ovr["expires"],
                "reason": ovr["reason"],
            }
        )

    unregistered_targets = sum(1 for reg in target_registration.values() if not reg)
    active_exploitation = sum(1 for item in evaluated if item.get("exploitation") == "active")
    records.append(
        {
            "event": "eval_done",
            "msg": f"SSVC 評価完了: {len(evaluated)} 件 (immediate={decision_totals['immediate']}, out-of-cycle={decision_totals['out-of-cycle']}, active={active_exploitation})",
            "run_id": run_id,
            "total_findings": len(evaluated),
            "counts_by_decision": decision_totals,
            "active_exploitation": active_exploitation,
            "unregistered_targets": unregistered_targets,
            "no_scan_result_targets": len(no_scan_result_targets),
            "unscanned_assets": unscanned_count,
            "expired_overrides": len(expired_overrides),
        }
    )
    return records


def push_metrics(url, body, attempts=3, wait_seconds=10):
    """vmagent に送る。失敗したら少し待って送り直し、それでも駄目なら例外を上げる。"""
    last_error = None
    for attempt in range(1, attempts + 1):
        request = urllib.request.Request(
            url, data=body.encode("utf-8"), method="POST", headers={"Content-Type": "text/plain"}
        )
        try:
            with urllib.request.urlopen(request, timeout=30) as response:
                if 200 <= response.status < 300:
                    return
                last_error = f"HTTP {response.status}"
        except (urllib.error.URLError, OSError) as e:
            last_error = repr(e)
        print(
            json.dumps(
                {
                    "event": "push",
                    "level": "error",
                    "msg": f"メトリクス送信に失敗した ({attempt}/{attempts}): {last_error}",
                },
                ensure_ascii=False,
            ),
            file=sys.stderr,
            flush=True,
        )
        if attempt < attempts:
            time.sleep(wait_seconds)
    raise RuntimeError(f"メトリクスを送れなかった: {url}: {last_error}")


def _emit_error(event, msg, **extra):
    payload = {"event": event, "level": "error", "msg": msg}
    payload.update(extra)
    print(json.dumps(payload, ensure_ascii=False), file=sys.stderr, flush=True)


def run_evaluation(
    assets_path,
    overrides_path,
    decision_table_path,
    vlogs_url,
    push_url=None,
    metrics_file=None,
    run_id=None,
    today=None,
    now=None,
    k8s_client=None,
    kev_url=DEFAULT_KEV_URL,
    vulnrichment_base_url=DEFAULT_VULNRICHMENT_BASE_URL,
    epss_url=DEFAULT_EPSS_URL,
    repo_scan_lookback="7d",
    base_table_path=_DEFAULT_BASE_TABLE,
):
    """一連の評価処理を実行し、終了コード (0 または 1) を返す。"""
    if now is None:
        now = time.time()
    if today is None:
        today = datetime.datetime.now(JST).date()
    if run_id is None:
        run_id = str(int(now))
    if base_table_path is _DEFAULT_BASE_TABLE:
        base_table_path = DEFAULT_BASE_TABLE_PATH

    assets = load_assets(assets_path)
    override_entries = load_overrides(overrides_path)
    active_overrides, expired_overrides = split_overrides(override_entries, today)
    decision_table = load_decision_table(decision_table_path)
    base_decision_table = (
        load_decision_table(base_table_path) if base_table_path else None
    )

    if k8s_client is None:
        k8s_client = K8sClient()
    ingress_namespaces, k8s_findings, k8s_targets, no_scan_result_targets = collect_k8s_data(k8s_client)
    repo_findings, repo_targets, latest_repo_runs = collect_repo_scan_data(
        vlogs_url, lookback=repo_scan_lookback
    )

    missing_repos = sorted(set(assets["repositories"]) - set(latest_repo_runs))
    if missing_repos:
        _emit_error(
            "repo_scan_missing",
            f"VictoriaLogs に直近 {repo_scan_lookback} のスキャン結果が無いリポジトリがある: {', '.join(missing_repos)}",
            missing_repos=missing_repos,
        )

    all_findings = k8s_findings + repo_findings
    all_targets = k8s_targets | repo_targets

    cve_ids = [f["cve_id"] for f in all_findings if parse_cve_id(f["cve_id"])]
    kev_set = fetch_kev_set(url=kev_url)
    vulnrichment_map, vulnrichment_errors = fetch_vulnrichment_map(
        cve_ids, base_url=vulnrichment_base_url
    )
    epss_map, epss_errors = fetch_epss_map(cve_ids, base_url=epss_url)

    for err in epss_errors:
        _emit_error("epss_error", err)

    evaluated, counts, target_registration = evaluate_findings(
        all_findings,
        all_targets,
        assets,
        ingress_namespaces,
        active_overrides,
        kev_set,
        vulnrichment_map,
        epss_map,
        decision_table,
        vulnrichment_errors=vulnrichment_errors,
        base_decision_table=base_decision_table,
    )

    for record in build_log_records(
        evaluated,
        target_registration,
        assets,
        expired_overrides,
        run_id,
        no_scan_result_targets=no_scan_result_targets,
    ):
        print(json.dumps(record, ensure_ascii=False, sort_keys=True), flush=True)

    metrics_body = render_metrics(
        counts,
        target_registration,
        assets,
        expired_overrides,
        now,
        include_last_success=(not vulnrichment_errors),
        evaluated=evaluated,
        no_scan_result_targets=no_scan_result_targets,
    )
    if push_url:
        push_metrics(push_url, metrics_body)
    else:
        with open(metrics_file, "w", encoding="utf-8") as f:
            f.write(metrics_body)

    if vulnrichment_errors:
        _emit_error(
            "vulnrichment_error",
            f"Vulnrichment の取得に失敗した CVE がある: {', '.join(sorted(vulnrichment_errors))}",
            errors=vulnrichment_errors,
        )
        return 1

    return 0


def main(argv=None):
    parser = argparse.ArgumentParser(description=__doc__.splitlines()[0])
    parser.add_argument("--assets", required=True, help="資産の宣言 YAML (assets.yaml)")
    parser.add_argument("--overrides", required=True, help="例外ファイル YAML (overrides.yaml)")
    parser.add_argument(
        "--table",
        default=DEFAULT_TABLE_PATH,
        help="評価に使う SSVC Deployer 表 (CSV)",
    )
    parser.add_argument(
        "--base-table",
        default=DEFAULT_BASE_TABLE_PATH,
        help="判定出所の比較に使う上流 SSVC Deployer 表 (CSV)",
    )
    parser.add_argument("--vlogs-url", required=True, help="VictoriaLogs の /select/logsql/query")
    destination = parser.add_mutually_exclusive_group(required=True)
    destination.add_argument("--push-url", help="vmagent の /api/v1/import/prometheus")
    destination.add_argument("--metrics-file", help="送らずにファイルへ書く (手元で確かめる用)")
    parser.add_argument("--run-id", help="この実行の ID。省略時は今の時刻 (Unix 秒)")
    parser.add_argument(
        "--repo-scan-lookback",
        default="7d",
        help="VictoriaLogs で repo-scan の最新結果を探す期間 (既定: 7d)",
    )
    args = parser.parse_args(argv)

    return run_evaluation(
        assets_path=args.assets,
        overrides_path=args.overrides,
        decision_table_path=args.table,
        vlogs_url=args.vlogs_url,
        push_url=args.push_url,
        metrics_file=args.metrics_file,
        run_id=args.run_id,
        repo_scan_lookback=args.repo_scan_lookback,
        base_table_path=args.base_table,
    )


if __name__ == "__main__":
    sys.exit(main())
