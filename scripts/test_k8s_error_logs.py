#!/usr/bin/env python3
"""VMRule/k8s-error-logs の除外境界を検証する回帰テスト。

標準ライブラリのみで実行でき、VICTORIALOGS_QUERY_URL または --query-url を
指定した場合は実機の VictoriaLogs エンジン上でも同じ式を評価する。
"""

import argparse
import json
import os
from pathlib import Path
import re
import sys
import textwrap
import unittest
import urllib.parse
import urllib.request


ROOT = Path(__file__).resolve().parents[1]
RULES_PATH = ROOT / "k8s/base/victoria/rules.yaml"


def load_k8s_error_logs_rules():
    """rules.yaml から VMRule/k8s-error-logs 配下のルール定義を読み取る。"""
    text = RULES_PATH.read_text(encoding="utf-8")
    marker = "  name: k8s-error-logs\n"
    start = text.index(marker)
    rest = text[start:]
    doc_end = rest.find("\n---\n")
    section = rest if doc_end == -1 else rest[:doc_end]

    rules = {}
    lines = section.splitlines()
    i = 0
    while i < len(lines):
        m = re.match(r"^        - alert: (\S+)\s*$", lines[i])
        if not m:
            i += 1
            continue
        name = m.group(1)
        rule = {"alert": name}
        i += 1
        while i < len(lines) and not re.match(r"^        - alert: ", lines[i]):
            line = lines[i]
            kv = re.match(r"^          ([a-z_]+):\s*(.*?)\s*$", line)
            if kv:
                key, val = kv.group(1), kv.group(2)
                if val == "|-":
                    block = []
                    i += 1
                    while i < len(lines) and (lines[i].startswith("            ") or lines[i] == ""):
                        block.append(lines[i])
                        i += 1
                    rule[key] = textwrap.dedent("\n".join(block)).strip()
                    continue
                rule[key] = val.strip('"')
            i += 1
        rules[name] = rule
    return rules


def parse_filter_stages(expr):
    """LogsQL 式の先頭フィルター部分とパイプ以降を分離する。"""
    head, pipe_rest = expr.split("\n| ", 1)
    assert head.startswith("_time:5m "), head
    return head[len("_time:5m "):].strip(), "| " + pipe_rest


def extract_keel_preview_exclusion_block(expr):
    """KubernetesPodErrorLogs の NOT (...) ブロック内の条件列を取り出す。"""
    m = re.search(r"NOT \(\n(.*?)\n  \)", expr, re.DOTALL)
    assert m, "NOT (...) exclusion block not found"
    return [line.strip() for line in m.group(1).splitlines() if line.strip()]


def unquote_logsql_literal(literal):
    """LogsQL の文字列リテラル（"..." または `...`）から実文字列を復元する。"""
    if literal.startswith("`") and literal.endswith("`"):
        return literal[1:-1]
    assert literal.startswith('"') and literal.endswith('"'), literal
    return literal[1:-1].replace('\\"', '"').replace("\\\\", "\\")


def matches_logsql_phrase(text, phrase):
    """LogsQL のフレーズ一致（単語境界ベース）を近似評価する。"""
    tokens = re.findall(r"[0-9A-Za-z_]+", phrase)
    if not tokens:
        return True
    pattern = r"(?<![0-9A-Za-z_])" + r"[^0-9A-Za-z_]+".join(re.escape(t) for t in tokens) + r"(?![0-9A-Za-z_])"
    return re.search(pattern, text) is not None


