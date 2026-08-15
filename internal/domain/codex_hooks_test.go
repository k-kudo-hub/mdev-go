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

// conductor 由来の hook コマンド 2 形。
const (
	shellHook = "${CONDUCTOR_HOME:-$HOME/.claude-conductor}/scripts/pending-notify.sh"
	goHook    = "${CONDUCTOR_HOME:-$HOME/.claude-conductor}/bin/mdev hook notify"
)

// TestInspectCodexHooksAllConductor は全部が conductor 由来なら消してよいと
// 判定することを確かめる。
//
// 旧 Shell 形と新 Go 形の両方を対象にする。移行の途中で写された場合は前者、
// 移行後に写し直された場合は後者になる。
func TestInspectCodexHooksAllConductor(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		data []byte
	}{
		{
			name: "旧 Shell 形",
			data: codexHooksJSON(t, map[string][]string{
				"Stop":             {shellHook},
				"PostToolUse":      {"${CONDUCTOR_HOME:-$HOME/.claude-conductor}/scripts/pending-post-tool.sh"},
				"UserPromptSubmit": {"${CONDUCTOR_HOME:-$HOME/.claude-conductor}/scripts/pending-resolve.sh"},
			}),
		},
		{
			name: "新 Go 形",
			data: codexHooksJSON(t, map[string][]string{
				"Stop":             {goHook},
				"PostToolUse":      {"${CONDUCTOR_HOME:-$HOME/.claude-conductor}/bin/mdev hook post-tool"},
				"UserPromptSubmit": {"${CONDUCTOR_HOME:-$HOME/.claude-conductor}/bin/mdev hook resolve"},
			}),
		},
		{
			name: "両方が混ざっている(移行の途中)",
			data: codexHooksJSON(t, map[string][]string{"Stop": {shellHook, goHook}}),
		},
		{
			name: "hooks キーで包まれている",
			data: []byte(`{"hooks":` + string(codexHooksJSON(t, map[string][]string{"Stop": {goHook}})) + `}`),
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			report, err := domain.InspectCodexHooks(tt.data)
			if err != nil {
				t.Fatalf("InspectCodexHooks = %v", err)
			}
			if report.Verdict != domain.CodexHooksAllConductor {
				t.Errorf("Verdict = %v, want AllConductor (%+v)", report.Verdict, report)
			}
			if len(report.Foreign) != 0 {
				t.Errorf("conductor 以外と判定した: %+v", report.Foreign)
			}
		})
	}
}

// TestInspectCodexHooksMixed は他のツールの hook が混ざっていたら触らない
// 判定になることを確かめる。
//
// **巻き添えにしないことが要点である。** 利用者や他のツールが足したものを
// conductor の都合で消してはならない。
func TestInspectCodexHooksMixed(t *testing.T) {
	t.Parallel()

	data := codexHooksJSON(t, map[string][]string{
		"Stop":        {goHook},
		"PostToolUse": {"my-own-linter --fix"},
	})

	report, err := domain.InspectCodexHooks(data)
	if err != nil {
		t.Fatalf("InspectCodexHooks = %v", err)
	}
	if report.Verdict != domain.CodexHooksMixed {
		t.Fatalf("Verdict = %v, want Mixed", report.Verdict)
	}
	if len(report.Conductor) != 1 || report.Conductor[0].Event != "Stop" {
		t.Errorf("conductor 由来 = %+v", report.Conductor)
	}
	if len(report.Foreign) != 1 || report.Foreign[0].Command != "my-own-linter --fix" {
		t.Errorf("他ツール = %+v", report.Foreign)
	}
}

// TestInspectCodexHooksNone は消すものが無い場合を確かめる。
func TestInspectCodexHooksNone(t *testing.T) {
	t.Parallel()

	for _, data := range []string{`{}`, `{"hooks":{}}`, `{"Stop":[]}`, `{"Stop":[{"matcher":"","hooks":[]}]}`} {
		t.Run(data, func(t *testing.T) {
			t.Parallel()
			report, err := domain.InspectCodexHooks([]byte(data))
			if err != nil {
				t.Fatalf("InspectCodexHooks = %v", err)
			}
			if report.Verdict != domain.CodexHooksNone {
				t.Errorf("Verdict = %v, want None", report.Verdict)
			}
		})
	}
}

// TestInspectCodexHooksUnparsable は形が読めないときに触らない側へ倒れる
// ことを確かめる。
//
// conductor 由来だと判定できない以上、消してはいけない。
func TestInspectCodexHooksUnparsable(t *testing.T) {
	t.Parallel()

	// hook の並びが想定と違う形。コマンドを取り出せない。
	report, err := domain.InspectCodexHooks([]byte(`{"Stop":{"unexpected":true}}`))
	if err != nil {
		t.Fatalf("InspectCodexHooks = %v", err)
	}
	if report.Verdict == domain.CodexHooksAllConductor {
		t.Error("読めない形を「消してよい」と判定した")
	}
}

// TestInspectCodexHooksRejectsBrokenJSON は壊れた JSON を弾くことを確かめる。
func TestInspectCodexHooksRejectsBrokenJSON(t *testing.T) {
	t.Parallel()

	for _, data := range []string{`{"Stop":`, `null`, `[]`, `"文字列"`} {
		if _, err := domain.InspectCodexHooks([]byte(data)); err == nil {
			t.Errorf("InspectCodexHooks(%q) = nil, want エラー", data)
		}
	}
}

// TestRenderCodexHooksWarning は案内に手掛かりが入ることを確かめる。
func TestRenderCodexHooksWarning(t *testing.T) {
	t.Parallel()

	got := domain.RenderCodexHooksWarning("/c/hooks.json", []domain.HookCommand{
		{Event: "Stop", Command: goHook},
	})
	for _, want := range []string{"/c/hooks.json", "Stop", "触っていません"} {
		if !strings.Contains(got, want) {
			t.Errorf("%q が無い: %q", want, got)
		}
	}
}
