#!/bin/bash
# upload-log のゴールデン fixture を生成する。
#
# 現行 Shell 版(claude-conductor)の upload-log.sh を UPLOAD_LOG_LIB=1 で
# source し、cmd/mdev/testdata/golden-upload/cases.json が定義する入力を
# filter_secrets / build_markdown へ与えて、その出力を
# testdata/golden-upload/<case>/expected を保存する。
#
# 使い方:
#   scripts/gen-golden-upload.sh [claude-conductor のパス]
#
# 既定のパスは ../claude-conductor。HOME と CONDUCTOR_HOME は一時ディレクトリへ
# 向けるため、実環境のファイルには一切触れない(隔離のしかたは
# scripts/gen-golden-record.sh と同じ)。
#
# 2 つの kind で出力の取り方が違う。
#
#   filter_secrets: 関数の標準出力をそのままリダイレクトする。awk|sed は
#     行ごとに改行を付けるため、末尾の改行まで含めて Go 版と 1 バイト単位で
#     比べられる。
#   build_markdown: コマンド置換で受けてから書く。現行 upload-log.sh も
#     `MD=$(build_markdown ...)` で受けており、末尾の改行が落ちた **その値**が
#     実際にリポジトリへ書かれるものだからである。
#
# build_markdown の case は 11 フィールドすべてが非空のものだけにしてある。
# 現行版は 11 値を @tsv にして `IFS=$'\t' read` で受けるが、bash はタブを
# IFS の空白文字として扱うため、**空のフィールドがあると連続タブが 1 つに
# 畳まれて以降の値が 1 つずつずれる**(completed_at が空、または tools_used が
# 空配列のときに起きる)。Go 版はこのずれを修正しているので、ずれる入力では
# 両者が一致しない。その挙動差は internal/domain/upload_log_test.go の
# 「プレースホルダレコードは既定値で埋める」で Go 側だけを固定している。
set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
CONDUCTOR_SRC="${1:-$(cd "$REPO_ROOT/.." && pwd)/claude-conductor}"
GOLDEN_DIR="$REPO_ROOT/cmd/mdev/testdata/golden-upload"
CASES_FILE="$GOLDEN_DIR/cases.json"

for required in jq bash; do
    command -v "$required" >/dev/null 2>&1 || { echo "$required が必要です" >&2; exit 1; }
done
UPLOAD="$CONDUCTOR_SRC/scripts/upload-log.sh"
[ -f "$UPLOAD" ] || { echo "現行スクリプトが見つかりません: $UPLOAD" >&2; exit 1; }
[ -f "$CASES_FILE" ] || { echo "cases.json が見つかりません: $CASES_FILE" >&2; exit 1; }

WORK="$(mktemp -d)"
trap 'rm -rf "$WORK"' EXIT

# runner.sh は隔離した環境の中で source と関数呼び出しを行う。
# env -i でヒアドキュメントを渡せないため、いったんファイルへ書き出す。
cat > "$WORK/runner.sh" << 'RUNNER'
#!/bin/bash
set -euo pipefail
# shellcheck source=/dev/null
UPLOAD_LOG_LIB=1 source "$UPLOAD_SCRIPT"

case "$KIND" in
    filter_secrets)
        # 関数の出力をそのまま(末尾の改行も含めて)書き出す。
        printf '%s' "$CASE_INPUT" | filter_secrets > "$OUT_FILE"
        ;;
    build_markdown)
        # 現行版の呼び出し側と同じくコマンド置換で受ける。
        md=$(build_markdown "$CASE_RECORD" "$CASE_SUMMARY")
        printf '%s' "$md" > "$OUT_FILE"
        ;;
    *)
        echo "未知の kind: $KIND" >&2
        exit 1
        ;;
esac
RUNNER

# run_case <case json>
run_case() {
    local case_json="$1"
    local name kind out_dir
    name=$(printf '%s' "$case_json" | jq -r '.name')
    kind=$(printf '%s' "$case_json" | jq -r '.kind')

    out_dir="$GOLDEN_DIR/$name"
    mkdir -p "$out_dir"

    # CONDUCTOR_HOME には codex-rollout-lib.sh を置く。upload-log.sh はこれを
    # source するが、ここで使う 2 つの関数は codex 定義を参照しないため、
    # 有無で出力は変わらない。実環境と同じ経路を通すために置いておく。
    local home="$WORK/home/$name"
    mkdir -p "$home/scripts"
    cp "$CONDUCTOR_SRC/scripts/codex-rollout-lib.sh" "$home/scripts/"

    env -i \
        PATH="/usr/bin:/bin:/usr/sbin:/sbin:/opt/homebrew/bin" \
        HOME="$home" \
        CONDUCTOR_HOME="$home" \
        UPLOAD_SCRIPT="$UPLOAD" \
        KIND="$kind" \
        OUT_FILE="$out_dir/expected" \
        CASE_INPUT="$(printf '%s' "$case_json" | jq -rj '.input // ""')" \
        CASE_RECORD="$(printf '%s' "$case_json" | jq -rj '.record // ""')" \
        CASE_SUMMARY="$(printf '%s' "$case_json" | jq -rj '.summary // ""')" \
        bash "$WORK/runner.sh"
}

count=0
while IFS= read -r case_json; do
    run_case "$case_json"
    echo "生成: $(printf '%s' "$case_json" | jq -r '.name')"
    count=$((count + 1))
done < <(jq -c '.[]' "$CASES_FILE")

echo "$count 件の fixture を $GOLDEN_DIR に生成しました"
