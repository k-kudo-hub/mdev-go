#!/bin/bash
# waiting-toggle のゴールデン fixture を生成する。
#
# ⚠️ このスクリプトは claude-conductor のソースを必要とする。
#
# conductor をアーカイブした後は **再生成できない**。fixture は既にコミット
# されており、テストはそれを読むだけなので**動き続ける**(このスクリプトは
# 「その fixture がどう作られたか」の記録として残す)。
#
# 期待値を変えたくなった場合は、conductor の該当タグを取り出すか、fixture を
# 手で編集したうえで **何を根拠に変えたかをコミットに残す**こと。Shell 版と
# 一致していることが、これらの fixture の唯一の存在理由である。
#
# 現行 Shell 版(claude-conductor)の waiting-toggle.sh に同じ pending を
# 与えて走らせ、書き換わったファイルを
# cmd/mdev/testdata/golden-waiting-toggle/<case>/expected.json に保存する。
# 入力は同じ testdata の cases.json が定義し、Go 側のテストも同じ定義から
# pending を組み立てるため、両者は必ず同じ入力を見る。
#
# time は Shell が date コマンドで決めるため差し替えられない。生成時の値を
# time.txt に記録しておき、Go 側のテストは同じ文字列を渡して突き合わせる。
#
# 使い方:
#   scripts/gen-golden-waiting-toggle.sh [claude-conductor のパス]
#
# 既定のパスは ../claude-conductor。HOME は一時ディレクトリへ向けるため、
# 実環境のファイルには一切触れない。
set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
CONDUCTOR_SRC="${1:-$(cd "$REPO_ROOT/.." && pwd)/claude-conductor}"
GOLDEN_DIR="$REPO_ROOT/cmd/mdev/testdata/golden-waiting-toggle"
CASES_FILE="$GOLDEN_DIR/cases.json"

for required in jq bash; do
    command -v "$required" >/dev/null 2>&1 || { echo "$required が必要です" >&2; exit 1; }
done
TOGGLE="$CONDUCTOR_SRC/scripts/waiting-toggle.sh"
[ -f "$TOGGLE" ] || { echo "現行スクリプトが見つかりません: $TOGGLE" >&2; exit 1; }
[ -f "$CASES_FILE" ] || { echo "cases.json が見つかりません: $CASES_FILE" >&2; exit 1; }

WORK="$(mktemp -d)"
trap 'rm -rf "$WORK"' EXIT

SESSION="golden"

# 1 件の case を実行して expected.json と time.txt を書く。
run_case() {
    local case_json="$1"
    local name tab input
    name=$(printf '%s' "$case_json" | jq -r '.name')
    tab=$(printf '%s' "$case_json" | jq -r '.tab')
    input=$(printf '%s' "$case_json" | jq -r '.input')

    local sandbox="$WORK/run/$name"
    rm -rf "$sandbox"
    mkdir -p "$sandbox/.claude-pending/$SESSION"
    printf '%s' "$input" > "$sandbox/.claude-pending/$SESSION/a.json"

    # 環境は env -i で遮断し、必要なものだけ渡す。
    env -i \
        PATH="/usr/bin:/bin:/usr/sbin:/sbin:/opt/homebrew/bin" \
        HOME="$sandbox" \
        ZELLIJ_SESSION_NAME="$SESSION" \
        bash "$TOGGLE" "$tab"

    local out_dir="$GOLDEN_DIR/$name"
    mkdir -p "$out_dir"
    cp "$sandbox/.claude-pending/$SESSION/a.json" "$out_dir/expected.json"
    # Shell が date で決めた時刻。Go 側はこれを渡して突き合わせる。
    jq -r '.time' "$out_dir/expected.json" > "$out_dir/time.txt"
}

count=0
while IFS= read -r case_json; do
    run_case "$case_json"
    echo "生成: $(printf '%s' "$case_json" | jq -r '.name')"
    count=$((count + 1))
done < <(jq -c '.[]' "$CASES_FILE")

echo "$count 件の fixture を $GOLDEN_DIR に生成しました"
