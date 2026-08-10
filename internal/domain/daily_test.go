package domain_test

import (
	"encoding/json"
	"reflect"
	"testing"

	"github.com/k-kudo-hub/mdev-go/internal/domain"
)

// jsonOf は値を JSON 化してから汎用の map/slice へ読み直す。
// キーの並び順を問わず「JSON としての等価」で比較するために使う。
func jsonOf(t *testing.T, v any) map[string]any {
	t.Helper()

	b, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("Marshal() = %v", err)
	}
	var fields map[string]any
	if err := json.Unmarshal(b, &fields); err != nil {
		t.Fatalf("Unmarshal(%s) = %v", b, err)
	}
	return fields
}

// parseJSON は期待値の JSON リテラルを map へ読む。
func parseJSON(t *testing.T, raw string) map[string]any {
	t.Helper()

	var fields map[string]any
	if err := json.Unmarshal([]byte(raw), &fields); err != nil {
		t.Fatalf("Unmarshal(%s) = %v", raw, err)
	}
	return fields
}

func intPtr(v int) *int           { return &v }
func floatPtr(v float64) *float64 { return &v }

func TestDailyRecordMarshalClaude(t *testing.T) {
	t.Parallel()

	// 現行 record-output.sh:178-206 が claude の transcript から作る形。
	rec := domain.DailyRecord{
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
			TotalCostUSD:       floatPtr(0.0079),
		},
		Markers:         domain.DailyMarkers{Merged: false, Slack: true, Doc: true},
		Dir:             "/tmp/myapp",
		TaskType:        "dev",
		ClaudeSessionID: "sess-rec",
		TranscriptPath:  "/tmp/mock-transcript.jsonl",
	}

	want := parseJSON(t, `{
		"tab": "record-test",
		"session": "test-session",
		"completed_at": "2026-04-18T10:00:05+0900",
		"message": "Task complete",
		"summary": {
			"total_turns": 3,
			"total_tool_calls": 3,
			"tools_used": ["Edit", "Write", "mcp__slack__send_message"],
			"model": "claude-opus-4-6",
			"speed": "standard",
			"total_input_tokens": 450,
			"total_output_tokens": 225,
			"cache_read_tokens": 0,
			"cache_write_5m_tokens": 0,
			"cache_write_1h_tokens": 0,
			"total_cost_usd": 0.0079
		},
		"markers": {"merged": false, "slack": true, "doc": true},
		"dir": "/tmp/myapp",
		"task_type": "dev",
		"claude_session_id": "sess-rec",
		"transcript_path": "/tmp/mock-transcript.jsonl"
	}`)

	if got := jsonOf(t, rec); !reflect.DeepEqual(got, want) {
		t.Errorf("DailyRecord の JSON =\n  %v\nwant\n  %v", got, want)
	}
}

func TestDailyRecordMarshalCodex(t *testing.T) {
	t.Parallel()

	// codex は cache_write_5m/1h ではなく cache_write_tokens を持ち、
	// 価格が分からないモデルでは cost が null になる(record-output.sh:96-114)。
	rec := domain.DailyRecord{
		Tab:         "cx-parse-test",
		Session:     "test-session",
		CompletedAt: "2026-08-07T20:44:13+0900",
		Message:     "merged",
		Summary: &domain.DailySummary{
			TotalTurns:        2,
			TotalToolCalls:    2,
			ToolsUsed:         []string{"exec"},
			Model:             "gpt-unknown-model",
			Speed:             "standard",
			TotalInputTokens:  1000000,
			TotalOutputTokens: 100000,
			CacheReadTokens:   500000,
			CacheWriteTokens:  intPtr(200000),
			TotalCostUSD:      nil,
		},
		Markers: domain.DailyMarkers{Merged: true},
		Agent:   "codex",
	}

	want := parseJSON(t, `{
		"tab": "cx-parse-test",
		"session": "test-session",
		"completed_at": "2026-08-07T20:44:13+0900",
		"message": "merged",
		"summary": {
			"total_turns": 2,
			"total_tool_calls": 2,
			"tools_used": ["exec"],
			"model": "gpt-unknown-model",
			"speed": "standard",
			"total_input_tokens": 1000000,
			"total_output_tokens": 100000,
			"cache_read_tokens": 500000,
			"cache_write_tokens": 200000,
			"total_cost_usd": null
		},
		"markers": {"merged": true, "slack": false, "doc": false},
		"agent": "codex"
	}`)

	if got := jsonOf(t, rec); !reflect.DeepEqual(got, want) {
		t.Errorf("DailyRecord の JSON =\n  %v\nwant\n  %v", got, want)
	}
}

