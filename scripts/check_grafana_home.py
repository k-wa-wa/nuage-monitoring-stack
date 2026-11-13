#!/usr/bin/env python3
# /// script
# requires-python = ">=3.11"
# dependencies = ["PyYAML>=6.0,<7"]
# ///
"""Grafana ホーム・フォルダ・preview 除外の契約をレンダリング結果で検証する。

実行: uv run scripts/check_grafana_home.py
kustomize と helm を PATH に用意する。クラスタへの接続や書き込みは行わない。
"""

import configparser
from html.parser import HTMLParser
import json
from pathlib import Path, PurePosixPath
import subprocess

import yaml


ROOT = Path(__file__).resolve().parents[1]
LINK_UIDS = [
    "nuage-wall",
    "trivy-operator",
    "nuage-ai-agent-usage",
    "nuage-honeytrap-attack-map",
    "nuage-honeytrap",
    "nuage-node-health",
]


class Links(HTMLParser):
    def __init__(self):
        super().__init__()
        self.hrefs = []

    def handle_starttag(self, tag, attrs):
        if tag == "a":
            self.hrefs.append(dict(attrs).get("href"))


def require(condition, message):
    if not condition:
        raise ValueError(message)


def validate(documents, scope):
    def resource(kind, name):
        matches = [
            doc for doc in documents
            if doc["kind"] == kind and doc["metadata"]["name"] == name
        ]
        require(len(matches) == 1, f"{scope}: {kind}/{name} が一意に存在しない")
        return matches[0]

    dashboards = [
        doc for doc in documents
        if doc["kind"] == "ConfigMap"
        and doc["metadata"].get("labels", {}).get("grafana_dashboard") == "1"
    ]
    if scope == "preview":
        require(not dashboards, "preview に本番と重複するダッシュボードが存在する")
        return

    deployment = resource("Deployment", "vm-grafana")
    sidecar = next(
        container for container in deployment["spec"]["template"]["spec"]["containers"]
        if container["name"] == "grafana-sc-dashboard"
    )
    env = {item["name"]: item.get("value") for item in sidecar["env"]}
    annotation = env["FOLDER_ANNOTATION"]
    folder = PurePosixPath(env["FOLDER"])
    require(folder.is_absolute(), "sidecar の出力先が絶対パスではない")
    provider = yaml.safe_load(
        resource("ConfigMap", "vm-grafana-config-dashboards")["data"]["provider.yaml"]
    )["providers"]
    require(len(provider) == 1, "dashboard provider が一意ではない")
    options = provider[0]["options"]
    require(options["foldersFromFilesStructure"] is True, "フォルダ構造が無効である")
    require(PurePosixPath(options["path"]) == folder, "provider と sidecar の出力先が異なる")

    by_uid = {}
    paths = set()
    for cm in dashboards:
        target = cm["metadata"].get("annotations", {}).get(annotation)
        require(target == "nuage", f"{cm['metadata']['name']} の nuage 注釈が不足している")
        for filename, content in cm["data"].items():
            path = folder / target / filename
            require(path not in paths, f"ダッシュボードの出力先が重複する: {path}")
            paths.add(path)
            dashboard = json.loads(content)
            uid = dashboard["uid"]
            require(uid not in by_uid, f"UID が重複する: {uid}")
            by_uid[uid] = (dashboard, path)

    expected = set(LINK_UIDS) | {"nuage-home"}
    if scope == "base":
        expected.remove("trivy-operator")
    require(set(by_uid) == expected, f"{scope}: 自作ダッシュボードの UID 一覧が変わっている")
    home, home_path = by_uid["nuage-home"]
    config = configparser.ConfigParser(interpolation=None)
    config.read_string(resource("ConfigMap", "vm-grafana")["data"]["grafana.ini"])
    require(
        PurePosixPath(config["dashboards"]["default_home_dashboard_path"]) == home_path,
        "既定ホームのパスと sidecar が書くホームのパスが異なる",
    )
    links = Links()
    for panel in home["panels"]:
        require(panel["type"] == "text", "ホームに text 以外のパネルが存在する")
        require(panel["options"]["mode"] == "html", "リンク集が HTML ではない")
        links.feed(panel["options"]["content"])
    require(
        links.hrefs == [f"/grafana/d/{uid}" for uid in LINK_UIDS],
        "ホームの6リンクの UID・順序・サブパスが変わっている",
    )

    sync = yaml.safe_load(resource("ConfigMap", "vm-sync-job-config")["data"]["config.yaml"])
    standard_folder = sync["dashboards"]["common"]["annotations"][annotation]
    require(
        isinstance(standard_folder, str)
        and standard_folder not in ("", ".", "..", "nuage")
        and "/" not in standard_folder,
        "標準ダッシュボードの注釈が独立した単一フォルダではない",
    )


def main():
    for scope, path in [
        ("base", "k8s/base"),
        ("prod", "k8s/overlays/prod"),
        ("preview", "k8s/overlays/preview"),
    ]:
        result = subprocess.run(
            ["kustomize", "build", "--enable-helm", path],
            cwd=ROOT, check=True, stdout=subprocess.PIPE, text=True,
        )
        documents = [doc for doc in yaml.safe_load_all(result.stdout) if doc]
        validate(documents, scope)
        print(f"成功: {scope} のビルドと Grafana 構造検証")


if __name__ == "__main__":
    main()
