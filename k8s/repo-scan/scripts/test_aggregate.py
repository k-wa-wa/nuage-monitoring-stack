"""aggregate.py のテスト。標準ライブラリの unittest で動く。

    python3 -m unittest discover -s k8s/repo-scan/scripts

testdata/ は手元で実際に動かした trivy 0.76.1・semgrep の出力を削ったもの。
  - trivy.json: このリポジトリにかけた結果の脆弱性 2 件・設定ミス 2 件と、
                秘密情報を置いた試験用のディレクトリ (creds.ini・main.go) にかけた結果の秘密情報 3 件
  - semgrep.json: 同じ試験用のディレクトリにかけた結果 5 件 (秘密情報のルール 2 件を含む) と、このリポジトリにかけた結果 2 件
秘密情報の値は試験用に作った偽物で、trivy が伏せ字にした形 (Match・Code) のまま入っている。
"""

import copy
import json
import os
import sys
import tempfile
import unittest

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))

import aggregate  # noqa: E402

TESTDATA = os.path.join(os.path.dirname(os.path.abspath(__file__)), "testdata")
OWNER = "k-wa-wa"
REPO = "nuage-monitoring-stack"
COMMIT = "df2faa55f55d10ef2754ef7e26fa3e3a497a50e6"
# 秘密情報の値の代わり。出力のどこにも出てはいけない
SENTINEL = "sentinel-secret-value-must-not-leak-7f3a9c"


def load(name):
    with open(os.path.join(TESTDATA, name), encoding="utf-8") as f:
        return json.load(f)


def with_sentinel_trivy(doc):
    """trivy の秘密情報の Match・Code の行に、見張りの値を差し込む。"""
    doc = copy.deepcopy(doc)
    for result in doc["Results"]:
        for secret in result.get("Secrets") or []:
            secret["Match"] = f"token = {SENTINEL}"
            for line in secret["Code"]["Lines"]:
                line["Content"] = f"token = {SENTINEL}"
                line["Highlighted"] = f"token = {SENTINEL}"
    return doc


def with_sentinel_semgrep(doc):
    """semgrep の全結果の lines に、秘密情報のルールでは message にも、見張りの値を差し込む。"""
    doc = copy.deepcopy(doc)
    for result in doc["results"]:
        result["extra"]["lines"] = f"key = {SENTINEL}"
        if ".secrets." in result["check_id"]:
            result["extra"]["message"] = f"Secret {SENTINEL} detected"
    return doc


def make_repo(name, findings, trivy=True, semgrep=True, errors=None):
    return {
        "owner": OWNER,
        "name": name,
        "commit": COMMIT,
        "errors": errors or [],
        "findings": findings,
        "scanned": {"trivy": trivy, "semgrep": semgrep},
    }


