# Honeytrap ダッシュボードの絞り込みと分類

`k8s/base/monitoring-pwa/grafana-dashboard-honeypot.yaml` が Attack Map & Analytics の管理元である。ConfigMap を GitOps 経由で反映すると、Grafana sidecar が定義を読み込む。

ダッシュボード最上部の **LogsQL フィルター** を編集して Enter を押すと、分類件数・地図・国別ランキング・ASN ランキング・時系列・一覧のすべてに適用する。変更値は `var-logsql` として URL に入り、同じ条件を共有できる。

| 条件 | 入力例 |
| --- | --- |
| 既定（探索・侵入／悪用試行） | `traffic_class:in("probe", "attack")` |
| 判定保留・運用通信を含む全件 | `*` |
| 判定保留だけ | `traffic_class:="unknown"` |
| HTTP の探索 | `protocol:="http" traffic_class:="probe"` |
| 送信元を限定 | `src_ip:="203.0.113.10"` |
| ドメインを限定 | `domain:="decoy.example.com"` |
| 国を限定 | `details.country:="JP"` |

入力するのは LogsQL のフィルター式であり、`stats`・`fields`・`limit` などのパイプは各パネル側で付加する。空欄ではなく `*` で全件を指定する。対象は `nuage-honeytrap` Namespace の `honeypot_attack` イベントに固定する。期間は Grafana の時間選択に従う。

## 分類と判定理由

分類は既存ログから検索時に生成する。アプリの変更やログの再取り込みは不要である。

| `traffic_class` | 表示 | 主な条件 |
| --- | --- | --- |
| `operational` | 緑・既知の監視・試験 | 第2入力欄の確認済み条件に一致 |
| `unknown` | 灰・判定保留 | 一般的な HTTP 取得、SSH 認証前の切断、PostgreSQL 初期接続の失敗、分類根拠が不足した通信 |
| `probe` | 橙・探索 | 秘密情報・設定・バックアップ・管理画面へのアクセス、SSH `none`、ユーザー・DB 名を指定した PostgreSQL 接続 |
| `attack` | 赤・侵入／悪用試行 | SSH パスワード・公開鍵認証要求、囮ログインへの POST、パストラバーサル・SQLi・XSS・コマンド実行を疑う文字列 |

分類は観測した操作の目安であり、攻撃意図や成功を断定しない。PostgreSQL は StartupMessage の後に拒否する実装であるため、ユーザー名を受け取っただけの接続をパスワード試行とは扱わない。HTTP `/` や `/robots.txt` だけの取得を安全とも攻撃とも断定しない。国・ASN・User-Agent は自動分類の安全判定に使用しない。文字列ルールは網羅的なペイロード検知器ではなく、未分類の悪用試行が判定保留に残る場合がある。

一覧には分類に加えて `classification_reason`、HTTP メソッド、クエリ、User-Agent を表示する。判定は元の本文・クエリで行い、その後に一覧表示用のマスクを適用する。囮ログイン本文や認証情報のキー名を含む本文・クエリは全体をマスクする。任意形式の秘密情報を完全に検出する保証はないため、詳細ログの共有時は内容を確認する。

## 確認済みの監視・試験

第2入力欄は既定で `NOT traffic_class:*` とし、運用通信を自動認定しない。自分で確認した試験・監視の送信元、ドメイン、期間などを組み合わせた条件だけを指定する。たとえば次の条件で既知の監視元を指定する。

```text
src_ip:="192.0.2.25" domain:="decoy.example.com" details.path:="/healthz"
```

この条件は分類ルールの後に適用するため、自分の侵入テストも明示的に運用通信に分類できる。運用通信を見るには第1入力欄を `*` または `traffic_class:="operational"` に変更する。User-Agent やプライベート IP だけで運用通信と判断しない。既存の kubelet ヘルスチェックはアプリでログ出力を省略しており、ダッシュボードにも現れない。

## パネルの集計範囲

地図・国別ランキングは国情報付きの Cloudflare 経由 HTTP に限る。SSH・PostgreSQL・国情報のない通信も、他のパネルには含まれる。一覧は最新1000件であり、集計パネルは選択期間の全対象を集計する。0件の分類は系列が返らず、分類件数パネルには表示されない。

## 検証

```bash
python scripts/check_honeytrap_dashboard.py
kubectl kustomize k8s/base/monitoring-pwa > /tmp/monitoring-pwa.yaml
```

LogsQL エンジンに対して分類・マスク・パネルクエリを検証する場合は、読み取り専用の検索 URL を指定する。

```bash
python scripts/check_honeytrap_dashboard.py \
  --query-url https://monitoring.wpcapp.net/grafana/api/datasources/proxy/uid/VictoriaLogs/select/logsql/query
```

検証データは検索の `union rows(...)` 内にだけ生成し、ログの挿入やダッシュボードの更新は行わない。