def eval_pod_error_logs_offline(rules, row):
    """rules.yaml から抽出したパターンで KubernetesPodErrorLogs の一致可否を判定する。"""
    filter_expr, _ = parse_filter_stages(rules["KubernetesPodErrorLogs"]["expr"])
    ns_match = re.search(r'kubernetes\.pod_namespace:~"([^"]+)"', filter_expr)
    msg_match = re.search(r'\s~("[^"]+")', filter_expr)
    eof_match = re.search(r'NOT ~"([^"]+)"', filter_expr)
    excl_terms = extract_keel_preview_exclusion_block(filter_expr)

    ns_regex = re.compile(ns_match.group(1))
    msg_regex = re.compile(unquote_logsql_literal(msg_match.group(1)))
    eof_regex = re.compile(eof_match.group(1))

    ns = row.get("kubernetes.pod_namespace", "")
    full_text = " ".join(str(v) for k, v in row.items() if k != "case" and v is not None)

    if not ns_regex.search(ns):
        return False
    if not msg_regex.search(full_text):
        return False
    if eof_regex.search(full_text):
        return False
    if matches_keel_preview_exclusion(row, full_text, excl_terms):
        return False
    return True


def matches_keel_preview_exclusion(row, full_text, terms):
    """keel プレビュー 404 除外条件の全項が真になるか判定する。"""
    ns = row.get("kubernetes.pod_namespace", "")
    for term in terms:
        if term.startswith("kubernetes.pod_namespace:="):
            expected_ns = unquote_logsql_literal(term.split(":=", 1)[1])
            if ns != expected_ns:
                return False
        elif term.startswith("~"):
            pattern = unquote_logsql_literal(term[1:])
            if not re.search(pattern, full_text):
                return False
        elif term.startswith('"'):
            phrase = unquote_logsql_literal(term)
            if not matches_logsql_phrase(full_text, phrase):
                return False
        else:
            raise AssertionError(f"Unsupported LogsQL term: {term}")
    return True


def eval_preview_not_found_offline(rules, row):
    """KeelPreviewImageNotFound の一致可否と抽出される image ラベルを返す。"""
    expr = rules["KeelPreviewImageNotFound"]["expr"]
    filter_expr, pipe_rest = parse_filter_stages(expr)
    terms = [line.strip() for line in filter_expr.splitlines() if line.strip()]
    full_text = " ".join(str(v) for k, v in row.items() if k != "case" and v is not None)
    if not matches_keel_preview_exclusion(row, full_text, terms):
        return False, None

    ext_match = re.search(r'extract_regexp (`[^`]+`|"[^"]+")', pipe_rest)
    assert ext_match
    ext_pattern = unquote_logsql_literal(ext_match.group(1))
    m = re.search(ext_pattern, full_text)
    if not m:
        return True, ""
    return True, f"{m.group('repo')}:{m.group('tag')}"


