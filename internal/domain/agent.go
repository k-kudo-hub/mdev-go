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
// patterns(スクリーン検出の正規表現)はまだ Shell 側の
// screen-detect-lib.sh が持っているためここには無い。
type AgentConfig struct {
	// Command はエージェントの起動コマンド。空なら名前自身が使われる。
	Command string `json:"command"`
	// ResumeArgs は再開時にコマンドとセッション ID の間へ挿す引数。
	ResumeArgs string `json:"resume_args"`
	// Detection は状態検出の方式("hooks" / "screen")。
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

// HasScreenDetectionAgent は screen 方式のエージェントが 1 つでも設定に
// あるかを返す。
//
// スクリーン検出は設定に screen 方式のエージェントがあって初めて意味を持つ。
// 1 つも無ければ走らせても見つかるものが無く、検出の中で走る
// `zellij action list-panes` の実行時間(実測 1.1〜1.5 秒)だけが残る。
// 呼び出し側はこの判定で検出そのものを省く。
func (c Config) HasScreenDetectionAgent() bool {
	for name := range c.Agents {
		if c.AgentDetection(name) == DetectionScreen {
			return true
		}
	}
	return false
}
