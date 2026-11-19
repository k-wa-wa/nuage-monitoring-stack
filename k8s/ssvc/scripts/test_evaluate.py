"""evaluate.py のテスト。標準ライブラリの unittest で動く。

    python3 -m unittest discover -s k8s/ssvc/scripts
"""

import contextlib
import csv
import datetime
import http.server
import io
import json
import os
import shutil
import subprocess
import tempfile
import threading
import unittest
from unittest import mock
import urllib.parse

import sys

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))

import evaluate  # noqa: E402

SCRIPTS_DIR = os.path.dirname(os.path.abspath(__file__))
UPSTREAM_CSV_PATH = os.path.join(SCRIPTS_DIR, "deployer_patch_application_priority_1_0_0.csv")
CSV_PATH = os.path.join(SCRIPTS_DIR, "deployer_patch_application_priority_nuage.csv")
SSVC_DIR = os.path.dirname(SCRIPTS_DIR)


class DecisionTableTest(unittest.TestCase):
    def _assert_csv_matches_table(self, csv_path):
        table = evaluate.load_decision_table(csv_path)
        self.assertEqual(len(table), 72)

        with open(csv_path, encoding="utf-8", newline="") as f:
            rows = list(csv.DictReader(f))
        self.assertEqual(len(rows), 72)

        seen = set()
        for row in rows:
            exp = row["Exploitation v1.1.0"].strip()
            expo = row["System Exposure v1.0.1"].strip()
            auto = row["Automatable v2.0.0"].strip()
            hi = row["Human Impact v2.0.2"].strip()
            expected = row["Defer, Scheduled, Out-of-Cycle, Immediate v1.0.0"].strip()
            seen.add((exp, expo, auto, hi))
            self.assertEqual(
                evaluate.evaluate_ssvc(exp, expo, auto, hi, table),
                expected,
                f"mismatch in {os.path.basename(csv_path)} for {(exp, expo, auto, hi)}",
            )
        self.assertEqual(len(seen), 72)
        return rows, table

    def test_all_72_combinations_match_csv(self):
        self._assert_csv_matches_table(UPSTREAM_CSV_PATH)
        self._assert_csv_matches_table(CSV_PATH)

    def test_custom_table_differs_from_upstream_only_on_expected_5_rows(self):
        upstream_rows, upstream_table = self._assert_csv_matches_table(UPSTREAM_CSV_PATH)
        custom_rows, custom_table = self._assert_csv_matches_table(CSV_PATH)

        dec_col = "Defer, Scheduled, Out-of-Cycle, Immediate v1.0.0"
        expected_changed_rows = {
            "64": ("scheduled", "immediate"),
            "65": ("out-of-cycle", "immediate"),
            "66": ("out-of-cycle", "immediate"),
            "68": ("out-of-cycle", "immediate"),
            "69": ("out-of-cycle", "immediate"),
        }

        actual_changed = {}
        for up_row, cu_row in zip(upstream_rows, custom_rows):
            row_id = up_row["row"].strip()
            self.assertEqual(row_id, cu_row["row"].strip())
            for col in (
                "Exploitation v1.1.0",
                "System Exposure v1.0.1",
                "Automatable v2.0.0",
                "Human Impact v2.0.2",
            ):
                self.assertEqual(up_row[col].strip(), cu_row[col].strip())
            up_dec = up_row[dec_col].strip()
            cu_dec = cu_row[dec_col].strip()
            if up_dec != cu_dec:
                actual_changed[row_id] = (up_dec, cu_dec)
                self.assertEqual(up_row["Exploitation v1.1.0"].strip(), "active")
                self.assertEqual(up_row["System Exposure v1.0.1"].strip(), "open")

        self.assertEqual(actual_changed, expected_changed_rows)

        diff_keys = evaluate.diff_decision_tables(upstream_table, custom_table)
        self.assertEqual(
            diff_keys,
            frozenset(
                {
                    ("active", "open", "no", "low"),
                    ("active", "open", "no", "medium"),
                    ("active", "open", "no", "high"),
                    ("active", "open", "yes", "low"),
                    ("active", "open", "yes", "medium"),
                }
            ),
        )
        self.assertEqual(evaluate.DEFAULT_TABLE_PATH, CSV_PATH)
        self.assertEqual(evaluate.DEFAULT_BASE_TABLE_PATH, UPSTREAM_CSV_PATH)
        with open(os.path.join(SSVC_DIR, "cronjob.yaml"), encoding="utf-8") as f:
            cronjob_text = f.read()
        self.assertIn("--table=/scripts/deployer_patch_application_priority_nuage.csv", cronjob_text)
        self.assertIn("--base-table=/scripts/deployer_patch_application_priority_1_0_0.csv", cronjob_text)

    def test_missing_or_invalid_inputs_return_unknown(self):
        table = evaluate.load_decision_table(CSV_PATH)
        self.assertEqual(evaluate.evaluate_ssvc(None, "open", "yes", "high", table), "unknown")
        self.assertEqual(evaluate.evaluate_ssvc("active", None, "yes", "high", table), "unknown")
        self.assertEqual(evaluate.evaluate_ssvc("active", "open", None, "high", table), "unknown")
        self.assertEqual(evaluate.evaluate_ssvc("active", "open", "yes", None, table), "unknown")
        self.assertEqual(evaluate.evaluate_ssvc("bogus", "open", "yes", "high", table), "unknown")


