package app

import (
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
func (h *HookHandler) HandleNotify(raw []byte, env HookEnv) error {
	in := domain.ParseHookInput(raw)
	if in.SessionID == "" {
		return nil
	}

	session := domain.SessionName(env.ZellijSession)
	tab := domain.ResolveTabName(env.TaskTabName, in.Cwd)
	agent := domain.AgentName(env.TaskAgent)
	now := h.Clock.Now()

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
			return fmt.Errorf("レジストリの更新に失敗しました: %w", err)
		}
	}

	existing := h.Pending.Event(session, in.SessionID)
	if !domain.ShouldOverwritePending(existing, in.HookEventName) {
		return nil
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
		return fmt.Errorf("pending の書き込みに失敗しました: %w", err)
	}
	return nil
}
