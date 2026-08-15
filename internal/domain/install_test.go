package domain_test

import (
	"strings"
	"testing"

	"github.com/k-kudo-hub/mdev-go/internal/domain"
)

// testPaths はテストで使う設置先である。
var testPaths = domain.InstallPaths{
	Home:          "/home/dev",
	ConductorHome: "/home/dev/.claude-conductor",
	Settings:      "/home/dev/.claude/settings.json",
	CodexConfig:   "/home/dev/.codex/config.toml",
	Zshrc:         "/home/dev/.zshrc",
}

// TestInstallPaths は組み立てるパスを確かめる。
//
// hooks のコマンド文字列とレイアウトが同じ規約(CONDUCTOR_HOME/bin/mdev)に
// 従うため、ここがずれると設置したバイナリと呼ばれるバイナリが食い違う。
func TestInstallPaths(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		got  string
		want string
	}{
		{name: "mdev", got: testPaths.MdevBinaryPath(), want: "/home/dev/.claude-conductor/bin/mdev"},
		{name: "scripts", got: testPaths.ScriptsDir(), want: "/home/dev/.claude-conductor/scripts"},
		{name: "init.zsh", got: testPaths.InitZshPath(), want: "/home/dev/.claude-conductor/init.zsh"},
		{
			name: "相対パス",
			got:  testPaths.ConductorPath("layouts/multi.kdl"),
			want: "/home/dev/.claude-conductor/layouts/multi.kdl",
		},
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

// TestCheckDependencies は依存の判定を確かめる。
//
// 現行 install.sh は zellij / jq / fzf / claude の 4 つを必須にしていた。
// Go 版は jq と fzf を外す(設定の加工は Go が行い、絞り込みの画面は mdev
// 自身が描くため、どちらも一度も呼ばない)。claude も単体では必須にしない。
func TestCheckDependencies(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		// have は PATH にあるコマンド。
		have []string
		// darwin は macOS かどうか。
		darwin bool
		ok     bool
		// wantProblem は続けられない理由に含まれるべき語。
		wantProblem string
		wantAgents  []string
		wantOptions []string
	}{
		{
			name: "zellij と claude があれば足りる",
			have: []string{"zellij", "claude"}, ok: true,
			wantAgents: []string{"claude"},
		},
		{
			name: "codex だけでも足りる",
			have: []string{"zellij", "codex"}, ok: true,
			wantAgents: []string{"codex"},
		},
		{
			name: "両方あれば両方を数える",
			have: []string{"zellij", "claude", "codex"}, ok: true,
			wantAgents: []string{"claude", "codex"},
		},
		{
			name: "jq と fzf は要らない",
			have: []string{"zellij", "claude"}, ok: true,
			wantAgents: []string{"claude"},
		},
		{
			name: "zellij が無ければ止める",
			have: []string{"claude"}, ok: false,
			wantProblem: "zellij",
			wantAgents:  []string{"claude"},
		},
		{
			name: "エージェントが無ければ止める",
			have: []string{"zellij"}, ok: false,
			wantProblem: "エージェント",
		},
		{
			name: "macOS では terminal-notifier を任意として数える",
			have: []string{"zellij", "claude", "terminal-notifier"}, darwin: true, ok: true,
			wantAgents: []string{"claude"}, wantOptions: []string{"terminal-notifier"},
		},
		{
			name: "macOS 以外では terminal-notifier を見ない",
			have: []string{"zellij", "claude", "terminal-notifier"}, ok: true,
			wantAgents: []string{"claude"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			have := map[string]bool{}
			for _, name := range tt.have {
				have[name] = true
			}
			// jq と fzf は在っても無くても結果を変えない。
			have["jq"] = false
			have["fzf"] = false

			report := domain.CheckDependencies(func(n string) bool { return have[n] }, tt.darwin)

			if report.OK() != tt.ok {
				t.Errorf("OK = %v, want %v (%+v)", report.OK(), tt.ok, report)
			}
			if tt.wantProblem != "" && !strings.Contains(report.Problem(), tt.wantProblem) {
				t.Errorf("Problem = %q, want %q を含む", report.Problem(), tt.wantProblem)
			}
			if tt.ok && report.Problem() != "" {
				t.Errorf("続けられるのに理由が出た: %q", report.Problem())
			}
			if strings.Join(report.Agents, ",") != strings.Join(tt.wantAgents, ",") {
				t.Errorf("Agents = %v, want %v", report.Agents, tt.wantAgents)
			}
			if strings.Join(report.Optional, ",") != strings.Join(tt.wantOptions, ",") {
				t.Errorf("Optional = %v, want %v", report.Optional, tt.wantOptions)
			}
		})
	}
}

// TestZshrcConfigured は .zshrc が入口を読み込んでいるかの判定を確かめる。
func TestZshrcConfigured(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		zshrc string
		want  bool
	}{
		{name: "現行の書き方", zshrc: domain.ZshrcSourceLine, want: true},
		{name: "絶対パスで書いてある", zshrc: `source /home/dev/.claude-conductor/init.zsh`, want: true},
		{name: "他の設定に混ざっている", zshrc: "export A=1\n" + domain.ZshrcSourceLine + "\nalias x=y\n", want: true},
		{name: "読み込んでいない", zshrc: "export A=1\n", want: false},
		{name: "空", zshrc: "", want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := domain.ZshrcConfigured(tt.zshrc); got != tt.want {
				t.Errorf("ZshrcConfigured = %v, want %v", got, tt.want)
			}
		})
	}
}

// TestMdevRepoURL は更新元が mdev-go を指すことを確かめる。
//
// ここが conductor のままだと、移行後の更新が消えたリポジトリを見に行く
// (ADR D8 の移行の要)。
func TestMdevRepoURL(t *testing.T) {
	t.Parallel()

	if !strings.HasSuffix(domain.MdevRepoURL, domain.MdevRepoSlug) {
		t.Errorf("MdevRepoURL = %q, want %q で終わる", domain.MdevRepoURL, domain.MdevRepoSlug)
	}
	if strings.Contains(domain.MdevRepoURL, "claude-conductor") {
		t.Errorf("更新元が conductor を指している: %q", domain.MdevRepoURL)
	}
}
