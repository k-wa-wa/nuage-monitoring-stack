# SSVC 宣言ファイル

このディレクトリの `assets.yaml` と `overrides.yaml` の 2 つだけが人の編集対象である。`k8s/ssvc/` の他のファイル（スクリプトや CronJob 定義など）は評価の仕組みであり、普段は触らない。

変更は PR で行い、`master` に入ると翌日の CronJob（毎日 日本時間 5:00）から反映される。

## `assets.yaml`（資産の宣言）

k8s の Namespace・`repo-scan` のリポジトリ・未スキャン機器（VM・LXC・Proxmox 等）の重要度と露出を宣言する。

- **いつ足す・直すか**:
  - Security ダッシュボードの **SSVC: Unregistered Targets** に未登録の対象が出たとき
  - 新しい機器やアプリを増やしたとき、または役割・公開範囲が変わったとき
- **重要度 (`human_impact`) の目安と露出 (`exposure`) の規則**:
  - `assets.yaml` 先頭コメントの基準に従う。露出は Ingress の有無で自動判定されるため、自動判定と違う値にしたいときだけ `reason` 付きで上書きする。

## `overrides.yaml`（例外・人手判断）

CVE ごとに SSVC の判定を上書きする。

- **いつ書くか**:
  - Security ダッシュボードで「判定不可 (`unknown`)」になった CVE（Vulnrichment 欠損や GHSA 等）を人が判断したとき
  - 該当機能を使っていないなど、影響が無い (`not-affected`) と判断したとき
- **書き方の例**:

```yaml
overrides:
  - cve: CVE-2024-1234
    namespace: pechka        # k8s ワークロードは namespace + workload
    workload: api
    decision: defer          # defer / scheduled / out-of-cycle / immediate / not-affected
    reason: "該当機能を使用していない"
    expires: "2026-12-31"    # YYYY-MM-DD
  - cve: GHSA-xxxx-yyyy-zzzz
    repository: nix-config   # リポジトリは repository
    decision: not-affected
    reason: "ビルド専用ツールで実行環境に含まれない"
    expires: "2026-12-31"
```

- **期限切れの扱い**: `expires` を過ぎると例外は無効になり、通常の評価に戻る。期限切れの例外はダッシュボードの **SSVC: Expired Overrides** に表示される。
