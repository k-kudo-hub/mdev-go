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

// claude-conductor の test.sh セクション 26i1b が使う新形式(v2)の rollout。
// 会話もツール実行も event_msg / item_completed の item として記録され、
// 旧 user_message イベントは 1 つも出ない。content 要素の type は UserMessage が
// "text"、AgentMessage が "Text" と大文字小文字が揺れる(実データどおり)。
const codexRolloutV2 = `{"timestamp":"2026-08-10T16:24:48.000Z","type":"session_meta","payload":{"id":"thread-cxv2","cwd":"/tmp/myapp","cli_version":"0.147.0","source":"exec"}}
{"timestamp":"2026-08-10T16:24:48.100Z","type":"turn_context","payload":{"model":"gpt-5.6-sol","approval_policy":"never"}}
{"timestamp":"2026-08-10T16:24:49.000Z","type":"event_msg","payload":{"type":"task_started","turn_id":"t1"}}
{"timestamp":"2026-08-10T16:24:50.000Z","type":"event_msg","payload":{"type":"item_completed","item":{"type":"UserMessage","id":"u1","content":[{"type":"text","text":"V2USERMARKER fix the bug","text_elements":[]}]}}}
{"timestamp":"2026-08-10T16:24:51.000Z","type":"event_msg","payload":{"type":"item_completed","item":{"type":"Reasoning","id":"r1","content":[{"type":"Text","text":"V2REASONMARKER internal deliberation"}]}}}
{"timestamp":"2026-08-10T16:24:52.000Z","type":"event_msg","payload":{"type":"item_completed","item":{"type":"CommandExecution","id":"exec-1","process_id":"111","command":["/bin/zsh","-lc","npm test"],"cwd":"file:///tmp/myapp","status":"completed"}}}
{"timestamp":"2026-08-10T16:24:53.000Z","type":"event_msg","payload":{"type":"item_completed","item":{"type":"McpToolCall","id":"exec-2","server":"node_repl","tool":"js","arguments":{"code":"console.log(1)"}}}}
{"timestamp":"2026-08-10T16:24:53.200Z","type":"event_msg","payload":{"type":"item_completed","item":{"type":"FileChange","id":"exec-4","changes":{"/tmp/myapp/app.ts":{"type":"update","unified_diff":"@@ -0,0 +1 @@\n+fixed\n","move_path":null}},"status":"completed"}}}
{"timestamp":"2026-08-10T16:24:53.400Z","type":"event_msg","payload":{"type":"item_completed","item":{"type":"Extension","kind":"web.search","id":"exec-5","query":"how to fix","action":{"type":"search","query":null,"queries":["how to fix"]},"results":[]}}}
{"timestamp":"2026-08-10T16:24:54.000Z","type":"event_msg","payload":{"type":"item_completed","item":{"type":"AgentMessage","id":"m1","content":[{"type":"Text","text":"V2AGENTMARKER tests pass"}],"phase":"commentary"}}}
{"timestamp":"2026-08-10T16:24:55.000Z","type":"event_msg","payload":{"type":"item_completed","item":{"type":"UserMessage","id":"u2","content":[{"type":"text","text":"now merge it","text_elements":[]}]}}}
{"timestamp":"2026-08-10T16:24:56.000Z","type":"event_msg","payload":{"type":"item_completed","item":{"type":"CommandExecution","id":"exec-3","process_id":"112","command":["/bin/zsh","-lc","gh pr merge 12 --squash"],"cwd":"file:///tmp/myapp","status":"completed"}}}
{"timestamp":"2026-08-10T16:24:57.000Z","type":"event_msg","payload":{"type":"token_count","info":{"total_token_usage":{"input_tokens":1500000,"cached_input_tokens":500000,"cache_write_input_tokens":200000,"output_tokens":100000,"reasoning_output_tokens":0,"total_tokens":1600000}}}}
{"timestamp":"2026-08-10T16:24:58.000Z","type":"event_msg","payload":{"type":"item_completed","item":{"type":"AgentMessage","id":"m2","content":[{"type":"Text","text":"merged"}],"phase":"final_answer"}}}
{"timestamp":"2026-08-10T16:24:58.100Z","type":"event_msg","payload":{"type":"task_complete","last_agent_message":"merged","turn_id":"t1"}}
`