func TestDailyRecordMarshalFallback(t *testing.T) {
	t.Parallel()

	// transcript が無い / パースできない場合。summary は null になり、
	// 空の任意フィールドはキーごと省略される(record-output.sh:237-259)。
	rec := domain.DailyRecord{
		Tab:             "no-transcript",
		Session:         "test-session",
		CompletedAt:     "2026-04-18T11:00:00+0900",
		Message:         "Quick task",
		Summary:         nil,
		ClaudeSessionID: "sess-notranscript",
	}

	want := parseJSON(t, `{
		"tab": "no-transcript",
		"session": "test-session",
		"completed_at": "2026-04-18T11:00:00+0900",
		"message": "Quick task",
		"summary": null,
		"markers": {"merged": false, "slack": false, "doc": false},
		"claude_session_id": "sess-notranscript"
	}`)

	if got := jsonOf(t, rec); !reflect.DeepEqual(got, want) {
		t.Errorf("DailyRecord の JSON =\n  %v\nwant\n  %v", got, want)
	}
}

func TestDailySummaryToolsUsedIsAlwaysArray(t *testing.T) {
	t.Parallel()

	// 現行版の `unique` は空でも [] を返す。Go の nil スライスは null に
	// なってしまうため、空配列として出ることを固定する。
	rec := domain.DailyRecord{Summary: &domain.DailySummary{ToolsUsed: []string{}}}

	summary, ok := jsonOf(t, rec)["summary"].(map[string]any)
	if !ok {
		t.Fatal("summary がオブジェクトではない")
	}
	tools, ok := summary["tools_used"].([]any)
	if !ok {
		t.Fatalf("tools_used = %#v, want 空配列", summary["tools_used"])
	}
	if len(tools) != 0 {
		t.Errorf("tools_used = %v, want []", tools)
	}
}

func TestDailyLayouts(t *testing.T) {
	t.Parallel()

	// completed_at は registry の updated_at と同じ `%Y-%m-%dT%H:%M:%S%z`、
	// daily ファイル名は `%Y-%m-%d`。
	if domain.DailyCompletedAtLayout != domain.RegistryUpdatedAtLayout {
		t.Errorf("DailyCompletedAtLayout = %q, want %q",
			domain.DailyCompletedAtLayout, domain.RegistryUpdatedAtLayout)
	}
	if domain.DailyFileDateLayout != "2006-01-02" {
		t.Errorf("DailyFileDateLayout = %q, want 2006-01-02", domain.DailyFileDateLayout)
	}
}

func TestDailyDefaultMessages(t *testing.T) {
	t.Parallel()

	// pending の message が空のときに使う既定値(record-output.sh:126, 216, 241)。
	if domain.ParseFailedMessage != "Parse failed" {
		t.Errorf("ParseFailedMessage = %q", domain.ParseFailedMessage)
	}
	if domain.NoSummaryMessage != "No summary available" {
		t.Errorf("NoSummaryMessage = %q", domain.NoSummaryMessage)
	}
}

func TestDailyRecordHasDedupeKey(t *testing.T) {
	t.Parallel()

	// daily log の置換キーとして使えるのは、タスクを一意に指す
	// claude_session_id を持つレコードだけである。
	for name, tc := range map[string]struct {
		claudeSessionID string
		want            bool
	}{
		"claude のセッション ID": {claudeSessionID: "0199f0e2-1234-7000-8000-abcdef012345", want: true},
		"空":                {claudeSessionID: "", want: false},
		// スクリーン検出はタブ名の slug から `screen-<slug>` を組み立てる。
		// 同じタブ名なら別のタスクでも同じ値になるため、キーにすると
		// 過去のタスクの記録を消してしまう。
		"スクリーン検出の合成 ID":     {claudeSessionID: "screen-alpha-dev-2917289248", want: false},
		"screen- だけ":        {claudeSessionID: "screen-", want: false},
		"screen を含むが前置きでない": {claudeSessionID: "sess-screen-1", want: true},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			record := domain.DailyRecord{Tab: "t", ClaudeSessionID: tc.claudeSessionID}
			if got := record.HasDedupeKey(); got != tc.want {
				t.Errorf("HasDedupeKey() = %v, want %v", got, tc.want)
			}
		})
	}
}
