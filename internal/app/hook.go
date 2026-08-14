package app

import (
	"errors"
	"fmt"

	"github.com/k-kudo-hub/mdev-go/internal/domain"
)

// HookHandler は Claude Code の hook イベントを処理するユースケースである。
//
// 現行 Shell 版の pending-notify.sh / pending-post-tool.sh /
// pending-resolve.sh に 1:1 で対応する 3 つのメソッドを持つ。
type HookHandler struct {
	Pending  PendingStore
	Registry RegistryStore
	Focuser  Focuser
	Clock    Clock
}

// HandleNotify は Notification / Stop hook を処理する
// (現行 pending-notify.sh 相当)。
//
// 書き込みの中身と順序は codex の notify(CodexNotifier.Notify)と同じなので
// applyTurnRecord に任せる。この経路に固有なのは、エージェント名が claude へ
// 落ちること、event が入力の hook_event_name であること、上書き判定が
// Notification と Waiting の両方を守ることの 3 点である。
func (h *HookHandler) HandleNotify(raw []byte, env HookEnv) error {
	in := domain.ParseHookInput(raw)
	if in.SessionID == "" {
		return nil
	}

	return applyTurnRecord(h.Pending, h.Registry, h.Clock.Now(), turnRecord{
		Session:        domain.SessionName(env.ZellijSession),
		SessionID:      in.SessionID,
		Tab:            domain.ResolveTabName(env.TaskTabName, in.Cwd),
		Dir:            in.Cwd,
		TaskType:       env.TaskType,
		Agent:          domain.AgentName(env.TaskAgent),
		Message:        in.Message,
		Event:          in.HookEventName,
		TranscriptPath: in.TranscriptPath,
		RegisterTask:   env.IsTaskTab(),
		Overwrite: func(existing string) bool {
			return domain.ShouldOverwritePending(existing, in.HookEventName)
		},
	})
}

// HandlePostTool は PostToolUse hook を処理する
// (現行 pending-post-tool.sh 相当)。
//
// 許可要求(Notification)への応答はツール実行の再開として現れるため、
// このタイミングで Notification の pending だけを解決する。Stop の pending は
// タスクが done であることを表しており、ユーザーが次の指示を出すまで残す
// (解決するのは HandleResolve)。
//
// レジストリには触れない。タスクの生死は変わらないためである。
func (h *HookHandler) HandlePostTool(raw []byte, env HookEnv) error {
	in := domain.ParseHookInput(raw)
	if in.SessionID == "" {
		return nil
	}

	session := domain.SessionName(env.ZellijSession)
	if h.Pending.Event(session, in.SessionID) != domain.EventNotification {
		return nil
	}

	var errs []error
	if err := h.Pending.Delete(session, in.SessionID); err != nil {
		errs = append(errs, fmt.Errorf("pending の削除に失敗しました: %w", err))
	}
	if err := h.focusMain(env); err != nil {
		errs = append(errs, err)
	}
	return errors.Join(errs...)
}

// HandleResolve は UserPromptSubmit hook を処理する
// (現行 pending-resolve.sh 相当)。
//
// ユーザーが次の指示を出した時点でタスクは待ち状態ではなくなるため、
// event を問わず pending を削除する(Waiting の解除もこの経路である)。
// 応答後もタスクは動き続けるので、レジストリは最新の内容へ更新する。
//
// pending が無くても Main へフォーカスを戻す。ユーザーがタスクタブで入力を
// 終えたら常にダッシュボードへ戻す、という操作感を保つためである。
func (h *HookHandler) HandleResolve(raw []byte, env HookEnv) error {
	in := domain.ParseHookInput(raw)
	if in.SessionID == "" {
		return nil
	}

	session := domain.SessionName(env.ZellijSession)
	var errs []error
	if err := h.Pending.Delete(session, in.SessionID); err != nil {
		errs = append(errs, fmt.Errorf("pending の削除に失敗しました: %w", err))
	}

	if env.IsTaskTab() {
		// notify と違い cwd の basename へフォールバックしない。
		// レジストリ登録自体が TASK_TAB_NAME 非空を条件にしているためである。
		entry := domain.RegistryEntry{
			Tab:             env.TaskTabName,
			Session:         session,
			ClaudeSessionID: in.SessionID,
			UpdatedAt:       h.Clock.Now().Format(domain.RegistryUpdatedAtLayout),
			Dir:             in.Cwd,
			TaskType:        env.TaskType,
			Agent:           domain.AgentName(env.TaskAgent),
			TranscriptPath:  in.TranscriptPath,
		}
		if err := h.Registry.Upsert(entry); err != nil {
			errs = append(errs, fmt.Errorf("レジストリの更新に失敗しました: %w", err))
		}
	}

	if err := h.focusMain(env); err != nil {
		errs = append(errs, err)
	}
	return errors.Join(errs...)
}

// focusMain は zellij セッション内であればダッシュボードへフォーカスを戻す。
func (h *HookHandler) focusMain(env HookEnv) error {
	if !env.InZellij() {
		return nil
	}
	if err := h.Focuser.FocusTab(domain.MainTabName); err != nil {
		return fmt.Errorf("タブ %q へのフォーカス移動に失敗しました: %w", domain.MainTabName, err)
	}
	return nil
}
