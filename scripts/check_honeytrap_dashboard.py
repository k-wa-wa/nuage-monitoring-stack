#!/usr/bin/env python3
"""ダッシュボードの共通フィルターと、任意指定した読み取り専用 LogsQL API で分類を検証する。"""

import argparse
from datetime import datetime, timedelta, timezone
import json
from pathlib import Path
import re
import urllib.parse
import urllib.request
import urllib.error


ROOT = Path(__file__).resolve().parents[1]
MANIFEST = ROOT / "k8s/base/monitoring-pwa/grafana-dashboard-honeypot.yaml"


def load_dashboard():
    raw = MANIFEST.read_text().split("  honeypot-attack-map.json: |\n", 1)[1]
    return json.loads("\n".join(line[4:] for line in raw.splitlines()))


def interpolate(expr, variables):
    values = dict(variables)
    if "logsql" in values:
        values["effective_logsql"] = values["logsql"] if values["logsql"].strip() else "*"

    def replace(match):
        value = values[match[1]]
        if match[2] == "json":
            return json.dumps(value)
        if match[2] == "percentencode":
            return urllib.parse.quote(value, safe="-_.!~*'()")
        if match[2] == "regex":
            items = value if isinstance(value, list) else [value]
            return ".*" if "$__all" in items else "|".join(re.escape(item) for item in items)
        return value

    return re.sub(r"\$\{(\w+):(raw|regex|json|percentencode)\}", replace, expr)


def query(endpoint, expr, **params):
    body = urllib.parse.urlencode({"query": expr, **params}).encode()
    request = urllib.request.Request(endpoint, data=body)
    with urllib.request.urlopen(request, timeout=30) as response:
        return [json.loads(line) for line in response if line.strip()]


def validate_structure(dashboard):
    variables = {item["name"]: item for item in dashboard["templating"]["list"]}
    assert dashboard["templating"]["list"][0]["name"] == "traffic_class"
    assert variables["traffic_class"]["type"] == "custom"
    assert variables["traffic_class"]["multi"] and variables["traffic_class"]["includeAll"]
    assert variables["traffic_class"]["current"]["value"] == ["$__all"]
    assert variables["logsql"]["type"] == "textbox"
    assert variables["logsql"]["current"]["value"] == ""
    assert variables["known_traffic"]["current"]["value"] == "NOT traffic_class:*"
    assert [item["name"] for item in variables.values() if item.get("hide", 0) != 2] == ["traffic_class", "logsql"]
    assert variables["effective_logsql"]["type"] == "query"
    assert variables["effective_logsql"]["hide"] == 2
    assert variables["effective_logsql"]["skipUrlSync"]
    assert variables["known_traffic"]["type"] == "constant"
    assert variables["known_traffic"]["skipUrlSync"]
    assert variables["traffic_classifier"]["hide"] == 2
    assert variables["traffic_classifier"]["skipUrlSync"]
    ids = [panel["id"] for panel in dashboard["panels"]]
    assert len(ids) == len(set(ids))
    for panel in dashboard["panels"]:
        for target in panel.get("targets", []):
            expr = target["expr"]
            assert expr.count("${effective_logsql:raw}") == 1, panel["title"]
            assert expr.count("${traffic_class:regex}") == 1, panel["title"]
            assert expr.count("${known_traffic:raw}") == 1, panel["title"]
            assert expr.count("${traffic_classifier:raw}") == 1, panel["title"]
            assert expr.index("${effective_logsql:raw}") < expr.index("| stats") if "| stats" in expr else True
    table = next(panel for panel in dashboard["panels"] if panel["id"] == 4)
    assert "classification_reason" in table["targets"][0]["expr"]
    assert "details.method" in table["targets"][0]["expr"]
    assert "details.query" in table["targets"][0]["expr"]
    assert "details.user_agent" in table["targets"][0]["expr"]
    return {name: item["current"]["value"] for name, item in variables.items()}


