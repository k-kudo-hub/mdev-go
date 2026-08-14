package domain_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/k-kudo-hub/mdev-go/internal/domain"
)

// hooksTemplate は同梱の hooks.json(`.hooks` の中身)を読む。
//
// assets からではなく testdata から読むのは、domain がそこへ依存できない
// ためである(ADR-0002)。中身は assets/hooks.json と同じものを写してある。
func hooksTemplate(t *testing.T) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", "hooks.json"))
	if err != nil {
		t.Fatalf("hooks.json が読めない: %v", err)
	}
	return b
}

// TestNewHookSettings は settings.json が無いときに書く中身を確かめる。
//
// 雛形は Shell 版を指しているが、書き出す時点で mdev の形になっていなければ
// ならない。新規インストールで scripts/ を呼ぶ hooks が入ると、その環境には
// scripts/ が無いので hook がすべて失敗する。
func TestNewHookSettings(t *testing.T) {
	t.Parallel()

	got, err := domain.NewHookSettings(hooksTemplate(t))
	if err != nil {
		t.Fatalf("NewHookSettings = %v", err)
	}
	if strings.Contains(string(got), "/scripts/") {
		t.Errorf("Shell 版の呼び出しが残っている:\n%s", got)
	}
	for _, want := range domain.SwitchedHookCommandSuffixes() {
		if !strings.Contains(string(got), want) {
			t.Errorf("%q が無い:\n%s", want, got)
		}
	}

	var doc struct {
		Hooks map[string]json.RawMessage `json:"hooks"`
	}
	if err := json.Unmarshal(got, &doc); err != nil {
		t.Fatalf("JSON として読めない: %v\n%s", err, got)
	}
	for _, event := range []string{"Notification", "Stop", "PostToolUse", "UserPromptSubmit"} {
		if _, ok := doc.Hooks[event]; !ok {
			t.Errorf("%q が無い:\n%s", event, got)
		}
	}
}

// TestInstallHooksAddsMissingEvents は足りないイベントだけを足すことを確かめる。
func TestInstallHooksAddsMissingEvents(t *testing.T) {
	t.Parallel()

	// Stop だけを自分で設定している利用者。
	const settings = `{
  "permissions": {"allow": ["Bash(ls:*)"]},
  "hooks": {
    "Stop": [{"matcher": "", "hooks": [{"type": "command", "command": "my-own-notify"}]}]
  }
}
`
	result, err := domain.InstallHooks([]byte(settings), hooksTemplate(t))
	if err != nil {
		t.Fatalf("InstallHooks = %v", err)
	}

	// 既にある Stop は触らない。
	if !strings.Contains(string(result.Settings), "my-own-notify") {
		t.Errorf("利用者の hook を消した:\n%s", result.Settings)
	}
	for _, added := range result.AddedEvents {
		if added == "Stop" {
			t.Error("既にある Stop を足した")
		}
	}
	// 無かったイベントは足す。
	want := []string{"Notification", "PostToolUse", "UserPromptSubmit"}
	if strings.Join(result.AddedEvents, ",") != strings.Join(want, ",") {
		t.Errorf("足したイベント = %v, want %v", result.AddedEvents, want)
	}
	// 触っていない設定は 1 バイトも動かない。
	if !strings.Contains(string(result.Settings), `"permissions": {"allow": ["Bash(ls:*)"]}`) {
		t.Errorf("他の設定が変わった:\n%s", result.Settings)
	}
	if !json.Valid(result.Settings) {
		t.Errorf("壊れた JSON になった:\n%s", result.Settings)
	}
}