// codexV2ItemTools は codexRolloutV2 が持つ item ビューのツールである。
// 名前は `.name // .tool // .kind // .type` なので、McpToolCall は .tool の "js"、
// Extension は .kind の "web.search"、名前を持たない item は型そのものになる。
// merged 判定の走査対象は CommandExecution の実行コマンドと McpToolCall の
// ツール名だけで、引数の本文も出力も入らない。
func codexV2ItemTools() []domain.CodexToolCall {
	return []domain.CodexToolCall{
		{Name: "CommandExecution", MergeCommand: "/bin/zsh -lc npm test"},
		{Name: "js", MergeTool: "js"},
		{Name: "FileChange"},
		{Name: "web.search"},
		{Name: "CommandExecution", MergeCommand: "/bin/zsh -lc gh pr merge 12 --squash"},
	}
}

func TestParseCodexTranscriptSection26i1b(t *testing.T) {
	t.Parallel()

	got, ok := domain.ParseCodexTranscript([]byte(codexRolloutV2))
	if !ok {
		t.Fatal("ParseCodexTranscript() ok = false, want true")
	}

	// test.sh セクション 26i1b の期待値。response_item を 1 つも持たない
	// rollout なので、ツールは item ビューで数える(Reasoning やメッセージの
	// item はツールではないので対象外)。turns は UserMessage item の件数。
	want := domain.CodexTranscript{
		TotalTurns:        2,
		Tools:             codexV2ItemTools(),
		ItemTools:         codexV2ItemTools(),
		ToolsUsed:         []string{"CommandExecution", "FileChange", "js", "web.search"},
		Model:             "gpt-5.6-sol",
		TotalInputTokens:  1000000,
		TotalOutputTokens: 100000,
		CacheReadTokens:   500000,
		CacheWriteTokens:  200000,
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("ParseCodexTranscript() =\n  %+v\nwant\n  %+v", got, want)
	}

	// CommandExecution の command に gh pr merge があるので merged が立つ。
	if markers := domain.CodexMarkers(got); markers != (domain.DailyMarkers{Merged: true}) {
		t.Errorf("CodexMarkers() = %+v, want {Merged:true}", markers)
	}
}

func TestParseCodexTranscriptPrefersCallViewOverItemView(t *testing.T) {
	t.Parallel()

	// 同じ活動の別ビューなので、response_item のツール呼び出しが 1 件でもあれば
	// そちらだけを数える(合算すると二重計上になる)。
	raw := codexRolloutV2 +
		`{"timestamp":"2026-08-10T16:24:59.000Z","type":"response_item","payload":{"type":"custom_tool_call","id":"c1","status":"completed","call_id":"call1","name":"exec","input":"const r = await tools.exec_command({\"cmd\":\"ls\"});"}}` + "\n"

	got, ok := domain.ParseCodexTranscript([]byte(raw))
	if !ok {
		t.Fatal("ParseCodexTranscript() ok = false, want true")
	}
	wantTools := []domain.CodexToolCall{
		{Name: "exec", MergeCommand: `const r = await tools.exec_command({"cmd":"ls"});`, MergeTool: "exec"},
	}
	if !reflect.DeepEqual(got.Tools, wantTools) {
		t.Errorf("Tools = %+v, want %+v", got.Tools, wantTools)
	}
	if !reflect.DeepEqual(got.ToolsUsed, []string{"exec"}) {
		t.Errorf("ToolsUsed = %v, want [exec]", got.ToolsUsed)
	}
	// 採用されなかった item ビューも残す。merged 判定は両ビューを走査するため。
	if !reflect.DeepEqual(got.ItemTools, codexV2ItemTools()) {
		t.Errorf("ItemTools = %+v, want %+v", got.ItemTools, codexV2ItemTools())
	}
	if markers := domain.CodexMarkers(got); markers != (domain.DailyMarkers{Merged: true}) {
		t.Errorf("CodexMarkers() = %+v, want {Merged:true}", markers)
	}
}

