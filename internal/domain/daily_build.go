package domain

// AgentCodex は codex のエージェント名。集計方法の分岐に使う。
// 現行 record-output.sh:64 の `[ "$AGENT" = "codex" ]` に対応する。
const AgentCodex = "codex"

// DailySource は daily レコードのうち pending と時刻から決まる部分である。
// transcript の集計結果と合わせて 1 レコードになる。
type DailySource struct {
	Tab             string
	Session         string
	CompletedAt     string
	Message         string
	Dir             string
	TaskType        string
	ClaudeSessionID string
	TranscriptPath  string
	Agent           string
}

// BuildDailyRecord は daily log に追記する 1 レコードを組み立てる。
//
// 現行 record-output.sh と同じ 3 段のフォールバックを持つ。
//
//  1. transcript がある(hasTranscript)→ agent に応じて codex / claude として集計する
//  2. 集計に失敗した → summary を null にし、message が空なら "Parse failed" を入れる
//  3. そもそも transcript が無い → summary を null にし、message が空なら
//     "No summary available" を入れる
//
// 既定文言を当てるのはフォールバックのときだけである。集計に成功した場合、
// message が空なら空のまま記録する(現行版も既定値を与えていない)。
func BuildDailyRecord(source DailySource, transcript []byte, hasTranscript bool, pricing Pricing) DailyRecord {
	if !hasTranscript {
		return fallbackDailyRecord(source, NoSummaryMessage)
	}
	if source.Agent == AgentCodex {
		return buildCodexDailyRecord(source, transcript, pricing)
	}
	return buildClaudeDailyRecord(source, transcript, pricing)
}

// buildClaudeDailyRecord は claude の transcript から 1 レコードを作る。
func buildClaudeDailyRecord(source DailySource, transcript []byte, pricing Pricing) DailyRecord {
	parsed, ok := ParseClaudeTranscript(transcript)
	if !ok {
		return fallbackDailyRecord(source, ParseFailedMessage)
	}

	// 単価エントリに必須キーが欠けている場合、現行版は jq の掛け算エラーで
	// レコード全体が Parse failed に落ちる(summary だけでなく markers も出ない)。
	cost, ok := ClaudeCost(parsed, pricing)
	if !ok {
		return fallbackDailyRecord(source, ParseFailedMessage)
	}
	record := baseDailyRecord(source, source.Message)
	record.Summary = &DailySummary{
		TotalTurns:         parsed.TotalTurns,
		TotalToolCalls:     len(parsed.Tools),
		ToolsUsed:          parsed.ToolsUsed,
		Model:              parsed.Model,
		Speed:              parsed.Speed,
		TotalInputTokens:   parsed.TotalInputTokens,
		TotalOutputTokens:  parsed.TotalOutputTokens,
		CacheReadTokens:    parsed.CacheReadTokens,
		CacheWrite5mTokens: &parsed.CacheWrite5mTokens,
		CacheWrite1hTokens: &parsed.CacheWrite1hTokens,
		TotalCostUSD:       &cost,
	}
	record.Markers = ClaudeMarkers(parsed.Tools)
	return record
}

// buildCodexDailyRecord は codex の rollout から 1 レコードを作る。
func buildCodexDailyRecord(source DailySource, transcript []byte, pricing Pricing) DailyRecord {
	parsed, ok := ParseCodexTranscript(transcript)
	if !ok {
		return fallbackDailyRecord(source, ParseFailedMessage)
	}

	// 単価表に input / output が欠けたエントリがあると、現行版は jq の
	// 掛け算エラーでレコード全体が Parse failed に落ちる。
	cost, priced, ok := CodexCost(parsed, pricing)
	if !ok {
		return fallbackDailyRecord(source, ParseFailedMessage)
	}

	record := baseDailyRecord(source, source.Message)
	summary := &DailySummary{
		TotalTurns:        parsed.TotalTurns,
		TotalToolCalls:    len(parsed.Tools),
		ToolsUsed:         parsed.ToolsUsed,
		Model:             parsed.Model,
		Speed:             StandardSpeed,
		TotalInputTokens:  parsed.TotalInputTokens,
		TotalOutputTokens: parsed.TotalOutputTokens,
		CacheReadTokens:   parsed.CacheReadTokens,
		CacheWriteTokens:  &parsed.CacheWriteTokens,
	}
	// 単価が分からないモデルでは cost を null のままにする。
	if priced {
		summary.TotalCostUSD = &cost
	}
	record.Summary = summary
	record.Markers = CodexMarkers(parsed)
	return record
}

// fallbackDailyRecord は summary を持たないレコードを作る。
// message が空のときだけ defaultMessage を入れる(現行版の `${MESSAGE:-...}`)。
func fallbackDailyRecord(source DailySource, defaultMessage string) DailyRecord {
	message := source.Message
	if message == "" {
		message = defaultMessage
	}
	return baseDailyRecord(source, message)
}

// baseDailyRecord は summary と markers 以外を埋めたレコードを返す。
func baseDailyRecord(source DailySource, message string) DailyRecord {
	return DailyRecord{
		Tab:             source.Tab,
		Session:         source.Session,
		CompletedAt:     source.CompletedAt,
		Message:         message,
		Dir:             source.Dir,
		TaskType:        source.TaskType,
		ClaudeSessionID: source.ClaudeSessionID,
		TranscriptPath:  source.TranscriptPath,
		Agent:           source.Agent,
	}
}
