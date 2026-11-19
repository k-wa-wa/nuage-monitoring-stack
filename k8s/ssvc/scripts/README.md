# SSVC 評価スクリプトと判定表

`evaluate.py` は CERT/CC SSVC Deployer 判定表をもとに k8s とリポジトリの脆弱性を評価する。

## 判定表の運用

- `deployer_patch_application_priority_1_0_0.csv`: CERT/CC 上流の元の判定表である。上流との同一性を保つため変更しない。
- `deployer_patch_application_priority_nuage.csv`: 自宅クラスター向けに調整した判定表であり、本番 CronJob (`k8s/ssvc/cronjob.yaml`) はこちらを使用する。

### 上流からの変更箇所（5 行）

`Exploitation = active`（悪用が確認済み）かつ `System Exposure = open`（外から届く、または Ingress 公開）の組を、`Human Impact` に関わらず `immediate` に引き上げている。もともと `immediate` である行（67・70・71）および `controlled`・`small` の行は変更していない。

| row | Exploitation | Exposure | Automatable | Human Impact | 元の判定 | 変更後 |
|---|---|---|---|---|---|---|
| 64 | active | open | no | low | scheduled | immediate |
| 65 | active | open | no | medium | out-of-cycle | immediate |
| 66 | active | open | no | high | out-of-cycle | immediate |
| 68 | active | open | yes | low | out-of-cycle | immediate |
| 69 | active | open | yes | medium | out-of-cycle | immediate |

- **理由**: 悪用が確認されており外から届く脆弱性は、対象サービスの重要度に関わらず直ちに対処する方が自宅クラスターの運用に合う。元の表では悪用が確認済みの中重要度（`medium`）と、悪用報告の無い最高重要度（`very high`）が同じ `out-of-cycle` に並び区別できなかった。
- **ログへの記録**: 変更した 5 行で決まった評価結果は、`ssvc_finding` ログの `decision_source` を `ssvc_table_custom` として出力し、元の表の行で決まったもの（`ssvc_table`）と区別する。
