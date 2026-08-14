package domain_test

import (
	"testing"

	"github.com/k-kudo-hub/mdev-go/internal/domain"
)

// TestParseCodexNotification は codex の notify JSON の解釈を確かめる。
//
// 期待値は現行 codex-notify.sh の jq 式(`.type // empty`、
// `."thread-id" // empty`、`.cwd // empty`、
// `."last-assistant-message" // "Task complete"`)に同じ入力を流して確認した。
func TestParseCodexNotification(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		raw  string
		want domain.CodexNotification
		ok   bool
	}{
		{
			name: "ターン完了を解釈する",
			raw: `{"type":"agent-turn-complete","thread-id":"th-1","cwd":"/w/repo",` +
				`"last-assistant-message":"直しました"}`,
			want: domain.CodexNotification{ThreadID: "th-1", Dir: "/w/repo", Message: "直しました"},
			ok:   true,
		},
		{
			// jq の `//` は null と false だけを偽と見るため、空文字は値として扱う。
			name: "last-assistant-message が無ければ既定の文言になる",
			raw:  `{"type":"agent-turn-complete","thread-id":"th-1","cwd":"/w"}`,
			want: domain.CodexNotification{ThreadID: "th-1", Dir: "/w", Message: "Task complete"},
			ok:   true,
		},
		{
			name: "last-assistant-message が null でも既定の文言になる",
			raw:  `{"type":"agent-turn-complete","thread-id":"th-1","last-assistant-message":null}`,
			want: domain.CodexNotification{ThreadID: "th-1", Message: "Task complete"},
			ok:   true,
		},
		{
			name: "空文字の last-assistant-message はそのまま使う",
			raw:  `{"type":"agent-turn-complete","thread-id":"th-1","last-assistant-message":""}`,
			want: domain.CodexNotification{ThreadID: "th-1", Message: ""},
			ok:   true,
		},
		{
			name: "cwd が無くても解釈できる",
			raw:  `{"type":"agent-turn-complete","thread-id":"th-1","last-assistant-message":"m"}`,
			want: domain.CodexNotification{ThreadID: "th-1", Message: "m"},
			ok:   true,
		},
		{name: "空の引数は捨てる", raw: ``},
		{name: "別の種別は捨てる", raw: `{"type":"session-start","thread-id":"th-1"}`},
		{name: "type が無ければ捨てる", raw: `{"thread-id":"th-1"}`},
		{name: "thread-id が空なら捨てる", raw: `{"type":"agent-turn-complete","thread-id":""}`},
		{name: "thread-id が無ければ捨てる", raw: `{"type":"agent-turn-complete"}`},
		{name: "壊れた JSON は捨てる", raw: `{"type":`},
		{name: "JSON でなければ捨てる", raw: `agent-turn-complete`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, ok := domain.ParseCodexNotification([]byte(tt.raw))
			if ok != tt.ok {
				t.Fatalf("ok = %v, want %v", ok, tt.ok)
			}
			if got != tt.want {
				t.Errorf("ParseCodexNotification = %#v, want %#v", got, tt.want)
			}
		})
	}
}

// TestShouldOverwriteCodexPending は上書き判定を確かめる。
//
// Claude Code 側と違って Notification を守らないのが要点である
// (現行 codex-notify.sh は Waiting しか見ていない)。
func TestShouldOverwriteCodexPending(t *testing.T) {
	t.Parallel()

	tests := []struct {
		existing string
		want     bool
	}{
		{existing: "", want: true},
		{existing: domain.EventStop, want: true},
		{existing: domain.EventNotification, want: true},
		{existing: domain.EventUnknown, want: true},
		{existing: domain.EventWaiting, want: false},
	}
	for _, tt := range tests {
		t.Run("existing="+tt.existing, func(t *testing.T) {
			t.Parallel()
			if got := domain.ShouldOverwriteCodexPending(tt.existing); got != tt.want {
				t.Errorf("ShouldOverwriteCodexPending(%q) = %v, want %v", tt.existing, got, tt.want)
			}
		})
	}
}
