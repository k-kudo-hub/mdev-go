#!/bin/bash
# news 取得のゴールデン fixture を生成する。
#
# 現行 Shell 版(claude-conductor)の fetch-news.sh に
# cmd/mdev/testdata/golden-news/feed.xml を食わせ、書き出されたニュース
# ファイルを同じディレクトリの expected.json に保存する。Go 側のテストは
# 同じ feed.xml を解析した結果をこれとバイト単位で突き合わせる。
#
# 使い方:
#   scripts/gen-golden-news.sh [claude-conductor のパス]
#
# 既定のパスは ../claude-conductor。HOME と CONDUCTOR_HOME は一時ディレクトリへ
# 向け、curl も差し替えるため、実環境にも外部にも一切触れない
# (隔離のしかたは scripts/gen-golden-record.sh と同じ)。
set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
CONDUCTOR_SRC="${1:-$(cd "$REPO_ROOT/.." && pwd)/claude-conductor}"
GOLDEN_DIR="$REPO_ROOT/cmd/mdev/testdata/golden-news"
FEED="$GOLDEN_DIR/feed.xml"

for required in jq bash awk; do
    command -v "$required" >/dev/null 2>&1 || { echo "$required が必要です" >&2; exit 1; }
done
FETCH="$CONDUCTOR_SRC/scripts/fetch-news.sh"
[ -f "$FETCH" ] || { echo "現行スクリプトが見つかりません: $FETCH" >&2; exit 1; }
[ -f "$FEED" ] || { echo "feed.xml が見つかりません: $FEED" >&2; exit 1; }

WORK="$(mktemp -d)"
trap 'rm -rf "$WORK"' EXIT

# feed.xml をそのまま返す curl の代役。fetch-news.sh は curl の引数を見ないので
# 中身を出すだけでよい。
mkdir -p "$WORK/bin"
cat > "$WORK/bin/curl" << CURL
#!/bin/bash
cat "$FEED"
CURL
chmod +x "$WORK/bin/curl"

mkdir -p "$WORK/home" "$WORK/conductor"
env -i \
    PATH="$WORK/bin:/usr/bin:/bin:/usr/sbin:/sbin:/opt/homebrew/bin" \
    HOME="$WORK/home" \
    CONDUCTOR_HOME="$WORK/conductor" \
    bash "$FETCH"

today=$(date '+%Y-%m-%d')
src="$WORK/conductor/news/$today.json"
[ -f "$src" ] || { echo "ニュースファイルが作られませんでした" >&2; exit 1; }
cp "$src" "$GOLDEN_DIR/expected.json"
echo "生成: $GOLDEN_DIR/expected.json"
