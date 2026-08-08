package domain

// pending イベント名。現行 Shell 版(claude-conductor)が pending ファイルの
// event フィールドに書く値をそのまま使う。
const (
	// EventNotification は許可待ちなど、ユーザーの応答が必要な状態。
	EventNotification = "Notification"
	// EventStop はエージェントのターンが終わった(done)状態。
	EventStop = "Stop"
	// EventWaiting は外部の返答待ち(PR レビュー等)として退避された状態。
	EventWaiting = "Waiting"
	// EventUnknown は hook_event_name が入力に無かったときの既定値。
	EventUnknown = "unknown"
)

// DefaultPendingMessage は入力に message が無いときに記録する文言。
// 現行 pending-notify.sh の `jq -r '.message // "Needs attention"'` に対応する。
const DefaultPendingMessage = "Needs attention"

// DefaultAgent は TASK_AGENT が空のときに記録するエージェント名。
const DefaultAgent = "claude"

// DefaultSessionName は ZELLIJ_SESSION_NAME が空のときに使うセッション名。
// 現行版の `${ZELLIJ_SESSION_NAME:-unknown}` に対応する。
const DefaultSessionName = "unknown"

// MainTabName はダッシュボードのタブ名。pending 解決後のフォーカス先。
const MainTabName = "Main"

// PendingTimeLayout は pending の time フィールドの書式(現行版の `date '+%H:%M:%S'`)。
const PendingTimeLayout = "15:04:05"

// Pending は「タスクがユーザーの応答を待っている」ことを表す 1 レコードである。
//
// 現行 Shell 版と同じ JSON 形状を持つ(ADR-0001 のデータ互換)。
// 値はすべて string で、transcript_path / dir / task_type / prev_event は
// 空のときキーごと省略される。読み手が `// empty` 相当の単純な判定を
// 続けられるようにするためで、現行版の jq 出力と同じ規則である。
type Pending struct {
	Tab             string `json:"tab"`
	Session         string `json:"session"`
	ClaudeSessionID string `json:"claude_session_id"`
	Message         string `json:"message"`
	Event           string `json:"event"`
	Time            string `json:"time"`
	Agent           string `json:"agent"`
	TranscriptPath  string `json:"transcript_path,omitempty"`
	Dir             string `json:"dir,omitempty"`
	TaskType        string `json:"task_type,omitempty"`
	PrevEvent       string `json:"prev_event,omitempty"`
}

// SessionName は ZELLIJ_SESSION_NAME からセッション名を決める。
// 空なら DefaultSessionName を返す。
func SessionName(zellijSession string) string {
	if zellijSession == "" {
		return DefaultSessionName
	}
	return zellijSession
}

// AgentName は TASK_AGENT からエージェント名を決める。空なら DefaultAgent を返す。
func AgentName(taskAgent string) string {
	if taskAgent == "" {
		return DefaultAgent
	}
	return taskAgent
}
