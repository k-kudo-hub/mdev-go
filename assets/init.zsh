# Claude Conductor - シェルの入口
#
# .zshrc からこの 1 行で読み込む:
#   source "$HOME/.claude-conductor/init.zsh"
#
# ここは PATH を通して mdev へ橋渡しするだけの入口である。エイリアスと
# 関数の中身は `mdev init zsh` が出力する。**この形にしておけば、機能を
# 足しても .zshrc も入口も書き換えずに済む。**
#
# mdev(セッションの起動)は関数ではなく PATH 上のバイナリが受ける。ここで
# 同名の関数を定義すると、バイナリを直に呼びたい場面で関数が横取りする。

export CONDUCTOR_HOME="${CONDUCTOR_HOME:-$HOME/.claude-conductor}"

# 既に通っていれば足さない(source を繰り返しても PATH が伸び続けない)。
if [[ ":$PATH:" != *":$CONDUCTOR_HOME/bin:"* ]]; then
    export PATH="$CONDUCTOR_HOME/bin:$PATH"
fi

# バイナリが無い状態でも .zshrc は落ちてはならない(更新の途中や、
# インストール前に .zshrc だけ用意した場合)。
if [[ -x "$CONDUCTOR_HOME/bin/mdev" ]]; then
    eval "$("$CONDUCTOR_HOME/bin/mdev" init zsh)"
fi
