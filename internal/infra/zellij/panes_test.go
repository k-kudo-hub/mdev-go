package zellij

import (
	"reflect"
	"testing"
	"time"

	"github.com/k-kudo-hub/mdev-go/internal/app"
)

// listPanesJSON は test.sh 17b7 が使うペイン一覧である。
// tab-bar プラグイン、codex のエージェントペイン、task-control のペイン、
// claude のエージェントペインが並んでいる。
const listPanesJSON = `[
  {"id":0,"is_plugin":true,"tab_name":"cx-task","title":"tab-bar"},
  {"id":5,"is_plugin":false,"tab_name":"cx-task","terminal_command":"env TASK_TAB_NAME=cx-task TASK_TYPE=dev TASK_AGENT=codex codex","title":"codex"},
  {"id":6,"is_plugin":false,"tab_name":"cx-task","terminal_command":"bash task-control.sh cx-task","title":"bar"},
  {"id":7,"is_plugin":false,"tab_name":"cl-task","terminal_command":"env TASK_TAB_NAME=cl-task TASK_TYPE=dev TASK_AGENT=claude claude","title":"claude"}
]`

func TestTabControllerListAgentPanes(t *testing.T) {
	t.Parallel()

	var got []string
	c := &TabController{
		output: func(_ time.Duration, name string, args ...string) string {
			got = append([]string{name}, args...)
			return listPanesJSON
		},
	}

	want := []app.AgentPane{
		{Tab: "cx-task", ID: "5", Agent: "codex"},
		{Tab: "cl-task", ID: "7", Agent: "claude"},
	}
	if panes := c.ListAgentPanes(); !reflect.DeepEqual(panes, want) {
		t.Errorf("ListAgentPanes() = %+v, want %+v", panes, want)
	}
	wantCmd := []string{"zellij", "action", "list-panes", "-t", "-c", "-j"}
	if !reflect.DeepEqual(got, wantCmd) {
		t.Errorf("実行コマンド = %v, want %v", got, wantCmd)
	}
}

func TestTabControllerListAgentPanesIgnoresBadOutput(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		output string
	}{
		{name: "空(コマンドが失敗した場合を含む)", output: ""},
		{name: "JSON として壊れている", output: "{"},
		{name: "配列ではない", output: `{"id":1}`},
		{name: "空の配列", output: "[]"},
		{
			name: "TASK_AGENT= の後ろが無い",
			// jq の capture は `[^ ]+` が 1 文字も取れないと一致しないため、
			// このペインは一覧に出てこない。
			output: `[{"id":1,"is_plugin":false,"tab_name":"t","terminal_command":"env TASK_AGENT= x"}]`,
		},
		{
			name:   "タブ名が空のペインは飛ばす",
			output: `[{"id":1,"is_plugin":false,"tab_name":"","terminal_command":"env TASK_AGENT=codex codex"}]`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			c := &TabController{
				output: func(time.Duration, string, ...string) string { return tt.output },
			}
			if panes := c.ListAgentPanes(); len(panes) != 0 {
				t.Errorf("ListAgentPanes() = %+v, want 空", panes)
			}
		})
	}
}

func TestTabControllerDumpScreen(t *testing.T) {
	t.Parallel()

	var got []string
	c := &TabController{
		output: func(_ time.Duration, name string, args ...string) string {
			got = append([]string{name}, args...)
			return "line1\nline2\n"
		},
	}

	if out := c.DumpScreen("5"); out != "line1\nline2\n" {
		t.Errorf("DumpScreen() = %q", out)
	}
	want := []string{"zellij", "action", "dump-screen", "-p", "terminal_5"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("実行コマンド = %v, want %v", got, want)
	}
}

func TestTabControllerDumpScreenReturnsEmptyOnFailure(t *testing.T) {
	t.Parallel()

	// ハングして打ち切られた場合など。呼び出し側はこのペインを飛ばす。
	c := &TabController{output: func(time.Duration, string, ...string) string { return "" }}
	if out := c.DumpScreen("5"); out != "" {
		t.Errorf("DumpScreen() = %q, want 空", out)
	}
}