class YamlParserTest(unittest.TestCase):
    def test_parse_nested_maps_lists_comments_and_quotes(self):
        text = """
# 先頭コメント
namespaces:
  pechka:
    human_impact: medium # インラインコメント
    workloads:
      mongodb:
        exposure: controlled
        reason: "ClusterIP だけで #api からしか使わない"
  nuage-honeytrap:
    human_impact: very high
    exposure: open
    reason: 'single ''quoted'' value'

repositories:
  pechka:
    namespaces: [pechka, nuage-honeytrap]
  nix-config:
    human_impact: medium
    exposure: small
    reason: "host config"

overrides:
  - cve: CVE-2024-1234
    namespace: pechka
    workload: api
    decision: defer
    reason: "not reachable"
    expires: "2026-12-31"
  - cve: GHSA-1111-2222-3333
    repository: nix-config
    decision: not-affected
    reason: "build only"
    expires: 2026-11-30
"""
        parsed = evaluate.parse_yaml(text)
        self.assertEqual(parsed["namespaces"]["pechka"]["human_impact"], "medium")
        self.assertEqual(
            parsed["namespaces"]["pechka"]["workloads"]["mongodb"]["reason"],
            "ClusterIP だけで #api からしか使わない",
        )
        self.assertEqual(
            parsed["namespaces"]["nuage-honeytrap"]["reason"],
            "single 'quoted' value",
        )
        self.assertEqual(
            parsed["repositories"]["pechka"]["namespaces"],
            ["pechka", "nuage-honeytrap"],
        )
        self.assertEqual(len(parsed["overrides"]), 2)
        self.assertEqual(parsed["overrides"][1]["expires"], "2026-11-30")

    def test_unsupported_yaml_syntax_raises(self):
        for bad in (
            "a:\n\tb: 1\n",
            "a: &anchor 1\n",
            "a: |\n  multiline\n",
            "a: !custom foo\n",
        ):
            with self.assertRaises(ValueError, msg=bad):
                evaluate.parse_yaml(bad)

    def test_repo_manifests_parse_and_match_yq(self):
        assets_path = os.path.join(SSVC_DIR, "declarations", "assets.yaml")
        overrides_path = os.path.join(SSVC_DIR, "declarations", "overrides.yaml")
        assets = evaluate.load_assets(assets_path)
        overrides = evaluate.load_overrides(overrides_path)
        self.assertIn("nuage-honeytrap", assets["namespaces"])
        self.assertEqual(assets["namespaces"]["nuage-honeytrap"]["exposure"], "open")
        self.assertTrue(assets["namespaces"]["nuage-honeytrap"]["exposure_reason"])
        self.assertTrue(assets["namespaces"]["nuage-honeytrap"]["reason"])
        self.assertIn("nuc-1", assets["hosts"])
        self.assertIsInstance(overrides, list)

        yq = shutil.which("yq")
        if yq:
            for path in (assets_path, overrides_path):
                res = subprocess.run([yq, ".", path], capture_output=True, text=True, check=True)
                with open(path, encoding="utf-8") as f:
                    self.assertEqual(evaluate.parse_yaml(f.read()), json.loads(res.stdout))


class AssetAndOverrideValidationTest(unittest.TestCase):
    def _write_temp(self, content):
        f = tempfile.NamedTemporaryFile("w", delete=False, suffix=".yaml", encoding="utf-8")
        f.write(content)
        f.close()
        self.addCleanup(os.unlink, f.name)
        return f.name

    def test_exposure_override_requires_reason(self):
        # Namespace 直下の exposure 上書きは exposure_reason が必須 (reason だけでは不可)
        path = self._write_temp(
            "namespaces:\n  pechka:\n    human_impact: high\n    reason: 'impact'\n    exposure: controlled\n"
        )
        with self.assertRaises(ValueError):
            evaluate.load_assets(path)

        path_ok = self._write_temp(
            "namespaces:\n  pechka:\n    human_impact: high\n    reason: 'impact'\n"
            "    exposure: controlled\n    exposure_reason: 'no ingress'\n"
        )
        loaded = evaluate.load_assets(path_ok)
        self.assertEqual(loaded["namespaces"]["pechka"]["exposure"], "controlled")
        self.assertEqual(loaded["namespaces"]["pechka"]["exposure_reason"], "no ingress")

        path_wl = self._write_temp(
            "namespaces:\n  pechka:\n    human_impact: high\n    workloads:\n      api:\n        exposure: small\n"
        )
        with self.assertRaises(ValueError):
            evaluate.load_assets(path_wl)

    def test_repo_unknown_namespace_raises(self):
        path = self._write_temp(
            "namespaces:\n  pechka:\n    human_impact: medium\n"
            "repositories:\n  pechka:\n    namespaces: [pechka, missing-ns]\n"
        )
        with self.assertRaises(ValueError):
            evaluate.load_assets(path)

    def test_exposure_resolution_and_repo_max(self):
        path = self._write_temp(
            """
namespaces:
  ns-ingress:
    human_impact: low
  ns-internal:
    human_impact: high
  ns-forced-open:
    human_impact: medium
    exposure: open
    exposure_reason: "tunnel"
  ns-with-wl:
    human_impact: medium
    workloads:
      db:
        exposure: small
        reason: "internal only"
      admin:
        human_impact: very high
repositories:
  multi:
    namespaces: [ns-ingress, ns-internal]
  forced:
    namespaces: [ns-internal, ns-forced-open]
  direct:
    human_impact: high
    exposure: small
    reason: "offline"
"""
        )
        assets = evaluate.load_assets(path)
        ingress_ns = {"ns-ingress", "ns-with-wl"}

        # Ingress 有無の自動判定
        self.assertEqual(
            evaluate.resolve_asset_for_k8s("ns-ingress", "web", assets, ingress_ns)["exposure"],
            "open",
        )
        self.assertEqual(
            evaluate.resolve_asset_for_k8s("ns-internal", "worker", assets, ingress_ns)["exposure"],
            "controlled",
        )
        # Namespace 上書き
        self.assertEqual(
            evaluate.resolve_asset_for_k8s("ns-forced-open", "app", assets, set())["exposure"],
            "open",
        )
        # Workload 上書き
        db = evaluate.resolve_asset_for_k8s("ns-with-wl", "db", assets, ingress_ns)
        self.assertEqual(db["exposure"], "small")
        self.assertEqual(db["exposure_source"], "declared_workload")
        self.assertEqual(db["human_impact"], "medium")

        admin = evaluate.resolve_asset_for_k8s("ns-with-wl", "admin", assets, ingress_ns)
        self.assertEqual(admin["exposure"], "open")
        self.assertEqual(admin["human_impact"], "very high")
        self.assertEqual(admin["human_impact_source"], "declared_workload")

        # 未登録 Namespace
        self.assertFalse(
            evaluate.resolve_asset_for_k8s("unregistered-ns", "app", assets, ingress_ns)["registered"]
        )

        # リポジトリ: 複数 Namespace は最も重い値 (high + open)
        multi = evaluate.resolve_asset_for_repo("multi", assets, ingress_ns)
        self.assertEqual(multi["human_impact"], "high")
        self.assertEqual(multi["exposure"], "open")

        forced = evaluate.resolve_asset_for_repo("forced", assets, set())
        self.assertEqual(forced["human_impact"], "high")
        self.assertEqual(forced["exposure"], "open")

        direct = evaluate.resolve_asset_for_repo("direct", assets, set())
        self.assertEqual(direct["human_impact"], "high")
        self.assertEqual(direct["exposure"], "small")

        self.assertFalse(
            evaluate.resolve_asset_for_repo("unknown-repo", assets, set())["registered"]
        )

    def test_override_validation_and_expiration(self):
        path = self._write_temp(
            """
overrides:
  - cve: CVE-2024-0001
    namespace: pechka
    workload: api
    decision: defer
    reason: "valid today"
    expires: "2026-11-19"
  - cve: CVE-2024-0002
    repository: nix-config
    decision: not-affected
    reason: "expired yesterday"
    expires: "2026-11-18"
"""
        )
        entries = evaluate.load_overrides(path)
        active, expired = evaluate.split_overrides(entries, datetime.date(2026, 11, 19))
        self.assertIn(("CVE-2024-0001", "k8s", "pechka", "api", ""), active)
        self.assertEqual(len(expired), 1)
        self.assertEqual(expired[0]["cve"], "CVE-2024-0002")

        # 翌日になると両方期限切れ
        active2, expired2 = evaluate.split_overrides(entries, datetime.date(2026, 11, 20))
        self.assertEqual(len(active2), 0)
        self.assertEqual(len(expired2), 2)


