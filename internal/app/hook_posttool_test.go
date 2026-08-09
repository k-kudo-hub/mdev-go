package app_test

import (
	"errors"
	"reflect"
	"testing"

	"github.com/k-kudo-hub/mdev-go/internal/app"
)

func TestHandlePostToolResolvesNotificationOnly(t *testing.T) {
	t.Parallel()

	// PostToolUse は「許可を承認したら Notification が解決した」ことだけを扱う。
	// Stop の pending は UserPromptSubmit が解決するので触らない。
	tests := []struct {
		name        string
		existing    string
		wantDeleted bool
	}{
		{name: "Notification は削除する", existing: "Notification", wantDeleted: true},
		{name: "Stop は削除しない", existing: "Stop", wantDeleted: false},
		{name: "Waiting は削除しない", existing: "Waiting", wantDeleted: false},
		{name: "pending 無しは何もしない", existing: "", wantDeleted: false},
		{name: "壊れ JSON(event 空)も削除しない", existing: "", wantDeleted: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			h, pending, registry, focuser := newHandler()
			pending.events[pendingKey("test-session", "sess-aaa")] = tt.existing

			env := app.HookEnv{ZellijSession: "test-session"}
			if err := h.HandlePostTool([]byte(`{"session_id":"sess-aaa"}`), env); err != nil {
				t.Fatalf("HandlePostTool() = %v", err)
			}

			wantDeleted := []string(nil)
			wantFocused := []string(nil)
			if tt.wantDeleted {
				wantDeleted = []string{pendingKey("test-session", "sess-aaa")}
				wantFocused = []string{"Main"}
			}
			if !reflect.DeepEqual(pending.deleted, wantDeleted) {
				t.Errorf("deleted = %v, want %v", pending.deleted, wantDeleted)
			}
			// 削除したときだけ Main へ戻る。
			if !reflect.DeepEqual(focuser.focused, wantFocused) {
				t.Errorf("focused = %v, want %v", focuser.focused, wantFocused)
			}
			// PostToolUse はレジストリに触れない。
			if len(registry.upserted) != 0 {
				t.Errorf("registry = %+v, want 触れない", registry.upserted)
			}
		})
	}
}

func TestHandlePostToolNoopWithoutSessionID(t *testing.T) {
	t.Parallel()

	h, pending, _, focuser := newHandler()
	env := app.HookEnv{ZellijSession: "test-session"}
	if err := h.HandlePostTool([]byte(`{}`), env); err != nil {
		t.Fatalf("HandlePostTool() = %v", err)
	}
	if len(pending.deleted) != 0 || len(focuser.focused) != 0 {
		t.Errorf("deleted = %v, focused = %v, want いずれも空", pending.deleted, focuser.focused)
	}
}

func TestHandlePostToolSkipsFocusOutsideZellij(t *testing.T) {
	t.Parallel()

	h, pending, _, focuser := newHandler()
	pending.events[pendingKey("unknown", "sess-aaa")] = "Notification"

	if err := h.HandlePostTool([]byte(`{"session_id":"sess-aaa"}`), app.HookEnv{}); err != nil {
		t.Fatalf("HandlePostTool() = %v", err)
	}
	// zellij 外でも pending は削除するが、フォーカス移動はしない。
	if len(pending.deleted) != 1 {
		t.Errorf("deleted = %v, want 1 件", pending.deleted)
	}
	if len(focuser.focused) != 0 {
		t.Errorf("focused = %v, want フォーカス移動なし", focuser.focused)
	}
}

func TestHandlePostToolPropagatesErrors(t *testing.T) {
	t.Parallel()

	wantErr := errors.New("失敗")
	env := app.HookEnv{ZellijSession: "s"}
	raw := []byte(`{"session_id":"sid"}`)

	t.Run("削除の失敗", func(t *testing.T) {
		t.Parallel()
		h, pending, _, focuser := newHandler()
		pending.events[pendingKey("s", "sid")] = "Notification"
		pending.deleteErr = wantErr
		if err := h.HandlePostTool(raw, env); !errors.Is(err, wantErr) {
			t.Errorf("HandlePostTool() = %v, want %v", err, wantErr)
		}
		// 現行版は set -e を使っておらず、失敗しても後続の副作用へ進む。
		if len(focuser.focused) != 1 {
			t.Errorf("focused = %v, want 削除に失敗してもフォーカス移動する", focuser.focused)
		}
	})

	t.Run("フォーカスの失敗", func(t *testing.T) {
		t.Parallel()
		h, pending, _, focuser := newHandler()
		pending.events[pendingKey("s", "sid")] = "Notification"
		focuser.focusErr = wantErr
		if err := h.HandlePostTool(raw, env); !errors.Is(err, wantErr) {
			t.Errorf("HandlePostTool() = %v, want %v", err, wantErr)
		}
	})
}
