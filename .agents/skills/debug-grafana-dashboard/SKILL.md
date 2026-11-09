---
name: debug-grafana-dashboard
description: Grafana ダッシュボードの表示・レイアウト調整や動作確認を本番資材に反映する前に、Grafana API 経由で一時的な検証用ダッシュボードを作成・デバッグ・クリーンアップする手順。不要な Kubernetes リソースを残さず安全に検証する。
---

# Grafana ダッシュボード検証・デバッグスキル (debug-grafana-dashboard)

このスキルは、`nuage-monitoring-stack` において Grafana ダッシュボードのレイアウト変更やパネル追加を行う際、本番資材（ConfigMap や GitOps の master ブランチ）に反映する前に、Grafana API を使って一時的な検証用ダッシュボードを作成し、実機（ブラウザ・常設モニター）でデバッグおよびクリーンアップを行うためのガイドラインである。

---

## 1. 特徴とメリット

- **ゼロ・フットプリント**: Kubernetes の ConfigMap などをクラスタ上に残さず、Grafana API 経由のみで完結する。
- **即時反映**: sidecar の ConfigMap 同期待ちや GitOps のパイプラインを経由せず、数秒でブラウザ確認可能。
- **安全な切り離し**: UID に `-debug` 接尾辞を付与するため、本番のダッシュボード（`nuage-wall` など）を上書き・破壊しない。
- **1 コマンドクリーンアップ**: デバッグ完了後は delete コマンド 1 回で Grafana DB から完全に消去される。

---

## 2. 実行手順

ヘルパースクリプト `.agents/skills/debug-grafana-dashboard/scripts/grafana_debug.py` を使用する。
実行には環境変数 `GRAFANA_PASSWORD`（Grafana の管理者パスワード）の設定が必要である。

```bash
export GRAFANA_PASSWORD="<管理者パスワード>"
```

### Step 1: 検証用ダッシュボードの作成

手元の修正ファイル（ConfigMap YAML または JSON）を指定して作成する。
自動的に UID に `-debug`、タイトルに `(検証)` が付与される。

```bash
# ConfigMap YAML または生の JSON ファイルを指定
./.agents/skills/debug-grafana-dashboard/scripts/grafana_debug.py create <ファイルパス>
```

実行例:
```bash
./.agents/skills/debug-grafana-dashboard/scripts/grafana_debug.py create k8s/base/monitoring-pwa/grafana-dashboard-wall.yaml
```

出力された確認用 URL（通常表示、`hideLogo=true`、Kiosk モード、TV モード）をブラウザで開き、表示を確認する。

### Step 2: 実機・ブラウザでのデバッグ観点

1. **画面の収まり（スクロール有無）**:
   - 画面の拡大率 100% で縦スクロールバーが出ていないか。
   - 一般的な 1080p（1920x1080）画面やブラウザウィンドウで 1 ページに収まっているか。
2. **フッターや UI の重なり**:
   - Grafana 12.4+ / 13.x の固定ロゴフッター（"Powered by Grafana"）がパネルの上に被さっていないか。
   - `?hideLogo=true` を付与してロゴが非表示になるか。
3. **パネルの視認性**:
   - stat パネルの文字・数値が見切れていないか。
   - bargauge や timeseries パネルの凡例・ラベルが正常に描画されているか。

### Step 3: 一時ダッシュボードの一覧確認

現在作成されている検証用ダッシュボードを確認する。

```bash
./.agents/skills/debug-grafana-dashboard/scripts/grafana_debug.py list
```

### Step 4: クリーンアップ（ダッシュボードの削除）

デバッグ完了後、必ず検証用ダッシュボードを削除する。誤削除防止のため、`-debug` で終わらない UID は `--force` を指定しない限り拒否される。

```bash
./.agents/skills/debug-grafana-dashboard/scripts/grafana_debug.py delete <uid>
```

実行例:
```bash
./.agents/skills/debug-grafana-dashboard/scripts/grafana_debug.py delete nuage-wall-debug
```

---

## 3. 本番資材への本反映

検証用ダッシュボードでレイアウトや設定が確定したら、本番の ConfigMap マニフェスト（例: `k8s/base/monitoring-pwa/grafana-dashboard-wall.yaml`）に反映し、GitOps の手順に従って PR を作成する。
