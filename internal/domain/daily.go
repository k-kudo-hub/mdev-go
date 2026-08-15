package domain

import (
	"encoding/json"
)

// DailyCompletedAtLayout は daily レコードの completed_at の書式。
// 現行 record-output.sh の `date '+%Y-%m-%dT%H:%M:%S%z'` に対応する
// (registry の updated_at と同じ書式)。
const DailyCompletedAtLayout = RegistryUpdatedAtLayout

// DailyFileDateLayout は daily ファイル名の日付部分の書式
// (現行版の `$(date '+%Y-%m-%d').jsonl`)。
const DailyFileDateLayout = "2006-01-02"

// pending の message が空のときに daily へ書く既定の文言。
// transcript をパースできなかった場合と、そもそも transcript が無い場合とで
// 文言が違う(現行 record-output.sh:126 / 216 / 241)。
const (
	// ParseFailedMessage は transcript はあるがパースできなかったときの文言。
	ParseFailedMessage = "Parse failed"
	// NoSummaryMessage は transcript が無いときの文言。
	NoSummaryMessage = "No summary available"
)

// DailyRecord は daily log(`$CONDUCTOR_HOME/daily/<session>/<日付>.jsonl`)の
// 1 行である。タスクタブを閉じたときに 1 件追記される。
//
// pending と違い、値は string だけではない。total_turns などは数値、markers は
// 真偽値、summary は object または null、total_cost_usd は数値または null になる。
// 現行 Shell 版が jq で組み立てている形をそのまま写したものである。
//
// dir / task_type / claude_session_id / transcript_path / agent は空のとき
// キーごと省略される(現行版の `+ (if $dir != "" then {dir: $dir} else {} end)`)。
//
// なお restore-task.sh は後からこのレコードへ `"restored": true` を足す。
// mdev はその読み書きをしないため、この型には持たせていない。
type DailyRecord struct {
	Tab             string        `json:"tab"`
	Session         string        `json:"session"`
	CompletedAt     string        `json:"completed_at"`
	Message         string        `json:"message"`
	Summary         *DailySummary `json:"summary"`
	Markers         DailyMarkers  `json:"markers"`
	Dir             string        `json:"dir,omitempty"`
	TaskType        string        `json:"task_type,omitempty"`
	ClaudeSessionID string        `json:"claude_session_id,omitempty"`
	TranscriptPath  string        `json:"transcript_path,omitempty"`
	Agent           string        `json:"agent,omitempty"`
}

// ScreenSessionIDPrefix はスクリーン検出が合成する claude_session_id の前置きである
// (現行 screen-detect-lib.sh の `--arg claude_session_id "screen-$slug"`)。
//
// hook を持たないエージェントの完了はタブの画面から検出するため、そこには
// エージェントが発行した本物のセッション ID が無い。代わりにタブ名から作った
// slug(ScreenTabSlug)を使うが、これはタブ名の純関数であり、同じ名前のタブなら
// 別のタスクでも同じ値になる。
const ScreenSessionIDPrefix = "screen-"

// HasDedupeKey は daily log の置換キーとして使えるレコードかを返す。
//
// 置換キーは (tab, claude_session_id) の組である。claude_session_id が無いレコードは
// もちろん、スクリーン検出が合成した ID を持つレコードもキーにはできない。合成 ID は
// タブ名だけで決まるため、同じ名前のタブで前に行ったタスクの記録にまで一致してしまい、
// 消してはいけない履歴を消すからである。キーが無い場合は置換せず追記する
// (記録が重複することはあっても、過去の記録は失われない)。
func (r DailyRecord) HasDedupeKey() bool {
	return r.ClaudeSessionID != "" && !IsScreenSessionID(r.ClaudeSessionID)
}

// DailyMarkers はタスク中に何をしたかの目印である。
// ダッシュボードの Done ペインがアイコン表示に使う。
type DailyMarkers struct {
	// Merged は PR をマージしたか。
	Merged bool `json:"merged"`
	// Slack は Slack へ投稿したか。
	Slack bool `json:"slack"`
	// Doc はドキュメント(.md 等)を書いたか。
	Doc bool `json:"doc"`
}

// DailySummary は transcript から集計したセッションの要約である。
//
// キャッシュ書き込みのトークン数はエージェントによってキーが違う。claude は
// 5 分 / 1 時間の TTL 別に分かれ(cache_write_5m_tokens / cache_write_1h_tokens)、
// codex は 1 つにまとまる(cache_write_tokens)。現行版がエージェントごとに
// 別々の jq 式で組み立てているためで、使わない側はキーごと省略する。
//
// TotalCostUSD は codex で単価が分からないモデルのとき null になる。claude は
// 必ず何らかの単価にフォールバックするため null にならない。
type DailySummary struct {
	TotalTurns        int      `json:"total_turns"`
	TotalToolCalls    int      `json:"total_tool_calls"`
	ToolsUsed         []string `json:"tools_used"`
	Model             string   `json:"model"`
	Speed             string   `json:"speed"`
	TotalInputTokens  int      `json:"total_input_tokens"`
	TotalOutputTokens int      `json:"total_output_tokens"`
	CacheReadTokens   int      `json:"cache_read_tokens"`

	// claude のみ。
	CacheWrite5mTokens *int `json:"cache_write_5m_tokens,omitempty"`
	CacheWrite1hTokens *int `json:"cache_write_1h_tokens,omitempty"`

	// codex のみ。
	CacheWriteTokens *int `json:"cache_write_tokens,omitempty"`

	TotalCostUSD *float64 `json:"total_cost_usd"`
}

// MarshalJSON は tools_used が空でも null ではなく [] として出力されるようにする。
// 現行版の `unique` は空入力でも [] を返すため、読み手が `.tools_used | length`
// をそのまま使える形を保つ。
func (s DailySummary) MarshalJSON() ([]byte, error) {
	if s.ToolsUsed == nil {
		s.ToolsUsed = []string{}
	}
	// 別名の型にすることでこのメソッドの再帰呼び出しを避ける。
	type summary DailySummary
	return json.Marshal(summary(s))
}