class TrivyTest(unittest.TestCase):
    def setUp(self):
        self.findings = aggregate.trivy_findings(load("trivy.json"), OWNER, REPO, COMMIT)

    def by_category(self, category):
        return [f for f in self.findings if f["category"] == category]

    def test_counts(self):
        counts = aggregate.count_findings(self.findings, aggregate.TRIVY_CATEGORIES)
        self.assertEqual(len(self.by_category("vulnerability")), 2)
        self.assertEqual(len(self.by_category("misconfiguration")), 2)
        self.assertEqual(len(self.by_category("secret")), 3)
        self.assertEqual(counts[("secret", "Critical")], 3)
        self.assertEqual(sum(counts.values()), 7)

    def test_vulnerability_fields(self):
        vuln = self.by_category("vulnerability")[0]
        self.assertEqual(vuln["rule_id"], "CVE-2025-30204")
        self.assertEqual(vuln["severity"], "High")
        self.assertEqual(vuln["pkg_name"], "github.com/golang-jwt/jwt/v5")
        self.assertEqual(vuln["installed_version"], "v5.2.1")
        self.assertEqual(vuln["fixed_version"], "5.2.2")
        self.assertEqual(vuln["file"], "monitoring-pwa/backend/go.mod")
        self.assertIsNone(vuln["line"])
        self.assertEqual(
            vuln["url"],
            f"https://github.com/{OWNER}/{REPO}/blob/{COMMIT}/monitoring-pwa/backend/go.mod",
        )

    def test_misconfiguration_fields(self):
        misconf = self.by_category("misconfiguration")[0]
        self.assertEqual(misconf["rule_id"], "KSV-0001")
        self.assertEqual(misconf["severity"], "Medium")
        self.assertEqual(misconf["title"], "Can elevate its own privileges")
        self.assertEqual(misconf["line"], 21)
        self.assertTrue(misconf["url"].endswith("/k8s/base/monitoring-pwa/monitoring-pwa.yaml#L21"))

    def test_secret_fields_only(self):
        secret = self.by_category("secret")[0]
        self.assertEqual(secret["rule_id"], "aws-access-key-id")
        self.assertEqual(secret["title"], "AWS Access Key ID")
        self.assertEqual(secret["file"], "creds.ini")
        self.assertEqual(secret["line"], 1)
        self.assertNotIn("message", secret)
        # trivy が伏せ字にした Match も出さない (伏せ字でも末尾などが残ることがあるため)
        for record in self.by_category("secret"):
            dumped = json.dumps(record, ensure_ascii=False)
            self.assertNotIn("****", dumped)
            self.assertNotIn("aws_access_key_id =", dumped)
            self.assertNotIn("const tok", dumped)


class SemgrepTest(unittest.TestCase):
    def setUp(self):
        self.findings = aggregate.semgrep_findings(load("semgrep.json"), OWNER, REPO, COMMIT)

    def test_category_and_severity(self):
        self.assertEqual(len(self.findings), 7)
        self.assertTrue(all(f["category"] == "code" for f in self.findings))
        by_rule = {f["rule_id"].rsplit(".", 1)[-1]: f for f in self.findings}
        self.assertEqual(by_rule["subprocess-shell-true"]["severity"], "High")  # ERROR
        self.assertEqual(by_rule["eval-detected"]["severity"], "Medium")  # WARNING
        self.assertEqual(by_rule["run-as-non-root"]["severity"], "Low")  # INFO

    def test_severity_mapping_table(self):
        cases = {
            "ERROR": "High",
            "WARNING": "Medium",
            "INFO": "Low",
            "CRITICAL": "Critical",
            "HIGH": "High",
            "MEDIUM": "Medium",
            "LOW": "Low",
            "EXPERIMENT": "Unknown",
            "": "Unknown",
        }
        for given, expected in cases.items():
            doc = {"results": [{"check_id": "a.b.c", "path": "x.py", "start": {"line": 1}, "extra": {"severity": given}}]}
            (finding,) = aggregate.semgrep_findings(doc, OWNER, REPO, COMMIT)
            self.assertEqual(finding["severity"], expected, given)

    def test_paths_and_url(self):
        finding = next(f for f in self.findings if f["file"] == ".github/workflows/auto-preview-label.yml")
        self.assertEqual(
            finding["url"],
            f"https://github.com/{OWNER}/{REPO}/blob/{COMMIT}/.github/workflows/auto-preview-label.yml#L{finding['line']}",
        )
        # 絶対パスや ./ で始まるパスも、リポジトリからの相対パスにそろえる
        doc = {
            "results": [
                {"check_id": "a.b", "path": "/work/repos/r/src/x.go", "start": {"line": 3}, "extra": {}},
                {"check_id": "a.b", "path": "./src/y.go", "start": {"line": 4}, "extra": {}},
            ]
        }
        files = [f["file"] for f in aggregate.semgrep_findings(doc, OWNER, "r", COMMIT, "/work/repos/r")]
        self.assertEqual(files, ["src/x.go", "src/y.go"])

    def test_secret_rules_use_rule_name_as_title(self):
        secrets = [f for f in self.findings if ".secrets." in f["rule_id"]]
        self.assertEqual(len(secrets), 2)
        for finding in secrets:
            self.assertEqual(finding["title"], finding["rule_id"].rsplit(".", 1)[-1])


