package cli

import (
	"bytes"
	"errors"
	"strings"
	"testing"

	"github.com/k-kudo-hub/mdev-go/internal/app"
)

// fakeHookSettingsService は hooks 切り替えユースケースの代役である。
type fakeHookSettingsService struct {
	switchDryRuns  []bool
	restoreDryRuns []bool

	switchResult  app.SwitchHooksResult
	restoreResult app.RestoreHooksResult
	switchErr     error
	restoreErr    error
}

func (s *fakeHookSettingsService) Switch(dryRun bool) (app.SwitchHooksResult, error) {
	s.switchDryRuns = append(s.switchDryRuns, dryRun)
	return s.switchResult, s.switchErr
}

func (s *fakeHookSettingsService) Restore(dryRun bool) (app.RestoreHooksResult, error) {
	s.restoreDryRuns = append(s.restoreDryRuns, dryRun)
	return s.restoreResult, s.restoreErr
}

func newHooksDeps(settings *fakeHookSettingsService) Deps {
	return Deps{
		Hooks:        &fakeHookService{},
		Record:       &fakeRecordService{},
		HookSettings: settings,
		Getenv:       func(key string) string { return testEnv[key] },
	}
}

// runCLIOut は標準出力も取れる runCLI である。
func runCLIOut(t *testing.T, deps Deps, args ...string) (code int, stdout, stderr string) {
	t.Helper()

	cmd := NewRootCommand(deps)
	var out bytes.Buffer
	cmd.SetIn(strings.NewReader(""))
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs(args)

	var errBuf bytes.Buffer
	code = execute(cmd, &errBuf)
	return code, out.String(), errBuf.String()
}

// testChanges は表示の確認に使う置換 2 件。
var testChanges = []app.HookCommandChange{
	{
		Event:  "Notification",
		Before: "${CONDUCTOR_HOME:-$HOME/.claude-conductor}/scripts/pending-notify.sh",
		After:  "${CONDUCTOR_HOME:-$HOME/.claude-conductor}/bin/mdev hook notify",
	},
	{
		Event:  "PostToolUse",
		Before: "${CONDUCTOR_HOME:-$HOME/.claude-conductor}/scripts/pending-post-tool.sh",
		After:  "${CONDUCTOR_HOME:-$HOME/.claude-conductor}/bin/mdev hook post-tool",
	},
}

// testRestoreChanges は restore の表示の確認に使う置換 2 件である。
// restore は switch の逆向きなので before / after が入れ替わる。
var testRestoreChanges = []app.HookCommandChange{
	{
		Event:  "Notification",
		Before: "${CONDUCTOR_HOME:-$HOME/.claude-conductor}/bin/mdev hook notify",
		After:  "${CONDUCTOR_HOME:-$HOME/.claude-conductor}/scripts/pending-notify.sh",
	},
	{
		Event:  "PostToolUse",
		Before: "${CONDUCTOR_HOME:-$HOME/.claude-conductor}/bin/mdev hook post-tool",
		After:  "${CONDUCTOR_HOME:-$HOME/.claude-conductor}/scripts/pending-post-tool.sh",
	},
}

func TestHooksSwitchShowsChangesAndBackup(t *testing.T) {
	t.Parallel()

	settings := &fakeHookSettingsService{switchResult: app.SwitchHooksResult{
		SettingsPath: "/home/x/.claude/settings.json",
		Changes:      testChanges,
		BackupPath:   "/home/x/.claude/settings.json.mdev-backup-20260809T133344Z",
	}}

	code, stdout, stderr := runCLIOut(t, newHooksDeps(settings), "hooks", "switch")
	if code != exitOK {
		t.Fatalf("終了コード = %d, want %d (stderr=%q)", code, exitOK, stderr)
	}
	if want := []bool{false}; len(settings.switchDryRuns) != 1 || settings.switchDryRuns[0] != want[0] {
		t.Errorf("Switch の dryRun = %v, want %v", settings.switchDryRuns, want)
	}

	// 変更前後の内容とバックアップ先が読み取れること。
	for _, want := range []string{
		"/home/x/.claude/settings.json",
		"Notification",
		"PostToolUse",
		testChanges[0].Before,
		testChanges[0].After,
		testChanges[1].Before,
		testChanges[1].After,
		"/home/x/.claude/settings.json.mdev-backup-20260809T133344Z",
	} {
		if !strings.Contains(stdout, want) {
			t.Errorf("出力に %q が無い:\n%s", want, stdout)
		}
	}
}

