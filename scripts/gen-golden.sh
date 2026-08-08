#!/bin/bash
# ゴールデンテストの fixture を生成する。
#
# 現行 Shell 版(claude-conductor)の hook スクリプトに
# internal/infra/store/testdata/golden/cases.json が定義する標準入力・環境変数・
# 実行前ファイルを与え、生成された pending / レジストリの実ファイルと
# zellij 呼び出しの記録を testdata/golden/<case>/expected/ に保存する。
#
# 使い方:
#   scripts/gen-golden.sh [claude-conductor のパス]
#
# 既定のパスは ../claude-conductor。HOME と CONDUCTOR_HOME は一時ディレクトリへ
# 向けるため、実環境のファイルには一切触れない。
set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
CONDUCTOR_SRC="${1:-$(cd "$REPO_ROOT/.." && pwd)/claude-conductor}"
GOLDEN_DIR="$REPO_ROOT/internal/infra/store/testdata/golden"
CASES_FILE="$GOLDEN_DIR/cases.json"

for required in jq bash; do
    command -v "$required" >/dev/null 2>&1 || { echo "$required が必要です" >&2; exit 1; }
done
[ -d "$CONDUCTOR_SRC/scripts" ] || { echo "現行スクリプトが見つかりません: $CONDUCTOR_SRC/scripts" >&2; exit 1; }
[ -f "$CASES_FILE" ] || { echo "cases.json が見つかりません: $CASES_FILE" >&2; exit 1; }

WORK="$(mktemp -d)"
trap 'rm -rf "$WORK"' EXIT

# hook 名から現行スクリプト名への対応。
script_for_hook() {
    case "$1" in
        notify)    echo "pending-notify.sh" ;;
        post-tool) echo "pending-post-tool.sh" ;;
        resolve)   echo "pending-resolve.sh" ;;
        *)         echo "未知の hook: $1" >&2; return 1 ;;
    esac
}

# zellij の呼び出しを記録するスタブを用意する。実際の zellij は起動しない。
STUB_BIN="$WORK/bin"
mkdir -p "$STUB_BIN"
cat > "$STUB_BIN/zellij" << 'STUB'
#!/bin/bash
# action go-to-tab-name Main -> "go-to-tab-name Main" として記録する
shift  # "action" を捨てる
printf '%s\n' "$*" >> "$ZELLIJ_CALL_LOG"
STUB
chmod +x "$STUB_BIN/zellij"