class VulnrichmentAndWorkloadTest(unittest.TestCase):
    def test_vulnrichment_path_and_extraction(self):
        self.assertEqual(evaluate.vulnrichment_path("CVE-2023-44487"), "2023/44xxx/CVE-2023-44487.json")
        self.assertEqual(evaluate.vulnrichment_path("CVE-2024-0001"), "2024/0xxx/CVE-2024-0001.json")
        self.assertEqual(evaluate.vulnrichment_path("CVE-2026-39824"), "2026/39xxx/CVE-2026-39824.json")
        self.assertIsNone(evaluate.vulnrichment_path("GHSA-abcd-efgh-ijkl"))

        doc_poc = {
            "containers": {
                "adp": [
                    {
                        "metrics": [
                            {
                                "other": {
                                    "type": "ssvc",
                                    "content": {
                                        "options": [
                                            {"Exploitation": "poc"},
                                            {"Automatable": "yes"},
                                        ]
                                    },
                                }
                            }
                        ]
                    }
                ]
            }
        }
        self.assertEqual(evaluate.extract_vulnrichment_ssvc(doc_poc), ("public poc", "yes"))

        doc_missing = {"containers": {"adp": [{"metrics": [{"other": {"type": "kev"}}]}]}}
        self.assertEqual(evaluate.extract_vulnrichment_ssvc(doc_missing), (None, None))

    def test_normalize_workload(self):
        self.assertEqual(evaluate.normalize_workload("ReplicaSet", "api-7d9c8f4b5"), "api")
        self.assertEqual(evaluate.normalize_workload("StatefulSet", "mongodb"), "mongodb")
        self.assertEqual(evaluate.normalize_workload("Pod", "backup-1700000000-dump-1"), "backup-dump")
        self.assertEqual(evaluate.normalize_workload("Pod", "nightly-1700000000-1"), "nightly")


