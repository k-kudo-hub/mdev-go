package domain

// InitZshScript はシェルへ流し込む定義である。
//
// `mdev init zsh` がこれを出し、入口(init.zsh)が eval する。**入口に中身を
// 置かないのが要点で**、機能を足しても入口も .zshrc も書き換えずに済む。
//
// mdev そのものは定義しない。PATH 上のバイナリが受ける。ここで同名の関数を
// 定義すると、バイナリを直に呼びたい場面で関数が横取りする。
//
// dev / zs / pending-clear は関数ではなく **エイリアス**にする。どれも引数を
// そのまま渡すだけなので関数にする理由が無く、`which dev` で正体が見える
// ぶん追いやすい。
const InitZshScript = `# mdev が出力するシェル定義(` + "`mdev init zsh`" + `)。編集しても次の起動で戻る。

alias zj='zellij'
alias zja='zellij attach'
alias zjl='zellij list-sessions'
alias zjk='zellij kill-session'

alias dev='mdev dev'
alias zs='mdev attach'
alias pending-clear='mdev pending clear'
`
