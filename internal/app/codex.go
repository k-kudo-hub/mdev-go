package app

import (
	"errors"
	"fmt"

	"github.com/k-kudo-hub/mdev-go/internal/domain"
)

// CodexTranscriptLocator は codex の会話ログ(rollout)の場所を探す。
//
// codex は notify の payload に会話ログの場所を入れてくれないため、スレッド ID
// から自分で引く必要がある。Claude Code の hook が transcript_path をくれるのと
// 対照的で、この port はその埋め合わせである。
type CodexTranscriptLocator interface {
	// Locate は threadID の会話ログの絶対パスを返す。見つからなければ空文字を
	// 返す。**error を返さない**のは、会話ログが無くても done の記録自体は
	// 成り立つためで、現行版もすべての引き方を 2>/dev/null の best-effort に
	// している(会話ログは作業ログのアップロードにしか使わない)。
	Locate(threadID string) string
}

// CodexNotifier は codex の notify を処理するユースケースである
// (現行 codex-notify.sh 相当)。
//
// codex には Notification / UserPromptSubmit に当たる通知が無いため、
// HookHandler と違ってターン完了の 1 経路しか持たない。許可待ちの検出と
// 解除は画面検出が担う。
type CodexNotifier struct {
	Pending    PendingStore
	Registry   RegistryStore
	Transcript CodexTranscriptLocator
	Clock      Clock
}

// Notify は codex のターン完了を pending とレジストリへ反映する。
//
// raw は codex が **最後の引数** として渡す JSON である(標準入力ではない)。
//
// 処理順は現行版に合わせている。レジストリの更新を pending の上書き判定より
// 先に行うため、Waiting を守って pending を書かない場合でもレジストリは
// 最新化される(再起動後の復元がタスクを取りこぼさない)。
//
// 途中の失敗で処理を打ち切らない。現行版は set -e を使っておらず、レジストリの
// 更新に失敗しても pending の書き込みへ進む。
func (n *CodexNotifier) Notify(raw []byte, env HookEnv) error {
	in, ok := domain.ParseCodexNotification(raw)
	if !ok {
		return nil
	}

	session := domain.SessionName(env.ZellijSession)
	tab := domain.ResolveTabName(env.TaskTabName, in.Dir)
	agent := agentNameForCodex(env.TaskAgent)
	transcript := n.Transcript.Locate(in.ThreadID)
	now := n.Clock.Now()

	var errs []error
	if env.IsTaskTab() {
		entry := domain.RegistryEntry{
			Tab:             tab,
			Session:         session,
			ClaudeSessionID: in.ThreadID,
			UpdatedAt:       now.Format(domain.RegistryUpdatedAtLayout),
			Dir:             in.Dir,
			TaskType:        env.TaskType,
			Agent:           agent,
			TranscriptPath:  transcript,
		}
		if err := n.Registry.Upsert(entry); err != nil {
			errs = append(errs, fmt.Errorf("レジストリの更新に失敗しました: %w", err))
		}
	}

	if !domain.ShouldOverwriteCodexPending(n.Pending.Event(session, in.ThreadID)) {
		return errors.Join(errs...)
	}

	pending := domain.Pending{
		Tab:             tab,
		Session:         session,
		ClaudeSessionID: in.ThreadID,
		Message:         in.Message,
		Event:           domain.EventStop,
		Time:            now.Format(domain.PendingTimeLayout),
		Agent:           agent,
		TranscriptPath:  transcript,
		Dir:             in.Dir,
		TaskType:        env.TaskType,
	}
	if err := n.Pending.Save(session, in.ThreadID, pending); err != nil {
		errs = append(errs, fmt.Errorf("pending の書き込みに失敗しました: %w", err))
	}
	return errors.Join(errs...)
}

// agentNameForCodex は TASK_AGENT が空のときに codex へ落とす
// (現行版の `${TASK_AGENT:-codex}`)。
//
// domain.AgentName は claude へ落とすため、この経路では使えない。
func agentNameForCodex(taskAgent string) string {
	if taskAgent == "" {
		return domain.DefaultCodexAgent
	}
	return taskAgent
}