class EvaluateFindingsTest(unittest.TestCase):
    def setUp(self):
        self.base_table = evaluate.load_decision_table(UPSTREAM_CSV_PATH)
        self.table = evaluate.load_decision_table(CSV_PATH)
        f = tempfile.NamedTemporaryFile("w", delete=False, suffix=".yaml", encoding="utf-8")
        f.write(
            """
namespaces:
  pechka:
    human_impact: very high
    workloads:
      worker:
        exposure: controlled
        reason: "no ingress"
repositories:
  pechka:
    namespaces: [pechka]
hosts:
  nuc-1:
    kind: proxmox
    human_impact: very high
    exposure: controlled
    scan: none
    reason: "hypervisor"
"""
        )
        f.close()
        self.addCleanup(os.unlink, f.name)
        self.assets = evaluate.load_assets(f.name)

    def test_kev_unknown_unregistered_and_override(self):
        findings = [
            # 1. KEV (Vulnrichment では none だが KEV で active -> open + yes + very high => immediate)
            {
                "source": "k8s",
                "namespace": "pechka",
                "workload": "api",
                "container": "api",
                "repository": "",
                "target": "pechka/api",
                "cve_id": "CVE-2024-1001",
                "pkg_name": "pkg-a",
                "installed_version": "1.0",
                "fixed_version": "1.1",
                "trivy_severity": "HIGH",
                "title": "kev test",
                "url": "",
            },
            # 2. GHSA (CVE ID でないので unknown)
            {
                "source": "k8s",
                "namespace": "pechka",
                "workload": "api",
                "container": "api",
                "repository": "",
                "target": "pechka/api",
                "cve_id": "GHSA-1234-5678-9012",
                "pkg_name": "pkg-b",
                "installed_version": "1.0",
                "fixed_version": "",
                "trivy_severity": "HIGH",
                "title": "ghsa test",
                "url": "",
            },
            # 3. Vulnrichment 欠損 CVE -> 例外で上書き (有効)
            {
                "source": "k8s",
                "namespace": "pechka",
                "workload": "worker",
                "container": "worker",
                "repository": "",
                "target": "pechka/worker",
                "cve_id": "CVE-2024-9999",
                "pkg_name": "pkg-c",
                "installed_version": "1.0",
                "fixed_version": "",
                "trivy_severity": "MEDIUM",
                "title": "overridden missing",
                "url": "",
            },
            # 4. 期限切れ例外 -> 効かず通常の評価 (none + open + no + very high => scheduled)
            {
                "source": "repo",
                "namespace": "",
                "workload": "",
                "container": "",
                "repository": "pechka",
                "target": "pechka",
                "cve_id": "CVE-2024-2002",
                "pkg_name": "pkg-d",
                "installed_version": "1.0",
                "fixed_version": "",
                "trivy_severity": "LOW",
                "title": "expired override",
                "url": "",
            },
            # 5. 未登録 Namespace -> unregistered
            {
                "source": "k8s",
                "namespace": "shadow-ns",
                "workload": "rogue",
                "container": "rogue",
                "repository": "",
                "target": "shadow-ns/rogue",
                "cve_id": "CVE-2024-1001",
                "pkg_name": "pkg-a",
                "installed_version": "1.0",
                "fixed_version": "",
                "trivy_severity": "CRITICAL",
                "title": "unregistered ns",
                "url": "",
            },
            # 6. 未登録リポジトリ -> unregistered
            {
                "source": "repo",
                "namespace": "",
                "workload": "",
                "container": "",
                "repository": "unknown-repo",
                "target": "unknown-repo",
                "cve_id": "CVE-2024-1001",
                "pkg_name": "pkg-a",
                "installed_version": "1.0",
                "fixed_version": "",
                "trivy_severity": "CRITICAL",
                "title": "unregistered repo",
                "url": "",
            },
        ]
        targets = {
            ("k8s", "pechka", "api"),
            ("k8s", "pechka", "worker"),
            ("k8s", "shadow-ns", "rogue"),
            ("repo", "", "pechka"),
            ("repo", "", "unknown-repo"),
        }
        active_ovr = {
            ("CVE-2024-9999", "k8s", "pechka", "worker", ""): {
                "cve": "CVE-2024-9999",
                "target_type": "k8s",
                "namespace": "pechka",
                "workload": "worker",
                "repository": "",
                "target": "pechka/worker",
                "decision": "not-affected",
                "reason": "false positive",
                "expires": "2026-12-31",
            }
        }
        expired_ovr = [
            {
                "cve": "CVE-2024-2002",
                "target_type": "repo",
                "namespace": "",
                "workload": "",
                "repository": "pechka",
                "target": "pechka",
                "decision": "defer",
                "reason": "old exception",
                "expires": "2026-11-01",
            }
        ]
        kev_set = {"CVE-2024-1001"}
        vuln_map = {
            "CVE-2024-1001": ("none", "yes"),
            "CVE-2024-2002": ("none", "no"),
        }
        epss_map = {"CVE-2024-1001": (0.95, 0.99)}

        evaluated, counts, reg = evaluate.evaluate_findings(
            findings,
            targets,
            self.assets,
            {"pechka"},
            active_ovr,
            kev_set,
            vuln_map,
            epss_map,
            self.table,
            base_decision_table=self.base_table,
        )

        by_title = {r["title"]: r for r in evaluated}
        self.assertEqual(by_title["kev test"]["exploitation"], "active")
        self.assertEqual(by_title["kev test"]["exploitation_source"], "KEV")
        self.assertEqual(by_title["kev test"]["decision"], "immediate")
        self.assertEqual(by_title["kev test"]["decision_source"], "ssvc_table")
        self.assertEqual(by_title["kev test"]["epss"], 0.95)

        self.assertEqual(by_title["ghsa test"]["decision"], "unknown")
        self.assertEqual(by_title["ghsa test"]["decision_source"], "missing_data")

        self.assertEqual(by_title["overridden missing"]["decision"], "not-affected")
        self.assertEqual(by_title["overridden missing"]["decision_source"], "override")

        self.assertEqual(by_title["expired override"]["decision"], "scheduled")
        self.assertEqual(by_title["expired override"]["decision_source"], "ssvc_table")

        self.assertEqual(by_title["unregistered ns"]["decision"], "unregistered")
        self.assertEqual(by_title["unregistered repo"]["decision"], "unregistered")

        metrics = evaluate.render_metrics(
            counts, reg, self.assets, expired_ovr, 1700000000, evaluated=evaluated
        )
        # CVE ID はメトリクスに出ない
        self.assertNotIn("CVE-", metrics)
        self.assertNotIn("GHSA-", metrics)
        self.assertIn("nuage_ssvc_unregistered_targets_total 2", metrics)
        self.assertIn("nuage_ssvc_unscanned_assets_total 1", metrics)
        self.assertIn("nuage_ssvc_expired_overrides_total 1", metrics)
        self.assertIn("nuage_ssvc_active_exploitation_total 3", metrics)
        self.assertIn(
            'nuage_ssvc_exploitation_findings{source="k8s",namespace="pechka",target="api",exploitation="active"} 1',
            metrics,
        )
        self.assertIn("nuage_ssvc_last_success_timestamp_seconds 1700000000", metrics)
        # 未登録対象にも全 7 種の decision が 0 で送られる (前日の残存と二重カウントしない)
        for dec in evaluate.ALL_METRIC_DECISIONS:
            expected_val = 1 if dec == "unregistered" else 0
            self.assertIn(
                f'nuage_ssvc_findings{{source="k8s",namespace="shadow-ns",target="rogue",decision="{dec}"}} {expected_val}',
                metrics,
            )
            self.assertIn(
                f'nuage_ssvc_findings{{source="repo",namespace="",target="unknown-repo",decision="{dec}"}} {expected_val}',
                metrics,
            )
        self.assertIn(
            'nuage_ssvc_unscanned_assets{host="nuc-1",kind="proxmox",human_impact="very high",exposure="controlled"} 1',
            metrics,
        )

    def test_active_open_medium_becomes_immediate_with_custom_decision_source(self):
        # pechka (medium, open) の aivisspeech にある KEV 2 件に相当する入力 (automatable=no / yes)
        f_assets = tempfile.NamedTemporaryFile("w", delete=False, suffix=".yaml", encoding="utf-8")
        f_assets.write(
            """
namespaces:
  pechka:
    human_impact: medium
    workloads:
      mongodb:
        exposure: controlled
        reason: "internal only"
repositories:
  nuage-autopilot5:
    human_impact: medium
    exposure: small
    reason: "local cli"
"""
        )
        f_assets.close()
        self.addCleanup(os.unlink, f_assets.name)
        assets = evaluate.load_assets(f_assets.name)

        findings = [
            {
                "source": "k8s",
                "namespace": "pechka",
                "workload": "aivisspeech",
                "container": "aivisspeech",
                "repository": "",
                "target": "pechka/aivisspeech",
                "cve_id": "CVE-2026-111497",
                "pkg_name": "brotli",
                "installed_version": "1.0.9",
                "fixed_version": "1.2.0",
                "trivy_severity": "MEDIUM",
                "title": "aivisspeech kev no",
                "url": "",
            },
            {
                "source": "k8s",
                "namespace": "pechka",
                "workload": "aivisspeech",
                "container": "aivisspeech",
                "repository": "",
                "target": "pechka/aivisspeech",
                "cve_id": "CVE-2026-48710",
                "pkg_name": "starlette",
                "installed_version": "0.37.2",
                "fixed_version": "0.49.1",
                "trivy_severity": "HIGH",
                "title": "aivisspeech kev yes",
                "url": "",
            },
            # controlled + medium + active + no -> scheduled (上流表のまま)
            {
                "source": "k8s",
                "namespace": "pechka",
                "workload": "mongodb",
                "container": "mongodb",
                "repository": "",
                "target": "pechka/mongodb",
                "cve_id": "CVE-2026-111497",
                "pkg_name": "brotli",
                "installed_version": "1.0.9",
                "fixed_version": "1.2.0",
                "trivy_severity": "MEDIUM",
                "title": "controlled kev",
                "url": "",
            },
            # small + medium + active + yes -> out-of-cycle (上流表のまま)
            {
                "source": "repo",
                "namespace": "",
                "workload": "",
                "container": "",
                "repository": "nuage-autopilot5",
                "target": "nuage-autopilot5",
                "cve_id": "CVE-2026-48710",
                "pkg_name": "starlette",
                "installed_version": "0.37.2",
                "fixed_version": "0.49.1",
                "trivy_severity": "HIGH",
                "title": "small kev",
                "url": "",
            },
        ]
        targets = {
            ("k8s", "pechka", "aivisspeech"),
            ("k8s", "pechka", "mongodb"),
            ("repo", "", "nuage-autopilot5"),
        }
        kev_set = {"CVE-2026-111497", "CVE-2026-48710"}
        vuln_map = {
            "CVE-2026-111497": ("none", "no"),
            "CVE-2026-48710": ("active", "yes"),
        }
        evaluated, counts, reg = evaluate.evaluate_findings(
            findings,
            targets,
            assets,
            {"pechka"},
            {},
            kev_set,
            vuln_map,
            {},
            self.table,
            base_decision_table=self.base_table,
        )
        by_title = {r["title"]: r for r in evaluated}
        self.assertEqual(by_title["aivisspeech kev no"]["decision"], "immediate")
        self.assertEqual(by_title["aivisspeech kev no"]["decision_source"], "ssvc_table_custom")
        self.assertEqual(by_title["aivisspeech kev yes"]["decision"], "immediate")
        self.assertEqual(by_title["aivisspeech kev yes"]["decision_source"], "ssvc_table_custom")
        self.assertEqual(by_title["controlled kev"]["decision"], "scheduled")
        self.assertEqual(by_title["controlled kev"]["decision_source"], "ssvc_table")
        self.assertEqual(by_title["small kev"]["decision"], "out-of-cycle")
        self.assertEqual(by_title["small kev"]["decision_source"], "ssvc_table")

        metrics = evaluate.render_metrics(
            counts, reg, assets, [], 1700000000, evaluated=evaluated
        )
        self.assertIn("nuage_ssvc_active_exploitation_total 4", metrics)
        self.assertIn(
            'nuage_ssvc_exploitation_findings{source="repo",namespace="",target="nuage-autopilot5",exploitation="active"} 1',
            metrics,
        )

    def test_zero_active_exploitation_emits_zero_metric(self):
        evaluated, counts, reg = evaluate.evaluate_findings(
            [], {("k8s", "pechka", "api")}, self.assets, {"pechka"}, {}, set(), {}, {}, self.table
        )
        metrics = evaluate.render_metrics(
            counts, reg, self.assets, [], 1700000000, evaluated=evaluated
        )
        self.assertIn("nuage_ssvc_active_exploitation_total 0", metrics)
        self.assertIn(
            'nuage_ssvc_exploitation_findings{source="k8s",namespace="pechka",target="api",exploitation="active"} 0',
            metrics,
        )


