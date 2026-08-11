package store

import "github.com/k-kudo-hub/mdev-go/internal/app"

var _ app.TaskControlLauncher = (*MdevBinaryStore)(nil)

// TaskControlCommand はタスクタブ下部の操作バーを起動するコマンドを返す。
//
// 現行 create_task は `bash $CONDUCTOR_HOME/scripts/task-control.sh <tab>` を
// 起動していた。Go 版は同じ位置で `<CONDUCTOR_HOME>/bin/mdev pane task-control
// <tab>` を起動する。
//
// パスを CONDUCTOR_HOME から組み立てるのは、hooks のコマンド文字列と同じ規約に
// 揃えるためである。mdev-test のように CONDUCTOR_HOME を worktree へ向けて
// いる場合、そこで作ったタスクの操作バーも同じ worktree のバイナリになる。
//
// バイナリが実在するかは見ない。ここで確かめて別のものへ切り替えると、
// 「どの mdev が動いているか分からない」状態を作ってしまう。実在しなければ
// zellij がペインの起動に失敗し、その事実がそのまま画面に出る。
//
// タブ名の手前に `--` を置くのは、`-` で始まる名前をフラグと解釈させない
// ためである。タブ名は利用者が自由に付けられるので、`-wip` のような名前でも
// 操作バーが起動しなければならない。
func (s *MdevBinaryStore) TaskControlCommand(tab string) []string {
	path, _ := s.MdevBinary()
	return []string{path, "pane", "task-control", "--", tab}
}