func TestHooksSwitchDryRunDoesNotClaimToHaveWritten(t *testing.T) {
	t.Parallel()

	settings := &fakeHookSettingsService{switchResult: app.SwitchHooksResult{
		SettingsPath: "/home/x/.claude/settings.json",
		Changes:      testChanges,
		DryRun:       true,
	}}

	code, stdout, stderr := runCLIOut(t, newHooksDeps(settings), "hooks", "switch", "--dry-run")
	if code != exitOK {
		t.Fatalf("終了コード = %d, want %d (stderr=%q)", code, exitOK, stderr)
	}
	if len(settings.switchDryRuns) != 1 || !settings.switchDryRuns[0] {
		t.Errorf("Switch の dryRun = %v, want [true]", settings.switchDryRuns)
	}
	if !strings.Contains(stdout, "--dry-run") {
		t.Errorf("dry-run である旨が出ていない:\n%s", stdout)
	}
	if !strings.Contains(stdout, testChanges[0].After) {
		t.Errorf("変更内容が出ていない:\n%s", stdout)
	}
	if strings.Contains(stdout, "バックアップ") {
		t.Errorf("dry-run なのにバックアップに触れている:\n%s", stdout)
	}
}

func TestHooksSwitchWithoutChanges(t *testing.T) {
	t.Parallel()

	settings := &fakeHookSettingsService{switchResult: app.SwitchHooksResult{
		SettingsPath: "/home/x/.claude/settings.json",
	}}

	code, stdout, _ := runCLIOut(t, newHooksDeps(settings), "hooks", "switch")
	if code != exitOK {
		t.Fatalf("終了コード = %d, want %d", code, exitOK)
	}
	if !strings.Contains(stdout, "変更はありません") {
		t.Errorf("変更が無いことが伝わらない:\n%s", stdout)
	}
}

func TestHooksRestoreReportsStates(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		result     app.RestoreHooksResult
		args       []string
		wantOut    []string
		wantAbsent []string
	}{
		{
			name: "元へ戻した",
			result: app.RestoreHooksResult{
				SettingsPath: "/home/x/.claude/settings.json",
				Changes:      testRestoreChanges,
			},
			args: []string{"hooks", "restore"},
			// switch と対称に、戻す内容の before / after が読み取れること。
			wantOut: []string{
				"Notification",
				testRestoreChanges[0].Before,
				testRestoreChanges[0].After,
				"戻しました",
			},
			// 通常の復元はバックアップを使わない。触れてはならない。
			wantAbsent: []string{"バックアップ"},
		},
		{
			name: "既にスクリプトを指している",
			result: app.RestoreHooksResult{
				SettingsPath: "/home/x/.claude/settings.json",
			},
			args:       []string{"hooks", "restore"},
			wantOut:    []string{"変更はありません"},
			wantAbsent: []string{"戻しました", "バックアップ"},
		},
		{
			name: "dry-run",
			result: app.RestoreHooksResult{
				SettingsPath: "/home/x/.claude/settings.json",
				Changes:      testRestoreChanges,
				DryRun:       true,
			},
			args:       []string{"hooks", "restore", "--dry-run"},
			wantOut:    []string{"--dry-run", testRestoreChanges[0].After},
			wantAbsent: []string{"戻しました"},
		},
		{
			name: "settings.json が無くバックアップから復元した",
			result: app.RestoreHooksResult{
				SettingsPath:       "/home/x/.claude/settings.json",
				SettingsMissing:    true,
				BackupPath:         "/home/x/.claude/settings.json.mdev-backup-20260809T133344Z",
				RestoredFromBackup: true,
			},
			args: []string{"hooks", "restore"},
			wantOut: []string{
				"settings.json がありません",
				"settings.json.mdev-backup-20260809T133344Z",
				"復元しました",
			},
		},
		{
			name: "settings.json もバックアップも無い",
			result: app.RestoreHooksResult{
				SettingsPath:    "/home/x/.claude/settings.json",
				SettingsMissing: true,
			},
			args:       []string{"hooks", "restore"},
			wantOut:    []string{"settings.json がありません", "復元できません"},
			wantAbsent: []string{"復元しました"},
		},
		{
			name: "settings.json が無い状態の dry-run",
			result: app.RestoreHooksResult{
				SettingsPath:       "/home/x/.claude/settings.json",
				SettingsMissing:    true,
				BackupPath:         "/home/x/.claude/settings.json.mdev-backup-20260809T133344Z",
				RestoredFromBackup: true,
				DryRun:             true,
			},
			args:       []string{"hooks", "restore", "--dry-run"},
			wantOut:    []string{"--dry-run", "settings.json.mdev-backup-20260809T133344Z"},
			wantAbsent: []string{"復元しました"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			settings := &fakeHookSettingsService{restoreResult: tt.result}
			code, stdout, stderr := runCLIOut(t, newHooksDeps(settings), tt.args...)
			if code != exitOK {
				t.Fatalf("終了コード = %d, want %d (stderr=%q)", code, exitOK, stderr)
			}
			for _, want := range tt.wantOut {
				if !strings.Contains(stdout, want) {
					t.Errorf("出力に %q が無い:\n%s", want, stdout)
				}
			}
			for _, absent := range tt.wantAbsent {
				if strings.Contains(stdout, absent) {
					t.Errorf("出力に %q があってはならない:\n%s", absent, stdout)
				}
			}
		})
	}
}