def build_boundary_cases():
    """除外境界と検知対象を網羅した合成ログケースを返す。"""
    return [
        # 1. keel プレビュータグ初回 404（3 種の監視メッセージ）-> 即時通知からは除外、継続監視で捕捉
        {
            "case": "keel_preview_addjob_404",
            "expect_pod_error": False,
            "expect_preview_404": True,
            "expect_image": "k-wa-wa/pechka-api:pr-113",
            "row": {
                "kubernetes.pod_namespace": "keel",
                "kubernetes.pod_name": "keel-pod",
                "kubernetes.container_name": "keel",
                "_msg": 'time="2026-11-19T15:03:52Z" level=error msg="trigger.poll.RepositoryWatcher.addJob: failed to get image digest" error="Head \\"https://ghcr.io/v2/k-wa-wa/pechka-api/manifests/pr-113\\": http: non-successful response (status=404 body=\\"\\")" image="k-wa-wa/pechka-api:pr-113"',
            },
        },
        {
            "case": "keel_preview_watch_404",
            "expect_pod_error": False,
            "expect_preview_404": True,
            "expect_image": "k-wa-wa/pechka-api:pr-113",
            "row": {
                "kubernetes.pod_namespace": "keel",
                "kubernetes.pod_name": "keel-pod",
                "kubernetes.container_name": "keel",
                "_msg": 'time="2026-11-19T15:03:52Z" level=error msg="trigger.poll.RepositoryWatcher.Watch: failed to add image watch job" error="Head \\"https://ghcr.io/v2/k-wa-wa/pechka-api/manifests/pr-113\\": http: non-successful response (status=404 body=\\"\\")" image="namespace:pechka-pr-113,image:ghcr.io/k-wa-wa/pechka-api:pr-113"',
            },
        },
        {
            "case": "keel_preview_manager_404",
            "expect_pod_error": False,
            "expect_preview_404": False,
            "row": {
                "kubernetes.pod_namespace": "keel",
                "kubernetes.pod_name": "keel-pod",
                "kubernetes.container_name": "keel",
                "_msg": 'time="2026-11-19T15:03:53Z" level=error msg="trigger.poll.manager: got error(-s) while watching images" error="encountered errors while adding images: Head \\"https://ghcr.io/v2/k-wa-wa/pechka-api/manifests/pr-113\\": http: non-successful response (status=404 body=\\"\\"), Head \\"https://ghcr.io/v2/k-wa-wa/pechka-frontend/manifests/pr-113\\": http: non-successful response (status=404 body=\\"\\")"',
            },
        },
        # 1b. manager 行にプレビュー 404 と他のエラー（echo-bot:latest の 403 / 404）が混在する場合は即時検知対象
        {
            "case": "keel_preview_manager_mixed_403",
            "expect_pod_error": True,
            "expect_preview_404": False,
            "row": {
                "kubernetes.pod_namespace": "keel",
                "kubernetes.pod_name": "keel-pod",
                "kubernetes.container_name": "keel",
                "_msg": 'time="2026-11-26T01:43:55Z" level=error msg="trigger.poll.manager: got error(-s) while watching images" error="encountered errors while adding images: Head \\"https://ghcr.io/v2/k-wa-wa/nuage-monitoring-stack-monitoring-pwa-backend/manifests/pr-63\\": http: non-successful response (status=404 body=\\"\\"), Head \\"https://ghcr.io/v2/k-wa-wa/nuage-monitoring-stack-monitoring-pwa-echo-bot/manifests/latest\\": http: non-successful response (status=403 body=\\"\\"), Head \\"https://ghcr.io/v2/k-wa-wa/nuage-monitoring-stack-monitoring-pwa-frontend/manifests/pr-63\\": http: non-successful response (status=404 body=\\"\\")"',
            },
        },
        {
            "case": "keel_preview_manager_mixed_404",
            "expect_pod_error": True,
            "expect_preview_404": False,
            "row": {
                "kubernetes.pod_namespace": "keel",
                "kubernetes.pod_name": "keel-pod",
                "kubernetes.container_name": "keel",
                "_msg": 'time="2026-11-26T01:44:55Z" level=error msg="trigger.poll.manager: got error(-s) while watching images" error="encountered errors while adding images: Head \\"https://ghcr.io/v2/k-wa-wa/nuage-monitoring-stack-monitoring-pwa-backend/manifests/pr-63\\": http: non-successful response (status=404 body=\\"\\"), Head \\"https://ghcr.io/v2/k-wa-wa/nuage-monitoring-stack-monitoring-pwa-echo-bot/manifests/latest\\": http: non-successful response (status=404 body=\\"\\")"',
            },
        },
        # 2. PR #63 実障害再現: echo-bot の :latest タグ設定ミスによる 404（image 属性に pr-63 Namespace を含んでもタグが latest）
        {
            "case": "keel_latest_addjob_404",
            "expect_pod_error": True,
            "expect_preview_404": False,
            "row": {
                "kubernetes.pod_namespace": "keel",
                "kubernetes.pod_name": "keel-pod",
                "kubernetes.container_name": "keel",
                "_msg": 'time="2026-11-26T02:28:54Z" level=error msg="trigger.poll.RepositoryWatcher.addJob: failed to get image digest" error="Head \\"https://ghcr.io/v2/k-wa-wa/nuage-monitoring-stack-monitoring-pwa-echo-bot/manifests/latest\\": http: non-successful response (status=404 body=\\"\\")" image="k-wa-wa/nuage-monitoring-stack-monitoring-pwa-echo-bot:latest"',
            },
        },
        {
            "case": "keel_latest_watch_404",
            "expect_pod_error": True,
            "expect_preview_404": False,
            "row": {
                "kubernetes.pod_namespace": "keel",
                "kubernetes.pod_name": "keel-pod",
                "kubernetes.container_name": "keel",
                "_msg": 'time="2026-11-26T02:28:54Z" level=error msg="trigger.poll.RepositoryWatcher.Watch: failed to add image watch job" error="Head \\"https://ghcr.io/v2/k-wa-wa/nuage-monitoring-stack-monitoring-pwa-echo-bot/manifests/latest\\": http: non-successful response (status=404 body=\\"\\")" image="namespace:nuage-monitoring-stack-pr-63,image:ghcr.io/k-wa-wa/nuage-monitoring-stack-monitoring-pwa-echo-bot:latest"',
            },
        },
        {
            "case": "keel_preview_suffix_tag_404",
            "expect_pod_error": True,
            "expect_preview_404": False,
            "row": {
                "kubernetes.pod_namespace": "keel",
                "kubernetes.pod_name": "keel-pod",
                "kubernetes.container_name": "keel",
                "_msg": 'time="2026-11-26T02:28:54Z" level=error msg="trigger.poll.RepositoryWatcher.addJob: failed to get image digest" error="Head \\"https://ghcr.io/v2/k-wa-wa/pechka-api/manifests/pr-113-hotfix\\": http: non-successful response (status=404 body=\\"\\")" image="k-wa-wa/pechka-api:pr-113-hotfix"',
            },
        },
        {
            "case": "keel_preview_dot_tag_404",
            "expect_pod_error": True,
            "expect_preview_404": False,
            "row": {
                "kubernetes.pod_namespace": "keel",
                "kubernetes.pod_name": "keel-pod",
                "kubernetes.container_name": "keel",
                "_msg": 'time="2026-11-26T02:28:54Z" level=error msg="trigger.poll.RepositoryWatcher.addJob: failed to get image digest" error="Head \\"https://ghcr.io/v2/k-wa-wa/pechka-api/manifests/pr-113.1\\": http: non-successful response (status=404 body=\\"\\")" image="k-wa-wa/pechka-api:pr-113.1"',
            },
        },
        # 3. WatchTagJob の 404（pr-113 タグであっても即時検知対象）
        {
            "case": "keel_watchtagjob_404",
            "expect_pod_error": True,
            "expect_preview_404": False,
            "row": {
                "kubernetes.pod_namespace": "keel",
                "kubernetes.pod_name": "keel-pod",
                "kubernetes.container_name": "keel",
                "_msg": 'time="2026-11-20T18:47:55Z" level=error msg="trigger.poll.WatchTagJob: failed to check digest" error="Head \\"https://ghcr.io/v2/k-wa-wa/pechka-api/manifests/pr-113\\": http: non-successful response (status=404 body=\\"\\")" image="k-wa-wa/pechka-api:pr-113"',
            },
        },
        # 4. 実障害ログ 4 種（status=404 を含むケースでも検知漏れしないこと）
        {
            "case": "keel_provider_update_error",
            "expect_pod_error": True,
            "expect_preview_404": False,
            "row": {
                "kubernetes.pod_namespace": "keel",
                "kubernetes.pod_name": "keel-pod",
                "kubernetes.container_name": "keel",
                "_msg": 'time="2026-11-22T12:53:00Z" level=error msg="provider.kubernetes: got error while updating resource" error="dial tcp 10.96.0.1:443: connect: connection refused"',
            },
        },
        {
            "case": "keel_provider_update_404",
            "expect_pod_error": True,
            "expect_preview_404": False,
            "row": {
                "kubernetes.pod_namespace": "keel",
                "kubernetes.pod_name": "keel-pod",
                "kubernetes.container_name": "keel",
                "_msg": 'time="2026-11-22T12:53:00Z" level=error msg="provider.kubernetes: got error while updating resource" error="status=404 /manifests/pr-113"',
            },
        },
        {
            "case": "keel_notifier_error",
            "expect_pod_error": True,
            "expect_preview_404": False,
            "row": {
                "kubernetes.pod_namespace": "keel",
                "kubernetes.pod_name": "keel-pod",
                "kubernetes.container_name": "keel",
                "_msg": 'time="2026-11-22T12:53:00Z" level=error msg="could not send notification via notifier" error="status=404"',
            },
        },
        {
            "case": "keel_platform_resolver_warning",
            "expect_pod_error": True,
            "expect_preview_404": False,
            "row": {
                "kubernetes.pod_namespace": "keel",
                "kubernetes.pod_name": "keel-pod",
                "kubernetes.container_name": "keel",
                "_msg": 'time="2026-11-21T10:15:52Z" level=warning msg="platform resolver: failed to list Kubernetes nodes" error="Get \\"https://10.96.0.1:443/api/v1/nodes\\": dial tcp 10.96.0.1:443: connect: connection refused"',
            },
        },
        # 5. 既存 2 Namespace の 404（keel 以外の Namespace では除外しないこと）
        {
            "case": "pechka_404",
            "expect_pod_error": True,
            "expect_preview_404": False,
            "row": {
                "kubernetes.pod_namespace": "pechka",
                "kubernetes.pod_name": "api-pod",
                "kubernetes.container_name": "api",
                "_msg": 'level=error msg="trigger.poll.RepositoryWatcher.addJob: upstream error" error="Head \\"https://ghcr.io/v2/k-wa-wa/pechka-api/manifests/pr-113\\": (status=404)"',
            },
        },
        {
            "case": "bare_web_proxy_404",
            "expect_pod_error": True,
            "expect_preview_404": False,
            "row": {
                "kubernetes.pod_namespace": "bare-web-proxy",
                "kubernetes.pod_name": "proxy-pod",
                "kubernetes.container_name": "proxy",
                "_msg": 'level=error msg="upstream request failed status=404"',
            },
        },
        # 6. 既存の EOF 除外と監視対象外 Namespace 境界
        {
            "case": "pechka_argo_eof",
            "expect_pod_error": False,
            "expect_preview_404": False,
            "row": {
                "kubernetes.pod_namespace": "pechka",
                "kubernetes.pod_name": "argo-server-pod",
                "kubernetes.container_name": "argo-server",
                "_msg": 'level=warning msg="finished unary call" error="rpc error: code = ResourceExhausted desc = EOF"',
            },
        },
        {
            "case": "keel_similar_namespace",
            "expect_pod_error": False,
            "expect_preview_404": False,
            "row": {
                "kubernetes.pod_namespace": "keel-staging",
                "kubernetes.pod_name": "keel-pod",
                "kubernetes.container_name": "keel",
                "_msg": 'level=error msg="provider.kubernetes: got error while updating resource"',
            },
        },
    ]


