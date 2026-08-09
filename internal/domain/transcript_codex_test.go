package domain_test

import (
	"reflect"
	"testing"

	"github.com/k-kudo-hub/mdev-go/internal/domain"
)

// claude-conductor の test.sh セクション 26i が使う codex の rollout。
const codexRollout = `{"timestamp":"2026-08-07T20:44:09.850Z","type":"session_meta","payload":{"id":"thread-cx2","cwd":"/tmp/myapp","cli_version":"0.147.0","source":"exec"}}
{"timestamp":"2026-08-07T20:44:09.851Z","type":"turn_context","payload":{"model":"gpt-5.6-sol","approval_policy":"never"}}
{"timestamp":"2026-08-07T20:44:09.900Z","type":"event_msg","payload":{"type":"user_message","message":"fix the bug"}}
{"timestamp":"2026-08-07T20:44:10.000Z","type":"response_item","payload":{"type":"custom_tool_call","id":"c1","status":"completed","call_id":"call1","name":"exec","input":"const r = await tools.exec_command({\"cmd\":\"npm test\"});"}}
{"timestamp":"2026-08-07T20:44:10.100Z","type":"response_item","payload":{"type":"custom_tool_call_output","call_id":"call1","output":"ok"}}
{"timestamp":"2026-08-07T20:44:10.200Z","type":"response_item","payload":{"type":"message","role":"assistant","content":[{"type":"output_text","text":"done"}]}}
{"timestamp":"2026-08-07T20:44:11.000Z","type":"event_msg","payload":{"type":"user_message","message":"now merge it"}}
{"timestamp":"2026-08-07T20:44:12.000Z","type":"response_item","payload":{"type":"custom_tool_call","id":"c2","status":"completed","call_id":"call2","name":"exec","input":"const r = await tools.exec_command({\"cmd\":\"gh pr merge 12 --squash\"});"}}
{"timestamp":"2026-08-07T20:44:13.000Z","type":"event_msg","payload":{"type":"token_count","info":{"total_token_usage":{"input_tokens":1500000,"cached_input_tokens":500000,"cache_write_input_tokens":200000,"output_tokens":100000,"reasoning_output_tokens":0,"total_tokens":1600000}}}}
{"timestamp":"2026-08-07T20:44:13.100Z","type":"event_msg","payload":{"type":"task_complete","last_agent_message":"merged"}}
`

func TestParseCodexTranscriptSection26i(t *testing.T) {
	t.Parallel()

	got, ok := domain.ParseCodexTranscript([]byte(codexRollout))
	if !ok {
		t.Fatal("ParseCodexTranscript() ok = false, want true")
	}

	// test.sh セクション 26i の期待値。
	// input_tokens はキャッシュ済みを引いた実消費(1500000 - 500000)。
	want := domain.CodexTranscript{
		TotalTurns: 2,
		Tools: []domain.CodexToolCall{
			{Name: "exec", Input: `const r = await tools.exec_command({"cmd":"npm test"});`},
			{Name: "exec", Input: `const r = await tools.exec_command({"cmd":"gh pr merge 12 --squash"});`},
		},
		ToolsUsed:         []string{"exec"},
		Model:             "gpt-5.6-sol",
		TotalInputTokens:  1000000,
		TotalOutputTokens: 100000,
		CacheReadTokens:   500000,
		CacheWriteTokens:  200000,
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("ParseCodexTranscript() =\n  %+v\nwant\n  %+v", got, want)
	}
}

