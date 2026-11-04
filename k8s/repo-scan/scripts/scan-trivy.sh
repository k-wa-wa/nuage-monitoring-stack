#!/bin/sh
# クローンできた各リポジトリに trivy fs をかける (trivy のイメージで動かす)。
# 依存ライブラリの脆弱性 (vuln)・IaC の設定ミス (misconfig)・秘密情報の混入 (secret) を見る。
#
# 1 つのリポジトリが失敗しても残りを続ける。結果を /work/status/<repo>/trivy.exit と trivy.log に残し、
# aggregate.py が失敗として扱う。脆弱性 DB と設定チェックは最初のリポジトリで取得し、/work/cache/trivy を使い回す。
set -u

WORK=${WORK:-/work}

for status in "$WORK"/status/*/; do
  name=$(basename "$status")
  if [ "$(cat "$status/clone.exit" 2>/dev/null)" != "0" ]; then
    echo "trivy を飛ばす (クローンに失敗している): $name" >&2
    continue
  fi
  mkdir -p "$WORK/out/$name"
  trivy fs \
    --quiet \
    --cache-dir "$WORK/cache/trivy" \
    --timeout 15m \
    --scanners vuln,misconfig,secret \
    --format json \
    --output "$WORK/out/$name/trivy.json" \
    "$WORK/repos/$name" 2>"$status/trivy.log"
  code=$?
  echo "$code" >"$status/trivy.exit"
  if [ "$code" -eq 0 ]; then
    echo "trivy でスキャンした: $name"
  else
    echo "trivy のスキャンに失敗した: $name (終了コード $code)" >&2
    cat "$status/trivy.log" >&2
  fi
done
