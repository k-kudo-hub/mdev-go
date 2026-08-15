package domain_test

import (
	"strings"
	"testing"

	"github.com/k-kudo-hub/mdev-go/internal/domain"
)

// codexHooksJSON は Codex アプリが settings.json から写した形を組み立てる。
//
// 形は Claude Code の settings.json と同じで、イベント → matcher の配列 →
// hook の配列である。実環境の config.toml に残っていた
// `[hooks.state]."....json:stop:0:1"` という鍵がこの入れ子を裏づけている。
func codexHooksJSON(t *testing.T, commandsByEvent map[string][]string) []byte {
	t.Helper()

	var events []string
	for event, commands := range commandsByEvent {
		var hooks []string
		for _, command := range commands {
			hooks = append(hooks, `{"type":"command","command":`+quoteJSONString(command)+`}`)
		}
		events = append(events, quoteJSONString(event)+
			`:[{"matcher":"","hooks":[`+strings.Join(hooks, ",")+`]}]`)
	}
	return []byte("{" + strings.Join(events, ",") + "}")
}

// quoteJSONString は文字列を JSON のリテラルにする。
func quoteJSONString(s string) string {
	var b strings.Builder
	b.WriteByte('"')
	for _, r := range s {
		switch r {
		case '"':
			b.WriteString(`\"`)
		case '\\':
			b.WriteString(`\\`)
		default:
			b.WriteRune(r)
		}
	}
	b.WriteByte('"')
	return b.String()
}

// hook コマンドの 3 形。
const (
	// shellHook は 6-3 で撤去済みのスクリプトを指す(壊れている)。
	shellHook = "${CONDUCTOR_HOME:-$HOME/.claude-conductor}/scripts/pending-notify.sh"
	// expandedShellHook は展開済みの絶対パスで書かれた同じもの。
	expandedShellHook = "/Users/dev/.claude-conductor/scripts/pending-resolve.sh"
	// goHook は Go 版を指す(exit 0 で動く)。
	goHook = "${CONDUCTOR_HOME:-$HOME/.claude-conductor}/bin/mdev hook notify"
)

// TestBrokenCodexHooksFindsRemovedScripts は撤去済みのスクリプトを指す hook を
// 見つけることを確かめる。
//
// これが codex の会話で exit 127 になっていたものである。
func TestBrokenCodexHooksFindsRemovedScripts(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		data []byte
		want []string
	}{
		{
			name: "環境変数の展開形",
			data: codexHooksJSON(t, map[string][]string{"Stop": {shellHook}}),
			want: []string{"Stop"},
		},
		{
			name: "展開済みの絶対パス",
			data: codexHooksJSON(t, map[string][]string{"UserPromptSubmit": {expandedShellHook}}),
			want: []string{"UserPromptSubmit"},
		},
		{
			name: "複数のイベント",
			data: codexHooksJSON(t, map[string][]string{
				"Stop":        {shellHook},
				"PostToolUse": {expandedShellHook},
			}),
			want: []string{"PostToolUse", "Stop"},
		},
		{
			name: "hooks キーで包まれている",
			data: []byte(`{"hooks":` + string(codexHooksJSON(t, map[string][]string{"Stop": {shellHook}})) + `}`),
			want: []string{"Stop"},
		},
		{
			// Go 版と混ざっていても、壊れているものだけを拾う。
			name: "Go 版と混在",
			data: codexHooksJSON(t, map[string][]string{"Stop": {goHook, shellHook}}),
			want: []string{"Stop"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			broken, err := domain.BrokenCodexHooks(tt.data)
			if err != nil {
				t.Fatalf("BrokenCodexHooks = %v", err)
			}
			var events []string
			for _, hook := range broken {
				events = append(events, hook.Event)
			}
			if strings.Join(events, ",") != strings.Join(tt.want, ",") {
				t.Errorf("壊れた hook = %v, want %v", events, tt.want)
			}
		})
	}
}

// TestBrokenCodexHooksIgnoresWorkingHooks は動いている hook を壊れていると
// 言わないことを確かめる。
//
// **Go 版は exit 0 で動く。** 動いているものに警告を出すと、利用者は毎回の
// install で意味の無い赤を見ることになる。
func TestBrokenCodexHooksIgnoresWorkingHooks(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		data []byte
	}{
		{name: "Go 版だけ", data: codexHooksJSON(t, map[string][]string{"Stop": {goHook}})},
		{
			name: "conductor と無関係な hook",
			data: codexHooksJSON(t, map[string][]string{"PostToolUse": {"my-own-linter --fix"}}),
		},
		{
			// conductor とは無関係な scripts/ を持つ別のツール。
			name: "別ツールの scripts/",
			data: codexHooksJSON(t, map[string][]string{"Stop": {"/opt/other/scripts/hook.sh"}}),
		},
		{name: "hook が無い", data: []byte(`{}`)},
		{name: "空のイベント", data: []byte(`{"Stop":[]}`)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			broken, err := domain.BrokenCodexHooks(tt.data)
			if err != nil {
				t.Fatalf("BrokenCodexHooks = %v", err)
			}
			if len(broken) != 0 {
				t.Errorf("動いている hook を壊れていると判定した: %+v", broken)
			}
		})
	}
}

// TestBrokenCodexHooksUnparsable は読めない形で黙ることを確かめる。
// 壊れていると断定できない以上、警告を出す理由が無い。
func TestBrokenCodexHooksUnparsable(t *testing.T) {
	t.Parallel()

	broken, err := domain.BrokenCodexHooks([]byte(`{"Stop":{"unexpected":true}}`))
	if err != nil {
		t.Fatalf("BrokenCodexHooks = %v", err)
	}
	if len(broken) != 0 {
		t.Errorf("読めない形から壊れた hook を作った: %+v", broken)
	}
}

// TestBrokenCodexHooksRejectsBrokenJSON は壊れた JSON を弾くことを確かめる。
func TestBrokenCodexHooksRejectsBrokenJSON(t *testing.T) {
	t.Parallel()

	for _, data := range []string{`{"Stop":`, `null`, `[]`, `"文字列"`} {
		if _, err := domain.BrokenCodexHooks([]byte(data)); err == nil {
			t.Errorf("BrokenCodexHooks(%q) = nil, want エラー", data)
		}
	}
}

// TestRenderCodexHooksWarning は事実と選択肢が両方出ることを確かめる。
//
// **どちらを選ぶかは利用者が決める。** mdev はどちらも代行しない。
func TestRenderCodexHooksWarning(t *testing.T) {
	t.Parallel()

	lines := domain.RenderCodexHooksWarning("/c/hooks.json", []domain.HookCommand{
		{Event: "Stop", Command: shellHook},
	})
	joined := strings.Join(lines, "\n")

	for _, want := range []string{
		"/c/hooks.json", "Stop", "127", // 事実
		"rm /c/hooks.json", "動作に影響はありません", // 消す選択肢
		"bin/mdev hook <resolve|post-tool|notify>", "再信頼の確認", // 直す選択肢
	} {
		if !strings.Contains(joined, want) {
			t.Errorf("%q が無い:\n%s", want, joined)
		}
	}
}