func TestHooksRestorePassesDryRun(t *testing.T) {
	t.Parallel()

	settings := &fakeHookSettingsService{}
	if _, _, stderr := runCLIOut(t, newHooksDeps(settings), "hooks", "restore", "--dry-run"); stderr != "" {
		t.Fatalf("stderr = %q", stderr)
	}
	if len(settings.restoreDryRuns) != 1 || !settings.restoreDryRuns[0] {
		t.Errorf("Restore の dryRun = %v, want [true]", settings.restoreDryRuns)
	}
}

func TestHooksReportsUseCaseErrors(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		settings *fakeHookSettingsService
		args     []string
	}{
		{
			name:     "switch",
			settings: &fakeHookSettingsService{switchErr: errors.New("退避できない")},
			args:     []string{"hooks", "switch"},
		},
		{
			name:     "restore",
			settings: &fakeHookSettingsService{restoreErr: errors.New("退避できない")},
			args:     []string{"hooks", "restore"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			code, _, stderr := runCLIOut(t, newHooksDeps(tt.settings), tt.args...)
			if code != exitError {
				t.Errorf("終了コード = %d, want %d", code, exitError)
			}
			if !strings.Contains(stderr, "退避できない") {
				t.Errorf("stderr = %q, want 原因を含む", stderr)
			}
		})
	}
}

func TestHooksSwitchWarnsAboutMissingBinary(t *testing.T) {
	t.Parallel()

	// 切り替え自体は成功しているのでエラーにはしないが、無反応の原因に
	// なるため対処方法まで伝える。dry-run でも出す。
	for _, args := range [][]string{
		{"hooks", "switch"},
		{"hooks", "switch", "--dry-run"},
	} {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			t.Parallel()

			settings := &fakeHookSettingsService{switchResult: app.SwitchHooksResult{
				SettingsPath:      "/home/x/.claude/settings.json",
				Changes:           testChanges,
				MissingBinaryPath: "/home/x/.claude-conductor/bin/mdev",
				DryRun:            len(args) == 3,
			}}

			code, stdout, stderr := runCLIOut(t, newHooksDeps(settings), args...)
			if code != exitOK {
				t.Fatalf("終了コード = %d, want %d (stderr=%q)", code, exitOK, stderr)
			}
			got := stdout + stderr
			for _, want := range []string{
				"警告",
				"/home/x/.claude-conductor/bin/mdev",
				"make install",
			} {
				if !strings.Contains(got, want) {
					t.Errorf("出力に %q が無い:\n%s", want, got)
				}
			}
		})
	}
}

func TestHooksSwitchWarnsAboutRemainingScripts(t *testing.T) {
	t.Parallel()

	settings := &fakeHookSettingsService{switchResult: app.SwitchHooksResult{
		SettingsPath: "/home/x/.claude/settings.json",
		Changes:      testChanges,
		BackupPath:   "/home/x/.claude/settings.json.mdev-backup-20260809T133344Z",
		RemainingScripts: []app.HookCommand{
			{Event: "Stop", Command: "${CONDUCTOR_HOME}/scripts/pending-notify.sh --quiet"},
		},
	}}

	code, stdout, stderr := runCLIOut(t, newHooksDeps(settings), "hooks", "switch")
	if code != exitOK {
		t.Fatalf("終了コード = %d, want %d (stderr=%q)", code, exitOK, stderr)
	}
	got := stdout + stderr
	for _, want := range []string{
		"警告",
		"残っています",
		"Stop",
		"${CONDUCTOR_HOME}/scripts/pending-notify.sh --quiet",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("出力に %q が無い:\n%s", want, got)
		}
	}
}

