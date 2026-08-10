package domain

// エージェントの状態検出方式。
//
// 現行 task-lib.sh の agent_detection が返す値に対応する。
const (
	// DetectionHooks は Claude Code のライフサイクル hook が pending を持つ方式。
	DetectionHooks = "hooks"
	// DetectionScreen はタブの画面を走査して状態を判定する方式(issue #28)。
	DetectionScreen = "screen"
)

// AgentConfig は設定の `.agents.<name>` のうち mdev が使う値である。
//
// command / resume_args / patterns はタスク生成とスクリーン検出が使うもので、
// どちらもまだ Shell 側に残っているためここでは持たない。
type AgentConfig struct {
	Detection string `json:"detection"`
}

// AgentDetection はエージェントの状態検出方式を返す。
//
// 現行 task-lib.sh の agent_detection と同じく、明示的に設定されていない
// ものはすべて "hooks" に落ちる。エージェント名が空の場合、設定にない場合、
// detection が空文字や null の場合がこれにあたる。screen 方式だと分かって
// いるものだけを走査対象にする(未知のエージェントを勝手に画面走査しない)
// という既定である。
func (c Config) AgentDetection(agent string) string {
	if agent == "" {
		return DetectionHooks
	}
	if detection := c.Agents[agent].Detection; detection != "" {
		return detection
	}
	return DetectionHooks
}