class NoSecretValueTest(unittest.TestCase):
    """秘密情報の値が、標準出力 (ログ) にもメトリクスにも出ないこと。"""

    def test_sentinel_never_printed(self):
        findings = aggregate.trivy_findings(
            with_sentinel_trivy(load("trivy.json")), OWNER, REPO, COMMIT
        ) + aggregate.semgrep_findings(with_sentinel_semgrep(load("semgrep.json")), OWNER, REPO, COMMIT)
        repos = [make_repo(REPO, findings)]
        output = "\n".join(json.dumps(r, ensure_ascii=False) for r in aggregate.log_records(repos, "1"))
        output += aggregate.render_metrics(repos, 1700000000)
        self.assertGreater(len(findings), 0)
        self.assertNotIn(SENTINEL, output)

    def test_sentinel_never_printed_end_to_end(self):
        with tempfile.TemporaryDirectory() as work:
            targets = write_work(
                work,
                {
                    REPO: {
                        "trivy": with_sentinel_trivy(load("trivy.json")),
                        "semgrep": with_sentinel_semgrep(load("semgrep.json")),
                    }
                },
            )
            stdout, stderr, code, metrics = run_main(work, targets)
        self.assertEqual(code, 0)
        self.assertNotIn(SENTINEL, stdout + stderr + metrics)


class MetricsTest(unittest.TestCase):
    def test_all_combinations_with_zero(self):
        repos = [make_repo("a", []), make_repo("b", [])]
        lines = aggregate.render_metrics(repos, 1700000000).splitlines()
        findings = [l for l in lines if l.startswith("nuage_repo_scan_findings{")]
        self.assertEqual(len(findings), 2 * len(aggregate.CATEGORIES) * len(aggregate.SEVERITIES))
        self.assertTrue(all(l.endswith(" 0") for l in findings))
        self.assertIn('nuage_repo_scan_findings{repo="a",category="code",severity="Unknown"} 0', lines)
        self.assertIn('nuage_repo_scan_last_success_timestamp_seconds{repo="a"} 1700000000', lines)
        self.assertIn('nuage_repo_scan_target{repo="b"} 1', lines)

    def test_counts(self):
        findings = aggregate.trivy_findings(load("trivy.json"), OWNER, REPO, COMMIT)
        lines = aggregate.render_metrics([make_repo(REPO, findings)], 1700000000).splitlines()
        self.assertIn(f'nuage_repo_scan_findings{{repo="{REPO}",category="secret",severity="Critical"}} 3', lines)
        self.assertIn(f'nuage_repo_scan_findings{{repo="{REPO}",category="vulnerability",severity="Critical"}} 1', lines)
        self.assertIn(f'nuage_repo_scan_findings{{repo="{REPO}",category="vulnerability",severity="High"}} 1', lines)
        self.assertIn(f'nuage_repo_scan_findings{{repo="{REPO}",category="misconfiguration",severity="Medium"}} 2', lines)

    def test_failed_scanner_is_not_reported_as_zero(self):
        error = [{"step": "semgrep", "exit_code": "2", "log": "boom"}]
        repos = [make_repo("a", [], trivy=True, semgrep=False, errors=error)]
        text = aggregate.render_metrics(repos, 1700000000)
        self.assertNotIn('category="code"', text)
        self.assertIn('category="vulnerability"', text)
        self.assertNotIn("nuage_repo_scan_last_success_timestamp_seconds{", text)
        self.assertIn('nuage_repo_scan_target{repo="a"} 1', text)


