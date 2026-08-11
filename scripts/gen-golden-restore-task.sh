#!/bin/bash
# Done からのタスク復元のゴールデン fixture を生成する。
#
# 現行 Shell 版(claude-conductor)の restore-task.sh に
# cmd/mdev/testdata/golden-restore-task/cases.json が定義する daily ログを与えて
# 走らせ、次の 3 つを <case>/ へ保存する。
#
#   exit.txt    … 終了コード(0-5 の契約)
#   daily.jsonl … 実行後の daily ログ(restored の付き方)
#   zellij.log  … 実行された zellij コマンドの並び
#
# 使い方:
#   scripts/gen-golden-restore-task.sh [claude-conductor のパス]
#
# 既定のパスは ../claude-conductor。HOME と CONDUCTOR_HOME は一時ディレクトリへ
# 向け、zellij はスタブに差し替えるため、実環境のファイルには一切触れない。
#
# 環境ごとに変わるパスは保存前に置き換える。
#
#   <サンドボックス>            -> {SANDBOX}
#   -- bash <...>/task-control.sh -> -- {TASK_CONTROL}
#
# 後者は「タスクタブの操作バーを何で起動するか」の差で、Shell 版は
# task-control.sh、Go 版は `mdev pane task-control` を使う(フェーズ 3 で
# 決めた既知の差異)。復元の契約とは無関係なので、両側で同じ印に潰す。
set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
CONDUCTOR_SRC="${1:-$(cd "$REPO_ROOT/.." && pwd)/claude-conductor}"
GOLDEN_DIR="$REPO_ROOT/cmd/mdev/testdata/golden-restore-task"
CASES_FILE="$GOLDEN_DIR/cases.json"

for required in jq bash; do
    command -v "$required" >/dev/null 2>&1 || { echo "$required が必要です" >&2; exit 1; }
done
[ -f "$CONDUCTOR_SRC/scripts/restore-task.sh" ] \
    || { echo "現行スクリプトが見つかりません: $CONDUCTOR_SRC/scripts" >&2; exit 1; }
[ -f "$CASES_FILE" ] || { echo "cases.json が見つかりません: $CASES_FILE" >&2; exit 1; }

WORK="$(mktemp -d)"
trap 'rm -rf "$WORK"' EXIT

# zellij のスタブ。呼び出しを記録しつつ、タブ登録とフォーカス確認が通る
# だけの応答を返す(実測の zellij 0.44.1 に合わせ、go-to-tab-name は
# ヒットしたときだけ index を出す)。
STUB_BIN="$WORK/bin"
mkdir -p "$STUB_BIN"
cat > "$STUB_BIN/zellij" << 'STUB'
#!/bin/bash
shift  # "action" を捨てる(記録は gen-golden.sh と同じ形にする)
printf '%s\n' "$*" >> "$ZELLIJ_CALL_LOG"
case "$1" in
    new-tab)
        # new-tab -n <name> --cwd ...
        printf '%s\n' "$3" >> "$ZELLIJ_TABS"
        ;;
    query-tab-names)
        cat "$ZELLIJ_TABS" 2>/dev/null
        ;;
    go-to-tab-name)
        grep -Fxq -- "$2" "$ZELLIJ_TABS" 2>/dev/null && echo 1
        ;;
esac
exit 0
STUB
chmod +x "$STUB_BIN/zellij"

# 1 件の case を実行して fixture を書く。
run_case() {
    local case_json="$1"
    local name tab session completed daily
    name=$(printf '%s' "$case_json" | jq -r '.name')
    tab=$(printf '%s' "$case_json" | jq -r '.tab')
    session=$(printf '%s' "$case_json" | jq -r '.session')
    completed=$(printf '%s' "$case_json" | jq -r '.completed_at')
    daily=$(printf '%s' "$case_json" | jq -r '.daily')

    local sandbox="$WORK/run/$name"
    rm -rf "$sandbox"
    mkdir -p "$sandbox/home" "$sandbox/conductor" "$sandbox/proj" "$sandbox/proj2"
    cp -R "$CONDUCTOR_SRC/scripts" "$sandbox/conductor/scripts"
    cp "$CONDUCTOR_SRC/config.default.json" "$sandbox/conductor/config.default.json"
    echo '{}' > "$sandbox/transcript.jsonl"

    # daily ログを置く。ファイル名は完了時刻の先頭 10 文字で決まる。
    local daily_dir="$sandbox/conductor/daily/$session"
    mkdir -p "$daily_dir"
    printf '%s' "${daily//\{SANDBOX\}/$sandbox}" > "$daily_dir/${completed:0:10}.jsonl"

    local call_log="$sandbox/zellij-calls.txt"
    : > "$call_log"
    : > "$sandbox/tabs.txt"

    local rc=0
    env -i \
        PATH="$STUB_BIN:/usr/bin:/bin:/usr/sbin:/sbin:/opt/homebrew/bin" \
        HOME="$sandbox/home" \
        CONDUCTOR_HOME="$sandbox/conductor" \
        ZELLIJ_SESSION_NAME="$session" \
        ZELLIJ_CALL_LOG="$call_log" \
        ZELLIJ_TABS="$sandbox/tabs.txt" \
        LC_CTYPE="UTF-8" \
        bash "$sandbox/conductor/scripts/restore-task.sh" "$tab" "$session" "$completed" \
        >/dev/null 2>&1 || rc=$?

    local out_dir="$GOLDEN_DIR/$name"
    mkdir -p "$out_dir"
    printf '%s\n' "$rc" > "$out_dir/exit.txt"
    normalize "$sandbox" < "$daily_dir/${completed:0:10}.jsonl" > "$out_dir/daily.jsonl"
    normalize "$sandbox" < "$call_log" > "$out_dir/zellij.log"
}

# normalize <サンドボックス> は環境ごとに変わる文字列を印へ置き換える。
normalize() {
    sed -e "s|-- bash [^ ]*/task-control\.sh |-- {TASK_CONTROL} |" \
        -e "s|$1|{SANDBOX}|g"
}

# 設定は Go 側のテストも同じものを読む必要があるため、fixture として持ち込む。
cp "$CONDUCTOR_SRC/config.default.json" "$GOLDEN_DIR/config.default.json"

count=0
while IFS= read -r case_json; do
    run_case "$case_json"
    echo "生成: $(printf '%s' "$case_json" | jq -r '.name')"
    count=$((count + 1))
done < <(jq -c '.[]' "$CASES_FILE")

echo "$count 件の fixture を $GOLDEN_DIR に生成しました"
