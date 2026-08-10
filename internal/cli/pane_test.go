package cli

import (
	"bytes"
	"errors"
	"strings"
	"testing"
)

// stubPanes は cli から見たペインの呼び出しを記録する。
type stubPanes struct {
	ran    []string
	onced  []string
	output string
	err    error
}

func (s *stubPanes) Run(name string) error {
	s.ran = append(s.ran, name)
	return s.err
}

func (s *stubPanes) Once(name string) (string, error) {
	s.onced = append(s.onced, name)
	return s.output, s.err
}

// runPaneCommand は pane サブコマンドを実行し、標準出力とエラーを返す。
func runPaneCommand(t *testing.T, panes *stubPanes, args ...string) (string, error) {
	t.Helper()

	cmd := NewRootCommand(Deps{Panes: panes})
	out := &bytes.Buffer{}
	cmd.SetOut(out)
	cmd.SetErr(out)
	cmd.SetArgs(append([]string{"pane"}, args...))
	// Execute を先に走らせてから出力を読む(同じ return 文に並べると
	// 引数の評価順で実行前の空文字を読んでしまう)。
	err := cmd.Execute()
	return out.String(), err
}

func TestPaneCommandRunsInteractively(t *testing.T) {
	t.Parallel()

	for _, name := range paneNames {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			panes := &stubPanes{}
			if _, err := runPaneCommand(t, panes, name); err != nil {
				t.Fatalf("pane %s = %v", name, err)
			}
			if len(panes.ran) != 1 || panes.ran[0] != name {
				t.Errorf("起動したペイン = %v, want [%s]", panes.ran, name)
			}
			if len(panes.onced) != 0 {
				t.Errorf("--once なしで単発描画している: %v", panes.onced)
			}
		})
	}
}

func TestPaneCommandOnceWritesRenderedText(t *testing.T) {
	t.Parallel()

	// --once は描画結果をそのまま標準出力へ書く。余計な改行は足さない
	// (現行 Shell 版の ONCE 出力とバイト単位で比べるため)。
	panes := &stubPanes{output: "画面の中身\n"}
	out, err := runPaneCommand(t, panes, "dashboard", "--once")
	if err != nil {
		t.Fatalf("pane dashboard --once = %v", err)
	}

	if out != "画面の中身\n" {
		t.Errorf("出力 = %q, want %q", out, "画面の中身\n")
	}
	if len(panes.onced) != 1 || panes.onced[0] != "dashboard" {
		t.Errorf("単発描画したペイン = %v, want [dashboard]", panes.onced)
	}
	if len(panes.ran) != 0 {
		t.Errorf("--once なのに対話モードで起動している: %v", panes.ran)
	}
}

func TestPaneCommandRejectsUnknownName(t *testing.T) {
	t.Parallel()

	panes := &stubPanes{}
	_, err := runPaneCommand(t, panes, "unknown")
	if err == nil {
		t.Fatal("未知のペイン名がエラーにならない")
	}
	if len(panes.ran) != 0 {
		t.Errorf("未知の名前で起動している: %v", panes.ran)
	}
}

func TestPaneCommandRequiresExactlyOneName(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		args []string
	}{
		{name: "名前なし", args: nil},
		{name: "名前が 2 つ", args: []string{"dashboard", "news"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if _, err := runPaneCommand(t, &stubPanes{}, tt.args...); err == nil {
				t.Error("引数の数が不正なのにエラーにならない")
			}
		})
	}
}

func TestPaneCommandPropagatesError(t *testing.T) {
	t.Parallel()

	wantErr := errors.New("ペインが落ちた")
	panes := &stubPanes{err: wantErr}
	if _, err := runPaneCommand(t, panes, "done"); !errors.Is(err, wantErr) {
		t.Errorf("エラー = %v, want %v", err, wantErr)
	}
}

func TestPaneCommandHelpListsAllPanes(t *testing.T) {
	t.Parallel()

	out, err := runPaneCommand(t, &stubPanes{}, "--help")
	if err != nil {
		t.Fatalf("pane --help = %v", err)
	}
	for _, name := range paneNames {
		if !strings.Contains(out, name) {
			t.Errorf("ヘルプに %s が出ていない: %s", name, out)
		}
	}
}