func TestHooksSwitchWithoutWarningStaysQuiet(t *testing.T) {
	t.Parallel()

	settings := &fakeHookSettingsService{switchResult: app.SwitchHooksResult{
		SettingsPath: "/home/x/.claude/settings.json",
		Changes:      testChanges,
		BackupPath:   "/home/x/.claude/settings.json.mdev-backup-20260809T133344Z",
	}}

	_, stdout, stderr := runCLIOut(t, newHooksDeps(settings), "hooks", "switch")
	if got := stdout + stderr; strings.Contains(got, "警告") {
		t.Errorf("不要な警告が出ている:\n%s", got)
	}
}

func TestHooksSwitchReportsBackupPathOnWriteFailure(t *testing.T) {
	t.Parallel()

	// 書き込みに失敗しても退避は済んでいる。バックアップの場所を伝えないと、
	// 利用者は settings.json が半端な状態かどうかを判断できない。
	settings := &fakeHookSettingsService{
		switchResult: app.SwitchHooksResult{
			SettingsPath: "/home/x/.claude/settings.json",
			Changes:      testChanges,
			BackupPath:   "/home/x/.claude/settings.json.mdev-backup-20260809T133344Z",
		},
		switchErr: errors.New("書けない"),
	}

	code, stdout, stderr := runCLIOut(t, newHooksDeps(settings), "hooks", "switch")
	if code != exitError {
		t.Fatalf("終了コード = %d, want %d", code, exitError)
	}
	// runCLIOut は cobra の stderr を stdout と同じバッファへ集めている。
	got := stdout + stderr
	for _, want := range []string{
		"書けない",
		"変更されていません",
		"/home/x/.claude/settings.json.mdev-backup-20260809T133344Z",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("出力に %q が無い:\n%s", want, got)
		}
	}
}

func TestHooksSwitchWithoutBackupDoesNotMentionIt(t *testing.T) {
	t.Parallel()

	// 退避の前に失敗した場合は触れるバックアップが無い。
	settings := &fakeHookSettingsService{
		switchResult: app.SwitchHooksResult{SettingsPath: "/home/x/.claude/settings.json"},
		switchErr:    errors.New("退避できない"),
	}

	code, stdout, stderr := runCLIOut(t, newHooksDeps(settings), "hooks", "switch")
	if code != exitError {
		t.Fatalf("終了コード = %d, want %d", code, exitError)
	}
	if got := stdout + stderr; strings.Contains(got, "バックアップ") {
		t.Errorf("存在しないバックアップに触れている:\n%s", got)
	}
}

func TestHooksWithoutSubCommandShowsHelp(t *testing.T) {
	t.Parallel()

	settings := &fakeHookSettingsService{}
	code, stdout, stderr := runCLIOut(t, newHooksDeps(settings), "hooks")
	if code != exitOK {
		t.Fatalf("終了コード = %d, want %d (stderr=%q)", code, exitOK, stderr)
	}
	for _, name := range []string{"switch", "restore"} {
		if !strings.Contains(stdout, name) {
			t.Errorf("ヘルプに %q が無い:\n%s", name, stdout)
		}
	}
	if len(settings.switchDryRuns) != 0 || len(settings.restoreDryRuns) != 0 {
		t.Error("ユースケースが呼ばれている")
	}
}

func TestHooksRejectsUnknownSubCommandAndArgs(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		args []string
	}{
		{name: "未知のサブコマンド", args: []string{"hooks", "unknown"}},
		{name: "余分な引数", args: []string{"hooks", "switch", "extra"}},
		{name: "未知のフラグ", args: []string{"hooks", "restore", "--force"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			settings := &fakeHookSettingsService{}
			code, _, stderr := runCLIOut(t, newHooksDeps(settings), tt.args...)
			if code != exitError {
				t.Errorf("終了コード = %d, want %d", code, exitError)
			}
			if stderr == "" {
				t.Error("stderr が空")
			}
			if len(settings.switchDryRuns) != 0 || len(settings.restoreDryRuns) != 0 {
				t.Error("ユースケースが呼ばれている")
			}
		})
	}
}

func TestHookAndHooksAreDistinctCommands(t *testing.T) {
	t.Parallel()

	// `mdev hook`(Claude Code から呼ばれる)と `mdev hooks`(利用者が実行する)は
	// 名前が似ているため、取り違えが起きていないことを確かめる。
	settings := &fakeHookSettingsService{}
	hooks := &fakeHookService{}
	deps := newHooksDeps(settings)
	deps.Hooks = hooks

	if code, _, stderr := runCLIOut(t, deps, "hooks", "switch"); code != exitOK {
		t.Fatalf("hooks switch の終了コード = %d (stderr=%q)", code, stderr)
	}
	if len(hooks.calls) != 0 {
		t.Errorf("hook ユースケースが呼ばれた: %+v", hooks.calls)
	}
	if len(settings.switchDryRuns) != 1 {
		t.Errorf("hooks ユースケースが呼ばれていない: %v", settings.switchDryRuns)
	}
}

