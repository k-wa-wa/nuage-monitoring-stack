#!/bin/sh
# 対象の各リポジトリを浅くクローンし、コミットの SHA を控える (alpine/git のイメージで動かす)。
#
# 1 つのリポジトリが失敗しても残りを続ける。失敗は隠さない:
# 結果を /work/status/<repo>/clone.exit (終了コード) と clone.log (エラー出力) に残し、
# 最後の aggregate.py がそのリポジトリを失敗として扱って Job 全体を失敗で終える。
# 対象の一覧そのものが読めない・形が正しくないときは、ここで止める (以降のスキャンは意味が無いため)。
set -u

TARGETS=${TARGETS:-/etc/repo-scan/targets/repos.txt}
WORK=${WORK:-/work}

targets=$(grep -v -e '^[[:space:]]*#' -e '^[[:space:]]*$' "$TARGETS") || {
  echo "対象のリポジトリが読めない、または 1 つも無い: $TARGETS" >&2
  exit 1
}

for full in $targets; do
  if ! echo "$full" | grep -Eq '^[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+$'; then
    echo "対象の書き方が <owner>/<repo> になっていない: $full" >&2
    exit 1
  fi
done

mkdir -p "$WORK/repos" "$WORK/status"
for full in $targets; do
  name=${full#*/}
  status=$WORK/status/$name
  mkdir -p "$status"
  git clone --quiet --depth 1 --no-tags "https://github.com/$full.git" "$WORK/repos/$name" 2>"$status/clone.log"
  code=$?
  if [ "$code" -eq 0 ]; then
    git -C "$WORK/repos/$name" rev-parse HEAD >"$status/commit" 2>>"$status/clone.log"
    code=$?
  fi
  echo "$code" >"$status/clone.exit"
  if [ "$code" -eq 0 ]; then
    echo "クローンした: $full $(cat "$status/commit")"
  else
    echo "クローンに失敗した: $full (終了コード $code)" >&2
    cat "$status/clone.log" >&2
  fi
done