# 1 件の case を実行して出力を出力先へ集める。
# run_case <case json> <出力先ディレクトリ>
run_case() {
    local case_json="$1" out_dir="$2"
    local name hook stdin
    name=$(printf '%s' "$case_json" | jq -r '.name')
    hook=$(printf '%s' "$case_json" | jq -r '.hook')
    stdin=$(printf '%s' "$case_json" | jq -r '.stdin')

    local sandbox="$WORK/run/$name"
    rm -rf "$sandbox"
    mkdir -p "$sandbox/home" "$sandbox/conductor"
    # スクリプトは $CONDUCTOR_HOME/scripts/registry-lib.sh を source する。
    cp -R "$CONDUCTOR_SRC/scripts" "$sandbox/conductor/scripts"

    # 実行前のファイルを配置する。
    while IFS= read -r rel; do
        [ -n "$rel" ] || continue
        local dest
        case "$rel" in
            pending/*) dest="$sandbox/home/.claude-pending/${rel#pending/}" ;;
            tasks/*)   dest="$sandbox/conductor/tasks/${rel#tasks/}" ;;
            *) echo "未知の pre パス: $rel" >&2; return 1 ;;
        esac
        mkdir -p "$(dirname "$dest")"
        printf '%s' "$(printf '%s' "$case_json" | jq -r --arg k "$rel" '.pre[$k]')" > "$dest"
    done < <(printf '%s' "$case_json" | jq -r '.pre | keys[]?')

    local call_log="$sandbox/zellij-calls.txt"
    : > "$call_log"

    # 環境変数は case が定義するものだけを渡す(env -i で他を遮断する)。
    local -a env_args=()
    while IFS= read -r kv; do
        [ -n "$kv" ] || continue
        env_args+=("$kv")
    done < <(printf '%s' "$case_json" | jq -r '.env | to_entries[] | "\(.key)=\(.value)"')

    printf '%s' "$stdin" | env -i \
        PATH="$STUB_BIN:/usr/bin:/bin:/usr/sbin:/sbin:/opt/homebrew/bin" \
        HOME="$sandbox/home" \
        CONDUCTOR_HOME="$sandbox/conductor" \
        ZELLIJ_CALL_LOG="$call_log" \
        ${env_args[@]+"${env_args[@]}"} \
        bash "$sandbox/conductor/scripts/$(script_for_hook "$hook")"

    # 生成物を集める(空ディレクトリは持ち込まない)。
    rm -rf "$out_dir"
    mkdir -p "$out_dir/pending" "$out_dir/tasks"
    if [ -d "$sandbox/home/.claude-pending" ]; then
        (cd "$sandbox/home/.claude-pending" && find . -name '*.json' -type f -print0) \
            | while IFS= read -r -d '' f; do
                mkdir -p "$out_dir/pending/$(dirname "$f")"
                cp "$sandbox/home/.claude-pending/$f" "$out_dir/pending/$f"
            done
    fi
    if [ -d "$sandbox/conductor/tasks" ]; then
        (cd "$sandbox/conductor/tasks" && find . -name '*.json' -type f -print0) \
            | while IFS= read -r -d '' f; do
                mkdir -p "$out_dir/tasks/$(dirname "$f")"
                cp "$sandbox/conductor/tasks/$f" "$out_dir/tasks/$f"
            done
    fi
    cp "$call_log" "$out_dir/zellij-calls.txt"
}

# pending の time とレジストリの updated_at が同じ秒に収まっているかを確認する。
#
# Shell 版は date を 2 回呼ぶため、秒をまたぐと Go 側の 1 つの固定時刻では
# 再現できない fixture ができてしまう。実行前から置いてあり今回書き換えられて
# いない pending(Stop が Notification を上書きしない case など)は、時刻が
# 違って当然なので照合の対象から外す。
timestamps_consistent() {
    local case_json="$1" out_dir="$2" registry_time pending_time rel pre_time
    registry_time=$(find "$out_dir/tasks" -name '*.json' -type f -exec jq -r '.updated_at // empty' {} \; 2>/dev/null | head -1)
    if [ -z "$registry_time" ]; then
        # レジストリが無い case は pending の time から時刻を復元するため、
        # 照合するものが無い。
        return 0
    fi

    while IFS= read -r -d '' path; do
        pending_time=$(jq -r '.time // empty' "$path" 2>/dev/null)
        [ -n "$pending_time" ] || continue
        if [ "${registry_time:11:8}" = "$pending_time" ]; then
            continue
        fi
        # 実行前の内容のままなら差があってよい。
        rel="pending/${path#"$out_dir/pending/"}"
        pre_time=$(printf '%s' "$case_json" | jq -r --arg k "$rel" '.pre[$k] // ""' | jq -r '.time // empty' 2>/dev/null)
        if [ -n "$pre_time" ] && [ "$pre_time" = "$pending_time" ]; then
            continue
        fi
        return 1
    done < <(find "$out_dir/pending" -name '*.json' -type f -print0 2>/dev/null)
    return 0
}

count=0
while IFS= read -r case_json; do
    name=$(printf '%s' "$case_json" | jq -r '.name')
    out_dir="$GOLDEN_DIR/$name/expected"

    attempt=0
    while :; do
        run_case "$case_json" "$out_dir"
        if timestamps_consistent "$case_json" "$out_dir"; then
            break
        fi
        attempt=$((attempt + 1))
        if [ "$attempt" -ge 5 ]; then
            echo "$name: pending と registry の時刻が一致しません" >&2
            exit 1
        fi
    done

    echo "生成: $name"
    count=$((count + 1))
done < <(jq -c '.[]' "$CASES_FILE")

echo "$count 件の fixture を $GOLDEN_DIR に生成しました"
