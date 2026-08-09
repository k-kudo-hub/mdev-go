package domain_test

import (
	"testing"

	"github.com/k-kudo-hub/mdev-go/internal/domain"
)

func TestClaudeMarkersMerged(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		tools []domain.ClaudeToolUse
		want  bool
	}{
		{
			name:  "MCP のマージツール",
			tools: []domain.ClaudeToolUse{{Name: "mcp__github__merge_pull_request"}},
			want:  true,
		},
		{
			name:  "MCP ツール名は完全一致",
			tools: []domain.ClaudeToolUse{{Name: "mcp__github__merge_pull_request_dry"}},
			want:  false,
		},
		{
			name:  "Bash の gh pr merge",
			tools: []domain.ClaudeToolUse{{Name: "Bash", Command: "gh pr merge 42 --squash"}},
			want:  true,
		},
		{
			name:  "空白は 1 個以上なら何個でもよい",
			tools: []domain.ClaudeToolUse{{Name: "Bash", Command: "gh  pr   merge"}},
			want:  true,
		},
		{
			name:  "タブ区切り",
			tools: []domain.ClaudeToolUse{{Name: "Bash", Command: "gh\tpr\tmerge"}},
			want:  true,
		},
		{
			name:  "改行区切り",
			tools: []domain.ClaudeToolUse{{Name: "Bash", Command: "gh\npr\nmerge"}},
			want:  true,
		},
		{
			name:  "復帰・改ページ・垂直タブ区切り",
			tools: []domain.ClaudeToolUse{{Name: "Bash", Command: "gh\rpr\vmerge"}},
			want:  true,
		},
		{
			// アンカーが無いので部分一致で真になる(現行版と同じ)。
			name:  "コマンドの途中にあっても真",
			tools: []domain.ClaudeToolUse{{Name: "Bash", Command: "echo gh pr merge"}},
			want:  true,
		},
		{
			name:  "区切りが無ければ偽",
			tools: []domain.ClaudeToolUse{{Name: "Bash", Command: "ghprmerge"}},
			want:  false,
		},
		{
			name:  "大文字小文字は区別する",
			tools: []domain.ClaudeToolUse{{Name: "Bash", Command: "GH PR MERGE"}},
			want:  false,
		},
		{
			name:  "前後に別の語が続くと偽",
			tools: []domain.ClaudeToolUse{{Name: "Bash", Command: "xghy pr merge"}},
			want:  false,
		},
		{
			// command を見るのは Bash だけ。
			name:  "Bash 以外のツールの command は見ない",
			tools: []domain.ClaudeToolUse{{Name: "Task", Command: "gh pr merge"}},
			want:  false,
		},
		{name: "ツールが無ければ偽", tools: nil, want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := domain.ClaudeMarkers(tt.tools).Merged; got != tt.want {
				t.Errorf("Merged = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestClaudeMarkersSlack(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		tool string
		want bool
	}{
		{name: "slack の MCP ツール", tool: "mcp__slack__send_message", want: true},
		{name: "前方一致なので接尾が違ってもよい", tool: "mcp__slackx", want: true},
		{name: "途中で切れていれば偽", tool: "mcp__slac", want: false},
		{name: "先頭でなければ偽", tool: "xmcp__slack", want: false},
		{name: "無関係なツール", tool: "Edit", want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := domain.ClaudeMarkers([]domain.ClaudeToolUse{{Name: tt.tool}}).Slack
			if got != tt.want {
				t.Errorf("Slack(%q) = %v, want %v", tt.tool, got, tt.want)
			}
		})
	}
}

func TestClaudeMarkersDoc(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		tool     string
		filePath string
		want     bool
	}{
		{name: "md", tool: "Write", filePath: "/x/a.md", want: true},
		{name: "mdx", tool: "Write", filePath: "/x/a.mdx", want: true},
		{name: "txt", tool: "Edit", filePath: "/x/a.txt", want: true},
		{name: "rst", tool: "Edit", filePath: "/x/a.rst", want: true},
		{name: "adoc", tool: "Edit", filePath: "/x/a.adoc", want: true},
		{name: "似た拡張子は偽", tool: "Write", filePath: "/x/a.mdd", want: false},
		{name: "末尾でなければ偽", tool: "Write", filePath: "/x/a.md.bak", want: false},
		{name: "大文字は偽", tool: "Write", filePath: "/x/README.MD", want: false},
		{name: "ドットが無ければ偽", tool: "Write", filePath: "/x/amd", want: false},
		{name: "file_path が空なら偽", tool: "Write", filePath: "", want: false},
		{name: "Write/Edit 以外は見ない", tool: "Read", filePath: "/x/a.md", want: false},
		{name: "NotebookEdit も対象外", tool: "NotebookEdit", filePath: "/x/a.md", want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			tools := []domain.ClaudeToolUse{{Name: tt.tool, FilePath: tt.filePath}}
			if got := domain.ClaudeMarkers(tools).Doc; got != tt.want {
				t.Errorf("Doc(%s %q) = %v, want %v", tt.tool, tt.filePath, got, tt.want)
			}
		})
	}
}

