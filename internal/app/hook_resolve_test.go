package app_test

import (
	"errors"
	"reflect"
	"testing"

	"github.com/k-kudo-hub/mdev-go/internal/app"
	"github.com/k-kudo-hub/mdev-go/internal/domain"
)

func TestHandleResolveDeletesPendingRegardlessOfEvent(t *testing.T) {
	t.Parallel()

	// UserPromptSubmit はユーザーが次の指示を出したことを意味するので、
	// event を問わず pending を消す(Waiting の解除もこの経路)。
	for _, existing := range []string{"Notification", "Stop", "Waiting", "unknown", ""} {
		t.Run("既存 event="+existing, func(t *testing.T) {
			t.Parallel()
			h, pending, _, focuser := newHandler()
			pending.events[pendingKey("test-session", "sess-aaa")] = existing

			env := app.HookEnv{ZellijSession: "test-session"}
			if err := h.HandleResolve([]byte(`{"session_id":"sess-aaa"}`), env); err != nil {
				t.Fatalf("HandleResolve() = %v", err)
			}

			want := []string{pendingKey("test-session", "sess-aaa")}
			if !reflect.DeepEqual(pending.deleted, want) {
				t.Errorf("deleted = %v, want %v", pending.deleted, want)
			}
			// pending が無くても Main へ戻る。
			if !reflect.DeepEqual(focuser.focused, []string{"Main"}) {
				t.Errorf("focused = %v, want [Main]", focuser.focused)
			}
		})
	}
}

func TestHandleResolveUpsertsRegistry(t *testing.T) {
	t.Parallel()

	h, _, registry, _ := newHandler()
	raw := []byte(`{"session_id":"reg-hook-1","transcript_path":"/tmp/reg-t1b.jsonl","cwd":"/tmp/reg-dir1"}`)
	env := app.HookEnv{ZellijSession: "reg-hooks", TaskTabName: "reg-tab", TaskType: "dev", TaskAgent: "claude"}

	if err := h.HandleResolve(raw, env); err != nil {
		t.Fatalf("HandleResolve() = %v", err)
	}

	want := []domain.RegistryEntry{{
		Tab:             "reg-tab",
		Session:         "reg-hooks",
		ClaudeSessionID: "reg-hook-1",
		UpdatedAt:       "2026-08-08T10:11:12+0900",
		Dir:             "/tmp/reg-dir1",
		TaskType:        "dev",
		Agent:           "claude",
		TranscriptPath:  "/tmp/reg-t1b.jsonl",
	}}
	if !reflect.DeepEqual(registry.upserted, want) {
		t.Errorf("registry = %+v, want %+v", registry.upserted, want)
	}
}

func TestHandleResolveUsesTaskTabNameDirectly(t *testing.T) {
	t.Parallel()

	// notify と違い、resolve は cwd の basename へフォールバックしない
	// (レジストリ登録自体が TASK_TAB_NAME 非空を条件にしているため)。
	h, _, registry, _ := newHandler()
	raw := []byte(`{"session_id":"sid","cwd":"/tmp/myapp"}`)
	env := app.HookEnv{ZellijSession: "s", TaskTabName: "explicit-tab"}

	if err := h.HandleResolve(raw, env); err != nil {
		t.Fatalf("HandleResolve() = %v", err)
	}
	if got := registry.upserted[0].Tab; got != "explicit-tab" {
		t.Errorf("registry.tab = %q, want explicit-tab", got)
	}
}

func TestHandleResolveRegistryGuard(t *testing.T) {
	t.Parallel()

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
			h, pending, registry, focuser := newHandler()
			if err := h.HandleResolve([]byte(`{"session_id":"sid"}`), tt.env); err != nil {
				t.Fatalf("HandleResolve() = %v", err)
			}
			if got := len(registry.upserted) > 0; got != tt.wantUpsert {
				t.Errorf("upsert された = %v, want %v", got, tt.wantUpsert)
			}
			// レジストリ登録の有無にかかわらず pending は消す。
			if len(pending.deleted) != 1 {
				t.Errorf("deleted = %v, want 1 件", pending.deleted)
			}
			if got := len(focuser.focused) > 0; got != tt.env.InZellij() {
				t.Errorf("フォーカスした = %v, want %v", got, tt.env.InZellij())
			}
		})
	}
}

func TestHandleResolveNoopWithoutSessionID(t *testing.T) {
	t.Parallel()

	h, pending, registry, focuser := newHandler()
	env := app.HookEnv{ZellijSession: "s", TaskTabName: "t"}
	if err := h.HandleResolve([]byte(`{}`), env); err != nil {
		t.Fatalf("HandleResolve() = %v", err)
	}
	if len(pending.deleted) != 0 || len(registry.upserted) != 0 || len(focuser.focused) != 0 {
		t.Errorf("deleted = %v, registry = %v, focused = %v, want すべて空",
			pending.deleted, registry.upserted, focuser.focused)
	}
}

func TestHandleResolvePropagatesErrors(t *testing.T) {
	t.Parallel()

	wantErr := errors.New("失敗")
	env := app.HookEnv{ZellijSession: "s", TaskTabName: "t"}
	raw := []byte(`{"session_id":"sid"}`)

	t.Run("削除の失敗", func(t *testing.T) {
		t.Parallel()
		h, pending, registry, focuser := newHandler()
		pending.deleteErr = wantErr
		if err := h.HandleResolve(raw, env); !errors.Is(err, wantErr) {
			t.Errorf("HandleResolve() = %v, want %v", err, wantErr)
		}
		// 現行版は set -e を使っておらず、失敗しても後続の副作用へ進む。
		if len(registry.upserted) != 1 || len(focuser.focused) != 1 {
			t.Errorf("upserted = %v, focused = %v, want 削除に失敗しても後続を実行する",
				registry.upserted, focuser.focused)
		}
	})

	t.Run("registry の失敗", func(t *testing.T) {
		t.Parallel()
		h, _, registry, focuser := newHandler()
		registry.upsertErr = wantErr
		if err := h.HandleResolve(raw, env); !errors.Is(err, wantErr) {
			t.Errorf("HandleResolve() = %v, want %v", err, wantErr)
		}
		if len(focuser.focused) != 1 {
			t.Errorf("focused = %v, want レジストリに失敗してもフォーカス移動する", focuser.focused)
		}
	})

	t.Run("フォーカスの失敗", func(t *testing.T) {
		t.Parallel()
		h, _, _, focuser := newHandler()
		focuser.focusErr = wantErr
		if err := h.HandleResolve(raw, env); !errors.Is(err, wantErr) {
			t.Errorf("HandleResolve() = %v, want %v", err, wantErr)
		}
	})
}