class EndToEndTest(unittest.TestCase):
    def test_end_to_end_with_mock_http(self):
        received_metrics = []

        class MockK8sClient:
            def list_all(self, path):
                if path.endswith("/ingresses"):
                    return [{"metadata": {"namespace": "pechka", "name": "pechka-ing"}}]
                if path.endswith("/vulnerabilityreports"):
                    return [
                        # PR 環境は除外される
                        {
                            "metadata": {
                                "namespace": "pechka-pr-99",
                                "name": "rs-api",
                                "labels": {
                                    "trivy-operator.resource.kind": "ReplicaSet",
                                    "trivy-operator.resource.name": "api-7d9c8f4b5",
                                    "trivy-operator.container.name": "api",
                                },
                            },
                            "report": {
                                "vulnerabilities": [
                                    {
                                        "vulnerabilityID": "CVE-2023-44487",
                                        "resource": "pkg",
                                        "installedVersion": "1.0",
                                        "fixedVersion": "1.1",
                                        "severity": "HIGH",
                                        "title": "pr should be ignored",
                                    }
                                ]
                            },
                        },
                        # 本番ワークロード (同じ ReplicaSet がロールアウトで 2 つあっても重複排除)
                        {
                            "metadata": {
                                "namespace": "pechka",
                                "name": "rs-api-old",
                                "labels": {
                                    "trivy-operator.resource.kind": "ReplicaSet",
                                    "trivy-operator.resource.name": "api-7d9c8f4b5",
                                    "trivy-operator.container.name": "api",
                                },
                            },
                            "report": {
                                "artifact": {"repository": "pechka/api", "tag": "v1"},
                                "vulnerabilities": [
                                    {
                                        "vulnerabilityID": "CVE-2023-44487",
                                        "resource": "h2",
                                        "installedVersion": "1.0",
                                        "fixedVersion": "1.1",
                                        "severity": "HIGH",
                                        "title": "rapid reset",
                                    }
                                ],
                            },
                        },
                        {
                            "metadata": {
                                "namespace": "pechka",
                                "name": "rs-api-new",
                                "labels": {
                                    "trivy-operator.resource.kind": "ReplicaSet",
                                    "trivy-operator.resource.name": "api-8f5c6b7d9",
                                    "trivy-operator.container.name": "api",
                                },
                            },
                            "report": {
                                "artifact": {"repository": "pechka/api", "tag": "v1"},
                                "vulnerabilities": [
                                    {
                                        "vulnerabilityID": "CVE-2023-44487",
                                        "resource": "h2",
                                        "installedVersion": "1.0",
                                        "fixedVersion": "1.1",
                                        "severity": "HIGH",
                                        "title": "rapid reset",
                                    }
                                ],
                            },
                        },
                    ]
                raise AssertionError(f"unexpected path: {path}")

        class Handler(http.server.BaseHTTPRequestHandler):
            def log_message(self, fmt, *args):
                pass

            def do_POST(self):
                length = int(self.headers.get("Content-Length", "0"))
                body = self.rfile.read(length).decode("utf-8")
                if self.path == "/select/logsql/query":
                    params = urllib.parse.parse_qs(body)
                    q = params["query"][0]
                    if 'event:="scan_done"' in q:
                        if 'scanner:="trivy"' not in q:
                            self.send_response(400)
                            self.end_headers()
                            return
                        lines = [
                            json.dumps({"_time": "2026-11-17T19:00:00Z", "repo": "pechka", "run_id": "100"}),
                            json.dumps({"_time": "2026-11-18T19:00:00Z", "repo": "pechka", "run_id": "200"}),
                        ]
                        resp = "\n".join(lines).encode("utf-8")
                    else:
                        if 'category:="vulnerability"' not in q or 'run_id:in("200")' not in q:
                            self.send_response(400)
                            self.end_headers()
                            return
                        lines = [
                            # クエリ条件で run_id="200" のみ返ることを再現しつつ、念のため 100 は返さない
                            json.dumps({
                                "repo": "pechka", "run_id": "200", "rule_id": "CVE-2024-99999",
                                "pkg_name": "new-lib", "installed_version": "0.2", "fixed_version": "0.3",
                                "severity": "MEDIUM", "title": "missing vulnrichment", "file": "go.mod",
                            }),
                        ]
                        resp = "\n".join(lines).encode("utf-8")
                    self.send_response(200)
                    self.end_headers()
                    self.wfile.write(resp)
                elif self.path == "/api/v1/import/prometheus":
                    received_metrics.append(body)
                    self.send_response(204)
                    self.end_headers()
                else:
                    self.send_response(404)
                    self.end_headers()

            def do_GET(self):
                if self.path == "/kev.json":
                    payload = {"vulnerabilities": [{"cveID": "CVE-2023-44487"}]}
                elif self.path == "/vulnrichment/2023/44xxx/CVE-2023-44487.json":
                    payload = {
                        "containers": {
                            "adp": [
                                {
                                    "metrics": [
                                        {
                                            "other": {
                                                "type": "ssvc",
                                                "content": {
                                                    "options": [
                                                        {"Exploitation": "active"},
                                                        {"Automatable": "yes"},
                                                    ]
                                                },
                                            }
                                        }
                                    ]
                                }
                            ]
                        }
                    }
                elif self.path.startswith("/epss"):
                    payload = {
                        "data": [
                            {"cve": "CVE-2023-44487", "epss": "0.99", "percentile": "0.999"}
                        ]
                    }
                else:
                    self.send_response(404)
                    self.end_headers()
                    return
                self.send_response(200)
                self.end_headers()
                self.wfile.write(json.dumps(payload).encode("utf-8"))

        server = http.server.ThreadingHTTPServer(("127.0.0.1", 0), Handler)
        thread = threading.Thread(target=server.serve_forever, daemon=True)
        thread.start()
        self.addCleanup(server.shutdown)
        self.addCleanup(server.server_close)
        base = f"http://127.0.0.1:{server.server_port}"

        with tempfile.TemporaryDirectory() as tmp:
            assets_path = os.path.join(tmp, "assets.yaml")
            with open(assets_path, "w", encoding="utf-8") as f:
                f.write("namespaces:\n  pechka:\n    human_impact: very high\nrepositories:\n  pechka:\n    namespaces: [pechka]\n")
            overrides_path = os.path.join(tmp, "overrides.yaml")
            with open(overrides_path, "w", encoding="utf-8") as f:
                f.write("overrides: []\n")

            stdout, stderr = io.StringIO(), io.StringIO()
            with contextlib.redirect_stdout(stdout), contextlib.redirect_stderr(stderr):
                code = evaluate.run_evaluation(
                    assets_path=assets_path,
                    overrides_path=overrides_path,
                    decision_table_path=CSV_PATH,
                    vlogs_url=f"{base}/select/logsql/query",
                    push_url=f"{base}/api/v1/import/prometheus",
                    run_id="999",
                    today=datetime.date(2026, 11, 19),
                    now=1700000000,
                    k8s_client=MockK8sClient(),
                    kev_url=f"{base}/kev.json",
                    vulnrichment_base_url=f"{base}/vulnrichment",
                    epss_url=f"{base}/epss",
                )

        self.assertEqual(code, 0, stderr.getvalue())
        records = [json.loads(line) for line in stdout.getvalue().splitlines()]
        findings = [r for r in records if r["event"] == "ssvc_finding"]
        self.assertEqual(len(findings), 2)
        by_src = {r["source"]: r for r in findings}
        self.assertEqual(by_src["k8s"]["decision"], "immediate")
        self.assertEqual(by_src["k8s"]["workload"], "api")
        self.assertEqual(by_src["k8s"]["epss"], 0.99)
        self.assertEqual(by_src["repo"]["decision"], "unknown")
        self.assertEqual(by_src["repo"]["cve_id"], "CVE-2024-99999")

        done = next(r for r in records if r["event"] == "eval_done")
        self.assertEqual(done["run_id"], "999")
        self.assertEqual(done["counts_by_decision"]["immediate"], 1)
        self.assertEqual(done["counts_by_decision"]["unknown"], 1)

        self.assertEqual(len(received_metrics), 1)
        self.assertIn(
            'nuage_ssvc_findings{source="k8s",namespace="pechka",target="api",decision="immediate"} 1',
            received_metrics[0],
        )
        self.assertIn(
            'nuage_ssvc_findings{source="repo",namespace="",target="pechka",decision="unknown"} 1',
            received_metrics[0],
        )
        self.assertNotIn("pechka-pr-99", received_metrics[0])

    def _run_with_routes(self, routes, k8s_client, overrides_yaml="overrides: []\n", human_impact="very high"):
        received_metrics = []

        class Handler(http.server.BaseHTTPRequestHandler):
            def log_message(self, fmt, *args):
                pass

            def do_POST(self):
                length = int(self.headers.get("Content-Length", "0"))
                body = self.rfile.read(length).decode("utf-8")
                if self.path == "/select/logsql/query":
                    params = urllib.parse.parse_qs(body)
                    q = params["query"][0]
                    if 'event:="scan_done"' in q:
                        resp = json.dumps(
                            {"_time": "2026-11-18T19:00:00Z", "repo": "pechka", "run_id": "200"}
                        ).encode("utf-8")
                    else:
                        resp = json.dumps(
                            {
                                "repo": "pechka",
                                "run_id": "200",
                                "rule_id": "CVE-2024-5555",
                                "pkg_name": "lib",
                                "installed_version": "1.0",
                                "fixed_version": "1.1",
                                "severity": "HIGH",
                                "title": "repo vuln",
                                "file": "go.mod",
                            }
                        ).encode("utf-8")
                    self.send_response(200)
                    self.end_headers()
                    self.wfile.write(resp)
                elif self.path == "/api/v1/import/prometheus":
                    received_metrics.append(body)
                    self.send_response(204)
                    self.end_headers()
                else:
                    self.send_response(404)
                    self.end_headers()

            def do_GET(self):
                key = self.path.split("?", 1)[0]
                status, payload = routes.get(key, (404, {}))
                self.send_response(status)
                self.end_headers()
                if payload is not None:
                    self.wfile.write(json.dumps(payload).encode("utf-8"))

        server = http.server.ThreadingHTTPServer(("127.0.0.1", 0), Handler)
        thread = threading.Thread(target=server.serve_forever, daemon=True)
        thread.start()
        self.addCleanup(server.shutdown)
        self.addCleanup(server.server_close)
        base = f"http://127.0.0.1:{server.server_port}"

        with tempfile.TemporaryDirectory() as tmp:
            assets_path = os.path.join(tmp, "assets.yaml")
            with open(assets_path, "w", encoding="utf-8") as f:
                f.write(
                    f"namespaces:\n  pechka:\n    human_impact: {human_impact}\n"
                    "repositories:\n  pechka:\n    namespaces: [pechka]\n"
                )
            overrides_path = os.path.join(tmp, "overrides.yaml")
            with open(overrides_path, "w", encoding="utf-8") as f:
                f.write(overrides_yaml)

            stdout, stderr = io.StringIO(), io.StringIO()
            with mock.patch("time.sleep"), contextlib.redirect_stdout(stdout), contextlib.redirect_stderr(stderr):
                code = evaluate.run_evaluation(
                    assets_path=assets_path,
                    overrides_path=overrides_path,
                    decision_table_path=CSV_PATH,
                    vlogs_url=f"{base}/select/logsql/query",
                    push_url=f"{base}/api/v1/import/prometheus",
                    run_id="999",
                    today=datetime.date(2026, 11, 19),
                    now=1700000000,
                    k8s_client=k8s_client,
                    kev_url=f"{base}/kev.json",
                    vulnrichment_base_url=f"{base}/vulnrichment",
                    epss_url=f"{base}/epss",
                )
        return code, stdout.getvalue(), stderr.getvalue(), received_metrics

    def test_vulnrichment_5xx_returns_exit_1_and_omits_last_success(self):
        class EmptyK8s:
            def list_all(self, path):
                return []

        routes = {
            "/kev.json": (200, {"vulnerabilities": []}),
            "/vulnrichment/2024/5xxx/CVE-2024-5555.json": (500, {"error": "upstream"}),
            "/epss": (200, {"data": []}),
        }
        code, stdout, stderr, metrics = self._run_with_routes(routes, EmptyK8s())
        self.assertEqual(code, 1)
        self.assertIn("vulnrichment_error", stderr)
        finding = next(json.loads(l) for l in stdout.splitlines() if '"ssvc_finding"' in l)
        self.assertEqual(finding["exploitation_source"], "error")
        self.assertEqual(finding["automatable_source"], "error")
        self.assertEqual(finding["decision"], "unknown")
        self.assertEqual(finding["decision_source"], "error")
        self.assertNotIn("nuage_ssvc_last_success_timestamp_seconds", metrics[0])

    def test_epss_failure_continues_evaluation(self):
        class EmptyK8s:
            def list_all(self, path):
                return []

        routes = {
            "/kev.json": (200, {"vulnerabilities": []}),
            "/vulnrichment/2024/5xxx/CVE-2024-5555.json": (
                200,
                {
                    "containers": {
                        "adp": [
                            {
                                "metrics": [
                                    {
                                        "other": {
                                            "type": "ssvc",
                                            "content": {
                                                "options": [
                                                    {"Exploitation": "none"},
                                                    {"Automatable": "no"},
                                                ]
                                            },
                                        }
                                    }
                                ]
                            }
                        ]
                    }
                },
            ),
            "/epss": (503, {"error": "unavailable"}),
        }
        code, stdout, stderr, metrics = self._run_with_routes(routes, EmptyK8s())
        self.assertEqual(code, 0)
        self.assertIn("epss_error", stderr)
        finding = next(json.loads(l) for l in stdout.splitlines() if '"ssvc_finding"' in l)
        self.assertIsNone(finding["epss"])
        self.assertEqual(finding["decision"], "scheduled")
        self.assertIn("nuage_ssvc_last_success_timestamp_seconds 1700000000", metrics[0])

    def test_kev_failure_aborts_without_push(self):
        class EmptyK8s:
            def list_all(self, path):
                return []

        routes = {
            "/kev.json": (500, {"error": "kev down"}),
        }
        with self.assertRaises(RuntimeError):
            self._run_with_routes(routes, EmptyK8s())

    def test_repository_override_applies_in_end_to_end(self):
        class EmptyK8s:
            def list_all(self, path):
                return []

        routes = {
            "/kev.json": (200, {"vulnerabilities": []}),
            "/vulnrichment/2024/5xxx/CVE-2024-5555.json": (404, {}),
            "/epss": (200, {"data": []}),
        }
        ovr = (
            "overrides:\n"
            "  - cve: CVE-2024-5555\n"
            "    repository: pechka\n"
            "    decision: defer\n"
            "    reason: 'repo exception'\n"
            "    expires: '2026-12-31'\n"
        )
        code, stdout, _, metrics = self._run_with_routes(routes, EmptyK8s(), overrides_yaml=ovr)
        self.assertEqual(code, 0)
        finding = next(json.loads(l) for l in stdout.splitlines() if '"ssvc_finding"' in l)
        self.assertEqual(finding["decision"], "defer")
        self.assertEqual(finding["decision_source"], "override")
        self.assertIn(
            'nuage_ssvc_findings{source="repo",namespace="",target="pechka",decision="defer"} 1',
            metrics[0],
        )

    def test_k8s_client_pagination_iter_items(self):
        seen_queries = []

        class PagedHandler(http.server.BaseHTTPRequestHandler):
            def log_message(self, fmt, *args):
                pass

            def do_GET(self):
                parsed = urllib.parse.urlparse(self.path)
                params = urllib.parse.parse_qs(parsed.query)
                seen_queries.append(params)
                if "continue" not in params:
                    payload = {"metadata": {"continue": "tok2"}, "items": [{"id": 1}, {"id": 2}]}
                else:
                    payload = {"metadata": {}, "items": [{"id": 3}]}
                self.send_response(200)
                self.end_headers()
                self.wfile.write(json.dumps(payload).encode("utf-8"))

        server = http.server.ThreadingHTTPServer(("127.0.0.1", 0), PagedHandler)
        thread = threading.Thread(target=server.serve_forever, daemon=True)
        thread.start()
        self.addCleanup(server.shutdown)
        self.addCleanup(server.server_close)

        client = evaluate.K8sClient(
            api_server=f"http://127.0.0.1:{server.server_port}",
            token="test-token",
            ca_file=None,
        )
        items = list(client.iter_items("/apis/test/v1/things", page_size=2))
        self.assertEqual([i["id"] for i in items], [1, 2, 3])
        self.assertEqual(seen_queries[0]["limit"], ["2"])
        self.assertEqual(seen_queries[1]["continue"], ["tok2"])

    def test_custom_table_end_to_end_medium_kev_becomes_immediate(self):
        class MediumK8s:
            def list_all(self, path):
                if path.endswith("/ingresses"):
                    return [{"metadata": {"namespace": "pechka", "name": "pechka-ing"}}]
                return []

        routes = {
            "/kev.json": (200, {"vulnerabilities": [{"cveID": "CVE-2024-5555"}]}),
            "/vulnrichment/2024/5xxx/CVE-2024-5555.json": (
                200,
                {
                    "containers": {
                        "adp": [
                            {
                                "metrics": [
                                    {
                                        "other": {
                                            "type": "ssvc",
                                            "content": {
                                                "options": [
                                                    {"Exploitation": "none"},
                                                    {"Automatable": "yes"},
                                                ]
                                            },
                                        }
                                    }
                                ]
                            }
                        ]
                    }
                },
            ),
            "/epss": (200, {"data": []}),
        }
        code, stdout, _, metrics = self._run_with_routes(routes, MediumK8s(), human_impact="medium")
        self.assertEqual(code, 0)
        records = [json.loads(l) for l in stdout.splitlines()]
        finding = next(r for r in records if r["event"] == "ssvc_finding")
        self.assertEqual(finding["decision"], "immediate")
        self.assertEqual(finding["decision_source"], "ssvc_table_custom")
        self.assertEqual(finding["exploitation"], "active")
        self.assertEqual(finding["exploitation_source"], "KEV")
        done = next(r for r in records if r["event"] == "eval_done")
        self.assertEqual(done["active_exploitation"], 1)
        self.assertIn("nuage_ssvc_active_exploitation_total 1", metrics[0])


