package shell

import (
	"os/exec"

	"github.com/k-kudo-hub/mdev-go/internal/app"
)

// CommandChecker は PATH 上にコマンドがあるかを調べる app.CommandChecker の
// 実装である。
type CommandChecker struct {
	// lookPath はコマンドの所在を調べる。テストで差し替える。
	lookPath func(string) (string, error)
}

var _ app.CommandChecker = CommandChecker{}

// NewCommandChecker は PATH を見る CommandChecker を返す。
func NewCommandChecker() CommandChecker {
	return CommandChecker{lookPath: exec.LookPath}
}

// Available は name が PATH にあるかを返す。
func (c CommandChecker) Available(name string) bool {
	_, err := c.lookPath(name)
	return err == nil
}
