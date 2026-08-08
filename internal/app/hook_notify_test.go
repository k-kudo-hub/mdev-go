package app_test

import (
	"errors"
	"reflect"
	"testing"

	"github.com/k-kudo-hub/mdev-go/internal/app"
	"github.com/k-kudo-hub/mdev-go/internal/domain"
)

// newHandler は fake で組んだ HookHandler と、その fake を返す。
func newHandler() (*app.HookHandler, *fakePendingStore, *fakeRegistryStore, *fakeFocuser) {
	pending := newFakePendingStore()
	registry := &fakeRegistryStore{}
	focuser := &fakeFocuser{}
	h := &app.HookHandler{
		Pending:  pending,
		Registry: registry,
		Focuser:  focuser,
		Clock:    testClock,
	}
	return h, pending, registry, focuser
}

func TestHandleNotifyWritesPending(t *testing.T) {
	t.Parallel()

	h, pending, registry, focuser := newHandler()
	raw := []byte(`{"session_id":"sess-aaa","message":"Permission needed","hook_event_name":"Notification","cwd":"/tmp/myapp","transcript_path":"/tmp/t.jsonl"}`)
	env := app.HookEnv{ZellijSession: "test-session", TaskTabName: "api-feature", TaskType: "dev"}

	if err := h.HandleNotify(raw, env); err != nil {
		t.Fatalf("HandleNotify() = %v", err)
	}

	got, ok := pending.saved[pendingKey("test-session", "sess-aaa")]
	if !ok {
		t.Fatalf("pending が保存されていない: %v", pending.saved)
	}
	want := domain.Pending{
		Tab:             "api-feature",
		Session:         "test-session",
		ClaudeSessionID: "sess-aaa",
		Message:         "Permission needed",
		Event:           "Notification",
		Time:            "10:11:12",
		Agent:           "claude",
		TranscriptPath:  "/tmp/t.jsonl",
		Dir:             "/tmp/myapp",
		TaskType:        "dev",
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("pending = %+v, want %+v", got, want)
	}

	wantEntry := domain.RegistryEntry{
		Tab:             "api-feature",
		Session:         "test-session",
		ClaudeSessionID: "sess-aaa",
		UpdatedAt:       "2026-08-08T10:11:12+0900",
		Dir:             "/tmp/myapp",
		TaskType:        "dev",
		Agent:           "claude",
		TranscriptPath:  "/tmp/t.jsonl",
	}
	if !reflect.DeepEqual(registry.upserted, []domain.RegistryEntry{wantEntry}) {
		t.Errorf("registry = %+v, want %+v", registry.upserted, []domain.RegistryEntry{wantEntry})
	}

	// Notification/Stop hook はフォーカスを動かさない。
	if len(focuser.focused) != 0 {
		t.Errorf("focused = %v, want フォーカス移動なし", focuser.focused)
	}
}

func TestHandleNotifyUsesDefaults(t *testing.T) {
	t.Parallel()

	h, pending, registry, _ := newHandler()
	// session_id 以外を持たない入力。zellij 外・タスクタブ外でもある。
	if err := h.HandleNotify([]byte(`{"session_id":"sess-bare"}`), app.HookEnv{}); err != nil {
		t.Fatalf("HandleNotify() = %v", err)
	}

	got := pending.saved[pendingKey("unknown", "sess-bare")]
	want := domain.Pending{
		Tab:             "unknown",
		Session:         "unknown",
		ClaudeSessionID: "sess-bare",
		Message:         "Needs attention",
		Event:           "unknown",
		Time:            "10:11:12",
		Agent:           "claude",
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("pending = %+v, want %+v", got, want)
	}
	if len(registry.upserted) != 0 {
		t.Errorf("registry = %+v, want 登録なし", registry.upserted)
	}
}

func TestHandleNotifyRecordsTaskAgent(t *testing.T) {
	t.Parallel()

	h, pending, registry, _ := newHandler()
	env := app.HookEnv{ZellijSession: "s", TaskTabName: "t", TaskAgent: "codex"}
	if err := h.HandleNotify([]byte(`{"session_id":"sid","hook_event_name":"Stop"}`), env); err != nil {
		t.Fatalf("HandleNotify() = %v", err)
	}
	if got := pending.saved[pendingKey("s", "sid")].Agent; got != "codex" {
		t.Errorf("pending.agent = %q, want codex", got)
	}
	if got := registry.upserted[0].Agent; got != "codex" {
		t.Errorf("registry.agent = %q, want codex", got)
	}
}

