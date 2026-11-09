#!/usr/bin/env python3
"""
Grafana 一時検証用ダッシュボード操作スクリプト。
本番のダッシュボード資材を汚さず、Grafana API 経由で一時的なダッシュボードを作成・確認・削除する。
標準ライブラリのみで動作する。
"""

import argparse
import base64
import json
import os
import re
import ssl
import sys
import urllib.error
import urllib.request

DEFAULT_GRAFANA_URL = os.environ.get("GRAFANA_URL", "https://monitoring.wpcapp.net/grafana")
DEFAULT_USER = os.environ.get("GRAFANA_USER", "admin")
DEFAULT_PASSWORD = os.environ.get("GRAFANA_PASSWORD", "prom-operator")


def get_ssl_context():
    ctx = ssl.create_default_context()
    ctx.check_hostname = False
    ctx.verify_mode = ssl.CERT_NONE
    return ctx


def make_request(path, method="GET", data=None, user=DEFAULT_USER, password=DEFAULT_PASSWORD, base_url=DEFAULT_GRAFANA_URL):
    url = f"{base_url.rstrip('/')}/{path.lstrip('/')}"
    auth_header = "Basic " + base64.b64encode(f"{user}:{password}".encode("utf-8")).decode("utf-8")
    headers = {
        "Authorization": auth_header,
        "Content-Type": "application/json",
        "Accept": "application/json",
    }
    body = json.dumps(data).encode("utf-8") if data is not None else None
    req = urllib.request.Request(url, data=body, headers=headers, method=method)

    try:
        with urllib.request.urlopen(req, context=get_ssl_context()) as res:
            return res.status, json.loads(res.read().decode("utf-8"))
    except urllib.error.HTTPError as e:
        err_body = e.read().decode("utf-8")
        try:
            return e.code, json.loads(err_body)
        except Exception:
            return e.code, {"message": err_body}


def extract_dashboard_json(file_path):
    with open(file_path, "r", encoding="utf-8") as f:
        content = f.read()

    # JSON として直接パースできる場合
    try:
        data = json.loads(content)
        if "data" in data and isinstance(data["data"], dict):
            # ConfigMap JSON
            for k, v in data["data"].items():
                if k.endswith(".json"):
                    return json.loads(v)
        return data
    except Exception:
        pass

    # ConfigMap YAML の場合: "xxx.json: |" ブロックを抽出
    match = re.search(r"^\s+([a-zA-Z0-9_\-\.]+\.json):\s*\|\s*$", content, re.MULTILINE)
    if match:
        key = match.group(1)
        lines = content.splitlines()
        idx = -1
        indent = 0
        for i, l in enumerate(lines):
            if f"{key}: |" in l:
                idx = i + 1
                break
        if idx != -1 and idx < len(lines):
            # 次の行のインデントを検出
            first_content = lines[idx]
            indent = len(first_content) - len(first_content.lstrip())
            json_lines = []
            for l in lines[idx:]:
                if not l.strip():
                    json_lines.append("")
                    continue
                current_indent = len(l) - len(l.lstrip())
                if current_indent < indent:
                    break
                json_lines.append(l[indent:])
            return json.loads("\n".join(json_lines))

    raise ValueError(f"ダッシュボード JSON を抽出できなかった: {file_path}")


def cmd_create(args):
    dashboard = extract_dashboard_json(args.file)

    orig_uid = dashboard.get("uid", "temp")
    orig_title = dashboard.get("title", "Temporary Dashboard")

    suffix = args.suffix
    debug_uid = f"{orig_uid}{suffix}"
    debug_title = f"{orig_title} (検証)"

    dashboard["uid"] = debug_uid
    dashboard["title"] = debug_title
    if "id" in dashboard:
        del dashboard["id"]

    payload = {
        "dashboard": dashboard,
        "overwrite": True,
    }

    status, res = make_request("api/dashboards/db", method="POST", data=payload)
    if status != 200:
        print(f"作成失敗 (HTTP {status}): {res.get('message', res)}", file=sys.stderr)
        sys.exit(1)

    url_path = res.get("url", f"/grafana/d/{debug_uid}")
    base = DEFAULT_GRAFANA_URL.rstrip("/")
    if base.endswith("/grafana") and url_path.startswith("/grafana/"):
        full_url = f"{base[: -len('/grafana')]}{url_path}"
    else:
        full_url = f"{base}{url_path}"

    print(f"検証用ダッシュボードを作成した: uid={debug_uid}")
    print()
    print("【確認用 URL】")
    print(f"- 通常表示:      {full_url}")
    print(f"- フッター非表示: {full_url}?hideLogo=true")
    print(f"- Kiosk モード:  {full_url}?kiosk&hideLogo=true")
    print(f"- TV モード:     {full_url}?kiosk=tv&hideLogo=true")
    print()
    print(f"削除コマンド: {sys.argv[0]} delete {debug_uid}")


def cmd_delete(args):
    uid = args.uid
    status, res = make_request(f"api/dashboards/uid/{uid}", method="DELETE")
    if status == 200:
        print(f"検証用ダッシュボードを削除した: uid={uid}")
    elif status == 404:
        print(f"ダッシュボードが見つからない (既に削除済み): uid={uid}")
    else:
        print(f"削除失敗 (HTTP {status}): {res.get('message', res)}", file=sys.stderr)
        sys.exit(1)


def cmd_list(args):
    status, res = make_request("api/search?type=dash-db")
    if status != 200:
        print(f"検索失敗 (HTTP {status}): {res}", file=sys.stderr)
        sys.exit(1)

    print(f"{'UID':<25} {'TITLE':<35} {'URL'}")
    print("-" * 80)
    for d in res:
        uid = d.get("uid", "")
        title = d.get("title", "")
        url = d.get("url", "")
        if args.all or "-debug" in uid or "(検証)" in title:
            print(f"{uid:<25} {title:<35} {url}")


def main():
    parser = argparse.ArgumentParser(description="Grafana 一時検証用ダッシュボード CLI")
    subparsers = parser.add_subparsers(dest="command", required=True)

    # create
    p_create = subparsers.add_parser("create", help="一時検証用ダッシュボードを作成する")
    p_create.add_argument("file", help="ダッシュボード定義ファイル (JSON または ConfigMap YAML)")
    p_create.add_argument("--suffix", default="-debug", help="検証用 UID の接尾辞 (既定: -debug)")
    p_create.set_defaults(func=cmd_create)

    # delete
    p_delete = subparsers.add_parser("delete", help="指定した UID のダッシュボードを削除する")
    p_delete.add_argument("uid", help="削除対象の UID (例: nuage-wall-debug)")
    p_delete.set_defaults(func=cmd_delete)

    # list
    p_list = subparsers.add_parser("list", help="検証用ダッシュボードを一覧表示する")
    p_list.add_argument("--all", action="store_true", help="すべてのダッシュボードを表示する")
    p_list.set_defaults(func=cmd_list)

    args = parser.parse_args()
    args.func(args)


if __name__ == "__main__":
    main()