func TestParseCodexTranscriptAggregates(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		raw  string
		want domain.CodexTranscript
	}{
		{
			name: "空の rollout",
			raw:  "",
			want: domain.CodexTranscript{
				Tools:     []domain.CodexToolCall{},
				ToolsUsed: []string{},
				Model:     "unknown",
			},
		},
		{
			// ツール呼び出しは payload.type が `_call` で終わるものだけ。
			// `_call_output` は終わらないので数えない。
			name: "_call で終わる response_item だけを数える",
			raw: `{"type":"response_item","payload":{"type":"custom_tool_call","name":"exec"}}
{"type":"response_item","payload":{"type":"custom_tool_call_output","output":"ok"}}
{"type":"response_item","payload":{"type":"function_call","name":"apply_patch"}}
{"type":"response_item","payload":{"type":"message"}}
{"type":"response_item","payload":{"type":null}}`,
			want: domain.CodexTranscript{
				Tools: []domain.CodexToolCall{
					{Name: "exec"},
					{Name: "apply_patch"},
				},
				ToolsUsed: []string{"apply_patch", "exec"},
				Model:     "unknown",
			},
		},
		{
			// `.name // .type` なので name が無ければ payload.type を使う。
			name: "name が無ければ type をツール名にする",
			raw:  `{"type":"response_item","payload":{"type":"local_shell_call"}}`,
			want: domain.CodexTranscript{
				Tools:     []domain.CodexToolCall{{Name: "local_shell_call"}},
				ToolsUsed: []string{"local_shell_call"},
				Model:     "unknown",
			},
		},
		{
			// model は最後の turn_context を採る(claude は最初)。
			name: "model は最後の turn_context",
			raw: `{"type":"turn_context","payload":{"model":"gpt-5.6-sol"}}
{"type":"turn_context","payload":{"model":"gpt-5.6-thinking"}}
{"type":"turn_context","payload":{"model":null}}`,
			want: domain.CodexTranscript{
				Tools:     []domain.CodexToolCall{},
				ToolsUsed: []string{},
				Model:     "gpt-5.6-thinking",
			},
		},
		{
			// usage も最後の token_count を採る(累計値のため)。
			name: "usage は最後の token_count",
			raw: `{"type":"event_msg","payload":{"type":"token_count","info":{"total_token_usage":{"input_tokens":10,"output_tokens":1}}}}
{"type":"event_msg","payload":{"type":"token_count","info":{"total_token_usage":{"input_tokens":30,"cached_input_tokens":5,"cache_write_input_tokens":7,"output_tokens":3}}}}`,
			want: domain.CodexTranscript{
				Tools:             []domain.CodexToolCall{},
				ToolsUsed:         []string{},
				Model:             "unknown",
				TotalInputTokens:  25,
				TotalOutputTokens: 3,
				CacheReadTokens:   5,
				CacheWriteTokens:  7,
			},
		},
		{
			// null の total_token_usage は候補から外れるので直前の値が残る。
			name: "null の total_token_usage は無視する",
			raw: `{"type":"event_msg","payload":{"type":"token_count","info":{"total_token_usage":{"input_tokens":10,"output_tokens":1}}}}
{"type":"event_msg","payload":{"type":"token_count","info":{"total_token_usage":null}}}`,
			want: domain.CodexTranscript{
				Tools:             []domain.CodexToolCall{},
				ToolsUsed:         []string{},
				Model:             "unknown",
				TotalInputTokens:  10,
				TotalOutputTokens: 1,
			},
		},
		{
			name: "user_message 以外の event_msg はターンに数えない",
			raw: `{"type":"event_msg","payload":{"type":"user_message","message":"a"}}
{"type":"event_msg","payload":{"type":"agent_message","message":"b"}}
{"type":"event_msg","payload":{"type":"task_complete"}}`,
			want: domain.CodexTranscript{
				TotalTurns: 1,
				Tools:      []domain.CodexToolCall{},
				ToolsUsed:  []string{},
				Model:      "unknown",
			},
		},
		{
			// `.input // .arguments // ""` の順。input が無ければ arguments。
			name: "input が無ければ arguments を使う",
			raw:  `{"type":"response_item","payload":{"type":"function_call","name":"exec","arguments":"gh pr merge 1"}}`,
			want: domain.CodexTranscript{
				Tools:     []domain.CodexToolCall{{Name: "exec", Input: "gh pr merge 1"}},
				ToolsUsed: []string{"exec"},
				Model:     "unknown",
			},
		},
		{
			// 文字列以外は tostring される(オブジェクトは compact JSON)。
			name: "input がオブジェクトなら JSON 文字列にする",
			raw:  `{"type":"response_item","payload":{"type":"function_call","name":"exec","input":{"cmd":"gh pr merge 1"}}}`,
			want: domain.CodexTranscript{
				Tools:     []domain.CodexToolCall{{Name: "exec", Input: `{"cmd":"gh pr merge 1"}`}},
				ToolsUsed: []string{"exec"},
				Model:     "unknown",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, ok := domain.ParseCodexTranscript([]byte(tt.raw))
			if !ok {
				t.Fatal("ParseCodexTranscript() ok = false, want true")
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("ParseCodexTranscript() =\n  %+v\nwant\n  %+v", got, tt.want)
			}
		})
	}
}

func TestParseCodexTranscriptRejects(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		raw  string
	}{
		{name: "JSON として壊れた行がある", raw: "{\"type\":\"turn_context\"}\nnot json\n"},
		{name: "トップレベルが数値", raw: "123"},
		{name: "event_msg の payload がスカラー", raw: `{"type":"event_msg","payload":5}`},
		{name: "response_item の payload がスカラー", raw: `{"type":"response_item","payload":"x"}`},
		{name: "turn_context の payload がスカラー", raw: `{"type":"turn_context","payload":1}`},
		{name: "token_count の info がスカラー", raw: `{"type":"event_msg","payload":{"type":"token_count","info":3}}`},
		{name: "total_token_usage がスカラー", raw: `{"type":"event_msg","payload":{"type":"token_count","info":{"total_token_usage":3}}}`},
		{name: "input_tokens が文字列", raw: `{"type":"event_msg","payload":{"type":"token_count","info":{"total_token_usage":{"input_tokens":"x"}}}}`},
		{name: "response_item の payload.type が数値", raw: `{"type":"response_item","payload":{"type":7}}`},
		{name: "turn_context の model が数値", raw: `{"type":"turn_context","payload":{"model":7}}`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if _, ok := domain.ParseCodexTranscript([]byte(tt.raw)); ok {
				t.Error("ParseCodexTranscript() ok = true, want false")
			}
		})
	}
}

