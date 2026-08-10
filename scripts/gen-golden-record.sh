#!/bin/bash
# record-output のゴールデンテスト用 fixture を生成する。
#
# 現行 Shell 版(claude-conductor)の record-output.sh に
# internal/infra/store/testdata/golden-record/cases.json が定義する入力
# (引数のタブ名・環境変数・設定・pending・transcript)を与え、追記された
# daily log を testdata/golden-record/<case>/expected/daily/ に保存する。
#
# 使い方:
#   scripts/gen-golden-record.sh [claude-conductor のパス]
#
# 既定のパスは ../claude-conductor。HOME と CONDUCTOR_HOME は一時ディレクトリへ
# 向けるため、実環境のファイルには一切触れない。hook 側の生成は
# scripts/gen-golden.sh にあり、隔離のしかたはそちらと同じである。
#
# transcript_path は実行のたびに変わる絶対パスなので、pending には
# {{TRANSCRIPT_DIR}} というプレースホルダを書いておき、実行直前に実パスへ
# 置換する。保存時は逆向きに置換して戻す(Go 側も同じ置換を行う)。
#
# case に "runs": N を書くと、同じ sandbox のまま record-output.sh を N 回続けて
# 走らせる。アップロードの失敗でタスク削除が中止され、同じ pending に対して
# record が何度も走る状況(daily の重複を置換で防ぐ挙動)を再現するためである。
set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
CONDUCTOR_SRC="${1:-$(cd "$REPO_ROOT/.." && pwd)/claude-conductor}"
GOLDEN_DIR="$REPO_ROOT/internal/infra/store/testdata/golden-record"
CASES_FILE="$GOLDEN_DIR/cases.json"

# pending に書くプレースホルダ。Go 側のゴールデンテストと同じ文字列。
PLACEHOLDER='{{TRANSCRIPT_DIR}}'

for required in jq bash; do
    command -v "$required" >/dev/null 2>&1 || { echo "$required が必要です" >&2; exit 1; }
done
[ -f "$CONDUCTOR_SRC/scripts/record-output.sh" ] || { echo "現行スクリプトが見つかりません: $CONDUCTOR_SRC/scripts" >&2; exit 1; }
[ -f "$CASES_FILE" ] || { echo "cases.json が見つかりません: $CASES_FILE" >&2; exit 1; }

WORK="$(mktemp -d)"
trap 'rm -rf "$WORK"' EXIT

# has_field <case json> <キー> : そのキーに文字列が入っているかを返す。
has_field() {
    [ "$(printf '%s' "$1" | jq -r --arg k "$2" 'has($k) and (.[$k] | type == "string")')" = "true" ]
}

# field <case json> <jq フィルタ> : 文字列フィールドを取り出す(無ければ空)。
field() {
    printf '%s' "$1" | jq -r "$2 // empty"
}

# write_field <case json> <jq フィルタ> <出力パス> [jq の追加引数...]
# jq -j を使い、末尾の改行を含めて元の文字列をそのまま書き出す。
write_field() {
    local case_json="$1" filter="$2" path="$3"
    shift 3
    mkdir -p "$(dirname "$path")"
    printf '%s' "$case_json" | jq -rj "$@" "$filter" > "$path"
}

# replace_in_file <パス> <置換前> <置換後>
replace_in_file() {
    local tmp="$1.tmp"
    sed "s|$2|$3|g" "$1" > "$tmp"
    mv "$tmp" "$1"
}

