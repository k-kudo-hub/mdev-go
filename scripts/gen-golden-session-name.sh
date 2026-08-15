#!/bin/bash
# セッション名切り詰めのゴールデン表を生成する。
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
# 現行 init.zsh の `_conductor_session_name` を **その場で source して** 呼び、
# 入力と出力の対を internal/domain/testdata/session-names.tsv に書き出す。
# Go 側の ZellijSessionName はこの表と 1 件も違わないことをテストで固定する。
#
# 使い方:
#   scripts/gen-golden-session-name.sh [claude-conductor のパス]
#
# 既定のパスは ../claude-conductor。読むだけで、実環境には何も書かない。
#
# zsh で動かすのが要点である。関数は zsh 向けに書かれており、`${#name}` の
# 文字数え・`${name:0:19}` の切り出し・`${h: -4}` の後方参照はどれも
# シェルによって意味が変わる。bash で代用すると別物を写すことになる。
set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
CONDUCTOR_SRC="${1:-$(cd "$REPO_ROOT/.." && pwd)/claude-conductor}"
OUT="$REPO_ROOT/internal/domain/testdata/session-names.tsv"
INIT="$CONDUCTOR_SRC/init.zsh"

command -v zsh >/dev/null 2>&1 || { echo "zsh が必要です" >&2; exit 1; }
[ -f "$INIT" ] || { echo "現行スクリプトが見つかりません: $INIT" >&2; exit 1; }

# 入力の一覧。name<TAB>hash_src(空なら name 自身が使われる)。
#
# 境界を狙って選んである: 24 文字ちょうど / 25 文字 / 切り出した 19 文字が
# `-` で終わる(末尾の `-` が落ちて 23 文字になる)/ マルチバイト(文字数え
# とバイト数えの差が出る)/ 同名で hash 源が違う / 空文字。
INPUTS=$(cat <<'CASES'
my-project	
a	
abcdefghij-abcdefghij-ab	
abcdefghij-abcdefghij-abc	
this-is-a-very-long-session-name	
this-is-a-very-long-session-name	/path/alpha
this-is-a-very-long-session-name	/path/beta
abcdefghijklmnopqr-stuvwxyz	
aaaaaaaaaaaaaaaaaaa-bbbbbbbb	
test-add-embedded-assets-and-cli-ports	/Users/dev/projects/mdev-go/.worktree/add-embedded-assets-and-cli-ports
test-add-installer-and-init-subcommands	/Users/dev/projects/mdev-go/.worktree/add-installer-and-init-subcommands
mdev-go-224042-long-enough-name	
日本語のとても長いセッション名前を付けてみたときの挙動	
プロジェクト-abcdefghijklmnopqrstu	
name-with-trailing-dash----------------	
	
CASES
)

# init.zsh は source した時点でエイリアスと関数を定義するだけで副作用が無い。
# 念のため CONDUCTOR_HOME を一時ディレクトリへ向けておく。
WORK="$(mktemp -d)"
trap 'rm -rf "$WORK"' EXIT

{
    printf '# 現行 init.zsh の _conductor_session_name の出力を写したもの。\n'
    printf '# scripts/gen-golden-session-name.sh が生成する。手で編集しないこと。\n'
    printf '# 列: name<TAB>hash_src<TAB>期待するセッション名\n'
    printf '%s\n' "$INPUTS" | CONDUCTOR_HOME="$WORK" zsh -c '
        source "'"$INIT"'"
        while IFS=$'"'"'\t'"'"' read -r name src; do
            if [[ -z "$src" ]]; then
                out=$(_conductor_session_name "$name")
            else
                out=$(_conductor_session_name "$name" "$src")
            fi
            printf "%s\t%s\t%s\n" "$name" "$src" "$out"
        done'
} > "$OUT"

echo "生成: $OUT ($(grep -vc '^#' "$OUT") 件)"
