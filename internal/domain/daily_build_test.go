package domain_test

import (
	"encoding/json"
	"reflect"
	"testing"

	"github.com/k-kudo-hub/mdev-go/internal/domain"
)

// recordSource は各テストで使い回す pending 由来の情報。
func recordSource() domain.DailySource {
	return domain.DailySource{
		Tab:             "record-test",
		Session:         "test-session",
		CompletedAt:     "2026-04-18T10:00:05+0900",
		Message:         "Task complete",
		Dir:             "/tmp/myapp",
		TaskType:        "dev",
		ClaudeSessionID: "sess-rec",
		TranscriptPath:  "/tmp/mock-transcript.jsonl",
	}
}

func TestBuildDailyRecordWithoutTranscript(t *testing.T) {
	t.Parallel()

	// 3 段目のフォールバック(record-output.sh:237-259)。
	src := recordSource()
	got := domain.BuildDailyRecord(src, nil, false, defaultPricing(t))

	if got.Summary != nil {
		t.Errorf("Summary = %+v, want nil", got.Summary)
	}
	if got.Markers != (domain.DailyMarkers{}) {
		t.Errorf("Markers = %+v, want すべて false", got.Markers)
	}
	if got.Message != "Task complete" {
		t.Errorf("Message = %q, want %q", got.Message, "Task complete")
	}
	// 任意フィールドは pending の内容がそのまま乗る。
	if got.Dir != "/tmp/myapp" || got.TaskType != "dev" ||
		got.ClaudeSessionID != "sess-rec" || got.TranscriptPath != "/tmp/mock-transcript.jsonl" {
		t.Errorf("任意フィールドが引き継がれていない: %+v", got)
	}
}

func TestBuildDailyRecordDefaultMessages(t *testing.T) {
	t.Parallel()

	// message が空のときだけ既定文言が入る。空でなければそのまま使う。
	tests := []struct {
		name           string
		message        string
		transcript     string
		hasTranscript  bool
		wantMessage    string
		wantNilSummary bool
	}{
		{
			name:           "transcript 無し・message 空",
			message:        "",
			hasTranscript:  false,
			wantMessage:    domain.NoSummaryMessage,
			wantNilSummary: true,
		},
		{
			name:           "transcript 無し・message あり",
			message:        "Quick task",
			hasTranscript:  false,
			wantMessage:    "Quick task",
			wantNilSummary: true,
		},
		{
			name:           "パース失敗・message 空",
			message:        "",
			transcript:     "not json",
			hasTranscript:  true,
			wantMessage:    domain.ParseFailedMessage,
			wantNilSummary: true,
		},
		{
			name:           "パース失敗・message あり",
			message:        "Broken",
			transcript:     "not json",
			hasTranscript:  true,
			wantMessage:    "Broken",
			wantNilSummary: true,
		},
		{
			// 成功時は既定文言を当てない(現行版も `--arg message "$MESSAGE"`)。
			name:           "成功・message 空なら空のまま",
			message:        "",
			transcript:     claudeTranscriptSection20,
			hasTranscript:  true,
			wantMessage:    "",
			wantNilSummary: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			src := recordSource()
			src.Message = tt.message
			got := domain.BuildDailyRecord(src, []byte(tt.transcript), tt.hasTranscript, defaultPricing(t))

			if got.Message != tt.wantMessage {
				t.Errorf("Message = %q, want %q", got.Message, tt.wantMessage)
			}
			if (got.Summary == nil) != tt.wantNilSummary {
				t.Errorf("Summary == nil は %v, want %v", got.Summary == nil, tt.wantNilSummary)
			}
		})
	}
}

func TestBuildDailyRecordClaude(t *testing.T) {
	t.Parallel()

	src := recordSource()
	got := domain.BuildDailyRecord(src, []byte(claudeTranscriptSection20), true, defaultPricing(t))

	// 450*5 + 225*25 = 2250 + 5625 = 7875 → 0.007875
	want := domain.DailyRecord{
		Tab:         "record-test",
		Session:     "test-session",
		CompletedAt: "2026-04-18T10:00:05+0900",
		Message:     "Task complete",
		Summary: &domain.DailySummary{
			TotalTurns:         3,
			TotalToolCalls:     3,
			ToolsUsed:          []string{"Edit", "Write", "mcp__slack__send_message"},
			Model:              "claude-opus-4-6",
			Speed:              "standard",
			TotalInputTokens:   450,
			TotalOutputTokens:  225,
			CacheReadTokens:    0,
			CacheWrite5mTokens: intPtr(0),
			CacheWrite1hTokens: intPtr(0),
			TotalCostUSD:       floatPtr(0.007875),
		},
		Markers:         domain.DailyMarkers{Slack: true, Doc: true},
		Dir:             "/tmp/myapp",
		TaskType:        "dev",
		ClaudeSessionID: "sess-rec",
		TranscriptPath:  "/tmp/mock-transcript.jsonl",
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("BuildDailyRecord() =\n  %s\nwant\n  %s", mustJSON(t, got), mustJSON(t, want))
	}
}

