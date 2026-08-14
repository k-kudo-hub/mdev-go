package domain

// codex の notify は ~/.codex/config.toml の `notify` に登録され、ターンが
// 終わるたびに JSON 1 個を **最後の引数** として渡して呼ばれる。標準入力から
// 読む Claude Code の hook とは渡され方が違う。
//
// codex には Notification / UserPromptSubmit に当たるものが無く、ここで扱う
// のは「ターンが終わった」だけである。許可待ちの検出と解除は画面検出
// (screen_classify.go)が担う。

// CodexTurnCompleteType は扱う唯一の通知種別である。
// これ以外(将来 codex が増やすもの)は黙って捨てる。
const CodexTurnCompleteType = "agent-turn-complete"

// DefaultCodexMessage は last-assistant-message が無いときに記録する文言
// (現行 codex-notify.sh の `// "Task complete"`)。
//
// Claude Code 側の DefaultPendingMessage("Needs attention")と違うのは、
// codex から来るのがターン完了の通知だけで、応答待ちではないためである。
const DefaultCodexMessage = "Task complete"

// DefaultCodexAgent は TASK_AGENT が空のときに記録するエージェント名
// (現行版の `${TASK_AGENT:-codex}`)。
//
// codex から呼ばれる経路だと分かっているため、DefaultAgent("claude")では
// なく codex に落とす。ここを取り違えると Done の一覧でエージェントが
// 入れ替わって見える。
const DefaultCodexAgent = "codex"

// CodexNotification は codex の notify が渡す JSON から、mdev が使う項目を
// 取り出したものである。
type CodexNotification struct {
	// ThreadID は codex のスレッド ID。pending / registry では
	// claude_session_id として扱う(両エージェントで鍵の名前を揃えている)。
	ThreadID string
	// Dir は codex が動いていた作業ディレクトリ(payload の cwd)。
	Dir string
	// Message は最後のアシスタントの発言。無ければ DefaultCodexMessage。
	Message string
}

// ParseCodexNotification は notify の JSON を解釈する。
//
// 現行版が黙って exit 0 する 3 つの条件を ok=false で表す。
//
//   - 引数が空(codex が payload 無しで呼んだ)
//   - type が agent-turn-complete でない
//   - thread-id が空
//
// 解釈できない JSON も type が空になるため ok=false に落ちる。現行版が
// `jq ... 2>/dev/null` でエラーを握り潰していたのと同じ扱いである。
func ParseCodexNotification(raw []byte) (CodexNotification, bool) {
	fields := jqFields(raw)
	if jqString(fields, "type") != CodexTurnCompleteType {
		return CodexNotification{}, false
	}
	threadID := jqString(fields, "thread-id")
	if threadID == "" {
		return CodexNotification{}, false
	}

	message := jqString(fields, "last-assistant-message")
	if !jqHasValue(fields, "last-assistant-message") {
		message = DefaultCodexMessage
	}
	return CodexNotification{
		ThreadID: threadID,
		Dir:      jqString(fields, "cwd"),
		Message:  message,
	}, true
}

// ShouldOverwriteCodexPending は既存の pending を上書きしてよいかを返す。
//
// Waiting(外部の返答待ちとして退避した状態)だけを守る。退避したタスクを
// ターン完了で done に見せ直すと、ダッシュボードに戻ってきてしまう。
//
// Claude Code 側の ShouldOverwritePending と違い、Notification は守らない。
// codex では許可待ちを pending に書く経路が無く(画面検出が担う)、守る側の
// レコードがそもそも存在しないためである。現行 codex-notify.sh も
// `EXISTING_EVENT = "Waiting"` しか見ていない。
func ShouldOverwriteCodexPending(existing string) bool {
	return existing != EventWaiting
}