func TestHandleNotifyNoopWithoutSessionID(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		raw  string
	}{
		{name: "session_id が無い", raw: `{"message":"no session id"}`},
		{name: "session_id が空文字", raw: `{"session_id":""}`},
		{name: "壊れた JSON", raw: `{broken`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			h, pending, registry, _ := newHandler()
			env := app.HookEnv{ZellijSession: "s", TaskTabName: "t"}
			if err := h.HandleNotify([]byte(tt.raw), env); err != nil {
				t.Fatalf("HandleNotify() = %v", err)
			}
			if len(pending.saved) != 0 {
				t.Errorf("pending = %+v, want 書き込みなし", pending.saved)
			}
			if len(registry.upserted) != 0 {
				t.Errorf("registry = %+v, want 登録なし", registry.upserted)
			}
		})
	}
}

func TestHandleNotifyRegistryGuard(t *testing.T) {
	t.Parallel()

	// hook は同一マシンの全 Claude Code セッションで発火するため、
	// conductor が作ったタスクタブ以外はレジストリに登録しない。
	tests := []struct {
		name       string
		env        app.HookEnv
		wantUpsert bool
	}{
		{name: "両方あれば登録", env: app.HookEnv{ZellijSession: "s", TaskTabName: "t"}, wantUpsert: true},
		{name: "TASK_TAB_NAME が無ければ登録しない", env: app.HookEnv{ZellijSession: "s"}, wantUpsert: false},
		{name: "zellij 外なら登録しない", env: app.HookEnv{TaskTabName: "t"}, wantUpsert: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			h, _, registry, _ := newHandler()
			if err := h.HandleNotify([]byte(`{"session_id":"sid","cwd":"/tmp/myapp"}`), tt.env); err != nil {
				t.Fatalf("HandleNotify() = %v", err)
			}
			if got := len(registry.upserted) > 0; got != tt.wantUpsert {
				t.Errorf("upsert された = %v, want %v (%+v)", got, tt.wantUpsert, registry.upserted)
			}
		})
	}
}

func TestHandleNotifyAppliesOverwriteRule(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		existing string
		event    string
		wantSave bool
	}{
		{name: "Notification は Waiting を潰す", existing: "Waiting", event: "Notification", wantSave: true},
		{name: "Notification は Notification を上書き", existing: "Notification", event: "Notification", wantSave: true},
		{name: "Stop は Notification を上書きしない", existing: "Notification", event: "Stop", wantSave: false},
		{name: "Stop は Waiting を上書きしない", existing: "Waiting", event: "Stop", wantSave: false},
		{name: "Stop は pending 無しなら書く", existing: "", event: "Stop", wantSave: true},
		{name: "Stop は壊れ JSON(event 空)を上書きする", existing: "", event: "Stop", wantSave: true},
		{name: "Stop は Stop を上書きする", existing: "Stop", event: "Stop", wantSave: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			h, pending, registry, _ := newHandler()
			pending.events[pendingKey("s", "sid")] = tt.existing

			raw := []byte(`{"session_id":"sid","hook_event_name":"` + tt.event + `"}`)
			env := app.HookEnv{ZellijSession: "s", TaskTabName: "t"}
			if err := h.HandleNotify(raw, env); err != nil {
				t.Fatalf("HandleNotify() = %v", err)
			}

			if got := len(pending.saved) > 0; got != tt.wantSave {
				t.Errorf("保存された = %v, want %v", got, tt.wantSave)
			}
			// 上書きを見送る場合でもレジストリ更新は先に行われる
			// (現行 pending-notify.sh は upsert を上書き判定より前に置いている)。
			if len(registry.upserted) != 1 {
				t.Errorf("registry = %+v, want 1 件", registry.upserted)
			}
		})
	}
}

func TestHandleNotifyPropagatesErrors(t *testing.T) {
	t.Parallel()

	wantErr := errors.New("書き込み失敗")
	env := app.HookEnv{ZellijSession: "s", TaskTabName: "t"}
	raw := []byte(`{"session_id":"sid"}`)

	t.Run("registry の失敗", func(t *testing.T) {
		t.Parallel()
		h, _, registry, _ := newHandler()
		registry.upsertErr = wantErr
		if err := h.HandleNotify(raw, env); !errors.Is(err, wantErr) {
			t.Errorf("HandleNotify() = %v, want %v", err, wantErr)
		}
	})

	t.Run("pending の失敗", func(t *testing.T) {
		t.Parallel()
		h, pending, _, _ := newHandler()
		pending.saveErr = wantErr
		if err := h.HandleNotify(raw, env); !errors.Is(err, wantErr) {
			t.Errorf("HandleNotify() = %v, want %v", err, wantErr)
		}
	})
}
