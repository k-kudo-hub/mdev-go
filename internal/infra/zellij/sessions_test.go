package zellij

import (
	"errors"
	"slices"
	"testing"
	"time"
)

// recordedCall は zellij へ渡した 1 回の呼び出しの記録である。
type recordedCall struct {
	name    string
	args    []string
	timeout time.Duration
}

// newTestSessionController は呼び出しを記録する SessionController を返す。
func newTestSessionController(out string, err error) (*SessionController, *[]recordedCall) {
	calls := &[]recordedCall{}
	c := &SessionController{
		output: func(timeout time.Duration, name string, args ...string) (string, error) {
			*calls = append(*calls, recordedCall{name: name, args: args, timeout: timeout})
			return out, err
		},
		run: func(timeout time.Duration, name string, args ...string) error {
			*calls = append(*calls, recordedCall{name: name, args: args, timeout: timeout})
			return err
		},
	}
	return c, calls
}

func TestSessionControllerCommands(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		call func(*SessionController)
		want []string
	}{
		{
			name: "list-sessions",
			call: func(c *SessionController) { _, _ = c.ListSessions() },
			want: []string{"list-sessions", "--no-formatting"},
		},
		{
			name: "list-clients はセッションを指定する",
			call: func(c *SessionController) { _, _ = c.ListClients("s1") },
			want: []string{"--session", "s1", "action", "list-clients"},
		},
		{
			name: "kill-session",
			call: func(c *SessionController) { _ = c.KillSession("s1") },
			want: []string{"kill-session", "s1"},
		},
		{
			// kill の直後は zellij から見てまだ動いているので --force が要る。
			name: "delete-session は --force を付ける",
			call: func(c *SessionController) { _ = c.DeleteSession("s1") },
			want: []string{"delete-session", "s1", "--force"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			c, calls := newTestSessionController("", nil)
			tt.call(c)

			if len(*calls) != 1 {
				t.Fatalf("呼び出し = %d 回, want 1", len(*calls))
			}
			got := (*calls)[0]
			if got.name != binaryName {
				t.Errorf("コマンド = %q, want %q", got.name, binaryName)
			}
			if !slices.Equal(got.args, tt.want) {
				t.Errorf("引数 = %v, want %v", got.args, tt.want)
			}
			// 掃除はセッションの起動前に走るため、必ず上限を持つ。
			if got.timeout != commandTimeout {
				t.Errorf("上限 = %v, want %v", got.timeout, commandTimeout)
			}
		})
	}
}

// TestListSessionsIgnoresErrorWhenOutputExists は「セッションが 1 つも無い」
// ときに zellij が非 0 で終わっても、出力が取れていれば成功として扱うことを
// 確かめる。掃除にとっては正常な状態である。
func TestListSessionsIgnoresErrorWhenOutputExists(t *testing.T) {
	t.Parallel()

	c, _ := newTestSessionController("No active zellij sessions found.\n", errors.New("exit status 1"))
	got, err := c.ListSessions()
	if err != nil {
		t.Fatalf("ListSessions() = %v", err)
	}
	if got == "" {
		t.Error("出力が捨てられています")
	}
}

// TestListSessionsReportsErrorWithoutOutput は出力も取れなかった場合に
// error を返すことを確かめる(掃除は判断材料が無ければ何もしない)。
func TestListSessionsReportsErrorWithoutOutput(t *testing.T) {
	t.Parallel()

	c, _ := newTestSessionController("", errors.New("zellij が無い"))
	if _, err := c.ListSessions(); err == nil {
		t.Error("error になりませんでした")
	}
}

// TestListClientsReportsError は list-clients の失敗が error として
// 伝わることを確かめる。呼び出し側はこれを「アタッチあり」に倒す。
func TestListClientsReportsError(t *testing.T) {
	t.Parallel()

	c, _ := newTestSessionController("", errors.New("session not found"))
	if _, err := c.ListClients("s1"); err == nil {
		t.Error("error になりませんでした")
	}
}
