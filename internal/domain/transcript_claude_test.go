package domain_test

import (
	"reflect"
	"testing"

	"github.com/k-kudo-hub/mdev-go/internal/domain"
)

// claude-conductor の test.sh セクション 20 が使う transcript。
// user 3 件 / tool_use 3 件(Edit・Write・mcp__slack__send_message)。
const claudeTranscriptSection20 = `{"type":"user","message":{"role":"user","content":"hello"},"cwd":"/tmp/myapp","sessionId":"sess-rec","uuid":"u1","timestamp":"2026-04-18T10:00:00Z"}
{"type":"assistant","message":{"role":"assistant","model":"claude-opus-4-6","content":[{"type":"text","text":"hi"}],"usage":{"input_tokens":100,"output_tokens":50}},"uuid":"a1","timestamp":"2026-04-18T10:00:01Z"}
{"type":"user","message":{"role":"user","content":"fix the bug"},"cwd":"/tmp/myapp","sessionId":"sess-rec","uuid":"u2","timestamp":"2026-04-18T10:00:02Z"}
{"type":"assistant","message":{"role":"assistant","model":"claude-opus-4-6","content":[{"type":"tool_use","name":"Edit","input":{"file_path":"/tmp/app.ts"}},{"type":"tool_use","name":"Write","input":{"file_path":"/tmp/README.md"}}],"usage":{"input_tokens":200,"output_tokens":100}},"uuid":"a2","timestamp":"2026-04-18T10:00:03Z"}
{"type":"user","message":{"role":"user","content":"send to slack"},"cwd":"/tmp/myapp","sessionId":"sess-rec","uuid":"u3","timestamp":"2026-04-18T10:00:04Z"}
{"type":"assistant","message":{"role":"assistant","model":"claude-opus-4-6","content":[{"type":"tool_use","name":"mcp__slack__send_message","input":{"channel":"general","text":"done"}}],"usage":{"input_tokens":150,"output_tokens":75}},"uuid":"a3","timestamp":"2026-04-18T10:00:05Z"}
`

// test.sh セクション 26a が使う transcript(キャッシュトークンあり)。
const claudeTranscriptCache = `{"type":"user","message":{"role":"user","content":"hello"},"cwd":"/tmp","sessionId":"sess-cost","uuid":"u1","timestamp":"2026-04-19T10:00:00Z"}
{"type":"assistant","message":{"role":"assistant","model":"claude-opus-4-6","content":[{"type":"text","text":"hi"}],"usage":{"input_tokens":100,"output_tokens":500,"cache_read_input_tokens":10000,"cache_creation_input_tokens":5000,"cache_creation":{"ephemeral_5m_input_tokens":2000,"ephemeral_1h_input_tokens":3000},"speed":"standard"}},"uuid":"a1","timestamp":"2026-04-19T10:00:01Z"}
{"type":"user","message":{"role":"user","content":"more"},"cwd":"/tmp","sessionId":"sess-cost","uuid":"u2","timestamp":"2026-04-19T10:00:02Z"}
{"type":"assistant","message":{"role":"assistant","model":"claude-opus-4-6","content":[{"type":"text","text":"done"}],"usage":{"input_tokens":50,"output_tokens":300,"cache_read_input_tokens":15000,"cache_creation_input_tokens":1000,"cache_creation":{"ephemeral_5m_input_tokens":1000,"ephemeral_1h_input_tokens":0},"speed":"standard"}},"uuid":"a2","timestamp":"2026-04-19T10:00:03Z"}
`

// test.sh セクション 26b が使う transcript(fast モード)。
const claudeTranscriptFast = `{"type":"user","message":{"role":"user","content":"hello"},"cwd":"/tmp","sessionId":"sess-fast","uuid":"u1","timestamp":"2026-04-19T10:00:00Z"}
{"type":"assistant","message":{"role":"assistant","model":"claude-opus-4-6","content":[{"type":"text","text":"hi"}],"usage":{"input_tokens":1000,"output_tokens":1000,"cache_read_input_tokens":0,"cache_creation_input_tokens":0,"cache_creation":{"ephemeral_5m_input_tokens":0,"ephemeral_1h_input_tokens":0},"speed":"fast"}},"uuid":"a1","timestamp":"2026-04-19T10:00:01Z"}
`