// TestHooksSwitchShowsFlavorPath は切り替えたときに印の置き場所を出す
// ことを確かめる。
//
// 印は conductor の install.sh が読むファイルで、利用者から見ると
// 「なぜ設定が巻き戻らなくなったのか」の根拠になる。どこに置いたかを
// 出さないと、手で消したいときに探せない。
func TestHooksSwitchShowsFlavorPath(t *testing.T) {
	t.Parallel()

	const flavorPath = "/tmp/fake/.claude-conductor/FLAVOR"
	tests := []struct {
		name   string
		result app.SwitchHooksResult
	}{
		{
			name: "hooks を切り替えた",
			result: app.SwitchHooksResult{
				SettingsPath: "/tmp/fake/.claude/settings.json",
				Changes:      []app.HookCommandChange{{Event: "Stop", Before: "a", After: "b"}},
				BackupPath:   "/tmp/fake/.claude/settings.json.mdev-backup-0",
				FlavorPath:   flavorPath,
			},
		},
		{
			// 変更が無くても印は書き直すので、こちらでも出す。
			name: "既に切り替え済みで変更が無い",
			result: app.SwitchHooksResult{
				SettingsPath: "/tmp/fake/.claude/settings.json",
				FlavorPath:   flavorPath,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			settings := &fakeHookSettingsService{switchResult: tt.result}
			code, stdout, stderr := runHooksCLI(t, newHooksDeps(settings), "hooks", "switch")

			if code != exitOK {
				t.Errorf("終了コード = %d, want %d (stderr=%q)", code, exitOK, stderr)
			}
			if !strings.Contains(stdout, flavorPath) {
				t.Errorf("標準出力に印の置き場所がありません:\n%s", stdout)
			}
		})
	}
}

// TestHooksSwitchDryRunHidesFlavorPath は dry-run で印の行を出さないことを
// 確かめる。書いていないものを「書いた」と読める形で出してはならない。
func TestHooksSwitchDryRunHidesFlavorPath(t *testing.T) {
	t.Parallel()

	settings := &fakeHookSettingsService{switchResult: app.SwitchHooksResult{
		SettingsPath: "/tmp/fake/.claude/settings.json",
		Changes:      []app.HookCommandChange{{Event: "Stop", Before: "a", After: "b"}},
		DryRun:       true,
	}}
	code, stdout, stderr := runHooksCLI(t, newHooksDeps(settings), "hooks", "switch", "--dry-run")

	if code != exitOK {
		t.Errorf("終了コード = %d, want %d (stderr=%q)", code, exitOK, stderr)
	}
	if strings.Contains(stdout, "FLAVOR") {
		t.Errorf("dry-run なのに印の行が出ています:\n%s", stdout)
	}
}

// TestHooksRestoreShowsFlavorPath は復元したときに印を消したことを出す
// ことを確かめる。
func TestHooksRestoreShowsFlavorPath(t *testing.T) {
	t.Parallel()

	const flavorPath = "/tmp/fake/.claude-conductor/FLAVOR"
	settings := &fakeHookSettingsService{restoreResult: app.RestoreHooksResult{
		SettingsPath: "/tmp/fake/.claude/settings.json",
		Changes:      []app.HookCommandChange{{Event: "Stop", Before: "b", After: "a"}},
		FlavorPath:   flavorPath,
	}}
	code, stdout, stderr := runHooksCLI(t, newHooksDeps(settings), "hooks", "restore")

	if code != exitOK {
		t.Errorf("終了コード = %d, want %d (stderr=%q)", code, exitOK, stderr)
	}
	if !strings.Contains(stdout, flavorPath) {
		t.Errorf("標準出力に印の置き場所がありません:\n%s", stdout)
	}
}

// runHooksCLI はコマンドを実行して終了コードと標準出力・標準エラーを返す。
func runHooksCLI(t *testing.T, deps Deps, args ...string) (int, string, string) {
	t.Helper()

	cmd := NewRootCommand(deps)
	var stdout, stderr bytes.Buffer
	cmd.SetIn(strings.NewReader(""))
	cmd.SetOut(&stdout)
	cmd.SetErr(&stderr)
	cmd.SetArgs(args)

	var execErr bytes.Buffer
	code := execute(cmd, &execErr)
	return code, stdout.String(), stderr.String() + execErr.String()
}
