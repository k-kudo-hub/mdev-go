package app_test

import (
	"testing"

	"github.com/k-kudo-hub/mdev-go/internal/app"
)

func TestHookEnvIsTaskTab(t *testing.T) {
	t.Parallel()

	// 現行版の `[ -n "$ZELLIJ_SESSION_NAME" ] && [ -n "$TASK_TAB_NAME" ]`。
	tests := []struct {
		name string
		env  app.HookEnv
		want bool
	}{
		{name: "両方あればタスクタブ", env: app.HookEnv{ZellijSession: "s", TaskTabName: "t"}, want: true},
		{name: "TASK_TAB_NAME が無い(conductor 外のセッション)", env: app.HookEnv{ZellijSession: "s"}, want: false},
		{name: "zellij 外", env: app.HookEnv{TaskTabName: "t"}, want: false},
		{name: "どちらも無い", env: app.HookEnv{}, want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := tt.env.IsTaskTab(); got != tt.want {
				t.Errorf("IsTaskTab() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestHookEnvInZellij(t *testing.T) {
	t.Parallel()

	if !(app.HookEnv{ZellijSession: "s"}).InZellij() {
		t.Error("ZELLIJ_SESSION_NAME があるのに InZellij() = false")
	}
	if (app.HookEnv{TaskTabName: "t"}).InZellij() {
		t.Error("ZELLIJ_SESSION_NAME が無いのに InZellij() = true")
	}
}

// fake が port を満たし、テストから副作用を観測できることを確認する。
func TestFakesRecordCalls(t *testing.T) {
	t.Parallel()

	pending := newFakePendingStore()
	pending.events[pendingKey("sess", "sid")] = "Notification"
	if got := pending.Event("sess", "sid"); got != "Notification" {
		t.Errorf("Event() = %q, want %q", got, "Notification")
	}
	if got := pending.Event("sess", "other"); got != "" {
		t.Errorf("未登録の Event() = %q, want 空文字", got)
	}
	if err := pending.Delete("sess", "sid"); err != nil {
		t.Fatalf("Delete() = %v", err)
	}
	if len(pending.deleted) != 1 {
		t.Errorf("deleted = %v, want 1 件", pending.deleted)
	}

	registry := &fakeRegistryStore{}
	if err := registry.Upsert(newTestRegistryEntry()); err != nil {
		t.Fatalf("Upsert() = %v", err)
	}
	if len(registry.upserted) != 1 {
		t.Errorf("upserted = %v, want 1 件", registry.upserted)
	}

	focuser := &fakeFocuser{}
	if err := focuser.FocusTab("Main"); err != nil {
		t.Fatalf("FocusTab() = %v", err)
	}
	if len(focuser.focused) != 1 || focuser.focused[0] != "Main" {
		t.Errorf("focused = %v, want [Main]", focuser.focused)
	}

	if got := testClock.Now().Format("2006-01-02T15:04:05-0700"); got != "2026-08-08T10:11:12+0900" {
		t.Errorf("Clock.Now() = %s, want 2026-08-08T10:11:12+0900", got)
	}
}