func TestParseClaudeTranscriptAggregates(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		raw  string
		want domain.ClaudeTranscript
	}{
		{
			// test.sh セクション 20 の期待値: turns 3 / tool_calls 3。
			name: "セクション 20 の transcript",
			raw:  claudeTranscriptSection20,
			want: domain.ClaudeTranscript{
				TotalTurns: 3,
				Tools: []domain.ClaudeToolUse{
					{Name: "Edit", FilePath: "/tmp/app.ts"},
					{Name: "Write", FilePath: "/tmp/README.md"},
					{Name: "mcp__slack__send_message"},
				},
				ToolsUsed:         []string{"Edit", "Write", "mcp__slack__send_message"},
				Model:             "claude-opus-4-6",
				Speed:             "standard",
				TotalInputTokens:  450,
				TotalOutputTokens: 225,
			},
		},
		{
			// test.sh セクション 26a の期待値。
			name: "キャッシュトークンを種別ごとに合算する",
			raw:  claudeTranscriptCache,
			want: domain.ClaudeTranscript{
				TotalTurns:         2,
				Tools:              []domain.ClaudeToolUse{},
				ToolsUsed:          []string{},
				Model:              "claude-opus-4-6",
				Speed:              "standard",
				TotalInputTokens:   150,
				TotalOutputTokens:  800,
				CacheReadTokens:    25000,
				CacheWrite5mTokens: 3000,
				CacheWrite1hTokens: 3000,
			},
		},
		{
			// test.sh セクション 26b。speed は最初に見つかったものを採る。
			name: "speed=fast を拾う",
			raw:  claudeTranscriptFast,
			want: domain.ClaudeTranscript{
				TotalTurns:        1,
				Tools:             []domain.ClaudeToolUse{},
				ToolsUsed:         []string{},
				Model:             "claude-opus-4-6",
				Speed:             "fast",
				TotalInputTokens:  1000,
				TotalOutputTokens: 1000,
			},
		},
		{
			// 実測: 空ファイルでも jq は成功し、0 と "unknown" が並んだ
			// summary が出る(フォールバックには落ちない)。
			name: "空の transcript",
			raw:  "",
			want: domain.ClaudeTranscript{
				Tools:     []domain.ClaudeToolUse{},
				ToolsUsed: []string{},
				Model:     "unknown",
				Speed:     "standard",
			},
		},
		{
			name: "空白だけの transcript",
			raw:  "\n\n  \n",
			want: domain.ClaudeTranscript{
				Tools:     []domain.ClaudeToolUse{},
				ToolsUsed: []string{},
				Model:     "unknown",
				Speed:     "standard",
			},
		},
		{
			// 実測: null 行は jq でも素通りする(null への添字は null)。
			name: "null 行は無視する",
			raw:  "null\n{\"type\":\"user\"}\n",
			want: domain.ClaudeTranscript{
				TotalTurns: 1,
				Tools:      []domain.ClaudeToolUse{},
				ToolsUsed:  []string{},
				Model:      "unknown",
				Speed:      "standard",
			},
		},
		{
			// 実測: content が配列でなくても `[]?` が吸収して成功する。
			name: "content が文字列でもツール 0 件として成功する",
			raw:  `{"type":"assistant","message":{"content":"notarray"}}`,
			want: domain.ClaudeTranscript{
				Tools:     []domain.ClaudeToolUse{},
				ToolsUsed: []string{},
				Model:     "unknown",
				Speed:     "standard",
			},
		},
		{
			// 実測: 空文字は jq で真なので `// "unknown"` が働かず "" になる。
			name: "model が空文字ならそのまま空文字",
			raw:  `{"type":"assistant","message":{"model":"","content":[],"usage":{"input_tokens":1,"output_tokens":1}}}`,
			want: domain.ClaudeTranscript{
				Tools:             []domain.ClaudeToolUse{},
				ToolsUsed:         []string{},
				Model:             "",
				Speed:             "standard",
				TotalInputTokens:  1,
				TotalOutputTokens: 1,
			},
		},
		{
			// model / speed は「最初に見つかったもの」。後続で変わっても採らない。
			name: "model と speed は最初の 1 件",
			raw: `{"type":"assistant","message":{"model":"claude-opus-4-6","usage":{"speed":"fast","input_tokens":1}}}
{"type":"assistant","message":{"model":"claude-haiku-4-5","usage":{"speed":"standard","input_tokens":2}}}`,
			want: domain.ClaudeTranscript{
				Tools:            []domain.ClaudeToolUse{},
				ToolsUsed:        []string{},
				Model:            "claude-opus-4-6",
				Speed:            "fast",
				TotalInputTokens: 3,
			},
		},
		{
			// `add` は null を無視するため、欠けたキーがあっても合計は壊れない。
			name: "トークンのキーが欠けていても合算できる",
			raw: `{"type":"assistant","message":{"usage":{"input_tokens":10}}}
{"type":"assistant","message":{"usage":{"output_tokens":20}}}`,
			want: domain.ClaudeTranscript{
				Tools:             []domain.ClaudeToolUse{},
				ToolsUsed:         []string{},
				Model:             "unknown",
				Speed:             "standard",
				TotalInputTokens:  10,
				TotalOutputTokens: 20,
			},
		},
		{
			// tools_used は jq の unique と同じく「ソートして重複除去」。
			name: "tools_used はソートして重複を除く",
			raw:  `{"type":"assistant","message":{"content":[{"type":"tool_use","name":"Write","input":{}},{"type":"tool_use","name":"Bash","input":{}},{"type":"tool_use","name":"Write","input":{}}]}}`,
			want: domain.ClaudeTranscript{
				Tools: []domain.ClaudeToolUse{
					{Name: "Write"},
					{Name: "Bash"},
					{Name: "Write"},
				},
				ToolsUsed: []string{"Bash", "Write"},
				Model:     "unknown",
				Speed:     "standard",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, ok := domain.ParseClaudeTranscript([]byte(tt.raw))
			if !ok {
				t.Fatalf("ParseClaudeTranscript() ok = false, want true")
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("ParseClaudeTranscript() =\n  %+v\nwant\n  %+v", got, tt.want)
			}
		})
	}
}

