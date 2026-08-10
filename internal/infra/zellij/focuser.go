// Package zellij は zellij CLI の呼び出しを担当する。
// internal/app が定義する port の実装(adapter)である(ADR-0002)。
package zellij

import (
	"context"
	"time"

	"github.com/k-kudo-hub/mdev-go/internal/app"
	"github.com/k-kudo-hub/mdev-go/internal/infra/proc"
)

// binaryName は呼び出す zellij の実行ファイル名。
const binaryName = "zellij"

// commandTimeout は zellij CLI 1 回の実行時間の上限である。
//
// zellij サーバが劣化すると CLI が返らなくなることがある。ダッシュボードの
// 読み直しは着弾して初めて次の合図を張るため(完了起点。internal/tui の
// poller を参照)、返らない呼び出しはポーリングをそこで止めてしまう。上限を
// 付けておけば、その回が失敗するだけでポーリングは回り続ける。
//
// list-tabs はタブ名を並べるだけで、正常時はミリ秒で返る。10 秒は「異常だと
// 判断してよい」ところに置いた値である。
const commandTimeout = 10 * time.Second

// Focuser は zellij のタブフォーカスを移す app.Focuser の実装である。
type Focuser struct {
	// run はコマンドを実行する。テストで差し替える。
	run func(name string, args ...string) error
}

var _ app.Focuser = (*Focuser)(nil)

// NewFocuser は zellij コマンドを実行する Focuser を返す。
func NewFocuser() *Focuser {
	return &Focuser{run: withTimeout(commandTimeout)}
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

// withTimeout は上限付きでコマンドを実行する関数を返す。
func withTimeout(timeout time.Duration) func(name string, args ...string) error {
	return func(name string, args ...string) error { return runCommand(timeout, name, args...) }
}

// runCommand は実際に外部コマンドを実行する。
// timeout が正の値なら、その時間でプロセスグループごと切る
// (直接の子だけを切ると孫が残る。internal/infra/proc を参照)。
func runCommand(timeout time.Duration, name string, args ...string) error {
	ctx, cancel := commandContext(timeout)
	defer cancel()
	return proc.Command(ctx, name, args...).Run()
}

// commandContext は上限付きの context を返す。timeout が 0 以下なら上限なし。
func commandContext(timeout time.Duration) (context.Context, context.CancelFunc) {
	if timeout <= 0 {
		return context.WithCancel(context.Background())
	}
	return context.WithTimeout(context.Background(), timeout)
}
