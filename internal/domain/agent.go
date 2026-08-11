package domain

import "encoding/json"

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
type AgentConfig struct {
	// Command はエージェントの起動コマンド。空なら名前自身が使われる。
	Command string `json:"command"`
	// ResumeArgs は再開時にコマンドとセッション ID の間へ挿す引数。
	ResumeArgs string `json:"resume_args"`
	// Detection は状態検出の方式("hooks" / "screen")。
	Detection string `json:"detection"`
	// Patterns は Detection が "screen" のときに画面を照合する正規表現。
	Patterns ScreenPatterns `json:"patterns"`
}

// AgentPatterns はエージェントのスクリーン検出パターンを返す。
//
// 現行 task-lib.sh の agent_patterns と同じく、エージェント名が空の場合、
// 設定に無い場合、patterns が無い場合はいずれも空になる。空のパターンは
// 「その状態には決して分類されない」ことを意味する。
func (c Config) AgentPatterns(agent string) ScreenPatterns {
	if agent == "" {
		return ScreenPatterns{}
	}
	return c.Agents[agent].Patterns
}

// UnmarshalJSON は patterns をキー単位で読む。
//
// 現行版は `jq -r '.agents[$a].patterns[$s] // [] | .[]' 2>/dev/null` を状態
// ごとに撃つため、**1 つのキーの型違いが他のキーへ波及しない**。オブジェクト
// でない patterns や配列でないキーはそこだけが空になり、同じエントリの
// command / resume_args / detection も無事である(evidence §1-6)。
//
// エラーを返さないのはそのためで、読めない形はすべて「設定されていない」に
// 落とす。設定の書き間違いで検出そのものが止まると、codex のタスクが一覧から
// 無言で消えるという最も気づきにくい壊れ方になる。
func (p *ScreenPatterns) UnmarshalJSON(data []byte) error {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(data, &fields); err != nil {
		return nil //nolint:nilerr // 読めない形は「設定されていない」に落とす(上のコメント)
	}
	p.Neutral = parsePatternList(fields["neutral"])
	p.Blocked = parsePatternList(fields["blocked"])
	p.Working = parsePatternList(fields["working"])
	return nil
}

// parsePatternList は 1 つの状態のパターン配列を読む。
//
// 要素は `jq -r` と同じく文字列以外もその表記のまま採る(数値なら "12"、
// 真偽値なら "true")。1 つの型違いで配列ごと消さないためである。
// 配列でない場合だけ nil を返す。
func parsePatternList(raw json.RawMessage) []string {
	var items []json.RawMessage
	if err := json.Unmarshal(raw, &items); err != nil {
		return nil
	}
	patterns := make([]string, 0, len(items))
	for _, item := range items {
		patterns = append(patterns, jqRawString(item))
	}
	return patterns
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
