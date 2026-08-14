#!/bin/bash
# mdev ブートストラップインストーラ
#
#   curl -fsSL https://raw.githubusercontent.com/k-kudo-hub/mdev-go/main/install.sh | bash
#
# このスクリプトがするのは 4 つだけである。
#
#   1. 依存(curl / zellij)の確認
#   2. 最新リリースの版を決める
#   3. その環境向けのバイナリを取得し、checksums.txt と突き合わせる
#   4. $CONDUCTOR_HOME/bin/mdev へ置いて `mdev install` を exec する
#
# 設定の書き換え(hooks / codex notify / レイアウト / config のマージ)は
# **すべて `mdev install` が行う**。ここに置くと、更新のたびに 2 つの実装を
# 揃え続けることになる(ADR-0004 D4)。
#
# bash 3.2(macOS 標準)で動く。連想配列も `${var,,}` も使わない。
set -euo pipefail

REPO="k-kudo-hub/mdev-go"
CONDUCTOR_HOME="${CONDUCTOR_HOME:-$HOME/.claude-conductor}"
BIN_DIR="$CONDUCTOR_HOME/bin"

RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[0;33m'
BOLD='\033[1m'
NC='\033[0m'

say()  { printf '%b\n' "$*"; }
fail() { printf '%b\n' "${RED}✗${NC} $*" >&2; exit 1; }

say "${BOLD}mdev インストーラ${NC}"
say ""

# --- 1. 依存の確認 ---------------------------------------------------------
# ここで見るのは取得に要るものだけである。エージェント CLI などの確認は
# `mdev install` が行う(判断を 1 か所に集める)。
for cmd in curl shasum; do
    command -v "$cmd" >/dev/null 2>&1 || fail "$cmd が必要です"
done
if ! command -v zellij >/dev/null 2>&1; then
    say "  ${YELLOW}!${NC} zellij が見つかりません。セッションを開くには必要です:"
    say "      brew install zellij"
fi

# --- 2. 環境の判定 ---------------------------------------------------------
os="$(uname -s)"
arch="$(uname -m)"
[ "$os" = "Darwin" ] || fail "今は macOS 向けのバイナリだけを配っています(uname -s = $os)"
case "$arch" in
    arm64)  asset="mdev_darwin_arm64" ;;
    x86_64) asset="mdev_darwin_amd64" ;;
    *)      fail "対応していない CPU です: $arch" ;;
esac

# --- 3. 版の決定 -----------------------------------------------------------
# GitHub の API ではなく、リダイレクト先の URL から版を読む。API は未認証だと
# 回数制限が厳しく、インストールの初回で弾かれることがある。
VERSION="${MDEV_VERSION:-}"
if [ -z "$VERSION" ]; then
    latest_url=$(curl -fsSLI -o /dev/null -w '%{url_effective}' \
        "https://github.com/$REPO/releases/latest" 2>/dev/null || true)
    VERSION="${latest_url##*/}"
fi
case "$VERSION" in
    v*) : ;;
    *)  fail "最新の版を判定できませんでした(MDEV_VERSION で指定できます)" ;;
esac
say "  ${GREEN}✓${NC} 版: $VERSION ($asset)"

# --- 4. 取得と検証 ---------------------------------------------------------
work="$(mktemp -d)"
trap 'rm -rf "$work"' EXIT

base="https://github.com/$REPO/releases/download/$VERSION"
curl -fsSL -o "$work/$asset" "$base/$asset" \
    || fail "バイナリを取得できませんでした: $base/$asset"
curl -fsSL -o "$work/checksums.txt" "$base/checksums.txt" \
    || fail "checksums.txt を取得できませんでした"

# checksums.txt は dist の中で作られており、名前だけが載っている。
expected=$(awk -v name="$asset" '$2 == name { print $1 }' "$work/checksums.txt")
[ -n "$expected" ] || fail "checksums.txt に $asset がありません"
actual=$(shasum -a 256 "$work/$asset" | awk '{print $1}')
[ "$expected" = "$actual" ] || fail "ダウンロードしたバイナリが checksums.txt と一致しません"
say "  ${GREEN}✓${NC} SHA-256 を照合しました"

# --- 5. 配置 ---------------------------------------------------------------
# 同じディレクトリへ一時名で置いてから rename する。実行中のバイナリを
# 直接書き換えることはできないが、rename なら安全に置き換わる。
mkdir -p "$BIN_DIR"
chmod +x "$work/$asset"
mv "$work/$asset" "$BIN_DIR/.mdev.new"
mv "$BIN_DIR/.mdev.new" "$BIN_DIR/mdev"
# ブラウザ経由ではないので隔離属性は付かないはずだが、付いていれば外す。
xattr -d com.apple.quarantine "$BIN_DIR/mdev" 2>/dev/null || true
say "  ${GREEN}✓${NC} $BIN_DIR/mdev へ配置しました"
say ""

# --- 6. 本体へ引き継ぐ -----------------------------------------------------
# 以降の設定はすべて mdev install が行う。exec で置き換えるのは、ここから先の
# 出力と終了コードをそのまま利用者へ返すためである。
exec "$BIN_DIR/mdev" install
