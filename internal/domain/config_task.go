package domain

import (
	"bytes"
	"encoding/json"
	"strconv"
	"strings"
)

// タスク作成が使う設定の既定値。
const (
	// DefaultSearchDepth は search_dirs 配下を掘る深さの既定である。
	//
	// 現行 task-create-loop.sh は `jq -r '.search_depth'` の結果をそのまま
	// `fd --max-depth` に渡すため、キーが無いと文字列 "null" が渡って fd が
	// 失敗し、候補が 1 件も出ない。設定の書き忘れで機能ごと沈黙するより
	// 既定で動くほうが良いので、Go は 1 に落とす(意図的な挙動差)。
	DefaultSearchDepth = 1
	// DefaultAgentCommand は .agent.command も .agents も無いときのコマンド。
	DefaultAgentCommand = "claude"
	// DefaultResumeArgs は resume_args が無いときに挿し込む引数。
	DefaultResumeArgs = "--resume"
)

// LayoutStep は task_types.<type>.layout の 1 要素である。
type LayoutStep struct {
	// Action は new-pane / move-focus / focus-previous-pane / resize のいずれか。
	// 未知の値は何もしない(現行版の case 文と同じ)。
	Action string
	// Direction は方向。キーが無い場合は JQNullText("null")が入る。
	// 現行版は `jq -r '.direction'` の結果をそのまま zellij へ渡すため、
	// その文字列まで含めて再現している(evidence §1-2)。
	Direction string
	// Command は new-pane で起動するコマンド。空なら `--` 以降を付けない。
	Command string
	// Amount は resize を繰り返す回数。既定は 1。
	Amount int
}

// TaskType は task_types の 1 エントリである。記述順を保つため配列で持つ。
type TaskType struct {
	// Name はキー(dev / k8s など)。選択後に TASK_TYPE として渡る。
	Name string
	// Description は選択 UI に並べる説明。
	Description string
	// Layout はタブ作成後に適用するレイアウト操作の並び。
	Layout []LayoutStep
}

// AgentSpec は 1 エージェントの起動設定である。
// 名前付き(.agents.<name>)と旧来の単一エージェント(.agent)の両方で使う。
type AgentSpec struct {
	Command    string `json:"command"`
	ResumeArgs string `json:"resume_args"`
}

// taskConfigRaw は Config の JSON をキーごとに未解釈のまま受ける入れ物である。
//
// すべて json.RawMessage にするのは、1 つのキーの型違いで設定全体の読み取りが
// 失敗しないようにするためである。現行版は 1 キーごとに `jq ... 2>/dev/null` を
// 撃つため、壊れたキーはそのキーだけが既定へ落ちる。
type taskConfigRaw struct {
	SearchDirs        json.RawMessage `json:"search_dirs"`
	SearchDepth       json.RawMessage `json:"search_depth"`
	SkipTaskNameInput json.RawMessage `json:"skip_task_name_input"`
	Agent             json.RawMessage `json:"agent"`
	Agents            json.RawMessage `json:"agents"`
	TaskTypes         json.RawMessage `json:"task_types"`
}

// SearchDepth は search_dirs 配下を掘る深さを返す。
// 設定に数値が無い場合は DefaultSearchDepth を返す。
func (c Config) SearchDepth() int {
	if c.searchDepth <= 0 {
		return DefaultSearchDepth
	}
	return c.searchDepth
}

// AgentNames は設定済みエージェントの名前を記述順で返す。
// 現行 task-lib.sh の `agent_names`(jq の keys_unsorted)に対応する。
func (c Config) AgentNames() []string { return c.agentNames }

// AgentCommand は agent の起動コマンドを語に分けて返す。
//
// 現行 task-lib.sh の `agent_command` と、その結果を
// `read -r -a agent_cmd <<< "$(...)"` で語分割する呼び出し側を合わせたものである。
//
//   - agent が空: `.agent.command`。空なら "claude"
//   - agent が非空: `.agents[agent].command`。空ならその名前自身
//
// 語分割はクォートを解釈しない単純な空白分割で、`read` が 1 行しか読まない
// ことまで含めて再現する(改行以降は捨てる。evidence §1-1)。
func (c Config) AgentCommand(agent string) []string {
	if agent == "" {
		return splitWords(fallback(c.Agent.Command, DefaultAgentCommand))
	}
	return splitWords(fallback(c.Agents[agent].Command, agent))
}

// AgentResumeArgs は再開時にコマンドとセッション ID の間へ挿す引数を返す。
// 設定が無い場合は "--resume"(DefaultResumeArgs)である。
func (c Config) AgentResumeArgs(agent string) []string {
	args := c.Agent.ResumeArgs
	if agent != "" {
		args = c.Agents[agent].ResumeArgs
	}
	return splitWords(fallback(args, DefaultResumeArgs))
}

// Layout は task_types.<name>.layout を返す。未知の型では空を返す。
func (c Config) Layout(name string) []LayoutStep {
	for _, t := range c.TaskTypes {
		if t.Name == name {
			return t.Layout
		}
	}
	return nil
}

// fallback は value が空文字なら def を返す。
// jq の `// empty` + `[[ -z ... ]]` の組み合わせに対応する(空文字は未設定と同じ)。
func fallback(value, def string) string {
	if value == "" {
		return def
	}
	return value
}

