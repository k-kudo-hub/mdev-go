package app_test

import (
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/k-kudo-hub/mdev-go/internal/app"
	"github.com/k-kudo-hub/mdev-go/internal/domain"
)

var _ app.ProcessExecer = (*fakeExecer)(nil)

// fakeExecer はプロセス置き換えの代役である。
type fakeExecer struct {
	// commands は Exec に渡されたコマンド。
	commands [][]string
	err      error
}

func (e *fakeExecer) Exec(command []string) error {
	e.commands = append(e.commands, command)
	return e.err
}

// configWithAgentCommand は .agent.command を持つ設定を組み立てる。
//
// JSON から起こすのは、語分割の対象になる文字列が設定ファイル経由で
// 入ってくる値だからである。構造体を直接組むと、読み取り側の解釈まで
// 込みで確かめられない。
func configWithAgentCommand(t *testing.T, command string) domain.Config {
	t.Helper()

	raw, err := json.Marshal(map[string]any{"agent": map[string]any{"command": command}})
	if err != nil {
		t.Fatalf("設定を組み立てられない: %v", err)
	}
	var config domain.Config
	if err := json.Unmarshal(raw, &config); err != nil {
		t.Fatalf("設定を読めない: %v", err)
	}
	return config
}

// TestAgentLauncherLaunch は設定から組み立てたコマンドで置き換えることを
// 確かめる。
//
// 期待値は現行 task-lib.sh の agent_command と、その結果を
// `read -r -a cmd <<< "$(...)"` で語分割する agent-launch.sh に同じ設定を
// 与えて確認した。
func TestAgentLauncherLaunch(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		command string
		// loaded は設定を読めたか。
		loaded bool
		want   []string
	}{
		{name: "設定どおりに起動する", command: "codex", loaded: true, want: []string{"codex"}},
		{
			name:    "空白で語に分ける",
			command: "codex --model gpt-5",
			loaded:  true,
			want:    []string{"codex", "--model", "gpt-5"},
		},
		{
			// read はクォートを解釈しないので、引用符は語の一部として残る。
			name:    "クォートは解釈しない",
			command: `claude --flag "a b"`,
			loaded:  true,
			want:    []string{"claude", "--flag", `"a`, `b"`},
		},
		{
			// read は 1 行しか読まない。
			name:    "2 行目以降は捨てる",
			command: "codex\nrm -rf /",
			loaded:  true,
			want:    []string{"codex"},
		},
		{name: "設定が空なら claude", command: "", loaded: true, want: []string{"claude"}},
		{name: "設定を読めなくても claude", command: "", loaded: false, want: []string{"claude"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			execer := &fakeExecer{}
			launcher := &app.AgentLauncher{
				Config: &fakeConfigLoader{config: configWithAgentCommand(t, tt.command), failed: !tt.loaded},
				Execer: execer,
			}

			if err := launcher.Launch(); err != nil {
				t.Fatalf("Launch = %v", err)
			}
			if len(execer.commands) != 1 {
				t.Fatalf("置き換え = %d 回, want 1", len(execer.commands))
			}
			if !reflect.DeepEqual(execer.commands[0], tt.want) {
				t.Errorf("コマンド = %q, want %q", execer.commands[0], tt.want)
			}
		})
	}
}

// TestAgentLauncherEmptyCommand は語が 1 つも無い設定を確かめる。
//
// 現行版との意図的な差異: 現行版は空の配列で exec を呼び、bash がそれを
// 「リダイレクトだけの exec」として黙って成功させるため、ペインが理由も
// 分からず閉じる。こちらは何が悪いのかを言って終わる。
func TestAgentLauncherEmptyCommand(t *testing.T) {
	t.Parallel()

	execer := &fakeExecer{}
	launcher := &app.AgentLauncher{
		Config: &fakeConfigLoader{config: configWithAgentCommand(t, "   ")},
		Execer: execer,
	}

	if err := launcher.Launch(); !errors.Is(err, app.ErrNoAgentCommand) {
		t.Errorf("Launch = %v, want %v", err, app.ErrNoAgentCommand)
	}
	if len(execer.commands) != 0 {
		t.Errorf("置き換えてはいけない: %q", execer.commands)
	}
}

// TestAgentLauncherExecFailure は置き換えに失敗したときの報告を確かめる。
// コマンド名が出ないと、設定のどこが悪いのか分からない。
func TestAgentLauncherExecFailure(t *testing.T) {
	t.Parallel()

	launcher := &app.AgentLauncher{
		Config: &fakeConfigLoader{config: configWithAgentCommand(t, "nosuchagent")},
		Execer: &fakeExecer{err: errors.New("見つかりません")},
	}

	err := launcher.Launch()
	if err == nil {
		t.Fatal("失敗を返すはず")
	}
	if got := err.Error(); !strings.Contains(got, "nosuchagent") {
		t.Errorf("説明 = %q, want コマンド名を含む", got)
	}
}
