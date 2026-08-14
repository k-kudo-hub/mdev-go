#!/bin/bash
# codex notify のゴールデンテスト用 fixture を生成する。
#
# 現行 Shell 版(claude-conductor)の codex-notify.sh に
# internal/infra/store/testdata/golden-codex/cases.json が定義する入力
# (payload の JSON・環境変数・実行前から置いてある pending・会話ログ)を
# 与え、書き換わった pending とタスクレジストリを
# testdata/golden-codex/<case>/expected/ に保存する。
#
# 使い方:
#   scripts/gen-golden-codex.sh [claude-conductor のパス]
#
# 既定のパスは ../claude-conductor。HOME・CONDUCTOR_HOME・CODEX_HOME はすべて
# 一時ディレクトリへ向け、env -i で環境も切るため、実環境のファイルには
# 一切触れない(隔離のしかたは scripts/gen-golden-record.sh と同じ)。
#
# 会話ログの絶対パスは実行のたびに変わるので、保存時に {{CODEX_HOME}} へ
# 置き換える(Go 側も同じ置換を行う)。
#
# 各 case には必ず expected/files.txt を書く。現行版が 1 つもファイルを書かない
# case(ターン完了以外の通知など)では expected/ が空になり、git は空ディレクトリを
# 追跡しないため、fixture が手元にしか存在しない状態になる。空の一覧という
# 「何も書かなかった」を表すファイルを置くことで、その期待値も git に載る。
set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
CONDUCTOR_SRC="${1:-$(cd "$REPO_ROOT/.." && pwd)/claude-conductor}"
GOLDEN_DIR="$REPO_ROOT/internal/infra/store/testdata/golden-codex"
CASES_FILE="$GOLDEN_DIR/cases.json"

# 会話ログのパスに差し込むプレースホルダ。Go 側のゴールデンテストと同じ文字列。
PLACEHOLDER='{{CODEX_HOME}}'

for required in jq bash; do
    command -v "$required" >/dev/null 2>&1 || { echo "$required が必要です" >&2; exit 1; }
done
NOTIFY="$CONDUCTOR_SRC/scripts/codex-notify.sh"
[ -f "$NOTIFY" ] || { echo "現行スクリプトが見つかりません: $NOTIFY" >&2; exit 1; }
[ -f "$CASES_FILE" ] || { echo "cases.json が見つかりません: $CASES_FILE" >&2; exit 1; }

WORK="$(mktemp -d)"
trap 'rm -rf "$WORK"' EXIT

# replace_in_file <パス> <置換前> <置換後>
replace_in_file() {
    local path="$1" from="$2" to="$3"
    python3 - "$path" "$from" "$to" <<'PY'
import sys
path, old, new = sys.argv[1], sys.argv[2], sys.argv[3]
with open(path, encoding="utf-8") as f:
    body = f.read()
with open(path, "w", encoding="utf-8") as f:
    f.write(body.replace(old, new))
PY
}

# run_case <case json> <出力先>
run_case() {
    local case_json="$1" out_dir="$2"
    local sandbox="$WORK/sandbox"
    rm -rf "$sandbox" "$out_dir"
    mkdir -p "$sandbox/home" "$sandbox/conductor" "$sandbox/codex"

    # 現行版は registry-lib.sh を CONDUCTOR_HOME/scripts から読むため、
    # sandbox 側へ実物を写す。
    mkdir -p "$sandbox/conductor/scripts"
    cp "$CONDUCTOR_SRC/scripts/registry-lib.sh" "$sandbox/conductor/scripts/"

    # 実行前から置いてある pending。
    while IFS= read -r rel; do
        [ -n "$rel" ] || continue
        mkdir -p "$(dirname "$sandbox/home/.claude-pending/$rel")"
        printf '%s' "$case_json" | jq -rj --arg k "$rel" '.pending[$k]' \
            > "$sandbox/home/.claude-pending/$rel"
    done < <(printf '%s' "$case_json" | jq -r '(.pending // {}) | keys[]')

    # 会話ログ。
    while IFS= read -r rel; do
        [ -n "$rel" ] || continue
        mkdir -p "$(dirname "$sandbox/codex/sessions/$rel")"
        printf '%s' "$case_json" | jq -rj --arg k "$rel" '.rollouts[$k]' \
            > "$sandbox/codex/sessions/$rel"
    done < <(printf '%s' "$case_json" | jq -r '(.rollouts // {}) | keys[]')

    # 環境変数は case が指定したものだけを渡す(env -i で他は切る)。
    local -a env_args=()
    while IFS= read -r pair; do
        [ -n "$pair" ] || continue
        env_args+=("$pair")
    done < <(printf '%s' "$case_json" | jq -r '(.env // {}) | to_entries[] | "\(.key)=\(.value)"')

    local payload
    payload=$(printf '%s' "$case_json" | jq -c '.payload')

    env -i \
        PATH="/usr/bin:/bin:/usr/sbin:/sbin:/opt/homebrew/bin" \
        HOME="$sandbox/home" \
        CONDUCTOR_HOME="$sandbox/conductor" \
        CODEX_HOME="$sandbox/codex" \
        "${env_args[@]}" \
        bash "$NOTIFY" "$payload"

    # 書き換わった pending とレジストリを保存する。
    mkdir -p "$out_dir"
    for src in "$sandbox/home/.claude-pending:pending" "$sandbox/conductor/tasks:registry"; do
        local from="${src%%:*}" name="${src##*:}"
        [ -d "$from" ] || continue
        cp -R "$from" "$out_dir/$name"
        while IFS= read -r -d '' f; do
            replace_in_file "$f" "$sandbox/codex" "$PLACEHOLDER"
        done < <(find "$out_dir/$name" -type f -print0)
    done

    # 書かれたファイルの一覧。何も書かれなかった case では空になる。
    (cd "$out_dir" && find . -type f -not -name files.txt \
        | sed 's|^\./||' | LC_ALL=C sort) > "$out_dir/files.txt"
}

count=0
while IFS= read -r case_json; do
    name=$(printf '%s' "$case_json" | jq -r '.name')
    run_case "$case_json" "$GOLDEN_DIR/$name/expected"
    echo "生成: $name"
    count=$((count + 1))
done < <(jq -c '.[]' "$CASES_FILE")

echo "$count 件の fixture を $GOLDEN_DIR に生成しました"
