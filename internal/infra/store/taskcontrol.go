package store

import (
	"os"

	"github.com/k-kudo-hub/mdev-go/internal/app"
)

var _ app.TaskControlLauncher = (*MdevBinaryStore)(nil)

// TaskControlCommand はタスクタブ下部の操作バーを起動するコマンドを返す。
//
// 現行 create_task は `bash $CONDUCTOR_HOME/scripts/task-control.sh <tab>` を
// 起動していた。Go 版は同じ位置で `mdev pane task-control <tab>` を起動する。
//
// # なぜ os.Executable を使うか(ADR D7-2)
//
// **今動いているバイナリが自分の子を起動する。** CONDUCTOR_HOME から組み立てて
// いたときは、`mdev test` で worktree のバイナリを動かしていても、操作バーだけが
// 設置済みのバイナリになりえた。同じセッションの中で 2 つの版が混ざると、
// 「切り替えたのに直っていない」の原因が特定できなくなる。
//
// hooks のコマンド文字列は今までどおり `${CONDUCTOR_HOME:-...}` の展開形の
// ままにする(ADR D7-3)。あちらは settings.json に残り続けるため、worktree の
// 絶対パスを焼き込むと、その worktree を消した後も hook が呼ばれ続ける。
// こちらはセッションが終われば消えるコマンド行なので、絶対パスでよい。
//
// 自分の場所を引けない場合だけ CONDUCTOR_HOME 配下へ落ちる。引けないのは
// 実行ファイルが消された後などで、そのときは設置済みのものが最善の推測になる。
//
// タブ名の手前に `--` を置くのは、`-` で始まる名前をフラグと解釈させない
// ためである。タブ名は利用者が自由に付けられるので、`-wip` のような名前でも
// 操作バーが起動しなければならない。
func (s *MdevBinaryStore) TaskControlCommand(tab string) []string {
	return []string{s.selfPath(), "pane", "task-control", "--", tab}
}

// selfPath は今動いているバイナリのパスを返す。
func (s *MdevBinaryStore) selfPath() string {
	if s.executable != nil {
		if path, err := s.executable(); err == nil && path != "" {
			return path
		}
	}
	path, _ := s.MdevBinary()
	return path
}

// osExecutable は今動いているバイナリの場所を返す(テストで差し替える)。
var osExecutable = os.Executable