# run_case <case json> <出力先ディレクトリ>
run_case() {
    local case_json="$1" out_dir="$2"
    local name tab session sandbox transcripts_dir runs run

    name=$(field "$case_json" '.name')
    tab=$(field "$case_json" '.tab')
    session=$(field "$case_json" '.env.ZELLIJ_SESSION_NAME')
    session="${session:-unknown}"
    runs=$(printf '%s' "$case_json" | jq -r '.runs // 1')

    sandbox="$WORK/run/$name"
    rm -rf "$sandbox"
    mkdir -p "$sandbox/home" "$sandbox/conductor"
    # record-output.sh は $CONDUCTOR_HOME/scripts/lock-lib.sh を source する。
    cp -R "$CONDUCTOR_SRC/scripts" "$sandbox/conductor/scripts"

    # 設定ファイル(どちらも任意)。
    if has_field "$case_json" config; then
        write_field "$case_json" '.config' "$sandbox/conductor/config.json"
    fi
    if has_field "$case_json" config_default; then
        write_field "$case_json" '.config_default' "$sandbox/conductor/config.default.json"
    fi

    # transcript を置く。
    transcripts_dir="$sandbox/transcripts"
    mkdir -p "$transcripts_dir"
    while IFS= read -r rel; do
        [ -n "$rel" ] || continue
        write_field "$case_json" '.transcripts[$k]' "$transcripts_dir/$rel" --arg k "$rel"
    done < <(printf '%s' "$case_json" | jq -r '(.transcripts // {}) | keys[]')

    # pending を置く(プレースホルダを実パスへ置換する)。
    while IFS= read -r rel; do
        [ -n "$rel" ] || continue
        local dest="$sandbox/home/.claude-pending/$rel"
        write_field "$case_json" '.pending[$k]' "$dest" --arg k "$rel"
        replace_in_file "$dest" "$PLACEHOLDER" "$transcripts_dir"
    done < <(printf '%s' "$case_json" | jq -r '(.pending // {}) | keys[]')

    # 実行前から存在する daily ファイル(追記されることの確認用)。
    if has_field "$case_json" existing_daily; then
        write_field "$case_json" '.existing_daily' \
            "$sandbox/conductor/daily/$session/$(date '+%Y-%m-%d').jsonl"
    fi

    # 環境変数は case が定義するものだけを渡す(env -i で他を遮断する)。
    local -a env_args=()
    while IFS= read -r kv; do
        [ -n "$kv" ] || continue
        env_args+=("$kv")
    done < <(printf '%s' "$case_json" | jq -r '.env | to_entries[] | "\(.key)=\(.value)"')

    # runs 回続けて走らせる(pending も daily も持ち越す = 再試行そのもの)。
    for ((run = 0; run < runs; run++)); do
        env -i \
            PATH="/usr/bin:/bin:/usr/sbin:/sbin:/opt/homebrew/bin" \
            HOME="$sandbox/home" \
            CONDUCTOR_HOME="$sandbox/conductor" \
            ${env_args[@]+"${env_args[@]}"} \
            bash "$sandbox/conductor/scripts/record-output.sh" "$tab"
    done

    # 生成物を集める(実パスをプレースホルダへ戻す)。
    rm -rf "$out_dir"
    mkdir -p "$out_dir/daily"
    # 何も追記されない case では daily が空になる。git は空ディレクトリを
    # 追跡しないため、目印を置いて「fixture が未生成」と区別できるようにする。
    touch "$out_dir/daily/.gitkeep"
    if [ -d "$sandbox/conductor/daily" ]; then
        while IFS= read -r -d '' f; do
            local rel dest
            rel="${f#"$sandbox/conductor/daily/"}"
            dest="$out_dir/daily/$rel"
            mkdir -p "$(dirname "$dest")"
            cp "$f" "$dest"
            replace_in_file "$dest" "$transcripts_dir" "$PLACEHOLDER"
        done < <(find "$sandbox/conductor/daily" -name '*.jsonl' -type f -print0)
    fi
}

# daily ファイル名の日付と、最後に追記された行の completed_at が
# 食い違っていないかを見る。
#
# 現行版は date を 2 回呼ぶため、日付をまたいだ瞬間に走ると Go 側の 1 つの
# 固定時刻では再現できない fixture ができる。
dates_consistent() {
    local out_dir="$1" file day completed
    while IFS= read -r -d '' file; do
        day=$(basename "$file" .jsonl)
        completed=$(tail -1 "$file" | jq -r '.completed_at // empty' 2>/dev/null || true)
        [ -n "$completed" ] || return 1
        [ "${completed:0:10}" = "$day" ] || return 1
    done < <(find "$out_dir/daily" -name '*.jsonl' -type f -print0 2>/dev/null)
    return 0
}

count=0
while IFS= read -r case_json; do
    name=$(field "$case_json" '.name')
    out_dir="$GOLDEN_DIR/$name/expected"

    attempt=0
    while :; do
        run_case "$case_json" "$out_dir"
        if dates_consistent "$out_dir"; then
            break
        fi
        attempt=$((attempt + 1))
        if [ "$attempt" -ge 5 ]; then
            echo "$name: daily のファイル名と completed_at の日付が一致しません" >&2
            exit 1
        fi
    done

    echo "生成: $name"
    count=$((count + 1))
done < <(jq -c '.[]' "$CASES_FILE")

echo "$count 件の fixture を $GOLDEN_DIR に生成しました"