func TestParseClaudeTranscriptRejects(t *testing.T) {
	t.Parallel()

	// いずれも現行版(jq)が失敗しフォールバックレコードへ落ちる入力である
	// ことを scratchpad の run.sh で実測済み。
	tests := []struct {
		name string
		raw  string
	}{
		{name: "JSON として壊れた行がある", raw: "{\"type\":\"user\"}\nnot json\n"},
		{name: "トップレベルが数値", raw: "123"},
		{name: "トップレベルが文字列", raw: `"str"`},
		{name: "トップレベルが真偽値", raw: "true"},
		{name: "トップレベルが配列", raw: "[1,2]"},
		{name: "message が文字列", raw: `{"type":"assistant","message":"str"}`},
		{name: "usage がスカラー", raw: `{"type":"assistant","message":{"usage":5}}`},
		{name: "cache_creation が文字列", raw: `{"type":"assistant","message":{"usage":{"cache_creation":"x"}}}`},
		{name: "input_tokens が文字列", raw: `{"type":"assistant","message":{"usage":{"input_tokens":"x"}}}`},
		{name: "content の要素がオブジェクトでない", raw: `{"type":"assistant","message":{"content":[42]}}`},
		{name: "tool_use に name が無い", raw: `{"type":"assistant","message":{"content":[{"type":"tool_use","input":{}}]}}`},
		{name: "tool_use の name が数値", raw: `{"type":"assistant","message":{"content":[{"type":"tool_use","name":1,"input":{}}]}}`},
		{name: "Write の file_path が数値", raw: `{"type":"assistant","message":{"content":[{"type":"tool_use","name":"Write","input":{"file_path":42}}]}}`},
		{name: "Edit の file_path が数値", raw: `{"type":"assistant","message":{"content":[{"type":"tool_use","name":"Edit","input":{"file_path":42}}]}}`},
		{name: "Bash の command が数値", raw: `{"type":"assistant","message":{"content":[{"type":"tool_use","name":"Bash","input":{"command":42}}]}}`},
		{name: "末尾にゴミがある", raw: `{"type":"user"} }`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if _, ok := domain.ParseClaudeTranscript([]byte(tt.raw)); ok {
				t.Error("ParseClaudeTranscript() ok = true, want false")
			}
		})
	}
}

func TestParseClaudeTranscriptToleratesOddInputs(t *testing.T) {
	t.Parallel()

	// 現行版が「エラーにせず false 相当で流す」入力。判定対象外のツールなら
	// file_path / command の型が違っても落ちない。
	tests := []struct {
		name string
		raw  string
		want []domain.ClaudeToolUse
	}{
		{
			name: "Write の input が無い",
			raw:  `{"type":"assistant","message":{"content":[{"type":"tool_use","name":"Write"}]}}`,
			want: []domain.ClaudeToolUse{{Name: "Write"}},
		},
		{
			name: "Write の input が null",
			raw:  `{"type":"assistant","message":{"content":[{"type":"tool_use","name":"Write","input":null}]}}`,
			want: []domain.ClaudeToolUse{{Name: "Write"}},
		},
		{
			name: "Write の input が文字列",
			raw:  `{"type":"assistant","message":{"content":[{"type":"tool_use","name":"Write","input":"x"}]}}`,
			want: []domain.ClaudeToolUse{{Name: "Write"}},
		},
		{
			name: "Write の file_path が null",
			raw:  `{"type":"assistant","message":{"content":[{"type":"tool_use","name":"Write","input":{"file_path":null}}]}}`,
			want: []domain.ClaudeToolUse{{Name: "Write"}},
		},
		{
			name: "判定対象外のツールなら file_path が数値でも落ちない",
			raw:  `{"type":"assistant","message":{"content":[{"type":"tool_use","name":"Read","input":{"file_path":42}}]}}`,
			want: []domain.ClaudeToolUse{{Name: "Read"}},
		},
		{
			name: "判定対象外のツールなら command が数値でも落ちない",
			raw:  `{"type":"assistant","message":{"content":[{"type":"tool_use","name":"Read","input":{"command":42}}]}}`,
			want: []domain.ClaudeToolUse{{Name: "Read"}},
		},
		{
			name: "tool_use 以外のブロックは数えない",
			raw:  `{"type":"assistant","message":{"content":[{"type":"text","text":"hi"},{"type":"thinking"}]}}`,
			want: []domain.ClaudeToolUse{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, ok := domain.ParseClaudeTranscript([]byte(tt.raw))
			if !ok {
				t.Fatal("ParseClaudeTranscript() ok = false, want true")
			}
			if !reflect.DeepEqual(got.Tools, tt.want) {
				t.Errorf("Tools = %+v, want %+v", got.Tools, tt.want)
			}
		})
	}
}
