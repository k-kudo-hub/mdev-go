#!/bin/bash
# config.json マージのゴールデン fixture を生成する。
#
# 現行 Shell 版(claude-conductor)の install.sh:138-149 の jq 式に
# internal/domain/testdata/golden-config-merge/cases.json が定義する設定を
# 与え、その出力を <case>/expected.json に保存する。
#
# 使い方:
#   scripts/gen-golden-config-merge.sh [claude-conductor のパス]
#
# 既定のパスは ../claude-conductor。実環境には一切触れない(一時ディレクトリの
# 中だけで動く)。既定の設定は mdev-go 側の assets/config.default.json を使い、
# fixture へも写す(domain のテストは assets を import できないため。ADR-0002)。
#
# jq 式は install.sh から **実行時に切り出す**。手で写すと、向こうが変わった
# ときに気づけないまま「現行版と一致」を名乗ることになる。
set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
CONDUCTOR_SRC="${1:-$(cd "$REPO_ROOT/.." && pwd)/claude-conductor}"
GOLDEN_DIR="$REPO_ROOT/internal/domain/testdata/golden-config-merge"
CASES_FILE="$GOLDEN_DIR/cases.json"
DEFAULTS="$REPO_ROOT/assets/config.default.json"

for required in jq python3; do
    command -v "$required" >/dev/null 2>&1 || { echo "$required が必要です" >&2; exit 1; }
done
INSTALL="$CONDUCTOR_SRC/install.sh"
[ -f "$INSTALL" ] || { echo "現行スクリプトが見つかりません: $INSTALL" >&2; exit 1; }
[ -f "$CASES_FILE" ] || { echo "cases.json が見つかりません: $CASES_FILE" >&2; exit 1; }

WORK="$(mktemp -d)"
trap 'rm -rf "$WORK"' EXIT

# install.sh から jq 式(`jq --slurpfile DEF ...` の引用符の中)を切り出す。
FILTER="$WORK/filter.jq"
python3 - "$INSTALL" > "$FILTER" <<'PY'
import re, sys
body = open(sys.argv[1], encoding="utf-8").read()
m = re.search(r"jq --slurpfile DEF \"[^\"]*\" '(.*?)' \\\n", body, re.S)
if not m:
    sys.exit("install.sh から jq 式を切り出せませんでした")
sys.stdout.write(m.group(1))
PY
[ -s "$FILTER" ] || { echo "jq 式が空です" >&2; exit 1; }

# 既定の設定を fixture へ写す。テストはこちらを読む。
cp "$DEFAULTS" "$GOLDEN_DIR/config.default.json"

count=0
while IFS= read -r case_json; do
    name=$(printf '%s' "$case_json" | jq -r '.name')
    out_dir="$GOLDEN_DIR/$name"
    mkdir -p "$out_dir"

    printf '%s' "$case_json" | jq '.config' > "$out_dir/config.json"
    jq --slurpfile DEF "$DEFAULTS" -f "$FILTER" "$out_dir/config.json" > "$out_dir/expected.json"

    echo "生成: $name"
    count=$((count + 1))
done < <(jq -c '.[]' "$CASES_FILE")

echo "$count 件の fixture を $GOLDEN_DIR に生成しました"
