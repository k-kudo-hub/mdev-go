// Package zellij は zellij CLI の呼び出しを担当する。
// internal/app が定義する port の実装(adapter)である(ADR-0002)。
package zellij

import (
	"os/exec"

	"github.com/k-kudo-hub/mdev-go/internal/app"
)

// binaryName は呼び出す zellij の実行ファイル名。
const binaryName = "zellij"

// Focuser は zellij のタブフォーカスを移す app.Focuser の実装である。
type Focuser struct {
	// run はコマンドを実行する。テストで差し替える。
	run func(name string, args ...string) error
}

var _ app.Focuser = (*Focuser)(nil)

// NewFocuser は zellij コマンドを実行する Focuser を返す。
func NewFocuser() *Focuser {
	return &Focuser{run: runCommand}
}

// FocusTab は名前でタブにフォーカスを移す。
//
// 失敗しても error を返さない。zellij の外で hook が発火した場合や、
// 対象のタブが既に閉じられている場合にコマンドは失敗するが、いずれも
// hook の処理としては正常な経過である。現行 Shell 版も
// `zellij action go-to-tab-name "Main" 2>/dev/null` として無視している。
func (f *Focuser) FocusTab(name string) error {
	_ = f.run(binaryName, "action", "go-to-tab-name", name)
	return nil
}

// runCommand は実際に外部コマンドを実行する。
func runCommand(name string, args ...string) error {
	return exec.Command(name, args...).Run()
}
