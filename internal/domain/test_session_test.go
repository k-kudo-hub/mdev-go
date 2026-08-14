package domain_test

import (
	"strings"
	"testing"

	"github.com/k-kudo-hub/mdev-go/internal/domain"
)

// TestNewTestSessionSpec は起動内容の組み立てを確かめる。
//
// データは worktree の中へ隔離する。worktree を消せばデータも一緒に消える。
func TestNewTestSessionSpec(t *testing.T) {
	t.Parallel()

	const wt = "/Users/dev/projects/mdev-go/.worktree/fix-bug"
	spec := domain.NewTestSessionSpec(wt)

	tests := []struct{ name, got, want string }{
		{name: "worktree", got: spec.Worktree, want: wt},
		{name: "データ", got: spec.ConductorHome, want: wt + "/.mdev-test"},
		{name: "バイナリ", got: spec.Binary, want: wt + "/.mdev-test/bin/mdev"},
		{name: "レイアウト", got: spec.Layout, want: wt + "/.mdev-test/layouts/multi.kdl"},
		{name: "セッション", got: spec.Session, want: "test-fix-bug"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if tt.got != tt.want {
				t.Errorf("= %q, want %q", tt.got, tt.want)
			}
		})
	}
}

// TestTestSessionSpecSeparatesByPath は先頭を共有する worktree が別の
// セッションになることを確かめる。
//
// 潰れると、片方の worktree の作業がもう片方の画面に出る。
func TestTestSessionSpecSeparatesByPath(t *testing.T) {
	t.Parallel()

	base := "/Users/dev/projects/mdev-go/.worktree/"
	a := domain.NewTestSessionSpec(base + "add-a-very-long-feature-name").Session
	b := domain.NewTestSessionSpec(base + "add-a-very-long-feature-name-two").Session
	if a == b {
		t.Errorf("別の worktree が同じセッションになった: %q", a)
	}
	for _, name := range []string{a, b} {
		if len([]rune(name)) > domain.SessionNameLimit {
			t.Errorf("セッション名が長すぎる: %q", name)
		}
	}
}

// TestTestSessionSpecLaunchCommand は端末で走らせるコマンド行を確かめる。
//
// 同名のセッションを先に消すのが要点である。残っていると zellij が古い
// 直列化レイアウトを復元し、worktree の今のレイアウトで始まらない。
func TestTestSessionSpecLaunchCommand(t *testing.T) {
	t.Parallel()

	spec := domain.NewTestSessionSpec("/w/fix-bug")
	cmd := spec.LaunchCommand()

	for _, want := range []string{
		"export CONDUCTOR_HOME='/w/fix-bug/.mdev-test'",
		"cd '/w/fix-bug'",
		"'/w/fix-bug/.mdev-test/bin/mdev' news fetch",
		"zellij delete-session 'test-fix-bug' --force",
		"zellij --new-session-with-layout '/w/fix-bug/.mdev-test/layouts/multi.kdl'",
	} {
		if !strings.Contains(cmd, want) {
			t.Errorf("%q が無い:\n%s", want, cmd)
		}
	}
	// 消してから作る順序でなければ意味が無い。
	if strings.Index(cmd, "delete-session") > strings.Index(cmd, "new-session-with-layout") {
		t.Errorf("作ってから消している:\n%s", cmd)
	}
}

// TestTestSessionSpecQuotesPaths は空白を含むパスでも壊れないことを確かめる。
func TestTestSessionSpecQuotesPaths(t *testing.T) {
	t.Parallel()

	cmd := domain.NewTestSessionSpec("/Users/my dev/wt").LaunchCommand()
	if !strings.Contains(cmd, "cd '/Users/my dev/wt'") {
		t.Errorf("囲まれていない:\n%s", cmd)
	}
}

// TestRenderTestLayout はレイアウトを組んだバイナリへ向けることを確かめる。
//
// CONDUCTOR_HOME 経由のままだと、隔離ディレクトリに置き忘れたときに
// 設置済みのバイナリが動いて「切り替えたのに直っていない」になる。
func TestRenderTestLayout(t *testing.T) {
	t.Parallel()

	const template = `args "-c" "\"${CONDUCTOR_HOME:-$HOME/.claude-conductor}/bin/mdev\" pane dashboard"` + "\n" +
		`args "-c" "${CONDUCTOR_HOME:-$HOME/.claude-conductor}/bin/mdev pane news"` + "\n"

	got := domain.RenderTestLayout(template, "/w/wt/.mdev-test/bin/mdev")

	if strings.Contains(got, "CONDUCTOR_HOME") {
		t.Errorf("環境変数のままの呼び出しが残っている:\n%s", got)
	}
	for _, want := range []string{
		`"\"/w/wt/.mdev-test/bin/mdev\" pane dashboard"`,
		`"\"/w/wt/.mdev-test/bin/mdev\" pane news"`,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("%q が無い:\n%s", want, got)
		}
	}
}

// TestResolveWorktree は指定から worktree を決める順序を確かめる。
func TestResolveWorktree(t *testing.T) {
	t.Parallel()

	dirs := map[string]bool{
		"/w/explicit":             true,
		"/main/.worktree/fix-bug": true,
		"/main/.worktree/other":   true,
	}
	isDir := func(p string) bool { return dirs[p] }

	tests := []struct {
		name  string
		input string
		root  string
		want  string
	}{
		{name: "パスを直に", input: "/w/explicit", root: "/main", want: "/w/explicit"},
		{name: "ブランチ名から", input: "fix-bug", root: "/main", want: "/main/.worktree/fix-bug"},
		{name: "見つからない", input: "missing", root: "/main", want: ""},
		{name: "リポジトリの外", input: "fix-bug", root: "", want: ""},
		{name: "空の指定", input: "", root: "/main", want: ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := domain.ResolveWorktree(tt.input, tt.root, isDir); got != tt.want {
				t.Errorf("ResolveWorktree = %q, want %q", got, tt.want)
			}
		})
	}
}
