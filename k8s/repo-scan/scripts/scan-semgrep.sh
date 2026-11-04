#!/bin/sh
# クローンできた各リポジトリに semgrep をかける (semgrep のイメージで動かす)。コードの静的解析を見る。
# ルールは p/default (semgrep.dev のレジストリから毎回取得する)。--metrics=off で利用状況を送らない。
#
# 1 つのリポジトリが失敗しても残りを続ける。結果を /work/status/<repo>/semgrep.exit と semgrep.log に残し、
# aggregate.py が失敗として扱う。
# リポジトリの中に入って "." を対象にするため、結果のパスはリポジトリからの相対パスになる。
set -u

WORK=${WORK:-/work}

for status in "$WORK"/status/*/; do
  name=$(basename "$status")
  if [ "$(cat "$status/clone.exit" 2>/dev/null)" != "0" ]; then
    echo "semgrep を飛ばす (クローンに失敗している): $name" >&2
    continue
  fi
  mkdir -p "$WORK/out/$name"
  (
    cd "$WORK/repos/$name" &&
      semgrep scan \
        --config p/default \
        --metrics=off \
        --disable-version-check \
        --quiet \
        --json \
        --output "$WORK/out/$name/semgrep.json" \
        .
  ) 2>"$status/semgrep.log"
  code=$?
  echo "$code" >"$status/semgrep.exit"
  if [ "$code" -eq 0 ]; then
    echo "semgrep でスキャンした: $name"
  else
    echo "semgrep のスキャンに失敗した: $name (終了コード $code)" >&2
    cat "$status/semgrep.log" >&2
  fi
done
