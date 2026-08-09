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
// 処理順は現行版に合わせている。レジストリの更新を pending の上書き判定より
// 先に行うため、Stop が Notification を上書きしない場合でもレジストリは
// 最新化される。
//
// 途中の失敗で処理を打ち切らない。現行版は set -e を使っておらず、レジストリの
// 更新に失敗しても pending の書き込みへ進むため、失敗経路でもファイル状態が
// 現行版と一致するよう副作用をすべて実行してからエラーをまとめて返す。
func (h *HookHandler) HandleNotify(raw []byte, env HookEnv) error {
	in := domain.ParseHookInput(raw)
	if in.SessionID == "" {
		return nil
	}

	session := domain.SessionName(env.ZellijSession)
	tab := domain.ResolveTabName(env.TaskTabName, in.Cwd)
	agent := domain.AgentName(env.TaskAgent)
	now := h.Clock.Now()

	var errs []error
	if env.IsTaskTab() {
		entry := domain.RegistryEntry{
			Tab:             tab,
			Session:         session,
			ClaudeSessionID: in.SessionID,
			UpdatedAt:       now.Format(domain.RegistryUpdatedAtLayout),
			Dir:             in.Cwd,
			TaskType:        env.TaskType,
			Agent:           agent,
			TranscriptPath:  in.TranscriptPath,
		}
		if err := h.Registry.Upsert(entry); err != nil {
			errs = append(errs, fmt.Errorf("レジストリの更新に失敗しました: %w", err))
		}
	}

	existing := h.Pending.Event(session, in.SessionID)
	if !domain.ShouldOverwritePending(existing, in.HookEventName) {
		return errors.Join(errs...)
	}

	pending := domain.Pending{
		Tab:             tab,
		Session:         session,
		ClaudeSessionID: in.SessionID,
		Message:         in.Message,
		Event:           in.HookEventName,
		Time:            now.Format(domain.PendingTimeLayout),
		Agent:           agent,
		TranscriptPath:  in.TranscriptPath,
		Dir:             in.Cwd,
		TaskType:        env.TaskType,
	}
	if err := h.Pending.Save(session, in.SessionID, pending); err != nil {
		errs = append(errs, fmt.Errorf("pending の書き込みに失敗しました: %w", err))
	}
	return errors.Join(errs...)
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