class DashboardTest(unittest.TestCase):
    def test_security_dashboard_keeps_existing_panels_and_adds_ssvc(self):
        dash_path = os.path.join(
            os.path.dirname(SSVC_DIR), "trivy-operator", "dashboards", "trivy-operator.json"
        )
        with open(dash_path, encoding="utf-8") as f:
            dash = json.load(f)

        all_ids = []

        def collect_ids(panels):
            for p in panels:
                all_ids.append(p["id"])
                if p.get("panels"):
                    collect_ids(p["panels"])

        collect_ids(dash["panels"])
        self.assertEqual(len(all_ids), len(set(all_ids)), "panel id duplicated")

        expected_existing_ids = {2, 4, 8, 23, 28, 29, 31, 32, 33, 34, 35, 36, 40, 41, 42, 50, 51, 52, 53, 54, 55, 56}
        self.assertTrue(expected_existing_ids.issubset(set(all_ids)))

        by_id = {p["id"]: p for p in dash["panels"]}
        for pid in (100, 101, 102, 103, 104, 105, 106, 107, 108, 109, 110):
            self.assertIn(pid, by_id)

        # SSVC Prioritization 行がダッシュボードの一番上で、既存の行がその下に元の順で並ぶ
        top_ids = [p["id"] for p in dash["panels"]]
        self.assertEqual(top_ids[0], 100)
        self.assertEqual(by_id[100]["gridPos"]["y"], 0)
        self.assertFalse(by_id[100]["collapsed"])
        row_order = [p["id"] for p in dash["panels"] if p.get("type") == "row"]
        self.assertEqual(row_order, [100, 31, 50, 2, 4, 8, 35])
        self.assertTrue(by_id[31]["collapsed"])
        for row_id in (100, 50, 2, 4, 8, 35):
            self.assertFalse(by_id[row_id]["collapsed"])

        # panels 配列の順が表示順（gridPos.y 非減少）と一致する
        ys = [p["gridPos"]["y"] for p in dash["panels"]]
        self.assertEqual(ys, sorted(ys))

        # 悪用が確認済み (Exploitation = active) パネルが SSVC 行の一番上 (Findings by Decision より上) にある
        self.assertLess(by_id[109]["gridPos"]["y"], by_id[101]["gridPos"]["y"])
        self.assertLess(by_id[110]["gridPos"]["y"], by_id[101]["gridPos"]["y"])
        p109_expr = by_id[109]["targets"][0]["expr"]
        p109_filter = p109_expr.split("\n|")[0]
        self.assertIn('exploitation:="active"', p109_filter)
        self.assertNotIn("decision", p109_filter)
        self.assertIn("exploitation_source", p109_expr)
        self.assertIn("decision", p109_expr)
        self.assertIn("nuage_ssvc_active_exploitation_total", by_id[110]["targets"][0]["expr"])

        self.assertIn("nuage_ssvc_last_success_timestamp_seconds", by_id[108]["targets"][0]["expr"])
        for pid in (101, 102, 110):
            for t in by_id[pid]["targets"]:
                self.assertNotIn("or vector(0)", t["expr"])
                self.assertIn("or vector(-1)", t["expr"])
        for pid in (104, 107, 109):
            self.assertEqual(by_id[pid].get("timeFrom"), "36h")

        # Immediate & Out-of-Cycle by Target (103): 悪用の状況列とソート順
        p103_exprs = "\n".join(t["expr"] for t in by_id[103]["targets"])
        self.assertIn('exploitation="active"', p103_exprs)
        self.assertIn('exploitation="public poc"', p103_exprs)
        self.assertIn('exploitation="none"', p103_exprs)
        sort_tf = next(tf for tf in by_id[103]["transformations"] if tf["id"] == "sortBy")
        sort_fields = [s["field"] for s in sort_tf["options"]["sort"]]
        self.assertEqual(
            sort_fields[:5], ["Immediate", "Active", "Public PoC", "Out-of-Cycle", "None"]
        )

        # Immediate & Out-of-Cycle CVE Details (104): 判定 -> 悪用の状況 (active -> public poc -> none) の順
        p104_expr = by_id[104]["targets"][0]["expr"]
        self.assertIn('format if (exploitation:="active") "1" as _exp_rank', p104_expr)
        self.assertIn('format if (exploitation:="public poc") "2" as _exp_rank', p104_expr)
        self.assertIn('format if (exploitation:="none") "3" as _exp_rank', p104_expr)
        self.assertIn("sort by (decision asc, _exp_rank asc", p104_expr)


if __name__ == "__main__":
    unittest.main()