func TestParseCodexTranscriptTurnsAreNotDoubleCounted(t *testing.T) {
	t.Parallel()

	// 旧イベントと新 item が同居しても、どちらか一方だけを採る。
	raw := codexRolloutV2 +
		`{"timestamp":"2026-08-10T16:25:01.000Z","type":"event_msg","payload":{"type":"user_message","message":"legacy duplicate of the same turn"}}` + "\n"

	got, ok := domain.ParseCodexTranscript([]byte(raw))
	if !ok {
		t.Fatal("ParseCodexTranscript() ok = false, want true")
	}
	if got.TotalTurns != 2 {
		t.Errorf("TotalTurns = %d, want 2", got.TotalTurns)
	}
}

func TestCodexMarkersIgnoresCommandOutput(t *testing.T) {
	t.Parallel()

	// CommandExecution item は stdout / aggregated_output も持つ。実機には
	// 「gh pr merge を含むファイルを cat しただけ」の item があり、item 全体を
	// 走査すると merged マーカーが誤検知する。走査対象は実行したコマンドに限る。
	raw := `{"timestamp":"2026-08-10T17:00:00.000Z","type":"turn_context","payload":{"model":"gpt-5.6-sol"}}
{"timestamp":"2026-08-10T17:00:01.000Z","type":"event_msg","payload":{"type":"item_completed","item":{"type":"UserMessage","id":"u1","content":[{"type":"text","text":"read the docs","text_elements":[]}]}}}
{"timestamp":"2026-08-10T17:00:02.000Z","type":"event_msg","payload":{"type":"item_completed","item":{"type":"CommandExecution","id":"exec-1","process_id":"1","command":["/bin/zsh","-lc","cat NOTES.md"],"cwd":"file:///tmp/myapp","status":"completed","exit_code":0,"stdout":"To land a PR run: gh pr merge 12 --squash\n","aggregated_output":"To land a PR run: gh pr merge 12 --squash\n"}}}
`
	got, ok := domain.ParseCodexTranscript([]byte(raw))
	if !ok {
		t.Fatal("ParseCodexTranscript() ok = false, want true")
	}
	if markers := domain.CodexMarkers(got); markers != (domain.DailyMarkers{}) {
		t.Errorf("CodexMarkers() = %+v, want すべて false", markers)
	}
}

