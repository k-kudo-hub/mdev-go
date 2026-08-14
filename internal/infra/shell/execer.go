package shell

import (
	"fmt"
	"os"
	"os/exec"
	"syscall"

	"github.com/k-kudo-hub/mdev-go/internal/app"
)

// Execer は今のプロセスを別のコマンドへ置き換える app.ProcessExecer の
// 実装である。
type Execer struct {
	// lookPath はコマンドの所在を調べる。テストで差し替える。
	lookPath func(string) (string, error)
	// exec はプロセスを置き換える。成功すれば戻らない。テストで差し替える。
	exec func(path string, argv []string, env []string) error
}

var _ app.ProcessExecer = (*Execer)(nil)

// NewExecer は実際にプロセスを置き換える Execer を返す。
func NewExecer() *Execer {
	return &Execer{lookPath: exec.LookPath, exec: syscall.Exec}
}

// Exec は command を実行し、成功すれば **戻らない**。
//
// 子プロセスを起こして待つのではなく execve でプロセスそのものを置き換える
// (現行 agent-launch.sh の `exec "${cmd[@]}"` と同じ)。zellij のペインは
// このプロセスを見ているので、間に 1 段挟むと、ペインを閉じたときの signal が
// エージェントへ届かなくなり、要らない中継役がエージェントの生存期間ずっと
// 居座る。
//
// 環境変数は今のプロセスのものをそのまま引き継ぐ。タスクタブの
// TASK_TAB_NAME などは、この経路ではなく zellij の new-tab が設定する。
func (e *Execer) Exec(command []string) error {
	if len(command) == 0 {
		return fmt.Errorf("実行するコマンドがありません")
	}
	path, err := e.lookPath(command[0])
	if err != nil {
		return fmt.Errorf("%s が見つかりません: %w", command[0], err)
	}
	// argv[0] にはコマンド名を渡す。解決後の絶対パスではなく利用者が書いた
	// 名前を渡すのは、シェルの exec と同じ見え方にするためである
	// (ps の表示や、エージェント自身が argv[0] を見る場合に効く)。
	argv := append([]string{command[0]}, command[1:]...)
	return e.exec(path, argv, os.Environ())
}