func TestCodexMarkers(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		tools []domain.CodexToolCall
		want  domain.DailyMarkers
	}{
		{
			name:  "gh pr merge を含むツール呼び出し",
			tools: []domain.CodexToolCall{{Name: "exec", Input: `tools.exec_command({"cmd":"gh pr merge 12"})`}},
			want:  domain.DailyMarkers{Merged: true},
		},
		{
			name:  "含まなければ偽",
			tools: []domain.CodexToolCall{{Name: "exec", Input: "npm test"}},
			want:  domain.DailyMarkers{},
		},
		{
			// codex は slack / doc を判定しない(現行版は常に false)。
			name:  "slack ツールでも slack は偽のまま",
			tools: []domain.CodexToolCall{{Name: "mcp__slack__send_message", Input: "/x/a.md"}},
			want:  domain.DailyMarkers{},
		},
		{name: "ツールが無ければすべて偽", tools: nil, want: domain.DailyMarkers{}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := domain.CodexMarkers(tt.tools); got != tt.want {
				t.Errorf("CodexMarkers() = %+v, want %+v", got, tt.want)
			}
		})
	}
}

func TestCodexCost(t *testing.T) {
	t.Parallel()

	pricing := defaultPricing(t)
	transcript, ok := domain.ParseCodexTranscript([]byte(codexRollout))
	if !ok {
		t.Fatal("ParseCodexTranscript() ok = false")
	}

	// test.sh セクション 26i:
	// 1M*$5 + 0.1M*$30 + 0.5M*$0.5 + 0.2M*$6.25 = 5 + 3 + 0.25 + 1.25 = 9.5
	cost, priced, ok := domain.CodexCost(transcript, pricing)
	if !ok || !priced {
		t.Fatalf("CodexCost() priced = %v, ok = %v, want ともに true", priced, ok)
	}
	if cost != 9.5 {
		t.Errorf("CodexCost() = %v, want 9.5", cost)
	}

	// 価格の分からないモデルは claude の単価を借りず priced=false になる(cost null)。
	transcript.Model = "gpt-unknown-model"
	if _, priced, ok := domain.CodexCost(transcript, pricing); priced || !ok {
		t.Errorf("CodexCost(未知モデル) priced = %v, ok = %v, want false, true", priced, ok)
	}
	transcript.Model = domain.UnknownModel
	if _, priced, ok := domain.CodexCost(transcript, pricing); priced || !ok {
		t.Errorf("CodexCost(unknown) priced = %v, ok = %v, want false, true", priced, ok)
	}
}

func TestCodexCostFailsOnMissingRequiredKeys(t *testing.T) {
	t.Parallel()

	transcript, ok := domain.ParseCodexTranscript([]byte(codexRollout))
	if !ok {
		t.Fatal("ParseCodexTranscript() ok = false")
	}

	// jq では input / output だけが素の参照で、欠けると掛け算エラーになり
	// Parse failed に落ちる(record-output.sh:84-85)。
	pricing := parsePricing(t, `{"gpt-5.6-sol":{"output":30}}`)
	if _, _, ok := domain.CodexCost(transcript, pricing); ok {
		t.Error("CodexCost(input 欠落) ok = true, want false")
	}

	// cache_hit / cache_write は `// 0` 付きなので、欠けても 0 で計算が進む。
	pricing = parsePricing(t, `{"gpt-5.6-sol":{"input":5,"output":30}}`)
	cost, priced, ok := domain.CodexCost(transcript, pricing)
	if !ok || !priced {
		t.Fatalf("CodexCost(cache 系欠落) priced = %v, ok = %v, want ともに true", priced, ok)
	}
	// 1M*$5 + 0.1M*$30 = 8.0(cache 2 項は 0 扱い)
	if cost != 8.0 {
		t.Errorf("CodexCost() = %v, want 8.0", cost)
	}
}

func TestParseCodexTranscriptRejectsNonStringToolName(t *testing.T) {
	t.Parallel()

	// 現行仕様との意図的な差異。現行版は `.name // .type` を unique に流すだけで
	// 型を見ないため、name が数値だと tools_used に数値が入った summary が出る
	// (実測: `{"tools_used":[7], ...}`)。tools_used を []string で表す Go 版では
	// 同じ JSON を作れないため、値の型を偽らずフォールバック(summary: null)へ
	// 落とす。実在の rollout では name は常に文字列である。
	raw := `{"type":"response_item","payload":{"type":"function_call","name":7}}`
	if _, ok := domain.ParseCodexTranscript([]byte(raw)); ok {
		t.Error("ParseCodexTranscript() ok = true, want false")
	}
}