class TargetsTest(unittest.TestCase):
    def write(self, text):
        f = tempfile.NamedTemporaryFile("w", delete=False, suffix=".txt")
        f.write(text)
        f.close()
        self.addCleanup(os.unlink, f.name)
        return f.name

    def test_repo_list_in_kustomize(self):
        path = os.path.join(os.path.dirname(os.path.abspath(__file__)), "..", "repos.txt")
        targets = aggregate.load_targets(path)
        self.assertEqual(len(targets), 10)
        self.assertIn(("k-wa-wa", "nuage-monitoring-stack"), targets)

    def test_comments_and_blank_lines(self):
        path = self.write("# c\n\n  k-wa-wa/a  \nk-wa-wa/b\n")
        self.assertEqual(aggregate.load_targets(path), [("k-wa-wa", "a"), ("k-wa-wa", "b")])

    def test_invalid(self):
        for text in ("", "# only comment\n", "no-owner\n", "a/b/c\n", "x/a\ny/a\n"):
            with self.assertRaises(ValueError, msg=text):
                aggregate.load_targets(self.write(text))


def write_work(work, repos):
    """clone.sh・scan-*.sh が残すものを作る。値が None の手順は失敗 (終了コード 1) として書く。"""
    names = []
    for name, steps in repos.items():
        names.append(f"{OWNER}/{name}")
        status = os.path.join(work, "status", name)
        out = os.path.join(work, "out", name)
        os.makedirs(status)
        os.makedirs(out)
        clone_ok = steps.get("clone", True)
        with open(os.path.join(status, "clone.exit"), "w") as f:
            f.write("0\n" if clone_ok else "128\n")
        if not clone_ok:
            with open(os.path.join(status, "clone.log"), "w") as f:
                f.write("fatal: repository not found\n")
            continue
        with open(os.path.join(status, "commit"), "w") as f:
            f.write(COMMIT + "\n")
        for scanner in ("trivy", "semgrep"):
            doc = steps.get(scanner)
            with open(os.path.join(status, f"{scanner}.exit"), "w") as f:
                f.write("0\n" if doc is not None else "1\n")
            if doc is None:
                with open(os.path.join(status, f"{scanner}.log"), "w") as f:
                    f.write(f"{scanner}: failed to download\n")
                continue
            with open(os.path.join(out, f"{scanner}.json"), "w") as f:
                json.dump(doc, f)
    targets = os.path.join(work, "repos.txt")
    with open(targets, "w") as f:
        f.write("\n".join(names) + "\n")
    return targets


def run_main(work, targets):
    import contextlib
    import io

    metrics_file = os.path.join(work, "metrics.txt")
    stdout, stderr = io.StringIO(), io.StringIO()
    with contextlib.redirect_stdout(stdout), contextlib.redirect_stderr(stderr):
        code = aggregate.main(
            ["--targets", targets, "--work", work, "--metrics-file", metrics_file, "--run-id", "1700000000"]
        )
    with open(metrics_file) as f:
        metrics = f.read()
    return stdout.getvalue(), stderr.getvalue(), code, metrics