// TestInstallHooksMigratesShellCommands は Shell 版からの書き換えを確かめる。
//
// 既存インストールからの移行がこの経路である。scripts/ を消す前に mdev を
// 指していなければ、hook が全部失敗する。
func TestInstallHooksMigratesShellCommands(t *testing.T) {
	t.Parallel()

	const settings = `{
  "hooks": {
    "Notification": [{"matcher": "", "hooks": [
      {"type": "command", "command": "${CONDUCTOR_HOME:-$HOME/.claude-conductor}/scripts/pending-notify.sh"}
    ]}],
    "Stop": [{"matcher": "", "hooks": [
      {"type": "command", "command": "${CONDUCTOR_HOME:-$HOME/.claude-conductor}/scripts/pending-notify.sh"}
    ]}],
    "PostToolUse": [{"matcher": "", "hooks": [
      {"type": "command", "command": "${CONDUCTOR_HOME:-$HOME/.claude-conductor}/scripts/pending-post-tool.sh"}
    ]}],
    "UserPromptSubmit": [{"matcher": "", "hooks": [
      {"type": "command", "command": "${CONDUCTOR_HOME:-$HOME/.claude-conductor}/scripts/pending-resolve.sh"}
    ]}]
  }
}
`
	result, err := domain.InstallHooks([]byte(settings), hooksTemplate(t))
	if err != nil {
		t.Fatalf("InstallHooks = %v", err)
	}

	if len(result.AddedEvents) != 0 {
		t.Errorf("足すものは無いはず: %v", result.AddedEvents)
	}
	if len(result.Changes) != 4 {
		t.Errorf("書き換え = %d 件, want 4", len(result.Changes))
	}
	if strings.Contains(string(result.Settings), "/scripts/") {
		t.Errorf("Shell 版の呼び出しが残っている:\n%s", result.Settings)
	}
	// 環境変数の展開形は残す(worktree の絶対パスを焼かない。ADR D7-3)。
	if !strings.Contains(string(result.Settings), "${CONDUCTOR_HOME:-$HOME/.claude-conductor}/bin/mdev") {
		t.Errorf("展開形が失われた:\n%s", result.Settings)
	}
}

// TestInstallHooksCreatesHooksKey は `.hooks` が無い settings.json を確かめる。
func TestInstallHooksCreatesHooksKey(t *testing.T) {
	t.Parallel()

	const settings = `{
  "permissions": {"allow": []}
}
`
	result, err := domain.InstallHooks([]byte(settings), hooksTemplate(t))
	if err != nil {
		t.Fatalf("InstallHooks = %v", err)
	}
	if !json.Valid(result.Settings) {
		t.Fatalf("壊れた JSON になった:\n%s", result.Settings)
	}
	if len(result.AddedEvents) != 4 {
		t.Errorf("足したイベント = %v, want 4 件", result.AddedEvents)
	}
	if !strings.Contains(string(result.Settings), `"permissions"`) {
		t.Errorf("他の設定が消えた:\n%s", result.Settings)
	}
}

// TestInstallHooksIsIdempotent は 2 回目が 1 バイトも書き換えないことを確かめる。
func TestInstallHooksIsIdempotent(t *testing.T) {
	t.Parallel()

	template := hooksTemplate(t)
	for _, start := range []string{"{}\n", `{"hooks":{}}`, `{"permissions":{"allow":[]}}`} {
		once, err := domain.InstallHooks([]byte(start), template)
		if err != nil {
			t.Fatalf("1 回目 = %v", err)
		}
		twice, err := domain.InstallHooks(once.Settings, template)
		if err != nil {
			t.Fatalf("2 回目 = %v", err)
		}
		if twice.Changed() {
			t.Errorf("2 回目が手を入れた: added=%v changes=%d", twice.AddedEvents, len(twice.Changes))
		}
		if string(twice.Settings) != string(once.Settings) {
			t.Errorf("2 回目が書き換えた\n--- 2 回目 ---\n%s\n--- 1 回目 ---\n%s",
				twice.Settings, once.Settings)
		}
	}
}

// TestInstallHooksRejectsBrokenSettings は壊れた settings.json を弾くことを
// 確かめる。黙って作り直すと利用者の設定が丸ごと消える。
func TestInstallHooksRejectsBrokenSettings(t *testing.T) {
	t.Parallel()

	if _, err := domain.InstallHooks([]byte(`{"hooks":`), hooksTemplate(t)); err == nil {
		t.Error("エラーを返すはず")
	}
}