def validate_classification(endpoint, dashboard, defaults):
    cases = [
        ("root", "unknown", {"protocol": "http", "details.path": "/"}),
        ("favicon", "unknown", {"protocol": "http", "details.path": "/favicon.ico"}),
        ("robots", "unknown", {"protocol": "http", "details.path": "/robots.txt"}),
        ("healthz", "probe", {"protocol": "http", "details.path": "/healthz"}),
        ("spoofed_monitor", "probe", {"protocol": "http", "details.path": "/healthz", "details.user_agent": "kube-probe/1.35"}),
        ("env", "probe", {"protocol": "http", "details.path": "/.env.production"}),
        ("git", "probe", {"protocol": "http", "details.path": "/.git/HEAD"}),
        ("ssh_key", "probe", {"protocol": "http", "details.path": "/.ssh/id_ed25519"}),
        ("backup", "probe", {"protocol": "http", "details.path": "/backup.tar.gz"}),
        ("heapdump", "probe", {"protocol": "http", "details.path": "/actuator/heapdump"}),
        ("crawler_env", "probe", {"protocol": "http", "details.path": "/.env", "details.user_agent": "KnownCrawler"}),
        ("private_ip_env", "probe", {"protocol": "http", "details.path": "/.env", "src_ip": "10.0.0.1"}),
        ("login_get", "probe", {"protocol": "http", "details.path": "/login", "details.method": "GET"}),
        ("agents", "probe", {"protocol": "http", "details.path": "/AGENTS.md"}),
        ("github_instructions", "probe", {"protocol": "http", "details.path": "/.github/copilot-instructions.md"}),
        ("index_html", "probe", {"protocol": "http", "details.path": "/index.html"}),
        ("robots_suffix", "probe", {"protocol": "http", "details.path": "/robots.txt.backup"}),
        ("favicon_child", "probe", {"protocol": "http", "details.path": "/favicon.ico/extra"}),
        ("missing_method", "probe", {"protocol": "http", "details.path": "/", "details.method": ""}),
        ("login", "attack", {"protocol": "http", "details.path": "/login", "details.method": "POST", "details.body": "username=test&password=fixture-secret"}),
        ("traversal", "attack", {"protocol": "http", "details.path": "/../../etc/passwd"}),
        ("encoded_traversal", "attack", {"protocol": "http", "details.path": "/%2e%2e%2fetc/passwd"}),
        ("sqli_root", "attack", {"protocol": "http", "details.path": "/", "details.query": "q=UNION+SELECT+1"}),
        ("sqli_body", "attack", {"protocol": "http", "details.path": "/search", "details.body": "q=UNION ALL SELECT 1"}),
        ("xss", "attack", {"protocol": "http", "details.path": "/", "details.query": "q=%3Cscript%3Ealert(1)"}),
        ("command", "attack", {"protocol": "http", "details.path": "/", "details.query": "x=1%3Bwget+example.invalid"}),
        ("ssh_banner", "unknown", {"protocol": "ssh", "details.stage": "pre_auth"}),
        ("ssh_none", "probe", {"protocol": "ssh", "details.auth_method": "none"}),
        ("ssh_password", "attack", {"protocol": "ssh", "details.auth_method": "password", "details.username": "test", "details.password": "fixture-secret"}),
        ("ssh_pubkey", "attack", {"protocol": "ssh", "details.auth_method": "publickey"}),
        ("pg_aborted", "unknown", {"protocol": "postgres", "details.stage": "handshake_aborted"}),
        ("pg_bad_packet", "unknown", {"protocol": "postgres", "details.stage": "invalid_packet_length"}),
        ("pg_startup", "probe", {"protocol": "postgres", "details.user": "test", "details.database": "test"}),
        ("json_secret", "unknown", {"protocol": "http", "details.path": "/", "details.body": '{"password": "fixture-secret"}'}),
        ("query_secret", "unknown", {"protocol": "http", "details.path": "/", "details.query": "token=fixture-secret&foo=bar"}),
        ("verified_monitor", "unknown", {"protocol": "http", "details.path": "/", "src_ip": "192.0.2.25", "domain": "fixture.invalid"}),
    ]
    for path in ["/", "/robots.txt", "/favicon.ico"]:
        for method in ["GET", "HEAD", "POST", "OPTIONS", "PUT", "DELETE"]:
            cases.append((method + path, "unknown" if method in ("GET", "HEAD") else "probe", {"protocol": "http", "details.path": path, "details.method": method}))
    for _, _, fields in cases:
        if fields.get("protocol") == "http":
            fields.setdefault("details.method", "GET")
    rows = [dict(fields, case=name) for name, _, fields in cases]
    synthetic = '_time:1s event:="__honeytrap_dashboard_fixture__" | union rows(' + ",".join(json.dumps(row) for row in rows) + ")"
    panels = {panel["id"]: panel for panel in dashboard["panels"]}
    table_expr = panels[4]["targets"][0]["expr"]
    common = table_expr.split("\n| replace_regexp", 1)[0]
    common = common.replace('kubernetes.pod_namespace:="nuage-honeytrap" event:="honeypot_attack"', synthetic, 1)

    actual = query(endpoint, interpolate(common, {**defaults, "logsql": "*"}) + " | fields case, traffic_class, classification_reason")
    actual_classes = {row["case"]: row["traffic_class"] for row in actual}
    expected_classes = {name: expected for name, expected, _ in cases}
    assert actual_classes == expected_classes, {name: (expected, actual_classes.get(name)) for name, expected in expected_classes.items() if actual_classes.get(name) != expected}
    assert all(row["classification_reason"] for row in actual)
    assert "パスワード試行は未観測" in next(row["classification_reason"] for row in actual if row["case"] == "pg_startup")
    selected = query(endpoint, interpolate(common, defaults) + " | fields case, traffic_class")
    assert {row["case"] for row in selected} == {name for name, _, _ in cases}
    for categories in [["probe"], ["probe", "attack"], ["unknown"], ["operational"]]:
        selected = query(endpoint, interpolate(common, {**defaults, "traffic_class": categories}) + " | fields case, traffic_class")
        assert {row["case"] for row in selected} == {name for name, category, _ in cases if category in categories}, categories
    known = 'src_ip:="192.0.2.25" domain:="fixture.invalid"'
    operational = query(endpoint, interpolate(common, {**defaults, "known_traffic": known, "logsql": 'traffic_class:="operational"'}) + " | fields case, traffic_class")
    assert [row["case"] for row in operational] == ["verified_monitor"]

    # 単一の入力欄で前方一致・正規表現・論理式・運用通信の除外を扱えることを確認する。
    filter_cases = [
        ('details.path:="/.git"*', {"git", "github_instructions"}),
        ('details.path:="/.git/HEAD"', {"git"}),
        ('protocol:="http" AND (details.path:="/.git"* OR details.path:="/.env"*)', {"git", "github_instructions", "env", "crawler_env", "private_ip_env"}),
        ('details.path:~"^/[.]git(/|$)"', {"git"}),
        ('details.user_agent:~"(?i)crawler"', {"crawler_env"}),
        ('traffic_class:="probe" AND NOT src_ip:="10.0.0.1"', {name for name, category, _ in cases if category == "probe" and name != "private_ip_env"}),
        ('* AND NOT (src_ip:="192.0.2.25" AND domain:="fixture.invalid")', {name for name, _, _ in cases if name != "verified_monitor"}),
    ]
    for expression, expected in filter_cases:
        filtered = query(endpoint, interpolate(common, {**defaults, "logsql": expression}) + " | fields case")
        assert {row["case"] for row in filtered} == expected, expression

    variable = next(v for v in dashboard["templating"]["list"] if v["name"] == "effective_logsql")
    for text in ["", " ", "\t\n", '*', 'details.path:="/.git"*', 'details.path:~"^/[.]git(/|$)"', r'details.path:~"^/\\.git(/|$)"', "details.path:='/AGENTS.md'", 'details.user_agent:~"日本語"', 'protocol:="http" OR']:
        # 隠し変数も実際の LogsQL エンジンで評価し、空欄以外の式を書き換えないことを確認する。
        normalized = query(endpoint, interpolate(variable["query"]["query"], {**defaults, "logsql": text}))
        expected = text if text.strip() else "*"
        assert [row["effective_filter"] for row in normalized] == [expected], (text, normalized)
    for blank in ["", " ", "\t\n"]:
        selected = query(endpoint, interpolate(common, {**defaults, "logsql": blank}) + " | fields case")
        assert {row["case"] for row in selected} == {name for name, _, _ in cases}
    try:
        query(endpoint, interpolate(common, {**defaults, "logsql": 'protocol:="http" OR'}))
    except urllib.error.HTTPError as error:
        assert error.code == 400
    else:
        raise AssertionError("不正な検索式を全件表示に置き換えてはならない")

    boundary_rows = [{"case": name, "protocol": "http", "details.path": path} for name, path in [("directory", "/.git"), ("child", "/.git/config"), ("similar", "/.github"), ("middle", "/foo/.git/config")]]
    boundary_source = '_time:1s event:="__honeytrap_dashboard_fixture__" | union rows(' + ",".join(json.dumps(row) for row in boundary_rows) + ")"
    boundary_common = table_expr.split("\n| replace_regexp", 1)[0].replace('kubernetes.pod_namespace:="nuage-honeytrap" event:="honeypot_attack"', boundary_source, 1)
    for expression, expected in [('details.path:="/.git"*', {"directory", "child", "similar"}), ('details.path:~"^/[.]git(/|$)"', {"directory", "child"})]:
        filtered = query(endpoint, interpolate(boundary_common, {**defaults, "logsql": expression}) + " | fields case")
        assert {row["case"] for row in filtered} == expected, expression

    # 分類後の表示用マスクを通しても、本文・クエリの秘密値が一覧に残らないことを確認する。
    masked_expr = table_expr.replace('kubernetes.pod_namespace:="nuage-honeytrap" event:="honeypot_attack"', synthetic, 1)
    masked_expr = masked_expr.split("\n| fields", 1)[0] + " | fields case, details.password, details.body, details.query, payload"
    masked = query(endpoint, interpolate(masked_expr, {**defaults, "logsql": "*"}))
    assert "fixture-secret" not in json.dumps(masked)
    print(f"PASS: {len(cases)} 件の分類例、分類選択、空欄の正規化、前方一致・論理式・不正な式、マスク")


def validate_live_queries(endpoint, dashboard, defaults):
    end = datetime.now(timezone.utc)
    params = {"start": (end - timedelta(hours=24)).isoformat(), "end": end.isoformat()}
    for panel in dashboard["panels"]:
        for target in panel.get("targets", []):
            for filter_expr in [defaults["logsql"], "*", 'traffic_class:="unknown"', 'protocol:="http" traffic_class:="probe"']:
                expr = interpolate(target["expr"], {**defaults, "logsql": filter_expr})
                result = query(endpoint, expr, **params)
                print(f"PASS: panel {panel['id']} / {filter_expr}: {len(result)} rows")


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--query-url", help="任意の LogsQL /select/logsql/query URL。POST は検索のみで、ログを挿入しない")
    args = parser.parse_args()
    dashboard = load_dashboard()
    defaults = validate_structure(dashboard)
    print("PASS: dashboard structure and shared filter wiring")
    if args.query_url:
        validate_classification(args.query_url, dashboard, defaults)
        validate_live_queries(args.query_url, dashboard, defaults)


if __name__ == "__main__":
    main()
