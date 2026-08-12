package zellij

import (
	"errors"
	"slices"
	"testing"
	"time"
)

// newZeroSessionController は標準出力・標準エラーを指定できる
// SessionController を返す。
func newZeroSessionController(stdout, stderr string, err error) (*SessionController, *[]recordedCall) {
	calls := &[]recordedCall{}
	c := NewSessionController()
	c.outputBoth = func(timeout time.Duration, name string, args ...string) (string, string, error) {
		*calls = append(*calls, recordedCall{name: name, args: args, timeout: timeout})
		return stdout, stderr, err
	}
	return c, calls
}

// TestListSessionsTreatsZeroSessionsAsEmpty は「セッションが 1 つも無い」を
// 失敗にしないことを確かめる。
//
// zellij はこのとき rc=1・標準出力は空・標準エラーへ
// 「No active zellij sessions found.」を出す(実機で確認)。error にすると
// セッションが無い環境で掃除全体が止まり、動いているゾンビサーバや孤児
// プロセスを片付けられない。
func TestListSessionsTreatsZeroSessionsAsEmpty(t *testing.T) {
	t.Parallel()

	c, _ := newZeroSessionController("", "No active zellij sessions found.\n", errors.New("exit status 1"))
	got, err := c.ListSessions()
	if err != nil {
		t.Fatalf("ListSessions() = %v(0 件は失敗ではない)", err)
	}
	if got != "" {
		t.Errorf("出力 = %q, want 空", got)
	}
}

// TestListSessionsReportsOtherFailures は 0 件以外の失敗を error として
// 返すことを確かめる。
//
// **どんな失敗も 0 件と読んではならない。** 0 件とみなすと、生きている
// セッションのサーバがすべて「一覧に出ないサーバ」= ゾンビに見えてしまい、
// 掃除が使用中のセッションを落とす。
func TestListSessionsReportsOtherFailures(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name           string
		stdout, stderr string
	}{
		{name: "別の失敗", stderr: "error: could not connect\n"},
		{name: "何も出ない失敗", stderr: ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			c, _ := newZeroSessionController(tt.stdout, tt.stderr, errors.New("exit status 2"))
			if _, err := c.ListSessions(); err == nil {
				t.Error("error になりませんでした(0 件と取り違えている)")
			}
		})
	}
}

// TestListSessionsReturnsOutputOnSuccess は通常の成功経路を確かめる。
func TestListSessionsReturnsOutputOnSuccess(t *testing.T) {
	t.Parallel()

	const out = "s [Created 1m 0s ago] \n"
	c, calls := newZeroSessionController(out, "", nil)
	got, err := c.ListSessions()
	if err != nil {
		t.Fatalf("ListSessions() = %v", err)
	}
	if got != out {
		t.Errorf("出力 = %q, want %q", got, out)
	}
	if want := []string{"list-sessions", "--no-formatting"}; !slices.Equal((*calls)[0].args, want) {
		t.Errorf("引数 = %v, want %v", (*calls)[0].args, want)
	}
}

// TestIsAttachedFallsBackToAttached は判断できない応答を「開いている」と
// 扱うことを確かめる。
//
// 誰も居ないと誤ると、実際に見ている画面のポーリングが落ちて固まって見える。
func TestIsAttachedFallsBackToAttached(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		out  string
		err  error
		want bool
	}{
		{name: "クライアント 1 つ", out: "CLIENT_ID X Y\n1 a b\n", want: true},
		{name: "見出しだけ = detached", out: "CLIENT_ID X Y\n", want: false},
		// 見出しが無い応答は判断できない。
		{name: "空の応答", out: "", want: true},
		{name: "形の違う応答", out: "なにかおかしい\n", want: true},
		{name: "失敗", err: errors.New("応答しない"), want: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			c := NewSessionController()
			c.output = func(time.Duration, string, ...string) (string, error) { return tt.out, tt.err }
			if got := c.IsAttached("s1"); got != tt.want {
				t.Errorf("IsAttached = %v, want %v", got, tt.want)
			}
		})
	}
}
