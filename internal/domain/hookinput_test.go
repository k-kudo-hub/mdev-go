package domain_test

import (
	"testing"

	"github.com/k-kudo-hub/mdev-go/internal/domain"
)

func TestResolveTabName(t *testing.T) {
	t.Parallel()

	// 現行 pending-notify.sh:16-19 の連鎖。
	//   TAB_NAME="${TASK_TAB_NAME:-$(basename "$(... .cwd // empty ...)")}"
	//   [ -z "$TAB_NAME" ] && TAB_NAME="unknown"
	// `basename ""` は空文字を返すため、cwd が無いときだけ "unknown" になる。
	tests := []struct {
		name        string
		taskTabName string
		cwd         string
		want        string
	}{
		{name: "TASK_TAB_NAME を最優先", taskTabName: "api-feature", cwd: "/tmp/myapp", want: "api-feature"},
		{name: "TASK_TAB_NAME が空なら cwd の basename", taskTabName: "", cwd: "/tmp/myapp", want: "myapp"},
		{name: "末尾スラッシュ付きでも basename", taskTabName: "", cwd: "/tmp/myapp/", want: "myapp"},
		{name: "相対パスでも basename", taskTabName: "", cwd: "myapp", want: "myapp"},
		{name: "ルートは basename と同じく /", taskTabName: "", cwd: "/", want: "/"},
		{name: "カレントは basename と同じく .", taskTabName: "", cwd: ".", want: "."},
		{name: "両方空なら unknown", taskTabName: "", cwd: "", want: "unknown"},
		{name: "TASK_TAB_NAME があれば cwd 無しでも使う", taskTabName: "review-pr", cwd: "", want: "review-pr"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := domain.ResolveTabName(tt.taskTabName, tt.cwd)
			if got != tt.want {
				t.Errorf("ResolveTabName(%q, %q) = %q, want %q", tt.taskTabName, tt.cwd, got, tt.want)
			}
		})
	}
}

func TestParseHookInput(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		raw  string
		want domain.HookInput
	}{
		{
			name: "Notification の代表的な入力",
			raw:  `{"session_id":"sess-aaa","message":"Permission needed","hook_event_name":"Notification","cwd":"/tmp/myapp","transcript_path":"/tmp/t.jsonl"}`,
			want: domain.HookInput{
				SessionID:      "sess-aaa",
				Message:        "Permission needed",
				HookEventName:  "Notification",
				Cwd:            "/tmp/myapp",
				TranscriptPath: "/tmp/t.jsonl",
			},
		},
		{
			name: "message が無ければ既定文言",
			raw:  `{"session_id":"sess-aaa","hook_event_name":"Stop"}`,
			want: domain.HookInput{SessionID: "sess-aaa", Message: "Needs attention", HookEventName: "Stop"},
		},
		{
			name: "hook_event_name が無ければ unknown",
			raw:  `{"session_id":"sess-aaa"}`,
			want: domain.HookInput{SessionID: "sess-aaa", Message: "Needs attention", HookEventName: "unknown"},
		},
		{
			// jq の `//` は null と false にしか作用しないため、
			// 明示的な空文字は既定値に置き換わらない。
			name: "message が空文字なら空文字のまま",
			raw:  `{"session_id":"sess-aaa","message":"","hook_event_name":"Stop"}`,
			want: domain.HookInput{SessionID: "sess-aaa", Message: "", HookEventName: "Stop"},
		},
		{
			name: "null は既定値に置き換わる",
			raw:  `{"session_id":"sess-aaa","message":null,"hook_event_name":null,"cwd":null,"transcript_path":null}`,
			want: domain.HookInput{SessionID: "sess-aaa", Message: "Needs attention", HookEventName: "unknown"},
		},
		{
			name: "false も jq と同じく既定値に置き換わる",
			raw:  `{"session_id":false,"message":false}`,
			want: domain.HookInput{SessionID: "", Message: "Needs attention", HookEventName: "unknown"},
		},
		{
			// jq -r は文字列以外をそのまま出力する。session_id が数値でも
			// 現行版は "123" として扱う。
			name: "文字列以外は jq -r と同じく JSON 表記",
			raw:  `{"session_id":123,"hook_event_name":true}`,
			want: domain.HookInput{SessionID: "123", Message: "Needs attention", HookEventName: "true"},
		},
		{
			name: "壊れた JSON は全項目が既定値",
			raw:  `{not json`,
			want: domain.HookInput{Message: "Needs attention", HookEventName: "unknown"},
		},
		{
			name: "オブジェクト以外の JSON も既定値",
			raw:  `["a"]`,
			want: domain.HookInput{Message: "Needs attention", HookEventName: "unknown"},
		},
		{
			name: "空入力も既定値",
			raw:  ``,
			want: domain.HookInput{Message: "Needs attention", HookEventName: "unknown"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := domain.ParseHookInput([]byte(tt.raw))
			if got != tt.want {
				t.Errorf("ParseHookInput(%s) = %+v, want %+v", tt.raw, got, tt.want)
			}
		})
	}
}
