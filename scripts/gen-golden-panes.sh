#!/bin/bash
# ダッシュボード系 4 ペインのゴールデン fixture を生成する。
#
# 現行 Shell 版(claude-conductor)の 4 つのペインスクリプトを ONCE モードで
# 走らせ、その標準出力を cmd/mdev/testdata/golden-panes/<case>/expected.txt に
# 保存する。入力(pending / daily / news)は同じ testdata の cases.json が定義し、
# Go 側のテストも同じ定義からファイルを組み立てるため、両者は必ず同じ入力を見る。
#
# 使い方:
#   scripts/gen-golden-panes.sh [claude-conductor のパス]
#
# 既定のパスは ../claude-conductor。HOME と CONDUCTOR_HOME は一時ディレクトリへ
# 向け、zellij はスタブに差し替えるため、実環境のファイルには一切触れない。
set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
CONDUCTOR_SRC="${1:-$(cd "$REPO_ROOT/.." && pwd)/claude-conductor}"
GOLDEN_DIR="$REPO_ROOT/cmd/mdev/testdata/golden-panes"
CASES_FILE="$GOLDEN_DIR/cases.json"

for required in jq bash; do
    command -v "$required" >/dev/null 2>&1 || { echo "$required が必要です" >&2; exit 1; }
done
[ -d "$CONDUCTOR_SRC/scripts" ] || { echo "現行スクリプトが見つかりません: $CONDUCTOR_SRC/scripts" >&2; exit 1; }
[ -f "$CASES_FILE" ] || { echo "cases.json が見つかりません: $CASES_FILE" >&2; exit 1; }

WORK="$(mktemp -d)"
trap 'rm -rf "$WORK"' EXIT

# daily とニュースのファイル名は「今日」の日付になる。Shell 版は date コマンドを
# 直接呼ぶため差し替えられない。そこで生成時の日付を case ごとに記録しておき、
# Go 側のテストは同じ日付を返す時計を差し込んで突き合わせる。
TODAY="$(date '+%Y-%m-%d')"

# ペイン名から現行スクリプト名と ONCE 環境変数への対応。
script_for_pane() {
    case "$1" in
        dashboard) echo "dashboard-loop.sh" ;;
        waiting)   echo "waiting-loop.sh" ;;
        done)      echo "done-loop.sh" ;;
        news)      echo "news-loop.sh" ;;
        *)         echo "未知のペイン: $1" >&2; return 1 ;;
    esac
}

once_var_for_pane() {
    case "$1" in
        dashboard) echo "CONDUCTOR_DASHBOARD_ONCE" ;;
        waiting)   echo "CONDUCTOR_WAITING_ONCE" ;;
        done)      echo "CONDUCTOR_DONE_ONCE" ;;
        news)      echo "CONDUCTOR_NEWS_ONCE" ;;
        *)         echo "未知のペイン: $1" >&2; return 1 ;;
    esac
}

# zellij のスタブ。MOCK_TABS から list-tabs の出力を作る以外は何もしない。
# 実際の zellij は起動せず、タブの生成・移動・終了も起きない。
STUB_BIN="$WORK/bin"
mkdir -p "$STUB_BIN"
cat > "$STUB_BIN/zellij" << 'STUB'
#!/bin/bash
if [[ "$1" == "action" && "$2" == "list-tabs" ]]; then
    echo "ID POSITION NAME"
    for t in ${MOCK_TABS:-}; do
        echo "1 x $t"
    done
fi
exit 0
STUB
chmod +x "$STUB_BIN/zellij"

# 1 件の case を実行して expected.txt を書く。
run_case() {
    local case_json="$1"
    local name pane session tabs
    name=$(printf '%s' "$case_json" | jq -r '.name')
    pane=$(printf '%s' "$case_json" | jq -r '.pane')
    session=$(printf '%s' "$case_json" | jq -r '.session')
    tabs=$(printf '%s' "$case_json" | jq -r '.tabs // ""')

    local sandbox="$WORK/run/$name"
    rm -rf "$sandbox"
    mkdir -p "$sandbox/home" "$sandbox/conductor"
    # dashboard-loop.sh は screen-detect-lib.sh を source し、
    # restore-session.sh を呼ぶため、スクリプト一式が要る。レジストリは空なので
    # 復元は何も起こさず、list-panes も空なのでスクリーン検出も働かない。
    cp -R "$CONDUCTOR_SRC/scripts" "$sandbox/conductor/scripts"

    # 入力ファイルを配置する。{TODAY} は生成時の日付に置き換える。
    local rel dest
    while IFS= read -r rel; do
        [ -n "$rel" ] || continue
        local resolved="${rel//\{TODAY\}/$TODAY}"
        case "$resolved" in
            pending/*) dest="$sandbox/home/.claude-pending/${resolved#pending/}" ;;
            daily/*)   dest="$sandbox/conductor/daily/${resolved#daily/}" ;;
            news/*)    dest="$sandbox/conductor/news/${resolved#news/}" ;;
            config.json) dest="$sandbox/conductor/config.json" ;;
            *) echo "未知の入力パス: $resolved" >&2; return 1 ;;
        esac
        mkdir -p "$(dirname "$dest")"
        printf '%s' "$(printf '%s' "$case_json" | jq -r --arg k "$rel" '.files[$k]')" > "$dest"
    done < <(printf '%s' "$case_json" | jq -r '.files | keys[]?')

    local out_dir="$GOLDEN_DIR/$name"
    mkdir -p "$out_dir"

    # 環境は env -i で遮断し、必要なものだけ渡す。ロケールは実行環境に合わせて
    # UTF-8 にする(タブ名のスラグ生成に使う tr がロケールに従うため)。
    env -i \
        PATH="$STUB_BIN:/usr/bin:/bin:/usr/sbin:/sbin:/opt/homebrew/bin" \
        HOME="$sandbox/home" \
        CONDUCTOR_HOME="$sandbox/conductor" \
        ZELLIJ_SESSION_NAME="$session" \
        MOCK_TABS="$tabs" \
        LC_CTYPE="UTF-8" \
        TERM="dumb" \
        "$(once_var_for_pane "$pane")=1" \
        bash "$sandbox/conductor/scripts/$(script_for_pane "$pane")" \
        > "$out_dir/expected.txt" 2>/dev/null

    printf '%s\n' "$TODAY" > "$out_dir/date.txt"
}

count=0
while IFS= read -r case_json; do
    run_case "$case_json"
    echo "生成: $(printf '%s' "$case_json" | jq -r '.name')"
    count=$((count + 1))
done < <(jq -c '.[]' "$CASES_FILE")

echo "$count 件の fixture を $GOLDEN_DIR に生成しました"
