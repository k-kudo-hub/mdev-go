package app

import "github.com/k-kudo-hub/mdev-go/internal/domain"

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
// 書き込みの中身と順序は hook 経路(HandleNotify)と同じなので applyTurnRecord
// に任せる。この経路に固有なのは 3 点だけで、いずれもそこへ渡す値として現れる。
//
//   - エージェント名の既定値が claude ではなく codex
//   - event は常に Stop(codex にはターン完了しか来ない)
//   - 上書き判定は Waiting だけを守る
func (n *CodexNotifier) Notify(raw []byte, env HookEnv) error {
	in, ok := domain.ParseCodexNotification(raw)
	if !ok {
		return nil
	}

	return applyTurnRecord(n.Pending, n.Registry, n.Clock.Now(), turnRecord{
		Session:        domain.SessionName(env.ZellijSession),
		SessionID:      in.ThreadID,
		Tab:            domain.ResolveTabName(env.TaskTabName, in.Dir),
		Dir:            in.Dir,
		TaskType:       env.TaskType,
		Agent:          agentNameForCodex(env.TaskAgent),
		Message:        in.Message,
		Event:          domain.EventStop,
		TranscriptPath: n.Transcript.Locate(in.ThreadID),
		RegisterTask:   env.IsTaskTab(),
		Overwrite:      domain.ShouldOverwriteCodexPending,
	})
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