def query_victorialogs(endpoint, expr):
    """読み取り専用 LogsQL クエリを実行して JSON 行リストを返す。"""
    body = urllib.parse.urlencode({"query": expr}).encode()
    request = urllib.request.Request(endpoint, data=body)
    with urllib.request.urlopen(request, timeout=30) as response:
        return [json.loads(line) for line in response if line.strip()]


def validate_against_victorialogs(endpoint, rules):
    """実機の VictoriaLogs エンジン上で rules.yaml の式をそのまま評価し境界を確認する。"""
    cases = build_boundary_cases()
    rows = [dict(c["row"], case=c["case"]) for c in cases]
    synthetic = '_time:1s event:="__k8s_error_logs_fixture__" | union rows(' + ",".join(json.dumps(r) for r in rows) + ")"

    pod_filter, _ = parse_filter_stages(rules["KubernetesPodErrorLogs"]["expr"])
    pod_query = f"{synthetic} | filter {pod_filter} | fields case"
    pod_hits = {row["case"] for row in query_victorialogs(endpoint, pod_query)}
    expected_pod_hits = {c["case"] for c in cases if c["expect_pod_error"]}
    if pod_hits != expected_pod_hits:
        raise AssertionError(f"KubernetesPodErrorLogs mismatch: got={sorted(pod_hits)}, expected={sorted(expected_pod_hits)}")

    prev_filter, prev_pipe = parse_filter_stages(rules["KeelPreviewImageNotFound"]["expr"])
    prev_query = f"{synthetic} | filter {prev_filter} {prev_pipe}"
    prev_rows = query_victorialogs(endpoint, prev_query)
    expected_prev_count = sum(1 for c in cases if c["expect_preview_404"])
    if len(prev_rows) != 1 or int(prev_rows[0]["errors"]) != expected_prev_count:
        raise AssertionError(f"KeelPreviewImageNotFound count mismatch: got={prev_rows}, expected_errors={expected_prev_count}")
    if prev_rows[0].get("image") != "k-wa-wa/pechka-api:pr-113":
        raise AssertionError(f"KeelPreviewImageNotFound image mismatch: {prev_rows[0]}")


