package app_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/k-kudo-hub/mdev-go/internal/app"
)

// settingsBefore / settingsAfter は切り替え前後の settings.json である。
// 置換規則そのものは domain のテストで固定しているので、ここでは
// 「読む → バックアップ → 書く」の順序と条件だけを見る。
const (
	settingsBefore = `{"hooks":{"Stop":[{"hooks":[{"type":"command","command":"$C/scripts/pending-notify.sh"}]}]}}`
	settingsAfter  = `{"hooks":{"Stop":[{"hooks":[{"type":"command","command":"$C/bin/mdev hook notify"}]}]}}`
)

func newSwitcher(store *fakeSettingsStore) *app.HookSwitcher {
	return &app.HookSwitcher{Settings: store}
}

func TestHookSwitcherSwitchWritesAfterBackup(t *testing.T) {
	t.Parallel()

	store := newFakeSettingsStore(settingsBefore)
	got, err := newSwitcher(store).Switch(false)
	if err != nil {
		t.Fatalf("Switch() = %v", err)
	}

	if len(got.Changes) != 1 {
		t.Fatalf("Changes = %+v, want 1 件", got.Changes)
	}
	if got.Changes[0].Event != "Stop" {
		t.Errorf("Changes[0].Event = %q, want Stop", got.Changes[0].Event)
	}
	if got.SettingsPath != store.Path() {
		t.Errorf("SettingsPath = %q, want %q", got.SettingsPath, store.Path())
	}
	if got.BackupPath == "" {
		t.Error("BackupPath が空: バックアップを作っていない")
	}
	if got.DryRun {
		t.Error("DryRun = true, want false")
	}

	// バックアップの中身は「書き換える前の内容」でなければならない。
	if len(store.backups) != 1 || store.backups[0] != settingsBefore {
		t.Errorf("バックアップ = %q, want %q", store.backups, settingsBefore)
	}
	if store.content != settingsAfter {
		t.Errorf("書き込み後の内容 = %q, want %q", store.content, settingsAfter)
	}
	// 読む → バックアップ → 書く の順であること。
	if want := []string{"Read", "Backup", "Write"}; !equalStrings(store.calls, want) {
		t.Errorf("呼び出し順 = %v, want %v", store.calls, want)
	}
}

func TestHookSwitcherSwitchIsIdempotent(t *testing.T) {
	t.Parallel()

	store := newFakeSettingsStore(settingsBefore)
	switcher := newSwitcher(store)
	if _, err := switcher.Switch(false); err != nil {
		t.Fatalf("1 回目 = %v", err)
	}

	store.calls = nil
	got, err := switcher.Switch(false)
	if err != nil {
		t.Fatalf("2 回目 = %v", err)
	}
	if len(got.Changes) != 0 {
		t.Errorf("2 回目の Changes = %+v, want 空", got.Changes)
	}
	if got.BackupPath != "" {
		t.Errorf("2 回目の BackupPath = %q, want 空", got.BackupPath)
	}
	// 変更が無いなら書き込みもバックアップもしない。無意味なバックアップが
	// 積み上がると、restore の探す「最新のバックアップ」が切り替え後の内容に
	// なってしまう。
	if want := []string{"Read"}; !equalStrings(store.calls, want) {
		t.Errorf("2 回目の呼び出し = %v, want %v", store.calls, want)
	}
	if store.content != settingsAfter {
		t.Errorf("内容が変わった = %q", store.content)
	}
}

func TestHookSwitcherSwitchDryRunDoesNotTouchFiles(t *testing.T) {
	t.Parallel()

	store := newFakeSettingsStore(settingsBefore)
	got, err := newSwitcher(store).Switch(true)
	if err != nil {
		t.Fatalf("Switch() = %v", err)
	}

	if !got.DryRun {
		t.Error("DryRun = false, want true")
	}
	if len(got.Changes) != 1 {
		t.Errorf("Changes = %+v, want 1 件(表示はする)", got.Changes)
	}
	if got.BackupPath != "" {
		t.Errorf("BackupPath = %q, want 空", got.BackupPath)
	}
	if want := []string{"Read"}; !equalStrings(store.calls, want) {
		t.Errorf("呼び出し = %v, want %v", store.calls, want)
	}
	if store.content != settingsBefore {
		t.Errorf("内容が変わった = %q", store.content)
	}
}

func TestHookSwitcherSwitchReportsErrors(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		prepare func(*fakeSettingsStore)
		want    string
	}{
		{
			name:    "settings.json が読めない",
			prepare: func(s *fakeSettingsStore) { s.readErr = errors.New("読めない") },
			want:    "読めない",
		},
		{
			name:    "settings.json が壊れている",
			prepare: func(s *fakeSettingsStore) { s.content = `{"hooks":` },
			want:    "JSON",
		},
		{
			name:    "バックアップに失敗",
			prepare: func(s *fakeSettingsStore) { s.backupErr = errors.New("退避できない") },
			want:    "退避できない",
		},
		{
			name:    "書き込みに失敗",
			prepare: func(s *fakeSettingsStore) { s.writeErr = errors.New("書けない") },
			want:    "書けない",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			store := newFakeSettingsStore(settingsBefore)
			tt.prepare(store)

			_, err := newSwitcher(store).Switch(false)
			if err == nil {
				t.Fatal("Switch() = nil, want エラー")
			}
			if !strings.Contains(err.Error(), tt.want) {
				t.Errorf("エラー = %v, want %q を含む", err, tt.want)
			}
		})
	}
}

