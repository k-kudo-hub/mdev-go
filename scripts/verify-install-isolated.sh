#!/bin/bash
# install / 移行 / mdev test の隔離検証。
#
# 使い方:
#   scripts/verify-install-isolated.sh bin/mdev
#
# **実環境には一切触れない。** HOME・CONDUCTOR_HOME・CODEX_HOME・**TMPDIR** を
# すべて一時ディレクトリへ向け、PATH も差し替えたスタブだけにする。読むのは
# claude-conductor のソース(移行前の状態を作るため)だけである。
#
# Go のテストで足りない部分をここが埋める。install は「複数の根に散らばった
# 実ファイルを、決まった順序で書き換える」処理なので、fake のファイル置き場
# ではなく本物のファイルシステム上で通しで確かめる意味がある。
#
# # TMPDIR を隔離する理由
#
# **zellij のソケット置き場は TMPDIR で決まる**(`$TMPDIR/zellij-<uid>`)。
# ここを実環境のままにすると、検証で作ったセッションが利用者の zellij サーバ
# から見えてしまい、`list-sessions` に並び、掃除の対象にもなる。HOME と
# CONDUCTOR_HOME だけ隔離しても足りない。
#
# 実際にこの手当てを怠って、検証用のセッションが実環境に残る事故を起こした。
# 冒頭のガードはその再発防止である。
#
# 3 つのシナリオを見る。
#
#   (a) まっさらな環境への設置と冪等性、セッションの起動
#   (b) 既存 Shell 環境からの移行(scripts 撤去・hooks 書き換え・
#       REPO_URL 切り替え・**ユーザーデータが無傷であること**)
#   (c) mdev test の解決とビルド(**実起動はしない**。下記)
#
# # (c) で実起動をしない理由
#
# `mdev test` の実起動は新しい端末の窓を開き、実 zellij でセッションを作る。
# 隔離のしようがない副作用なので、ここでは解決(dry-run)とビルドの成否までを
# 見る。実起動の確認はユーザーテスト 6-3 の項目にしてある。
set -uo pipefail
MDEV="${1:-bin/mdev}"
[ -x "$MDEV" ] || { echo "実行できる mdev を渡してください: $MDEV" >&2; exit 1; }
MDEV="$(cd "$(dirname "$MDEV")" && pwd)/$(basename "$MDEV")"

# --- 隔離の要: TMPDIR ------------------------------------------------------
# このスクリプト自身の TMPDIR を専用のディレクトリへ移す。以降の mktemp も
# zellij のソケット置き場もここに閉じ込められる。
#
# 移す先は実環境の一時ディレクトリの **配下** で構わない。zellij が見るのは
# `$TMPDIR/zellij-<uid>` なので、TMPDIR が 1 段でも深ければソケットは別の
# 場所になる。禁じるのは「一時ディレクトリの根そのもの」を TMPDIR にする
# ことで、それだと利用者のセッションと同じ置き場になってしまう。
REAL_TMPDIR="${TMPDIR:-/tmp}"
export TMPDIR="$(mktemp -d)/isolated-tmp"
mkdir -p "$TMPDIR"

