package cli

import (
	"bytes"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/k-kudo-hub/mdev-go/internal/app"
)

// hookCall は HookService への 1 回の呼び出しを記録する。
type hookCall struct {
	method string
	raw    string
	env    app.HookEnv
}

type fakeHookService struct {
	calls []hookCall
	err   error
}

func (s *fakeHookService) record(method string, raw []byte, env app.HookEnv) error {
	s.calls = append(s.calls, hookCall{method: method, raw: string(raw), env: env})
	return s.err
}

func (s *fakeHookService) HandleNotify(raw []byte, env app.HookEnv) error {
	return s.record("notify", raw, env)
}

func (s *fakeHookService) HandlePostTool(raw []byte, env app.HookEnv) error {
	return s.record("post-tool", raw, env)
}

func (s *fakeHookService) HandleResolve(raw []byte, env app.HookEnv) error {
	return s.record("resolve", raw, env)
}

// testEnv は hook が読む環境変数の値。
var testEnv = map[string]string{
	envZellijSession: "test-session",
	envTaskTabName:   "api-feature",
	envTaskType:      "dev",
	envTaskAgent:     "codex",
}

// runCLI は引数と標準入力を与えてコマンドを実行し、終了コードと stderr を返す。
func runCLI(t *testing.T, deps Deps, stdin string, args ...string) (int, string) {
	t.Helper()

	cmd := NewRootCommand(deps)
	cmd.SetIn(strings.NewReader(stdin))
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs(args)

	var stderr bytes.Buffer
	return execute(cmd, &stderr), stderr.String()
}

func newTestDeps(hooks *fakeHookService) Deps {
	return Deps{Hooks: hooks, Getenv: func(key string) string { return testEnv[key] }}
}

func TestHookSubCommandsPassStdinAndEnv(t *testing.T) {
	t.Parallel()

	tests := []struct {
		args   []string
		method string
	}{
		{args: []string{"hook", "notify"}, method: "notify"},
		{args: []string{"hook", "post-tool"}, method: "post-tool"},
		{args: []string{"hook", "resolve"}, method: "resolve"},
	}
	for _, tt := range tests {
		t.Run(tt.method, func(t *testing.T) {
			t.Parallel()

			hooks := &fakeHookService{}
			stdin := `{"session_id":"sess-aaa","hook_event_name":"Notification"}`
			code, stderr := runCLI(t, newTestDeps(hooks), stdin, tt.args...)

			if code != 0 {
				t.Errorf("終了コード = %d, want 0 (stderr: %s)", code, stderr)
			}
			want := []hookCall{{
				method: tt.method,
				raw:    stdin,
				env: app.HookEnv{
					ZellijSession: "test-session",
					TaskTabName:   "api-feature",
					TaskType:      "dev",
					TaskAgent:     "codex",
				},
			}}
			if !reflect.DeepEqual(hooks.calls, want) {
				t.Errorf("呼び出し = %+v, want %+v", hooks.calls, want)
			}
		})
	}
}

func TestHookSubCommandsAcceptEmptyStdin(t *testing.T) {
	t.Parallel()

	hooks := &fakeHookService{}
	code, stderr := runCLI(t, newTestDeps(hooks), "", "hook", "notify")

	if code != 0 {
		t.Errorf("終了コード = %d, want 0 (stderr: %s)", code, stderr)
	}
	if len(hooks.calls) != 1 || hooks.calls[0].raw != "" {
		t.Errorf("呼び出し = %+v, want 空入力で 1 回", hooks.calls)
	}
}

func TestHookSubCommandReportsUseCaseError(t *testing.T) {
	t.Parallel()

	hooks := &fakeHookService{err: errors.New("書き込み失敗")}
	code, stderr := runCLI(t, newTestDeps(hooks), `{}`, "hook", "resolve")

	if code != 1 {
		t.Errorf("終了コード = %d, want 1", code)
	}
	if !strings.Contains(stderr, "書き込み失敗") {
		t.Errorf("stderr = %q, エラー内容を含んでいない", stderr)
	}
}

func TestHookRejectsUnknownSubCommandAndArgs(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		args []string
	}{
		{name: "未知のサブコマンド", args: []string{"hook", "unknown"}},
		{name: "余分な引数", args: []string{"hook", "notify", "extra"}},
		// 未知のトップレベル引数はセッション名として扱われる(root の RunE)。
		// その挙動は TestRootTreatsUnknownArgAsSessionName で固定している。
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			hooks := &fakeHookService{}
			code, stderr := runCLI(t, newTestDeps(hooks), "", tt.args...)
			if code != 1 {
				t.Errorf("終了コード = %d, want 1", code)
			}
			if stderr == "" {
				t.Error("stderr が空")
			}
			if len(hooks.calls) != 0 {
				t.Errorf("呼び出し = %+v, want 呼ばれない", hooks.calls)
			}
		})
	}
}

func TestHookWithoutSubCommandShowsHelp(t *testing.T) {
	t.Parallel()

	hooks := &fakeHookService{}
	cmd := NewRootCommand(newTestDeps(hooks))
	var out bytes.Buffer
	cmd.SetIn(strings.NewReader(""))
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{"hook"})

	var stderr bytes.Buffer
	if code := execute(cmd, &stderr); code != 0 {
		t.Errorf("終了コード = %d, want 0 (stderr: %s)", code, stderr.String())
	}
	for _, name := range []string{"notify", "post-tool", "resolve"} {
		if !strings.Contains(out.String(), name) {
			t.Errorf("ヘルプに %q が出ていない: %s", name, out.String())
		}
	}
	if len(hooks.calls) != 0 {
		t.Errorf("呼び出し = %+v, want 呼ばれない", hooks.calls)
	}
}

func TestHookEnvReadsAllVariables(t *testing.T) {
	t.Parallel()

	// 環境変数が 1 つも無い場合はすべて空文字になる
	// (zellij の外・conductor 外のセッション)。
	deps := Deps{Getenv: func(string) string { return "" }}
	if got := hookEnv(deps); got != (app.HookEnv{}) {
		t.Errorf("hookEnv() = %+v, want ゼロ値", got)
	}
}

func TestHookFailureExitCodeIsNonBlocking(t *testing.T) {
	t.Parallel()

	// Claude Code の hook 仕様(https://code.claude.com/docs/en/hooks)では
	// 終了コード 2 が「ブロッキングエラー」で、Stop では会話が止まらなくなり、
	// UserPromptSubmit ではユーザーの入力が消える。mdev の hook は pending と
	// レジストリの更新という補助的な副作用であり、失敗しても会話を止めては
	// ならないため、2 以外(= 非ブロッキング)を返す。
	if exitError == exitBlocking {
		t.Fatalf("exitError = %d: ブロッキング扱いになる終了コードは使えない", exitError)
	}

	for _, name := range []string{"notify", "post-tool", "resolve"} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			hooks := &fakeHookService{err: errors.New("pending を書けない")}
			code, stderr := runCLI(t, newTestDeps(hooks), `{"session_id":"s"}`, "hook", name)

			if code != exitError {
				t.Errorf("終了コード = %d, want %d", code, exitError)
			}
			if code == exitBlocking {
				t.Errorf("終了コード = %d: 会話をブロックしてしまう", code)
			}
			// 失敗に気付けるよう、原因は stderr に出す
			// (transcript には stderr の 1 行目が出る)。
			if !strings.Contains(stderr, "pending を書けない") {
				t.Errorf("stderr = %q, want 原因を含む", stderr)
			}
		})
	}
}