func TestClaudeMarkersCombined(t *testing.T) {
	t.Parallel()

	// test.sh セクション 20 の transcript は slack と doc が真、merged は偽。
	transcript, ok := domain.ParseClaudeTranscript([]byte(claudeTranscriptSection20))
	if !ok {
		t.Fatal("ParseClaudeTranscript() ok = false")
	}
	want := domain.DailyMarkers{Merged: false, Slack: true, Doc: true}
	if got := domain.ClaudeMarkers(transcript.Tools); got != want {
		t.Errorf("ClaudeMarkers() = %+v, want %+v", got, want)
	}
}

func TestMergeCommandWhitespaceMatchesOniguruma(t *testing.T) {
	t.Parallel()

	// jq(Oniguruma)の `\s` は Unicode の White_Space であり、Go の RE2 の
	// `\s`(= [\t\n\f\r ])より広い。総当たりで実測したコードポイント
	// (evidence の 6 節)と同じ集合になっていることを固定する。
	whitespace := []rune{
		0x09, 0x0A, 0x0B, 0x0C, 0x0D, 0x20, 0x85, 0xA0, 0x1680,
		0x2000, 0x2001, 0x2002, 0x2003, 0x2004, 0x2005,
		0x2006, 0x2007, 0x2008, 0x2009, 0x200A,
		0x2028, 0x2029, 0x202F, 0x205F, 0x3000,
	}
	for _, r := range whitespace {
		command := "gh" + string(r) + "pr" + string(r) + "merge"
		tools := []domain.ClaudeToolUse{{Name: "Bash", Command: command}}
		if !domain.ClaudeMarkers(tools).Merged {
			t.Errorf("U+%04X を空白として扱っていない", r)
		}
	}

	// jq の `\s` に含まれないものは区切りとして認めない。
	notWhitespace := []rune{0x08, 0x1F, 0x200B, 0x180E, 0xFEFF}
	for _, r := range notWhitespace {
		command := "gh" + string(r) + "pr" + string(r) + "merge"
		tools := []domain.ClaudeToolUse{{Name: "Bash", Command: command}}
		if domain.ClaudeMarkers(tools).Merged {
			t.Errorf("U+%04X を空白として扱っている", r)
		}
	}
}

func TestClaudeMarkersDocAnchorMatchesOniguruma(t *testing.T) {
	t.Parallel()

	// Oniguruma の `$` は文字列末尾の改行 1 個の手前にも一致する
	// (実測: jq で "a.md\n" は真、"a.md\nb" は偽)。
	tests := []struct {
		filePath string
		want     bool
	}{
		{filePath: "/x/a.md", want: true},
		{filePath: "/x/a.md\n", want: true},
		{filePath: "/x/a.md\nb", want: false},
		{filePath: "/x/a.md\n\n", want: false},
	}
	for _, tt := range tests {
		tools := []domain.ClaudeToolUse{{Name: "Write", FilePath: tt.filePath}}
		if got := domain.ClaudeMarkers(tools).Doc; got != tt.want {
			t.Errorf("Doc(%q) = %v, want %v", tt.filePath, got, tt.want)
		}
	}
}