func TestBuildDailyRecordCodex(t *testing.T) {
	t.Parallel()

	src := recordSource()
	src.Agent = domain.AgentCodex
	src.Message = "merged"
	got := domain.BuildDailyRecord(src, []byte(codexRollout), true, defaultPricing(t))

	want := &domain.DailySummary{
		TotalTurns:        2,
		TotalToolCalls:    2,
		ToolsUsed:         []string{"exec"},
		Model:             "gpt-5.6-sol",
		Speed:             "standard",
		TotalInputTokens:  1000000,
		TotalOutputTokens: 100000,
		CacheReadTokens:   500000,
		CacheWriteTokens:  intPtr(200000),
		TotalCostUSD:      floatPtr(9.5),
	}
	if !reflect.DeepEqual(got.Summary, want) {
		t.Errorf("Summary =\n  %s\nwant\n  %s", mustJSON(t, got.Summary), mustJSON(t, want))
	}
	if got.Markers != (domain.DailyMarkers{Merged: true}) {
		t.Errorf("Markers = %+v, want merged のみ true", got.Markers)
	}
	if got.Agent != domain.AgentCodex {
		t.Errorf("Agent = %q, want %q", got.Agent, domain.AgentCodex)
	}
}

func TestBuildDailyRecordCodexUnknownModelHasNullCost(t *testing.T) {
	t.Parallel()

	// claude と違い、価格の分からないモデルでは cost が null になる
	// (test.sh セクション 26i の後半)。
	src := recordSource()
	src.Agent = domain.AgentCodex

	rollout := `{"type":"turn_context","payload":{"model":"gpt-unknown-model"}}
{"type":"event_msg","payload":{"type":"token_count","info":{"total_token_usage":{"input_tokens":10,"output_tokens":1}}}}`
	got := domain.BuildDailyRecord(src, []byte(rollout), true, defaultPricing(t))

	if got.Summary == nil {
		t.Fatal("Summary = nil, want summary あり")
	}
	if got.Summary.TotalCostUSD != nil {
		t.Errorf("TotalCostUSD = %v, want nil", *got.Summary.TotalCostUSD)
	}
	if got.Summary.Model != "gpt-unknown-model" {
		t.Errorf("Model = %q", got.Summary.Model)
	}
}

func TestBuildDailyRecordCodexUsesCodexParserOnlyForCodexAgent(t *testing.T) {
	t.Parallel()

	// agent が codex 以外なら codex の rollout でも claude として集計する
	// (現行版の分岐も `[ "$AGENT" = "codex" ]` だけを見る)。
	src := recordSource()
	src.Agent = "claude"
	got := domain.BuildDailyRecord(src, []byte(codexRollout), true, defaultPricing(t))

	if got.Summary == nil {
		t.Fatal("Summary = nil, want summary あり")
	}
	// claude の集計では codex の rollout に user 行もツールも見つからない。
	if got.Summary.TotalTurns != 0 || got.Summary.TotalToolCalls != 0 {
		t.Errorf("claude 集計になっていない: %+v", got.Summary)
	}
	// claude 側は cost が null にならない(必ず単価にフォールバックする)。
	if got.Summary.TotalCostUSD == nil {
		t.Error("TotalCostUSD = nil, want 数値")
	}
	// claude の summary は cache_write_5m/1h を持ち cache_write は持たない。
	if got.Summary.CacheWrite5mTokens == nil || got.Summary.CacheWriteTokens != nil {
		t.Errorf("キャッシュ書き込みのキーが claude 用になっていない: %+v", got.Summary)
	}
}

func TestBuildDailyRecordCodexParseFailureKeepsAgent(t *testing.T) {
	t.Parallel()

	src := recordSource()
	src.Agent = domain.AgentCodex
	src.Message = ""
	got := domain.BuildDailyRecord(src, []byte("not json"), true, defaultPricing(t))

	if got.Summary != nil {
		t.Errorf("Summary = %+v, want nil", got.Summary)
	}
	if got.Agent != domain.AgentCodex {
		t.Errorf("Agent = %q, want %q", got.Agent, domain.AgentCodex)
	}
	if got.Message != domain.ParseFailedMessage {
		t.Errorf("Message = %q, want %q", got.Message, domain.ParseFailedMessage)
	}
}

func TestBuildDailyRecordOmitsEmptyOptionalFields(t *testing.T) {
	t.Parallel()

	// 空の任意フィールドはキーごと省略される(現行版の `if $dir != "" then ...`)。
	src := domain.DailySource{
		Tab:         "minimal",
		Session:     "test-session",
		CompletedAt: "2026-04-18T10:00:05+0900",
	}
	got := domain.BuildDailyRecord(src, nil, false, domain.Pricing{})

	fields := jsonOf(t, got)
	for _, key := range []string{"dir", "task_type", "claude_session_id", "transcript_path", "agent"} {
		if _, ok := fields[key]; ok {
			t.Errorf("%s が出力されている", key)
		}
	}
	for _, key := range []string{"tab", "session", "completed_at", "message", "summary", "markers"} {
		if _, ok := fields[key]; !ok {
			t.Errorf("%s が出力されていない", key)
		}
	}
}

// mustJSON は差分を読みやすくするために JSON 文字列へ変換する。
func mustJSON(t *testing.T, v any) string {
	t.Helper()

	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		t.Fatalf("Marshal() = %v", err)
	}
	return string(b)
}
