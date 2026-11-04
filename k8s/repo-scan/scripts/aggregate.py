#!/usr/bin/env python3
"""trivy と semgrep の結果 (JSON) をまとめ、件数を VictoriaMetrics に送り、指摘の一覧を標準出力に出す。

標準ライブラリだけで動く。CronJob の最後のコンテナ (python のイメージ) で動かす。

入力 (clone.sh・scan-trivy.sh・scan-semgrep.sh が /work に残すもの):
  status/<repo>/clone.exit   クローンの終了コード
  status/<repo>/commit       クローンしたコミットの SHA
  status/<repo>/trivy.exit   trivy の終了コード
  status/<repo>/semgrep.exit semgrep の終了コード
  status/<repo>/*.log        各手順のエラー出力
  out/<repo>/trivy.json      trivy fs --format json の結果
  out/<repo>/semgrep.json    semgrep scan --json の結果

出力:
  - 標準出力: 指摘を 1 件 1 行の JSON で出す (event=finding)。vlagent が VictoriaLogs に入れ、
    JSON のキーがそのままログのフィールドになる ("msg" は _msg になる)。
    スキャンが成功したリポジトリ・スキャナごとに event=scan_done の行も出す。ダッシュボードはこの行の run_id で
    「リポジトリ・スキャナごとの最新の実行」を選ぶ (指摘が 0 件になった日も、古い指摘を出さないため)。
    リポジトリごとの成否は event=repo_status の行で出す。
  - メトリクス (Prometheus のテキスト形式) を vmagent の /api/v1/import/prometheus に送る。
      nuage_repo_scan_target{repo}                          対象のリポジトリ。毎回すべての対象について 1 を送る (アラートで「一度も成功していない」を見つけるため)
      nuage_repo_scan_findings{repo, category, severity}    指摘の件数。0 件の組も 0 として送る
      nuage_repo_scan_last_success_timestamp_seconds{repo}  クローンと両方のスキャンが成功した時刻
  - 終了コード: 1 つでも失敗したリポジトリがある、または送信に失敗したら 1。

秘密情報の指摘は、見つかった値そのもの (trivy の Match・Code の行、semgrep の lines・message) を出さない。
ルール ID・タイトル (ルールの名前)・ファイル・行だけにする。
"""

import argparse
import json
import os
import re
import sys
import time
import urllib.error
import urllib.parse
import urllib.request

CATEGORIES = ("vulnerability", "misconfiguration", "secret", "code")
TRIVY_CATEGORIES = ("vulnerability", "misconfiguration", "secret")
SEMGREP_CATEGORIES = ("code",)
# trivy-operator のメトリクスと同じ表記にそろえる
SEVERITIES = ("Critical", "High", "Medium", "Low", "Unknown")

TRIVY_SEVERITY = {
    "CRITICAL": "Critical",
    "HIGH": "High",
    "MEDIUM": "Medium",
    "LOW": "Low",
    "UNKNOWN": "Unknown",
}

# semgrep の重要度の対応。
# p/default のルールは ERROR / WARNING / INFO の 3 段階で、trivy の 5 段階に次のように寄せる。
#   ERROR   → High    (semgrep で最も重い段階。trivy の Critical に当たる段階は semgrep の OSS ルールに無いため、Critical にはしない)
#   WARNING → Medium
#   INFO    → Low
# 新しい semgrep の 5 段階の表記 (CRITICAL / HIGH / MEDIUM / LOW) が来たときは、同じ名前の段階にそのまま対応させる。
# どれにも当たらないもの (EXPERIMENT・INVENTORY など) は Unknown にする。
SEMGREP_SEVERITY = {
    "CRITICAL": "Critical",
    "ERROR": "High",
    "HIGH": "High",
    "WARNING": "Medium",
    "MEDIUM": "Medium",
    "INFO": "Low",
    "LOW": "Low",
}

# semgrep のルールのうち、秘密情報を探すもの。
# これらはメッセージに見つかった値 (メタ変数) を差し込むことがあるため、メッセージを出さずルールの名前だけを出す。
SEMGREP_SENSITIVE_RULE = re.compile(
    r"secret|password|passwd|credential|token|hardcod|api[-_]?key|private[-_]?key",
    re.IGNORECASE,
)

STEPS = ("clone", "trivy", "semgrep")
LOG_TAIL_LINES = 20
TITLE_MAX = 300