func TestHookSwitcherSwitchDoesNotWriteWhenBackupFails(t *testing.T) {
	t.Parallel()

	// バックアップを取れないまま書き換えると復元手段が無くなる。
	store := newFakeSettingsStore(settingsBefore)
	store.backupErr = errors.New("退避できない")

	if _, err := newSwitcher(store).Switch(false); err == nil {
		t.Fatal("Switch() = nil, want エラー")
	}
	if store.content != settingsBefore {
		t.Errorf("書き込まれた = %q, want 元のまま", store.content)
	}
	for _, call := range store.calls {
		if call == "Write" {
			t.Errorf("呼び出し = %v, want Write を含まない", store.calls)
		}
	}
}

func TestHookSwitcherRestoreWritesLatestBackup(t *testing.T) {
	t.Parallel()

	store := newFakeSettingsStore(settingsBefore)
	switcher := newSwitcher(store)
	if _, err := switcher.Switch(false); err != nil {
		t.Fatalf("Switch() = %v", err)
	}

	got, err := switcher.Restore(false)
	if err != nil {
		t.Fatalf("Restore() = %v", err)
	}
	if !got.Found {
		t.Error("Found = false, want true")
	}
	if !got.Changed {
		t.Error("Changed = false, want true")
	}
	if got.BackupPath == "" {
		t.Error("BackupPath が空")
	}
	if store.content != settingsBefore {
		t.Errorf("復元後の内容 = %q, want %q", store.content, settingsBefore)
	}
}

func TestHookSwitcherRestoreWithoutBackupReportsState(t *testing.T) {
	t.Parallel()

	// switch を一度も実行していない状態での restore はエラーにしない。
	store := newFakeSettingsStore(settingsBefore)
	got, err := newSwitcher(store).Restore(false)
	if err != nil {
		t.Fatalf("Restore() = %v", err)
	}
	if got.Found {
		t.Error("Found = true, want false")
	}
	if got.Changed {
		t.Error("Changed = true, want false")
	}
	if store.content != settingsBefore {
		t.Errorf("内容が変わった = %q", store.content)
	}
}

func TestHookSwitcherRestoreTwiceReportsNoChange(t *testing.T) {
	t.Parallel()

	store := newFakeSettingsStore(settingsBefore)
	switcher := newSwitcher(store)
	if _, err := switcher.Switch(false); err != nil {
		t.Fatalf("Switch() = %v", err)
	}
	if _, err := switcher.Restore(false); err != nil {
		t.Fatalf("1 回目の Restore() = %v", err)
	}

	store.calls = nil
	got, err := switcher.Restore(false)
	if err != nil {
		t.Fatalf("2 回目の Restore() = %v", err)
	}
	if !got.Found {
		t.Error("Found = false, want true(バックアップは残っている)")
	}
	if got.Changed {
		t.Error("Changed = true, want false(既に一致している)")
	}
	for _, call := range store.calls {
		if call == "Write" {
			t.Errorf("呼び出し = %v, want Write を含まない", store.calls)
		}
	}
}

func TestHookSwitcherRestoreDryRunDoesNotWrite(t *testing.T) {
	t.Parallel()

	store := newFakeSettingsStore(settingsBefore)
	switcher := newSwitcher(store)
	if _, err := switcher.Switch(false); err != nil {
		t.Fatalf("Switch() = %v", err)
	}

	got, err := switcher.Restore(true)
	if err != nil {
		t.Fatalf("Restore() = %v", err)
	}
	if !got.DryRun || !got.Changed {
		t.Errorf("結果 = %+v, want DryRun と Changed が true", got)
	}
	if store.content != settingsAfter {
		t.Errorf("内容が変わった = %q, want %q", store.content, settingsAfter)
	}
}

func TestHookSwitcherRestoreReportsErrors(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		prepare func(*fakeSettingsStore)
		want    string
	}{
		{
			name:    "バックアップの探索に失敗",
			prepare: func(s *fakeSettingsStore) { s.latestErr = errors.New("一覧できない") },
			want:    "一覧できない",
		},
		{
			name:    "settings.json が読めない",
			prepare: func(s *fakeSettingsStore) { s.readErr = errors.New("読めない") },
			want:    "読めない",
		},
		{
			name:    "書き込みに失敗",
			prepare: func(s *fakeSettingsStore) { s.writeErr = errors.New("書けない") },
			want:    "書けない",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			store := newFakeSettingsStore(settingsBefore)
			if _, err := newSwitcher(store).Switch(false); err != nil {
				t.Fatalf("Switch() = %v", err)
			}
			tt.prepare(store)

			if _, err := newSwitcher(store).Restore(false); err == nil {
				t.Fatal("Restore() = nil, want エラー")
			} else if !strings.Contains(err.Error(), tt.want) {
				t.Errorf("エラー = %v, want %q を含む", err, tt.want)
			}
		})
	}
}

func TestHookSwitcherRoundTripRestoresOriginalBytes(t *testing.T) {
	t.Parallel()

	// キー順とインデントを含めて元のバイト列に戻ること。
	const original = "{\n  \"model\": \"opus\",\n  \"hooks\": {\n    \"Stop\": [\n      {\n        \"hooks\": [\n          {\n            \"command\": \"${CONDUCTOR_HOME:-$HOME/.claude-conductor}/scripts/pending-notify.sh\"\n          }\n        ]\n      }\n    ]\n  }\n}\n"

	store := newFakeSettingsStore(original)
	switcher := newSwitcher(store)

	if _, err := switcher.Switch(false); err != nil {
		t.Fatalf("Switch() = %v", err)
	}
	if store.content == original {
		t.Fatal("切り替えで内容が変わっていない")
	}
	if _, err := switcher.Restore(false); err != nil {
		t.Fatalf("Restore() = %v", err)
	}
	if store.content != original {
		t.Errorf("往復後 = %q, want %q", store.content, original)
	}
}
