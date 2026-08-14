package cli

import (
	"errors"
	"strings"
	"testing"

	"github.com/k-kudo-hub/mdev-go/internal/app"
)

// fakeCodexService は codex の notify のユースケースの代役である。
type fakeCodexService struct {
	// raws は Notify に渡された payload。
	raws []string
	// envs は Notify に渡された環境。
	envs []app.HookEnv
	err  error
}

func (s *fakeCodexService) Notify(raw []byte, env app.HookEnv) error {
	s.raws = append(s.raws, string(raw))
	s.envs = append(s.envs, env)
	return s.err
}

// TestCodexNotifyCommandTakesLastArgument は payload の受け取り方を確かめる。
//
// codex は JSON を **最後の引数** として渡す。標準入力から読む Claude Code の
// hook とは違うため、ここを取り違えると通知が丸ごと落ちる。
func TestCodexNotifyCommandTakesLastArgument(t *testing.T) {
	t.Parallel()

	const payload = `{"type":"agent-turn-complete","thread-id":"th-1"}`

	tests := []struct {
		name string
		args []string
		want string
	}{
		{name: "引数 1 つ", args: []string{"codex", "notify", payload}, want: payload},
		{
			// codex が payload の前に何かを足しても最後を採る。
			name: "引数が複数なら最後を採る",
			args: []string{"codex", "notify", "extra", payload},
			want: payload,
		},
		{name: "引数が無ければ空", args: []string{"codex", "notify"}, want: ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			svc := &fakeCodexService{}
			deps := Deps{Codex: svc, Getenv: func(string) string { return "" }}
			code, _, stderr := runCLIWithOut(t, deps, tt.args...)

			if code != exitOK {
				t.Errorf("終了コード = %d, want %d (stderr=%q)", code, exitOK, stderr)
			}
			if len(svc.raws) != 1 {
				t.Fatalf("呼び出し = %d 回, want 1", len(svc.raws))
			}
			if svc.raws[0] != tt.want {
				t.Errorf("payload = %q, want %q", svc.raws[0], tt.want)
			}
		})
	}
}

// TestCodexNotifyCommandPassesEnv は環境変数の受け渡しを確かめる。
//
// codex は notify を子プロセスとして起動するため、タスクタブの環境がそのまま
// 継承される。ここを渡し損ねるとレジストリへの登録が丸ごと止まる。
func TestCodexNotifyCommandPassesEnv(t *testing.T) {
	t.Parallel()

	env := map[string]string{
		envZellijSession: "mdev-go-1",
		envTaskTabName:   "fix-bug",
		envTaskType:      "bugfix",
		envTaskAgent:     "codex",
	}
	svc := &fakeCodexService{}
	deps := Deps{Codex: svc, Getenv: func(k string) string { return env[k] }}

	code, _, stderr := runCLIWithOut(t, deps, "codex", "notify", "{}")

	if code != exitOK {
		t.Fatalf("終了コード = %d, want %d (stderr=%q)", code, exitOK, stderr)
	}
	want := app.HookEnv{
		ZellijSession: "mdev-go-1",
		TaskTabName:   "fix-bug",
		TaskType:      "bugfix",
		TaskAgent:     "codex",
	}
	if svc.envs[0] != want {
		t.Errorf("環境 = %#v, want %#v", svc.envs[0], want)
	}
}

// TestCodexNotifyCommandReportsFailure は失敗を終了コードで表すことを確かめる。
//
// codex は notify の終了コードを見ないが、手で走らせたときに失敗が分かる
// 必要がある。
func TestCodexNotifyCommandReportsFailure(t *testing.T) {
	t.Parallel()

	svc := &fakeCodexService{err: errors.New("書けない")}
	deps := Deps{Codex: svc, Getenv: func(string) string { return "" }}

	code, _, stderr := runCLIWithOut(t, deps, "codex", "notify", "{}")

	if code != exitError {
		t.Errorf("終了コード = %d, want %d", code, exitError)
	}
	if !strings.Contains(stderr, "書けない") {
		t.Errorf("標準エラー = %q", stderr)
	}
}