def load_targets(path):
    """対象の一覧 (<owner>/<repo> の行) を読む。空行と # の行は読み飛ばす。"""
    targets = []
    with open(path, encoding="utf-8") as f:
        for raw in f:
            line = raw.strip()
            if not line or line.startswith("#"):
                continue
            if not re.fullmatch(r"[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+", line):
                raise ValueError(f"対象の書き方が <owner>/<repo> になっていない: {line}")
            owner, name = line.split("/", 1)
            targets.append((owner, name))
    if not targets:
        raise ValueError(f"対象のリポジトリが 1 つも無い: {path}")
    names = [name for _, name in targets]
    duplicated = sorted({n for n in names if names.count(n) > 1})
    if duplicated:
        raise ValueError(f"<repo> の部分が重なっている (repo ラベルが区別できない): {', '.join(duplicated)}")
    return targets


def github_url(owner, repo, commit, path, line):
    """GitHub の該当行への URL。行が分からないときはファイルへの URL にする。"""
    url = "https://github.com/{}/{}/blob/{}/{}".format(
        owner, repo, commit, urllib.parse.quote(path, safe="/")
    )
    if line:
        url += f"#L{line}"
    return url


def _short(text):
    """1 行目だけを取り、長すぎるものは切る。"""
    lines = (text or "").strip().splitlines()
    text = lines[0].strip() if lines else ""
    if len(text) > TITLE_MAX:
        text = text[: TITLE_MAX - 1] + "…"
    return text


def _int_or_none(value):
    try:
        number = int(value)
    except (TypeError, ValueError):
        return None
    return number if number > 0 else None


def _finding(owner, repo, commit, scanner, category, severity, rule_id, title, path, line, **extra):
    record = {
        "event": "finding",
        "msg": f"{repo}: [{category}/{severity}] {title or rule_id}",
        "scanner": scanner,
        "repo": repo,
        "category": category,
        "severity": severity,
        "rule_id": rule_id,
        "title": title,
        "file": path,
        "line": line,
        "commit": commit,
        "url": github_url(owner, repo, commit, path, line),
    }
    record.update(extra)
    return record


def trivy_findings(doc, owner, repo, commit):
    """trivy fs --format json の結果から指摘を取り出す。"""
    findings = []
    for result in doc.get("Results") or []:
        target = result.get("Target") or ""
        for vuln in result.get("Vulnerabilities") or []:
            # trivy fs の脆弱性には行番号が無い (依存を書いたロックファイルなどへのリンクにする)
            findings.append(
                _finding(
                    owner, repo, commit, "trivy", "vulnerability",
                    TRIVY_SEVERITY.get(vuln.get("Severity"), "Unknown"),
                    vuln.get("VulnerabilityID") or "",
                    _short(vuln.get("Title")) or (vuln.get("VulnerabilityID") or ""),
                    target, None,
                    pkg_name=vuln.get("PkgName") or "",
                    installed_version=vuln.get("InstalledVersion") or "",
                    fixed_version=vuln.get("FixedVersion") or "",
                    rule_url=vuln.get("PrimaryURL") or "",
                )
            )
        for misconf in result.get("Misconfigurations") or []:
            # 既定の出力に PASS は含まれないが、念のため FAIL だけを数える
            if misconf.get("Status", "FAIL") != "FAIL":
                continue
            cause = misconf.get("CauseMetadata") or {}
            findings.append(
                _finding(
                    owner, repo, commit, "trivy", "misconfiguration",
                    TRIVY_SEVERITY.get(misconf.get("Severity"), "Unknown"),
                    misconf.get("ID") or misconf.get("AVDID") or "",
                    _short(misconf.get("Title")),
                    target, _int_or_none(cause.get("StartLine")),
                    message=_short(misconf.get("Message")),
                    rule_url=misconf.get("PrimaryURL") or "",
                )
            )
        for secret in result.get("Secrets") or []:
            # Match・Code・Offset は値 (伏せ字でも一部が残る) を含むため使わない。
            # Title はルールの名前 (例: "AWS Access Key ID") で、値を含まない。
            findings.append(
                _finding(
                    owner, repo, commit, "trivy", "secret",
                    TRIVY_SEVERITY.get(secret.get("Severity"), "Unknown"),
                    secret.get("RuleID") or "",
                    _short(secret.get("Title")),
                    target, _int_or_none(secret.get("StartLine")),
                )
            )
    return findings


def _relative_path(path, repo_dir):
    """semgrep のパスをリポジトリからの相対パスにそろえる。"""
    if repo_dir and os.path.isabs(path):
        path = os.path.relpath(path, repo_dir)
    while path.startswith("./"):
        path = path[2:]
    return path


