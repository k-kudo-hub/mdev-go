package domain_test

import (
	"encoding/json"
	"reflect"
	"testing"

	"github.com/k-kudo-hub/mdev-go/internal/domain"
)

// decode は JSON をキーと値のマップに戻す。キー順は比較対象にしない。
func decode(t *testing.T, b []byte) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatalf("Unmarshal(%s) = %v", b, err)
	}
	return m
}

func TestPendingMarshalOmitsEmptyOptionalFields(t *testing.T) {
	t.Parallel()

	// 現行 pending-notify.sh は transcript_path / dir / task_type が空のとき
	// キーごと出力しない。prev_event は waiting-toggle.sh が付け外しする。
	p := domain.Pending{
		Tab:             "api-feature",
		Session:         "test-session",
		ClaudeSessionID: "sess-aaa",
		Message:         "Permission needed",
		Event:           domain.EventNotification,
		Time:            "10:11:12",
		Agent:           "claude",
	}

	b, err := json.Marshal(p)
	if err != nil {
		t.Fatalf("Marshal() = %v", err)
	}
	got := decode(t, b)

	want := map[string]any{
		"tab":               "api-feature",
		"session":           "test-session",
		"claude_session_id": "sess-aaa",
		"message":           "Permission needed",
		"event":             "Notification",
		"time":              "10:11:12",
		"agent":             "claude",
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Marshal() = %v, want %v", got, want)
	}
}

func TestPendingMarshalKeepsFilledOptionalFields(t *testing.T) {
	t.Parallel()

	p := domain.Pending{
		Tab:             "api-feature",
		Session:         "test-session",
		ClaudeSessionID: "sess-aaa",
		Message:         "Task done",
		Event:           domain.EventWaiting,
		Time:            "10:11:12",
		Agent:           "codex",
		TranscriptPath:  "/tmp/t1.jsonl",
		Dir:             "/tmp/myapp",
		TaskType:        "dev",
		PrevEvent:       domain.EventStop,
	}

	b, err := json.Marshal(p)
	if err != nil {
		t.Fatalf("Marshal() = %v", err)
	}
	got := decode(t, b)

	want := map[string]any{
		"tab":               "api-feature",
		"session":           "test-session",
		"claude_session_id": "sess-aaa",
		"message":           "Task done",
		"event":             "Waiting",
		"time":              "10:11:12",
		"agent":             "codex",
		"transcript_path":   "/tmp/t1.jsonl",
		"dir":               "/tmp/myapp",
		"task_type":         "dev",
		"prev_event":        "Stop",
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Marshal() = %v, want %v", got, want)
	}
}

func TestPendingRoundTrip(t *testing.T) {
	t.Parallel()

	// 現行版が書いたファイルを読み直しても内容が失われないこと。
	// 全フィールドが string であることの確認も兼ねる。
	raw := []byte(`{
	  "tab": "api-feature",
	  "session": "test-session",
	  "claude_session_id": "sess-aaa",
	  "message": "waiting for review",
	  "event": "Waiting",
	  "time": "10:00:00",
	  "agent": "claude",
	  "transcript_path": "/tmp/t.jsonl",
	  "dir": "/tmp/myapp",
	  "task_type": "dev",
	  "prev_event": "Notification"
	}`)

	var p domain.Pending
	if err := json.Unmarshal(raw, &p); err != nil {
		t.Fatalf("Unmarshal() = %v", err)
	}

	b, err := json.Marshal(p)
	if err != nil {
		t.Fatalf("Marshal() = %v", err)
	}
	if got, want := decode(t, b), decode(t, raw); !reflect.DeepEqual(got, want) {
		t.Errorf("round trip = %v, want %v", got, want)
	}
}

func TestSessionName(t *testing.T) {
	t.Parallel()

	// 現行版の `${ZELLIJ_SESSION_NAME:-unknown}`。
	tests := []struct {
		name          string
		zellijSession string
		want          string
	}{
		{name: "zellij セッション名をそのまま使う", zellijSession: "test-session", want: "test-session"},
		{name: "空なら unknown", zellijSession: "", want: "unknown"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := domain.SessionName(tt.zellijSession); got != tt.want {
				t.Errorf("SessionName(%q) = %q, want %q", tt.zellijSession, got, tt.want)
			}
		})
	}
}

func TestAgentName(t *testing.T) {
	t.Parallel()

	// 現行版の `${TASK_AGENT:-claude}`。
	tests := []struct {
		name      string
		taskAgent string
		want      string
	}{
		{name: "TASK_AGENT をそのまま使う", taskAgent: "codex", want: "codex"},
		{name: "空なら claude", taskAgent: "", want: "claude"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := domain.AgentName(tt.taskAgent); got != tt.want {
				t.Errorf("AgentName(%q) = %q, want %q", tt.taskAgent, got, tt.want)
			}
		})
	}
}

func TestPendingUnmarshalIgnoresUnknownKeys(t *testing.T) {
	t.Parallel()

	// 将来のフィールド追加や他ツールが付けたキーで読み取りが壊れないこと。
	var p domain.Pending
	if err := json.Unmarshal([]byte(`{"event":"Stop","future_key":42}`), &p); err != nil {
		t.Fatalf("Unmarshal() = %v", err)
	}
	if p.Event != domain.EventStop {
		t.Errorf("Event = %q, want %q", p.Event, domain.EventStop)
	}
}