class TestK8sErrorLogsRules(unittest.TestCase):
    """VMRule/k8s-error-logs の構造と除外境界を検証する。"""

    @classmethod
    def setUpClass(cls):
        cls.rules = load_k8s_error_logs_rules()
        cls.cases = build_boundary_cases()

    def test_rule_structure_and_parity(self):
        self.assertIn("KubernetesPodErrorLogs", self.rules)
        self.assertIn("KeelPreviewImageNotFound", self.rules)
        pod_rule = self.rules["KubernetesPodErrorLogs"]
        prev_rule = self.rules["KeelPreviewImageNotFound"]
        self.assertEqual(pod_rule.get("record"), "")
        self.assertEqual(prev_rule.get("record"), "")
        self.assertEqual(prev_rule.get("for"), "15m")

        pod_filter, _ = parse_filter_stages(pod_rule["expr"])
        prev_filter, _ = parse_filter_stages(prev_rule["expr"])
        pod_excl = extract_keel_preview_exclusion_block(pod_filter)
        prev_terms = [line.strip() for line in prev_filter.splitlines() if line.strip()]
        self.assertEqual(pod_excl[:2], prev_terms[:2])
        self.assertIn("RepositoryWatcher\\.(addJob|Watch)", prev_terms[2])
        self.assertIn("RepositoryWatcher\\.(addJob|Watch)|manager", pod_excl[2])

    def test_offline_boundary_cases(self):
        for case in self.cases:
            with self.subTest(case=case["case"]):
                self.assertEqual(
                    eval_pod_error_logs_offline(self.rules, case["row"]),
                    case["expect_pod_error"],
                )
                matched, image = eval_preview_not_found_offline(self.rules, case["row"])
                self.assertEqual(matched, case["expect_preview_404"])
                if case["expect_preview_404"]:
                    self.assertEqual(image, case["expect_image"])

    def test_optional_live_victorialogs(self):
        endpoint = os.environ.get("VICTORIALOGS_QUERY_URL")
        if not endpoint:
            self.skipTest("VICTORIALOGS_QUERY_URL not set")
        validate_against_victorialogs(endpoint, self.rules)


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--query-url", help="VictoriaLogs /select/logsql/query URL")
    args, remaining = parser.parse_known_args()
    if args.query_url:
        os.environ["VICTORIALOGS_QUERY_URL"] = args.query_url
    unittest.main(argv=[sys.argv[0]] + remaining)


if __name__ == "__main__":
    main()