def semgrep_findings(doc, owner, repo, commit, repo_dir=None):
    """semgrep scan --json の結果から指摘を取り出す。category はすべて code。"""
    findings = []
    for result in doc.get("results") or []:
        extra = result.get("extra") or {}
        metadata = extra.get("metadata") or {}
        rule_id = result.get("check_id") or ""
        technology = metadata.get("technology") or []
        if isinstance(technology, str):
            technology = [technology]
        sensitive = bool(SEMGREP_SENSITIVE_RULE.search(rule_id)) or any(
            SEMGREP_SENSITIVE_RULE.search(str(t)) for t in technology
        )
        # extra.lines (該当のコード) はどのルールでも出さない。
        # 秘密情報を探すルールはメッセージも出さず、ルールの名前 (check_id の最後の部分) だけにする。
        rule_name = rule_id.rsplit(".", 1)[-1]
        title = rule_name if sensitive else (_short(extra.get("message")) or rule_name)
        path = _relative_path(result.get("path") or "", repo_dir)
        line = _int_or_none((result.get("start") or {}).get("line"))
        findings.append(
            _finding(
                owner, repo, commit, "semgrep", "code",
                SEMGREP_SEVERITY.get(str(extra.get("severity") or "").upper(), "Unknown"),
                rule_id, title, path, line,
                rule_url=metadata.get("source") or "",
            )
        )
    return findings


def count_findings(findings, categories):
    """category × severity の件数。0 件の組も含める。"""
    counts = {(c, s): 0 for c in categories for s in SEVERITIES}
    for f in findings:
        key = (f["category"], f["severity"])
        if key in counts:
            counts[key] += 1
    return counts


def _read(path):
    try:
        with open(path, encoding="utf-8", errors="replace") as f:
            return f.read()
    except FileNotFoundError:
        return None


def _log_tail(path):
    text = _read(path)
    if not text:
        return ""
    return "\n".join(text.strip().splitlines()[-LOG_TAIL_LINES:])


def _load_json(path):
    with open(path, encoding="utf-8") as f:
        return json.load(f)


def _semgrep_error_messages(path):
    """semgrep の JSON の errors のうち level=error のもの。読めなければ空にする (失敗の扱いは終了コードで決まる)。"""
    try:
        doc = _load_json(path)
        messages = [
            str(e.get("message") or e.get("type") or "")
            for e in doc.get("errors") or []
            if isinstance(e, dict) and e.get("level") == "error"
        ]
    except (OSError, ValueError, AttributeError):
        return ""
    return "\n".join(m for m in messages if m)


def collect(targets, work_dir):
    """各リポジトリの結果を読む。失敗した手順も記録する (隠さない)。"""
    repos = []
    for owner, name in targets:
        status_dir = os.path.join(work_dir, "status", name)
        out_dir = os.path.join(work_dir, "out", name)
        repo = {
            "owner": owner,
            "name": name,
            "commit": (_read(os.path.join(status_dir, "commit")) or "").strip(),
            "errors": [],
            "findings": [],
            "scanned": {},  # スキャナ名 → 成功したか
        }
        exits = {step: (_read(os.path.join(status_dir, f"{step}.exit")) or "").strip() for step in STEPS}

        if exits["clone"] != "0" or not repo["commit"]:
            repo["errors"].append(
                {
                    "step": "clone",
                    "exit_code": exits["clone"] or "missing",
                    "log": _log_tail(os.path.join(status_dir, "clone.log")),
                }
            )
            repo["scanned"] = {"trivy": False, "semgrep": False}
            repos.append(repo)
            continue

        parsers = {
            "trivy": lambda doc: trivy_findings(doc, owner, name, repo["commit"]),
            "semgrep": lambda doc: semgrep_findings(
                doc, owner, name, repo["commit"], os.path.join(work_dir, "repos", name)
            ),
        }
        for scanner, parse in parsers.items():
            if exits[scanner] != "0":
                log = _log_tail(os.path.join(status_dir, f"{scanner}.log"))
                if scanner == "semgrep":
                    # semgrep は失敗の理由 (ルールの取得の失敗など) を標準エラーではなく JSON の errors に書く
                    log = "\n".join(
                        filter(None, [log, _semgrep_error_messages(os.path.join(out_dir, "semgrep.json"))])
                    )
                repo["errors"].append(
                    {
                        "step": scanner,
                        "exit_code": exits[scanner] or "missing",
                        "log": log,
                    }
                )
                repo["scanned"][scanner] = False
                continue
            try:
                found = parse(_load_json(os.path.join(out_dir, f"{scanner}.json")))
            except (OSError, ValueError, AttributeError, TypeError) as e:
                repo["errors"].append(
                    {"step": scanner, "exit_code": "0", "log": f"結果の JSON を読めない: {e!r}"}
                )
                repo["scanned"][scanner] = False
                continue
            repo["findings"].extend(found)
            repo["scanned"][scanner] = True
        repos.append(repo)
    return repos


def _label_value(value):
    return value.replace("\\", "\\\\").replace("\n", "\\n").replace('"', '\\"')