class MainTest(unittest.TestCase):
    def test_partial_failure(self):
        with tempfile.TemporaryDirectory() as work:
            targets = write_work(
                work,
                {
                    "ok": {"trivy": load("trivy.json"), "semgrep": load("semgrep.json")},
                    "trivy-failed": {"trivy": None, "semgrep": load("semgrep.json")},
                    "clone-failed": {"clone": False},
                },
            )
            stdout, stderr, code, metrics = run_main(work, targets)

        # 失敗したリポジトリがあれば Job を失敗で終える。他のリポジトリは続けて処理する
        self.assertEqual(code, 1)
        self.assertIn("trivy-failed", stderr)
        self.assertIn("clone-failed", stderr)

        lines = metrics.splitlines()
        self.assertIn('nuage_repo_scan_last_success_timestamp_seconds{repo="ok"}', metrics)
        self.assertNotIn('nuage_repo_scan_last_success_timestamp_seconds{repo="trivy-failed"}', metrics)
        self.assertNotIn('nuage_repo_scan_last_success_timestamp_seconds{repo="clone-failed"}', metrics)
        for name in ("ok", "trivy-failed", "clone-failed"):
            self.assertIn(f'nuage_repo_scan_target{{repo="{name}"}} 1', lines)
        self.assertEqual(sum(1 for l in lines if l.startswith('nuage_repo_scan_findings{repo="ok"')), 20)
        trivy_failed = [l for l in lines if l.startswith('nuage_repo_scan_findings{repo="trivy-failed"')]
        self.assertEqual(len(trivy_failed), 5)
        self.assertTrue(all('category="code"' in l for l in trivy_failed))
        self.assertNotIn('nuage_repo_scan_findings{repo="clone-failed"', metrics)

        records = [json.loads(l) for l in stdout.splitlines()]
        status = {r["repo"]: r for r in records if r["event"] == "repo_status"}
        self.assertEqual(status["ok"]["status"], "ok")
        self.assertEqual(status["trivy-failed"]["status"], "failed")
        self.assertEqual(status["trivy-failed"]["errors"][0]["step"], "trivy")
        self.assertIn("failed to download", status["trivy-failed"]["errors"][0]["log"])
        self.assertEqual(status["clone-failed"]["errors"][0]["step"], "clone")
        done = {(r["repo"], r["scanner"]) for r in records if r["event"] == "scan_done"}
        self.assertEqual(done, {("ok", "trivy"), ("ok", "semgrep"), ("trivy-failed", "semgrep")})
        findings = [r for r in records if r["event"] == "finding"]
        self.assertEqual(len([f for f in findings if f["repo"] == "ok"]), 14)
        self.assertTrue(all(f["run_id"] == "1700000000" and f["msg"] for f in findings))

    def test_semgrep_failure_reason_from_json(self):
        # 手元で semgrep に存在しないルールセットを渡したとき (終了コード 7) の出力。標準エラーには何も出ない
        failed = {
            "errors": [
                {"code": 2, "level": "error", "type": "SemgrepError",
                 "message": "Failed to download configuration from https://semgrep.dev/c/p/does-not-exist-xyz HTTP 404."},
                {"code": 7, "level": "error", "type": "SemgrepError",
                 "message": "invalid configuration file found (1 configs were invalid)"},
            ],
            "results": [],
        }
        with tempfile.TemporaryDirectory() as work:
            targets = write_work(work, {"a": {"trivy": load("trivy.json"), "semgrep": None}})
            with open(os.path.join(work, "status", "a", "semgrep.exit"), "w") as f:
                f.write("7\n")
            with open(os.path.join(work, "status", "a", "semgrep.log"), "w") as f:
                f.write("")
            with open(os.path.join(work, "out", "a", "semgrep.json"), "w") as f:
                json.dump(failed, f)
            stdout, _, code, metrics = run_main(work, targets)
        self.assertEqual(code, 1)
        self.assertNotIn('category="code"', metrics)
        status = next(json.loads(l) for l in stdout.splitlines() if '"repo_status"' in l)
        self.assertEqual(status["errors"][0]["exit_code"], "7")
        self.assertIn("HTTP 404", status["errors"][0]["log"])

    def test_unreadable_json_is_a_failure(self):
        with tempfile.TemporaryDirectory() as work:
            targets = write_work(work, {"a": {"trivy": load("trivy.json"), "semgrep": load("semgrep.json")}})
            with open(os.path.join(work, "out", "a", "semgrep.json"), "w") as f:
                f.write("{not json")
            stdout, _, code, metrics = run_main(work, targets)
        self.assertEqual(code, 1)
        self.assertNotIn('category="code"', metrics)
        self.assertNotIn("nuage_repo_scan_last_success_timestamp_seconds{", metrics)


if __name__ == "__main__":
    unittest.main()
