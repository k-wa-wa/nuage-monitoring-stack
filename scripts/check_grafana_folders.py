#!/usr/bin/env python3
# /// script
# requires-python = ">=3.11"
# dependencies = ["PyYAML>=6.0,<7"]
# ///
"""Grafana フォルダ・標準ホーム・preview 除外の契約をレンダリング結果で検証する。

実行: uv run scripts/check_grafana_folders.py
kustomize と helm を PATH に用意する。クラスタへの接続や書き込みは行わない。
"""

import configparser
import json
from pathlib import Path, PurePosixPath
import subprocess

import yaml


ROOT = Path(__file__).resolve().parents[1]
CUSTOM_UIDS = [
    "nuage-wall",
    "trivy-operator",
    "nuage-ai-agent-usage",
    "nuage-honeytrap-attack-map",
    "nuage-honeytrap",
    "nuage-node-health",
]


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

    expected = set(CUSTOM_UIDS)
    if scope == "base":
        expected.remove("trivy-operator")
    require(set(by_uid) == expected, f"{scope}: 自作ダッシュボードの UID 一覧が変わっている")
    config = configparser.ConfigParser(interpolation=None)
    config.read_string(resource("ConfigMap", "vm-grafana")["data"]["grafana.ini"])
    require(
        not config.has_option("dashboards", "default_home_dashboard_path"),
        "標準ホームを上書きする設定が存在する",
    )
    grafana = next(
        container for container in deployment["spec"]["template"]["spec"]["containers"]
        if container["name"] == "grafana"
    )
    require(
        not any(item["name"] == "GF_DASHBOARDS_DEFAULT_HOME_DASHBOARD_PATH"
                for item in grafana.get("env", [])),
        "標準ホームを上書きする環境変数が存在する",
    )

    sync = yaml.safe_load(resource("ConfigMap", "vm-sync-job-config")["data"]["config.yaml"])
    standard_folder = sync["dashboards"]["common"]["annotations"][annotation]
    require(
        standard_folder == "標準",
        "チャート既定ダッシュボードのフォルダ注釈が標準ではない",
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
