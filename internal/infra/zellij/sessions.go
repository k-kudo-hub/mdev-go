package zellij

import (
	"time"

	"github.com/k-kudo-hub/mdev-go/internal/app"
)

// SessionController は zellij のセッション操作を担当する。
//
// どの呼び出しにも commandTimeout(10 秒)の上限を付ける。掃除はセッションの
// 起動前に走るため、zellij が返らなくなると起動そのものが止まってしまう。
type SessionController struct {
	// output はコマンドの標準出力と実行の成否を返す。テストで差し替える。
	output func(timeout time.Duration, name string, args ...string) (string, error)
	// run はコマンドを実行する。テストで差し替える。
	run func(timeout time.Duration, name string, args ...string) error
}

var (
	_ app.SessionLister       = (*SessionController)(nil)
	_ app.SessionClientLister = (*SessionController)(nil)
	_ app.SessionRemover      = (*SessionController)(nil)
)

// NewSessionController は zellij コマンドを実行する SessionController を返す。
func NewSessionController() *SessionController {
	return &SessionController{output: commandOutput, run: runCommand}
}

// ListSessions は `zellij list-sessions --no-formatting` の出力を返す。
//
// セッションが 1 つも無いとき zellij は非 0 で終わる。出力が取れていれば
// それを返して error にしないのは、「1 つも無い」は掃除にとって正常な
// 状態だからである。
func (c *SessionController) ListSessions() (string, error) {
	out, err := c.output(commandTimeout, binaryName, "list-sessions", "--no-formatting")
	if out != "" {
		return out, nil
	}
	if err != nil {
		return "", err //nolint:wrapcheck // 呼び出し側が用途に応じて包む
	}
	return "", nil
}

// ListClients はセッションにアタッチしているクライアントの一覧を返す。
//
// 失敗をそのまま error として返す。呼び出し側は「アタッチあり」に倒して
// そのセッションへ触れない(誰も居ないと誤って判断すると使用中のセッションを
// kill するため)。
func (c *SessionController) ListClients(session string) (string, error) {
	out, err := c.output(commandTimeout, binaryName, "--session", session, "action", "list-clients")
	if err != nil {
		return "", err //nolint:wrapcheck // 呼び出し側が用途に応じて包む
	}
	return out, nil
}

// KillSession は動いているセッションを終了させる。
func (c *SessionController) KillSession(name string) error {
	return c.run(commandTimeout, binaryName, "kill-session", name)
}

// DeleteSession はセッションのメタデータを削除する。
//
// --force を付けるのは、動いているセッションに対しても確認を求めさせない
// ためである。kill の直後は zellij から見てまだ動いていることがある。
func (c *SessionController) DeleteSession(name string) error {
	return c.run(commandTimeout, binaryName, "delete-session", name, "--force")
}