def render_metrics(repos, now):
    """Prometheus のテキスト形式。

    - 失敗したスキャナの category は送らない。0 を送ると「直った」ように見えるため、前回の値を残し、
      成功時刻が古くなること (アラート) で気づけるようにする。
    - 成功したスキャナの category は、0 件の組も 0 として送る (直った指摘が古い件数のまま残らないように)。
    """
    lines = [
        "# TYPE nuage_repo_scan_target gauge",
        "# TYPE nuage_repo_scan_findings gauge",
        "# TYPE nuage_repo_scan_last_success_timestamp_seconds gauge",
    ]
    for repo in repos:
        name = _label_value(repo["name"])
        lines.append(f'nuage_repo_scan_target{{repo="{name}"}} 1')
        categories = []
        if repo["scanned"].get("trivy"):
            categories.extend(TRIVY_CATEGORIES)
        if repo["scanned"].get("semgrep"):
            categories.extend(SEMGREP_CATEGORIES)
        counts = count_findings(repo["findings"], categories)
        for category in categories:
            for severity in SEVERITIES:
                lines.append(
                    f'nuage_repo_scan_findings{{repo="{name}",category="{category}",severity="{severity}"}} '
                    f"{counts[(category, severity)]}"
                )
        if not repo["errors"]:
            lines.append(f'nuage_repo_scan_last_success_timestamp_seconds{{repo="{name}"}} {int(now)}')
    return "\n".join(lines) + "\n"


def log_records(repos, run_id):
    """標準出力に出す JSON 行 (指摘・スキャンの完了・リポジトリごとの成否)。"""
    records = []
    for repo in repos:
        for scanner in ("trivy", "semgrep"):
            if repo["scanned"].get(scanner):
                records.append(
                    {
                        "event": "scan_done",
                        "msg": f"{repo['name']}: {scanner} のスキャンが終わった",
                        "scanner": scanner,
                        "repo": repo["name"],
                        "commit": repo["commit"],
                        "run_id": run_id,
                    }
                )
        for finding in repo["findings"]:
            record = dict(finding)
            record["run_id"] = run_id
            records.append(record)
        if repo["errors"]:
            steps = ", ".join(e["step"] for e in repo["errors"])
            records.append(
                {
                    "event": "repo_status",
                    "level": "error",
                    "msg": f"{repo['name']}: スキャンに失敗した ({steps})。成功時刻は更新しない",
                    "repo": repo["name"],
                    "status": "failed",
                    "commit": repo["commit"],
                    "run_id": run_id,
                    "errors": repo["errors"],
                }
            )
        else:
            records.append(
                {
                    "event": "repo_status",
                    "level": "info",
                    "msg": f"{repo['name']}: スキャンが成功した (指摘 {len(repo['findings'])} 件)",
                    "repo": repo["name"],
                    "status": "ok",
                    "commit": repo["commit"],
                    "run_id": run_id,
                    "findings": len(repo["findings"]),
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
                    "msg": f"件数の送信に失敗した ({attempt}/{attempts}): {last_error}",
                },
                ensure_ascii=False,
            ),
            file=sys.stderr,
            flush=True,
        )
        if attempt < attempts:
            time.sleep(wait_seconds)
    raise RuntimeError(f"件数を送れなかった: {url}: {last_error}")


def main(argv=None):
    parser = argparse.ArgumentParser(description=__doc__.splitlines()[0])
    parser.add_argument("--targets", required=True, help="対象の一覧 (<owner>/<repo> の行)")
    parser.add_argument("--work", required=True, help="clone.sh・scan-*.sh が結果を置いたディレクトリ")
    destination = parser.add_mutually_exclusive_group(required=True)
    destination.add_argument("--push-url", help="vmagent の /api/v1/import/prometheus")
    destination.add_argument("--metrics-file", help="送らずにファイルへ書く (手元で確かめる用)")
    parser.add_argument("--run-id", help="この実行の ID。省略時は今の時刻 (Unix 秒)")
    args = parser.parse_args(argv)

    now = time.time()
    run_id = args.run_id or str(int(now))
    targets = load_targets(args.targets)
    repos = collect(targets, args.work)

    for record in log_records(repos, run_id):
        print(json.dumps(record, ensure_ascii=False, sort_keys=True), flush=True)

    body = render_metrics(repos, now)
    if args.push_url:
        push_metrics(args.push_url, body)
    else:
        with open(args.metrics_file, "w", encoding="utf-8") as f:
            f.write(body)

    failed = [r["name"] for r in repos if r["errors"]]
    if failed:
        print(
            json.dumps(
                {
                    "event": "summary",
                    "level": "error",
                    "msg": f"スキャンに失敗したリポジトリがある: {', '.join(failed)}",
                    "failed_repos": failed,
                },
                ensure_ascii=False,
            ),
            file=sys.stderr,
            flush=True,
        )
        return 1
    return 0


if __name__ == "__main__":
    sys.exit(main())