// splitWords は bash の `read -r -a` と同じ語分割を行う。
//
// クォートは解釈せず、空白・タブで区切る。`read` は 1 行しか読まないため、
// 最初の改行以降は捨てる。
func splitWords(s string) []string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = s[:i]
	}
	return strings.Fields(s)
}

// unmarshalTaskKeys は Config のタスク作成向けフィールドを埋める。
// Config.UnmarshalJSON から呼ばれる。
func (c *Config) unmarshalTaskKeys(data []byte) {
	var raw taskConfigRaw
	// 全体がオブジェクトでない場合はすべて既定へ落とす。
	if err := json.Unmarshal(data, &raw); err != nil {
		return
	}

	// 配列でない search_dirs は「無かった」ものとして扱う。
	_ = json.Unmarshal(raw.SearchDirs, &c.SearchDirs)
	_ = json.Unmarshal(raw.Agent, &c.Agent)
	c.searchDepth = jqInt(raw.SearchDepth, 0)
	// jq の `== "true"` は真偽値 true にだけ一致する(文字列 "true" は別物)。
	c.SkipTaskNameInput = bytes.Equal(bytes.TrimSpace(raw.SkipTaskNameInput), []byte("true"))
	c.agentNames, c.Agents = parseAgents(raw.Agents)
	c.TaskTypes = parseTaskTypes(raw.TaskTypes)
}

// parseAgents は .agents を記述順の名前と設定表に分ける。
//
// detection(スクリーン検出が使う)と command / resume_args(タスク起動が
// 使う)は同じ AgentConfig にまとめて入れる。
func parseAgents(raw json.RawMessage) ([]string, map[string]AgentConfig) {
	names := objectKeys(raw)
	if len(names) == 0 {
		return nil, nil
	}

	var entries map[string]AgentConfig
	if err := json.Unmarshal(raw, &entries); err != nil {
		return nil, nil
	}
	return names, entries
}

// parseTaskTypes は .task_types を記述順の配列にする。
func parseTaskTypes(raw json.RawMessage) []TaskType {
	names := objectKeys(raw)
	if len(names) == 0 {
		return nil
	}

	var entries map[string]struct {
		Description string            `json:"description"`
		Layout      []json.RawMessage `json:"layout"`
	}
	if err := json.Unmarshal(raw, &entries); err != nil {
		return nil
	}

	types := make([]TaskType, 0, len(names))
	for _, name := range names {
		e := entries[name]
		steps := make([]LayoutStep, 0, len(e.Layout))
		for _, stepRaw := range e.Layout {
			steps = append(steps, parseLayoutStep(stepRaw))
		}
		types = append(types, TaskType{Name: name, Description: e.Description, Layout: steps})
	}
	return types
}

// parseLayoutStep は layout の 1 要素を読む。
//
// 現行版は action / direction を `jq -r`(キーが無ければ文字列 "null")、
// command を `jq -r '... // empty'`(空文字)、amount を `jq -r '.amount // 1'`
// で取る。その違いをそのまま写す。
func parseLayoutStep(raw json.RawMessage) LayoutStep {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil {
		return LayoutStep{Action: JQNullText, Direction: JQNullText, Amount: 1}
	}
	return LayoutStep{
		Action:    jqRawString(fields["action"]),
		Direction: jqRawString(fields["direction"]),
		Command:   jqOptionalString(fields["command"]),
		Amount:    jqInt(fields["amount"], 1),
	}
}

// jqOptionalString は `jq -r '.key // empty'` の出力に対応する。
// null / false / 欠落はいずれも空文字になる。
func jqOptionalString(raw json.RawMessage) string {
	if !JSONTruthy(raw) {
		return ""
	}
	return jqRawString(raw)
}

// jqInt は `jq -r '.key // def'` の結果を bash の算術で使ったときの値を返す。
//
// null / false / 欠落は def。数値と数字だけの文字列はその値。それ以外は 0 に
// なる(bash は数値に解釈できない語を未設定の変数として 0 と見る。evidence §1-5)。
func jqInt(raw json.RawMessage, def int) int {
	if !JSONTruthy(raw) {
		return def
	}
	text := string(bytes.TrimSpace(raw))
	if s := ""; json.Unmarshal(raw, &s) == nil {
		text = s
	}
	n, err := strconv.Atoi(text)
	if err != nil {
		return 0
	}
	return n
}

// objectKeys は JSON オブジェクトのキーを記述順で返す。
//
// jq の `keys_unsorted` / `to_entries` に対応する。Go の map は順序を持たない
// ため、トークン列から直接読み取る。オブジェクトでない場合は空を返す。
func objectKeys(raw json.RawMessage) []string {
	dec := json.NewDecoder(bytes.NewReader(raw))
	tok, err := dec.Token()
	if err != nil {
		return nil
	}
	if delim, ok := tok.(json.Delim); !ok || delim != '{' {
		return nil
	}

	var keys []string
	for dec.More() {
		tok, err := dec.Token()
		if err != nil {
			return keys
		}
		key, ok := tok.(string)
		if !ok {
			return keys
		}
		keys = append(keys, key)
		// 値は読み飛ばす(next token が構造体なら中身ごと)。
		var skip json.RawMessage
		if err := dec.Decode(&skip); err != nil {
			return keys
		}
	}
	return keys
}