# ガード: 一時ディレクトリの根を指していないこと。
# macOS の既定は /var/folders/<x>/<y>/T、Linux などは /tmp である。
guard_tmpdir() {
    local dir="${1%/}"
    case "$dir" in
        /tmp|/var/tmp)
            return 1 ;;
        /var/folders/*/T)
            return 1 ;;
    esac
    # 元の TMPDIR そのものも拒否する(上の型に当てはまらない設定への保険)。
    [ "$dir" != "${REAL_TMPDIR%/}" ]
}
if ! guard_tmpdir "$TMPDIR"; then
    echo "TMPDIR が実環境の一時ディレクトリそのものです: $TMPDIR" >&2
    echo "検証で作った zellij セッションが利用者の一覧に並ぶため中止します。" >&2
    exit 1
fi
# ソケット置き場が実環境と別になっていることを直に確かめる。
if [ "$TMPDIR/zellij-$(id -u)" = "${REAL_TMPDIR%/}/zellij-$(id -u)" ]; then
    echo "zellij のソケット置き場が実環境と同じです。中止します。" >&2
    exit 1
fi
echo "隔離 TMPDIR: $TMPDIR"
echo ""
pass=0; fail=0
ok()   { printf '  ✓ %s\n' "$1"; pass=$((pass+1)); }
ng()   { printf '  ✗ %s\n' "$1"; fail=$((fail+1)); }
check(){ if [ "$2" = "$3" ]; then ok "$1"; else ng "$1 (got=$2 want=$3)"; fi; }

echo "=== (a) まっさら環境 → install → セッション起動 ==="
W=$(mktemp -d); B="$W/bin"; mkdir -p "$B"
cp "$MDEV" "$B/mdev"
cat > "$B/zellij" <<ZJ
#!/bin/bash
echo "zellij \$*" >> "$W/zellij.log"
# 実機の zellij は 0 件のとき rc=1 で標準エラーへ文言を出す。rc=0 かつ無出力を
# 「0 件」と誤読しないための防御が mdev 側に入っているため、そこを再現する。
if [ "\$1" = "list-sessions" ]; then
    echo "No active zellij sessions found." >&2
    exit 1
fi
exit 0
ZJ
printf '#!/bin/bash\nexit 0\n' > "$B/claude"; chmod +x "$B/zellij" "$B/claude"
ENVBASE=(env -i PATH="$B:/usr/bin:/bin" HOME="$W/home" CONDUCTOR_HOME="$W/conductor" CODEX_HOME="$W/codex")
"${ENVBASE[@]}" "$B/mdev" install >/dev/null 2>&1
check "資産が配置される" "$(ls "$W/conductor" | tr '\n' ' ')" "REPO_URL VERSION config.default.json config.json hooks.json init.zsh layouts "
check "REPO_URL が mdev-go" "$(cat "$W/conductor/REPO_URL")" "https://github.com/k-kudo-hub/mdev-go"
before=$(find "$W/conductor" "$W/home" -type f -exec stat -f '%N %m %z' {} \; | sort)
sleep 1
"${ENVBASE[@]}" "$B/mdev" install >/dev/null 2>&1
after=$(find "$W/conductor" "$W/home" -type f -exec stat -f '%N %m %z' {} \; | sort)
check "2 回目は 1 バイトも書かない" "$([ "$before" = "$after" ] && echo same || echo diff)" "same"
mkdir -p "$W/conductor/bin" && cp "$MDEV" "$W/conductor/bin/mdev"
(cd "$W" && "${ENVBASE[@]}" "$B/mdev" >/dev/null 2>&1)
check "セッション起動が zellij を呼ぶ" "$(grep -c 'new-session-with-layout' "$W/zellij.log" 2>/dev/null || echo 0)" "1"
A_W="$W"

echo ""
echo "=== (b) 既存 Shell 環境からの移行 ==="
W=$(mktemp -d); C="$W/conductor"; B="$W/bin"
mkdir -p "$B" "$C/scripts" "$C/layouts" "$C/daily/s" "$C/tasks/s" "$W/home/.claude" "$W/home/.claude-pending/s" "$W/codex"
cp "$MDEV" "$B/mdev"
printf '#!/bin/bash\nexit 0\n' > "$B/claude"; printf '#!/bin/bash\nexit 0\n' > "$B/zellij"
printf '#!/bin/bash\nexit 0\n' > "$B/codex"; chmod +x "$B/claude" "$B/zellij" "$B/codex"
SRC=/Users/kazuto/projects/claude-conductor
cp "$SRC"/scripts/*.sh "$C/scripts/"; cp "$SRC"/layouts/*.kdl "$C/layouts/"
cp "$SRC"/init.zsh "$C/init.zsh"; cp "$SRC"/hooks.json "$C/hooks.json"
cp "$SRC"/config.default.json "$C/config.default.json"
python3 -c "
import json,pathlib
d=json.loads(pathlib.Path('$C/config.default.json').read_text())
d['search_dirs']=['~/mywork']; del d['agents']['codex']['patterns']
pathlib.Path('$C/config.json').write_text(json.dumps(d,ensure_ascii=False,indent=2)+chr(10))
h=json.loads(pathlib.Path('$C/hooks.json').read_text())
pathlib.Path('$W/home/.claude/settings.json').write_text(json.dumps({'permissions':{'allow':['Bash(ls:*)']},'hooks':h},ensure_ascii=False,indent=2)+chr(10))
"
echo go > "$C/FLAVOR"; echo "https://github.com/k-kudo-hub/claude-conductor" > "$C/REPO_URL"; echo v0.8.0 > "$C/VERSION"
printf '{"a":1}\n' > "$C/daily/s/2026-08-01.jsonl"; printf '{"b":2}\n' > "$C/tasks/s/x.json"
printf '{"c":3}\n' > "$W/home/.claude-pending/s/x.json"
printf 'notify = ["bash", "%s/scripts/codex-notify.sh"]\n' "$C" > "$W/codex/config.toml"
printf 'source "$HOME/.claude-conductor/init.zsh"\n' > "$W/home/.zshrc"
env -i PATH="$B:/usr/bin:/bin" HOME="$W/home" CONDUCTOR_HOME="$C" CODEX_HOME="$W/codex" "$B/mdev" install >/dev/null 2>&1
check "scripts/ が消える" "$([ -d "$C/scripts" ] && echo yes || echo no)" "no"
check "FLAVOR が消える" "$([ -e "$C/FLAVOR" ] && echo yes || echo no)" "no"
check "REPO_URL が mdev-go" "$(cat "$C/REPO_URL")" "https://github.com/k-kudo-hub/mdev-go"
check "hooks が mdev を指す" "$(python3 -c "
import json;d=json.load(open('$W/home/.claude/settings.json'))
print(sum('bin/mdev hook' in h['command'] for e in d['hooks'].values() for m in e for h in m['hooks']))")" "4"
check "hooks に Shell 版が残らない" "$(grep -c '/scripts/pending-' "$W/home/.claude/settings.json")" "0"
check "hooks の展開形が残る" "$(grep -c 'CONDUCTOR_HOME:-' "$W/home/.claude/settings.json")" "4"
check "利用者の permissions が残る" "$(python3 -c "
import json;print('yes' if json.load(open('$W/home/.claude/settings.json')).get('permissions') else 'no')")" "yes"
check "codex notify が移行" "$(grep -c 'codex-notify.sh' "$W/codex/config.toml")" "0"
check "layouts の scripts 参照" "$(grep -h '/scripts/' "$C/layouts/"*.kdl | wc -l | tr -d ' ')" "0"
check "init.zsh がシム" "$(grep -c 'mdev\" init zsh' "$C/init.zsh")" "1"
check "init.zsh に旧関数が無い" "$(grep -cE '^(mdev|dev|zs)\(\)' "$C/init.zsh")" "0"
check "config の search_dirs 保持" "$(python3 -c "
import json;print(json.load(open('$C/config.json'))['search_dirs'][0])")" "~/mywork"
check "config の patterns が補完" "$(python3 -c "
import json;print(len(json.load(open('$C/config.json'))['agents']['codex']['patterns']))")" "3"
check "daily 無傷" "$(cat "$C/daily/s/2026-08-01.jsonl")" '{"a":1}'
check "tasks 無傷" "$(cat "$C/tasks/s/x.json")" '{"b":2}'
check "pending 無傷" "$(cat "$W/home/.claude-pending/s/x.json")" '{"c":3}'
check ".zshrc 無変更" "$(cat "$W/home/.zshrc")" 'source "$HOME/.claude-conductor/init.zsh"'
B_W="$W"

echo ""
echo "=== (c) mdev test の解決とビルド ==="
# **実起動はしない。** `mdev test` の実起動は新しい端末の窓を開いて実 zellij で
# セッションを作る。隔離のしようがない副作用なので、ここでは解決(dry-run)と
# ビルドの成否までを見る。実起動の確認はユーザーテスト 6-3 の項目にしてある。
WT="$(cd "$(dirname "$MDEV")/.." && pwd)"
WT_NAME="$(basename "$WT")"
out=$("$MDEV" test --dry-run "$WT" 2>&1)
check "dry-run が解決する" "$(echo "$out" | grep -c '^SESSION=test-')" "1"
check "dry-run は隔離先を指す" "$(echo "$out" | grep -c "CONDUCTOR_HOME=$WT/.mdev-test")" "1"
check "dry-run は何も作らない" "$([ -e "$WT/.mdev-test" ] && echo yes || echo no)" "no"
out_name=$("$MDEV" test --dry-run "$WT_NAME" 2>&1)
check "ブランチ名でも解決する" "$(echo "$out_name" | grep -c "^WORKTREE=$WT\$")" "1"

# ビルドだけを直に確かめる(mdev test が内部で走らせるのと同じコマンド)。
BUILD_OUT="$W/built-mdev"
(cd "$WT" && go build -o "$BUILD_OUT" ./cmd/mdev) >/dev/null 2>&1
check "worktree のソースが組める" "$([ -x "$BUILD_OUT" ] && echo yes || echo no)" "yes"
check "組んだバイナリが動く" "$("$BUILD_OUT" version 2>/dev/null | head -1)" "dev"

echo ""
echo "=== 結果: $pass 件成功 / $fail 件失敗 ==="
rm -rf "$A_W" "$B_W"
[ "$fail" -eq 0 ]
