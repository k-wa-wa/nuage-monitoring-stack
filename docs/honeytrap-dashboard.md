# Honeytrap ダッシュボードの絞り込みと分類

`k8s/base/monitoring-pwa/grafana-dashboard-honeypot.yaml` が Attack Map & Analytics の管理元である。ConfigMap を GitOps 経由で反映すると、Grafana sidecar が定義を読み込む。

ダッシュボード最上部の **分類** で対象を複数選択し、必要なら **追加の検索条件 (LogsQL)** を入力して Enter を押す。分類件数・地図・国別ランキング・ASN ランキング・時系列・一覧のすべてに、両方の条件を適用する。初期値は分類が All（すべて）、検索欄が空欄である。検索欄が空欄・空白だけなら追加条件なしとなり、分類が All なら対象ログを全件表示する。変更値は `var-traffic_class` と `var-logsql` として URL に入り、同じ条件を共有できる。

「探索」「侵入・悪用試行」「判定保留」「既知の監視・試験」は分類ボックスから選択する。絞るときは All のチェックを外して対象を選ぶ。複数の分類を選んだ場合はいずれかに一致する通信を表示する。検索欄では、たとえば次の条件を指定する。

| 条件 | 入力例 |
| --- | --- |
| 追加条件なし | 空欄、または `*` |
| HTTP に限定 | `protocol:="http"` |
| 送信元を限定 | `src_ip:="203.0.113.10"` |
| ドメインを限定 | `domain:="decoy.example.com"` |
| 国を限定 | `details.country:="JP"` |
| パスが `/.git` から始まる | `details.path:="/.git"*` |
| パスが完全一致 | `details.path:="/.git/config"` |
| 複数のパスを対象にする | `details.path:="/.git"* OR details.path:="/.env"*` |
| HTTP かつパスを限定 | `protocol:="http" AND details.path:="/.git"*` |
| 確認済みの監視・試験の送信元を除外 | `NOT src_ip:="192.0.2.25"` |
| 正規表現（`/.git` 自体と配下だけ） | `details.path:~"^/[.]git(/\|$)"` |
| User-Agent に bot を含む（大小文字を区別しない） | `details.user_agent:~"(?i)bot"` |

パス・クエリ・メソッド・User-Agent・送信元 IP・国・ASN・ドメインなど、記録されたフィールドを条件にできる。`AND`・`OR`・`NOT` と括弧で組み合わせる。前方一致は文字列全体の先頭を見るため、`details.path:="/.git"*` は `/.github` にも一致する。`/.git` ディレクトリだけを対象にする場合は上表の正規表現、または `details.path:="/.git" OR details.path:="/.git/"*` を使用する。通常の `details.path:"/.git"` は単語・フレーズの検索であり、前方一致とは異なる。

入力するのは LogsQL のフィルター式であり、`stats`・`fields`・`limit` などのパイプは各パネル側で付加する。対象は `nuage-honeytrap` Namespace の `honeypot_attack` イベントに固定する。期間は Grafana の時間選択に従う。不正な検索式は検索エラーとし、黙って全件表示にはしない。

## 分類と判定理由

分類は既存ログから検索時に生成する。アプリの変更やログの再取り込みは不要である。

| `traffic_class` | 表示 | 主な条件 |
| --- | --- | --- |
| `operational` | 緑・既知の監視・試験 | 管理用の固定条件に一致（既定は未登録） |
| `unknown` | 灰・判定保留 | HTTP は `/`・`/robots.txt`・`/favicon.ico` の GET/HEAD の通常取得のみ。SSH 認証前の切断、PostgreSQL 初期接続の失敗なども含む |
| `probe` | 橙・探索 | 上記3パスの GET/HEAD 以外の HTTP 通信、SSH `none`、ユーザー・DB 名を指定した PostgreSQL 接続 |
| `attack` | 赤・侵入／悪用試行 | SSH パスワード・公開鍵認証要求、囮ログインへの POST、パストラバーサル・SQLi・XSS・コマンド実行を疑う文字列 |

分類は観測した操作の目安であり、攻撃意図や成功を断定しない。HTTP の探索パスは列挙方式にせず、`/AGENTS.md` や未登録のパスも原則として探索に含める。`/index.html`・`/index.php`・外部からの `/healthz` も探索となる。通常3パスであっても、認証要求・攻撃文字列を検出した場合は侵入・悪用試行を優先する。確認済みの運用条件はその後に適用する。PostgreSQL は StartupMessage の後に拒否する実装であるため、ユーザー名を受け取っただけの接続をパスワード試行とは扱わない。国・ASN・User-Agent は自動分類の安全判定に使用しない。

一覧には分類に加えて `classification_reason`、HTTP メソッド、クエリ、User-Agent を表示する。判定は元の本文・クエリで行い、その後に一覧表示用のマスクを適用する。囮ログイン本文や認証情報のキー名を含む本文・クエリは全体をマスクする。任意形式の秘密情報を完全に検出する保証はないため、詳細ログの共有時は内容を確認する。

## 確認済みの監視・試験

確認済みの監視・試験を表示から除外するときは、検索欄に `NOT` 条件を追加する。たとえば分類ボックスで探索・侵入／悪用試行を選択し、確認済みの送信元・ドメイン・パスを除外する条件は次のとおりである。

```text
NOT (src_ip:="192.0.2.25" AND domain:="decoy.example.com" AND details.path:="/healthz")
```

継続的に運用通信へ分類する条件は、非表示の定数変数 `known_traffic` に設定する。既定値は `NOT traffic_class:*` とし、自動認定は行わない。これは表示の絞り込みとは独立した管理用設定であり、URL に同期しない。設定済みの運用通信は分類ボックスで「既知の監視・試験」を選択すると表示できる。User-Agent やプライベート IP だけで運用通信と判断しない。既存の kubelet ヘルスチェックはアプリでログ出力を省略しており、ダッシュボードにも現れない。

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

検索欄の正規化は、入力に依存する非表示のクエリ変数 `effective_logsql` で行う。`limit 0` と `union rows(...)` で入力文字列だけを評価し、ログ内容は参照しない。引用符・バックスラッシュを保持するため、入力を URL エンコードして渡し、LogsQL の `urldecode` で復号する。空欄を `coalesce`、空白だけの値を正規表現で `*` に変換し、それ以外の式はそのまま各パネルに渡す。