func TestCodexMarkersDetectsMergeByToolName(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		raw  string
		want domain.DailyMarkers
	}{
		{
			// MCP 経由のマージは item の .tool で見分ける。
			name: "item ビューの MCP マージ",
			raw:  `{"type":"event_msg","payload":{"type":"item_completed","item":{"type":"McpToolCall","id":"m1","server":"github","tool":"merge_pull_request","arguments":{"pullNumber":12}}}}`,
			want: domain.DailyMarkers{Merged: true},
		},
		{
			// 呼び出しビューは .name で見分ける。
			name: "呼び出しビューの MCP マージ",
			raw:  `{"type":"response_item","payload":{"type":"function_call","id":"f1","call_id":"c1","name":"mcp__github__merge_pull_request","arguments":"{\"pullNumber\":12}"}}`,
			want: domain.DailyMarkers{Merged: true},
		},
		{
			// 引数の本文は走査しない。Slack 投稿が gh pr merge を引用しただけで
			// 誤検知してしまうためである。
			name: "MCP の引数に引用された gh pr merge は拾わない",
			raw:  `{"type":"event_msg","payload":{"type":"item_completed","item":{"type":"McpToolCall","id":"m1","server":"slack","tool":"send_message","arguments":{"text":"次は gh pr merge 12 をお願いします"}}}}`,
			want: domain.DailyMarkers{},
		},
		{
			// 文字列の command でも落ちず、マージを見つけられる。
			name: "文字列の command からマージを見つける",
			raw:  `{"type":"event_msg","payload":{"type":"item_completed","item":{"type":"CommandExecution","id":"e1","command":"gh pr merge 12 --squash","status":"completed"}}}`,
			want: domain.DailyMarkers{Merged: true},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, ok := domain.ParseCodexTranscript([]byte(tt.raw))
			if !ok {
				t.Fatal("ParseCodexTranscript() ok = false, want true")
			}
			if markers := domain.CodexMarkers(got); markers != tt.want {
				t.Errorf("CodexMarkers() = %+v, want %+v", markers, tt.want)
			}
		})
	}
}

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
			{Name: "exec", MergeCommand: `const r = await tools.exec_command({"cmd":"npm test"});`, MergeTool: "exec"},
			{Name: "exec", MergeCommand: `const r = await tools.exec_command({"cmd":"gh pr merge 12 --squash"});`, MergeTool: "exec"},
		},
		ItemTools:         []domain.CodexToolCall{},
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
				ItemTools: []domain.CodexToolCall{},
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
					{Name: "exec", MergeTool: "exec"},
					{Name: "apply_patch", MergeTool: "apply_patch"},
				},
				ItemTools: []domain.CodexToolCall{},
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
				ItemTools: []domain.CodexToolCall{},
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
				ItemTools: []domain.CodexToolCall{},
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
				ItemTools:         []domain.CodexToolCall{},
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
				ItemTools:         []domain.CodexToolCall{},
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
				ItemTools:  []domain.CodexToolCall{},
				ToolsUsed:  []string{},
				Model:      "unknown",
			},
		},
		{
			// `.input // .arguments // ""` の順。input が無ければ arguments。
			name: "input が無ければ arguments を使う",
			raw:  `{"type":"response_item","payload":{"type":"function_call","name":"exec","arguments":"gh pr merge 1"}}`,
			want: domain.CodexTranscript{
				Tools:     []domain.CodexToolCall{{Name: "exec", MergeCommand: "gh pr merge 1", MergeTool: "exec"}},
				ItemTools: []domain.CodexToolCall{},
				ToolsUsed: []string{"exec"},
				Model:     "unknown",
			},
		},
		{
			// 文字列以外は tostring される(オブジェクトは compact JSON)。
			name: "input がオブジェクトなら JSON 文字列にする",
			raw:  `{"type":"response_item","payload":{"type":"function_call","name":"exec","input":{"cmd":"gh pr merge 1"}}}`,
			want: domain.CodexTranscript{
				Tools:     []domain.CodexToolCall{{Name: "exec", MergeCommand: `{"cmd":"gh pr merge 1"}`, MergeTool: "exec"}},
				ItemTools: []domain.CodexToolCall{},
				ToolsUsed: []string{"exec"},
				Model:     "unknown",
			},
		},
		{
			// item ビューでツールとして数えるのは 4 種類だけ。会話や思考の item、
			// item が null の item_completed は数えない。
			name: "item ビューはツール種別の item だけを数える",
			raw: `{"type":"event_msg","payload":{"type":"item_completed","item":{"type":"CommandExecution"}}}
{"type":"event_msg","payload":{"type":"item_completed","item":{"type":"McpToolCall"}}}
{"type":"event_msg","payload":{"type":"item_completed","item":{"type":"FileChange"}}}
{"type":"event_msg","payload":{"type":"item_completed","item":{"type":"Extension"}}}
{"type":"event_msg","payload":{"type":"item_completed","item":{"type":"Reasoning"}}}
{"type":"event_msg","payload":{"type":"item_completed","item":{"type":"AgentMessage"}}}
{"type":"event_msg","payload":{"type":"item_completed","item":null}}
{"type":"event_msg","payload":{"type":"item_completed"}}`,
			want: domain.CodexTranscript{
				Tools: []domain.CodexToolCall{
					{Name: "CommandExecution"},
					{Name: "McpToolCall"},
					{Name: "FileChange"},
					{Name: "Extension"},
				},
				ItemTools: []domain.CodexToolCall{
					{Name: "CommandExecution"},
					{Name: "McpToolCall"},
					{Name: "FileChange"},
					{Name: "Extension"},
				},
				ToolsUsed: []string{"CommandExecution", "Extension", "FileChange", "McpToolCall"},
				Model:     "unknown",
			},
		},
		{
			// 名前は `.name // .tool // .kind // .type` の順に落ちる。
			name: "名前は name / tool / kind の順に優先する",
			raw: `{"type":"event_msg","payload":{"type":"item_completed","item":{"type":"Extension","name":"my_ext","tool":"t","kind":"k"}}}
{"type":"event_msg","payload":{"type":"item_completed","item":{"type":"Extension","tool":"t","kind":"k"}}}
{"type":"event_msg","payload":{"type":"item_completed","item":{"type":"Extension","kind":"k"}}}`,
			want: domain.CodexTranscript{
				Tools: []domain.CodexToolCall{
					{Name: "my_ext"}, {Name: "t"}, {Name: "k"},
				},
				ItemTools: []domain.CodexToolCall{
					{Name: "my_ext"}, {Name: "t"}, {Name: "k"},
				},
				ToolsUsed: []string{"k", "my_ext", "t"},
				Model:     "unknown",
			},
		},
		{
			// 配列の command は要素ごとに tostring して空白で繋ぐ。null は
			// 空文字ではなく "null" になる(join ではなく map(tostring) のため)。
			// arguments は走査しない。
			name: "配列の command は要素を tostring して繋ぐ",
			raw:  `{"type":"event_msg","payload":{"type":"item_completed","item":{"type":"CommandExecution","command":["a",1,null,true,{"x":1}],"arguments":5}}}`,
			want: domain.CodexTranscript{
				Tools:     []domain.CodexToolCall{{Name: "CommandExecution", MergeCommand: `a 1 null true {"x":1}`}},
				ItemTools: []domain.CodexToolCall{{Name: "CommandExecution", MergeCommand: `a 1 null true {"x":1}`}},
				ToolsUsed: []string{"CommandExecution"},
				Model:     "unknown",
			},
		},
		{
			// 配列でない command も落とさずそのまま tostring する。形が揺れても
			// レコードを丸ごと summary: null へ退避させないための型ガードである。
			name: "配列でない command も落とさず tostring する",
			raw: `{"type":"event_msg","payload":{"type":"item_completed","item":{"type":"CommandExecution","command":"gh pr merge 12 --squash"}}}
{"type":"event_msg","payload":{"type":"item_completed","item":{"type":"CommandExecution","command":{"a":1}}}}
{"type":"event_msg","payload":{"type":"item_completed","item":{"type":"CommandExecution","command":false}}}
{"type":"event_msg","payload":{"type":"item_completed","item":{"type":"CommandExecution"}}}`,
			want: domain.CodexTranscript{
				Tools: []domain.CodexToolCall{
					{Name: "CommandExecution", MergeCommand: "gh pr merge 12 --squash"},
					{Name: "CommandExecution", MergeCommand: `{"a":1}`},
					{Name: "CommandExecution"},
					{Name: "CommandExecution"},
				},
				ItemTools: []domain.CodexToolCall{
					{Name: "CommandExecution", MergeCommand: "gh pr merge 12 --squash"},
					{Name: "CommandExecution", MergeCommand: `{"a":1}`},
					{Name: "CommandExecution"},
					{Name: "CommandExecution"},
				},
				ToolsUsed: []string{"CommandExecution"},
				Model:     "unknown",
			},
		},
		{
			// 走査対象は item の種類ごとに決まっている。CommandExecution は
			// コマンド、McpToolCall はツール名だけで、それ以外は何も走査しない。
			name: "走査対象は CommandExecution のコマンドと McpToolCall のツール名",
			raw: `{"type":"event_msg","payload":{"type":"item_completed","item":{"type":"McpToolCall","server":"github","tool":"merge_pull_request","arguments":{"pullNumber":12}}}}
{"type":"event_msg","payload":{"type":"item_completed","item":{"type":"Extension","kind":"web.search","command":["gh","pr","merge"]}}}`,
			want: domain.CodexTranscript{
				Tools: []domain.CodexToolCall{
					{Name: "merge_pull_request", MergeTool: "merge_pull_request"},
					{Name: "web.search"},
				},
				ItemTools: []domain.CodexToolCall{
					{Name: "merge_pull_request", MergeTool: "merge_pull_request"},
					{Name: "web.search"},
				},
				ToolsUsed: []string{"merge_pull_request", "web.search"},
				Model:     "unknown",
			},
		},
		{
			// UserMessage item が 1 件でもあれば turns は item ビューで数える。
			name: "turns は UserMessage item を数える",
			raw: `{"type":"event_msg","payload":{"type":"item_completed","item":{"type":"UserMessage"}}}
{"type":"event_msg","payload":{"type":"item_completed","item":{"type":"AgentMessage"}}}`,
			want: domain.CodexTranscript{
				TotalTurns: 1,
				Tools:      []domain.CodexToolCall{},
				ItemTools:  []domain.CodexToolCall{},
				ToolsUsed:  []string{},
				Model:      "unknown",
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
		// 現行版は item に `.type` で添字を付けるため、オブジェクトでも null でも
		// なければ「Cannot index ... with string "type"」で落ちる。
		{name: "item がスカラー", raw: `{"type":"event_msg","payload":{"type":"item_completed","item":5}}`},
		{name: "item が配列", raw: `{"type":"event_msg","payload":{"type":"item_completed","item":["a"]}}`},
		{
			name: "item の name が数値",
			raw:  `{"type":"event_msg","payload":{"type":"item_completed","item":{"type":"CommandExecution","name":7}}}`,
		},
		{
			name: "item の tool が数値",
			raw:  `{"type":"event_msg","payload":{"type":"item_completed","item":{"type":"McpToolCall","tool":7}}}`,
		},
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
		name       string
		transcript domain.CodexTranscript
		want       domain.DailyMarkers
	}{
		{
			name: "gh pr merge を含むツール呼び出し",
			transcript: domain.CodexTranscript{
				Tools: []domain.CodexToolCall{{Name: "exec", MergeCommand: `tools.exec_command({"cmd":"gh pr merge 12"})`}},
			},
			want: domain.DailyMarkers{Merged: true},
		},
		{
			name: "含まなければ偽",
			transcript: domain.CodexTranscript{
				Tools: []domain.CodexToolCall{{Name: "exec", MergeCommand: "npm test"}},
			},
			want: domain.DailyMarkers{},
		},
		{
			// codex は slack / doc を判定しない(現行版は常に false)。
			name: "slack ツールでも slack は偽のまま",
			transcript: domain.CodexTranscript{
				Tools: []domain.CodexToolCall{{Name: "mcp__slack__send_message", MergeCommand: "/x/a.md", MergeTool: "mcp__slack__send_message"}},
			},
			want: domain.DailyMarkers{},
		},
		{
			// 採用ビューが response_item 側でも、item ビューのコマンドまで走査する
			// (真偽値なので二重計上にはならない)。
			name: "採用されなかった item ビューのコマンドでも立つ",
			transcript: domain.CodexTranscript{
				Tools:     []domain.CodexToolCall{{Name: "exec", MergeCommand: "npm test"}},
				ItemTools: []domain.CodexToolCall{{Name: "CommandExecution", MergeCommand: "/bin/zsh -lc gh pr merge 12"}},
			},
			want: domain.DailyMarkers{Merged: true},
		},
		{
			// MCP 経由のマージは引数の本文ではなくツール名で見分ける。
			name: "MCP のツール名が merge_pull_request なら立つ",
			transcript: domain.CodexTranscript{
				Tools: []domain.CodexToolCall{{Name: "merge_pull_request", MergeTool: "merge_pull_request"}},
			},
			want: domain.DailyMarkers{Merged: true},
		},
		{
			// 呼び出しビューの名前は mcp__github__merge_pull_request と長いので
			// 部分一致で拾う(claude 側は完全一致で見ている)。
			name: "呼び出しビューの MCP マージツール名でも立つ",
			transcript: domain.CodexTranscript{
				Tools: []domain.CodexToolCall{{Name: "mcp__github__merge_pull_request", MergeTool: "mcp__github__merge_pull_request"}},
			},
			want: domain.DailyMarkers{Merged: true},
		},
		{name: "ツールが無ければすべて偽", transcript: domain.CodexTranscript{}, want: domain.DailyMarkers{}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := domain.CodexMarkers(tt.transcript); got != tt.want {
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
