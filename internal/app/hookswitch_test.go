package app_test

import (
	"errors"
	"fmt"
	"io/fs"
	"reflect"
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

// newSwitcher は「切り替え先のバイナリが設置済み」の状態の switcher を返す。
// バイナリの有無を見るテストだけが newSwitcherWithBinary を使う。
func newSwitcher(store *fakeSettingsStore) *app.HookSwitcher {
	return newSwitcherWithBinary(store, fakeMdevBinary{path: testMdevBinaryPath, exists: true})
}

func newSwitcherWithBinary(store *fakeSettingsStore, binary fakeMdevBinary) *app.HookSwitcher {
	return &app.HookSwitcher{Settings: store, Binary: binary}
}

func TestHookSwitcherSwitchWarnsWhenBinaryIsNotInstalled(t *testing.T) {
	t.Parallel()

	// hooks が指すことになる mdev が無いと、切り替えは成功するのに
	// ダッシュボードだけが無反応になる。エラーにはしないが黙ってもいけない。
	tests := []struct {
		name    string
		content string
		dryRun  bool
	}{
		{name: "書き込みあり", content: settingsBefore},
		{name: "dry-run", content: settingsBefore, dryRun: true},
		{name: "既に切り替え済み", content: settingsAfter},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			store := newFakeSettingsStore(tt.content)
			binary := fakeMdevBinary{path: testMdevBinaryPath, exists: false}

			got, err := newSwitcherWithBinary(store, binary).Switch(tt.dryRun)
			if err != nil {
				t.Fatalf("Switch() = %v", err)
			}
			if got.MissingBinaryPath != testMdevBinaryPath {
				t.Errorf("MissingBinaryPath = %q, want %q", got.MissingBinaryPath, testMdevBinaryPath)
			}
		})
	}
}

func TestHookSwitcherSwitchReportsRemainingScripts(t *testing.T) {
	t.Parallel()

	// 置換規則に無い亜種は切り替わらずに残る。そのイベントだけ Shell 版の
	// まま取り残されるため、切り替えたことだけを報告してはいけない。
	const withVariant = `{"hooks":{"Stop":[{"hooks":[` +
		`{"type":"command","command":"$C/scripts/pending-notify.sh"},` +
		`{"type":"command","command":"$C/scripts/pending-notify.sh --quiet"}]}]}}`

	tests := []struct {
		name   string
		dryRun bool
	}{
		{name: "書き込みあり"},
		{name: "dry-run", dryRun: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			store := newFakeSettingsStore(withVariant)
			got, err := newSwitcher(store).Switch(tt.dryRun)
			if err != nil {
				t.Fatalf("Switch() = %v", err)
			}
			want := []app.HookCommand{
				{Event: "Stop", Command: "$C/scripts/pending-notify.sh --quiet"},
			}
			if !reflect.DeepEqual(got.RemainingScripts, want) {
				t.Errorf("RemainingScripts = %+v, want %+v", got.RemainingScripts, want)
			}
			// 規則に一致する側はきちんと切り替わっている。
			if len(got.Changes) != 1 {
				t.Errorf("Changes = %+v, want 1 件", got.Changes)
			}
		})
	}
}

func TestHookSwitcherSwitchWithoutRemainingScripts(t *testing.T) {
	t.Parallel()

	store := newFakeSettingsStore(settingsBefore)
	got, err := newSwitcher(store).Switch(false)
	if err != nil {
		t.Fatalf("Switch() = %v", err)
	}
	if got.RemainingScripts != nil {
		t.Errorf("RemainingScripts = %+v, want 空", got.RemainingScripts)
	}
}

func TestHookSwitcherSwitchDoesNotWarnWhenBinaryExists(t *testing.T) {
	t.Parallel()

	store := newFakeSettingsStore(settingsBefore)
	got, err := newSwitcher(store).Switch(false)
	if err != nil {
		t.Fatalf("Switch() = %v", err)
	}
	if got.MissingBinaryPath != "" {
		t.Errorf("MissingBinaryPath = %q, want 空", got.MissingBinaryPath)
	}
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

func TestHookSwitcherRestoreRewritesInPlace(t *testing.T) {
	t.Parallel()

	store := newFakeSettingsStore(settingsBefore)
	switcher := newSwitcher(store)
	if _, err := switcher.Switch(false); err != nil {
		t.Fatalf("Switch() = %v", err)
	}

	store.calls = nil
	got, err := switcher.Restore(false)
	if err != nil {
		t.Fatalf("Restore() = %v", err)
	}
	if len(got.Changes) != 1 || got.Changes[0].Event != "Stop" {
		t.Errorf("Changes = %+v, want Stop の 1 件", got.Changes)
	}
	if got.SettingsMissing || got.RestoredFromBackup || got.BackupPath != "" {
		t.Errorf("結果 = %+v, want バックアップを使っていない", got)
	}
	if store.content != settingsBefore {
		t.Errorf("復元後の内容 = %q, want %q", store.content, settingsBefore)
	}
	// 現在の内容を逆向きに書き換えるだけなので、バックアップは読まない。
	if want := []string{"Read", "Write"}; !equalStrings(store.calls, want) {
		t.Errorf("呼び出し = %v, want %v", store.calls, want)
	}
}

func TestHookSwitcherRestoreKeepsEditsMadeAfterSwitch(t *testing.T) {
	t.Parallel()

	// 切り替え後に Claude Code 自身が settings.json へ書いた変更
	// (permissions.allow の追加が典型)を消してはならない。
	// バックアップの全文を書き戻す実装ではこれが黙って失われる。
	const edited = `{"permissions":{"allow":["Bash(ls:*)"]},` +
		`"hooks":{"Stop":[{"hooks":[{"type":"command","command":"$C/bin/mdev hook notify"}]}]}}`
	const want = `{"permissions":{"allow":["Bash(ls:*)"]},` +
		`"hooks":{"Stop":[{"hooks":[{"type":"command","command":"$C/scripts/pending-notify.sh"}]}]}}`

	store := newFakeSettingsStore(settingsBefore)
	switcher := newSwitcher(store)
	if _, err := switcher.Switch(false); err != nil {
		t.Fatalf("Switch() = %v", err)
	}
	store.content = edited

	if _, err := switcher.Restore(false); err != nil {
		t.Fatalf("Restore() = %v", err)
	}
	if store.content != want {
		t.Errorf("復元後 = %q\nwant %q", store.content, want)
	}
}

func TestHookSwitcherRestoreWithoutChangesIsNoOp(t *testing.T) {
	t.Parallel()

	// 既にスクリプトを指している状態での restore はエラーにせず、
	// 何も書き込まない(冪等)。
	store := newFakeSettingsStore(settingsBefore)
	got, err := newSwitcher(store).Restore(false)
	if err != nil {
		t.Fatalf("Restore() = %v", err)
	}
	if len(got.Changes) != 0 {
		t.Errorf("Changes = %+v, want 空", got.Changes)
	}
	if want := []string{"Read"}; !equalStrings(store.calls, want) {
		t.Errorf("呼び出し = %v, want %v", store.calls, want)
	}
	if store.content != settingsBefore {
		t.Errorf("内容が変わった = %q", store.content)
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
	if !got.DryRun || len(got.Changes) != 1 {
		t.Errorf("結果 = %+v, want DryRun=true と Changes 1 件", got)
	}
	if store.content != settingsAfter {
		t.Errorf("内容が変わった = %q, want %q", store.content, settingsAfter)
	}
}

// notExistErr は settings.json が存在しないときに SettingsStore.Read が返す
// エラーを模す。実装は os.ReadFile のエラーを %w で包むため fs.ErrNotExist が
// 取り出せる(port の契約)。
func notExistErr() error {
	return fmt.Errorf("設定ファイル /tmp/fake/.claude/settings.json の読み取りに失敗しました: %w", fs.ErrNotExist)
}

func TestHookSwitcherRestoreFallsBackToBackupWhenSettingsMissing(t *testing.T) {
	t.Parallel()

	// settings.json ごと失った状態が復元の主目的シナリオである。
	// このときだけバックアップの全文で書き戻す。
	store := newFakeSettingsStore(settingsBefore)
	if _, err := newSwitcher(store).Switch(false); err != nil {
		t.Fatalf("Switch() = %v", err)
	}
	store.readErr = notExistErr()
	store.calls = nil

	got, err := newSwitcher(store).Restore(false)
	if err != nil {
		t.Fatalf("Restore() = %v", err)
	}
	if !got.SettingsMissing || !got.RestoredFromBackup {
		t.Errorf("結果 = %+v, want SettingsMissing と RestoredFromBackup が true", got)
	}
	if got.BackupPath != store.backupPaths[0] {
		t.Errorf("BackupPath = %q, want %q", got.BackupPath, store.backupPaths[0])
	}
	if len(got.Changes) != 0 {
		t.Errorf("Changes = %+v, want 空(全文復元なので差分は出さない)", got.Changes)
	}
	if store.content != settingsBefore {
		t.Errorf("復元後の内容 = %q, want %q", store.content, settingsBefore)
	}
	if want := []string{"Read", "LatestBackup", "Write"}; !equalStrings(store.calls, want) {
		t.Errorf("呼び出し = %v, want %v", store.calls, want)
	}
}

func TestHookSwitcherRestoreWithoutSettingsAndBackupReportsState(t *testing.T) {
	t.Parallel()

	// settings.json もバックアップも無い状態はエラーではなく報告すべき状態である。
	store := newFakeSettingsStore(settingsBefore)
	store.readErr = notExistErr()

	got, err := newSwitcher(store).Restore(false)
	if err != nil {
		t.Fatalf("Restore() = %v", err)
	}
	if !got.SettingsMissing {
		t.Error("SettingsMissing = false, want true")
	}
	if got.RestoredFromBackup || got.BackupPath != "" {
		t.Errorf("結果 = %+v, want 復元していない", got)
	}
	for _, call := range store.calls {
		if call == "Write" {
			t.Errorf("呼び出し = %v, want Write を含まない", store.calls)
		}
	}
}

func TestHookSwitcherRestoreDryRunDoesNotWriteBackupFallback(t *testing.T) {
	t.Parallel()

	store := newFakeSettingsStore(settingsBefore)
	if _, err := newSwitcher(store).Switch(false); err != nil {
		t.Fatalf("Switch() = %v", err)
	}
	store.readErr = notExistErr()
	store.calls = nil

	got, err := newSwitcher(store).Restore(true)
	if err != nil {
		t.Fatalf("Restore() = %v", err)
	}
	if !got.DryRun || !got.RestoredFromBackup {
		t.Errorf("結果 = %+v, want DryRun と RestoredFromBackup が true", got)
	}
	for _, call := range store.calls {
		if call == "Write" {
			t.Errorf("呼び出し = %v, want Write を含まない", store.calls)
		}
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
			name:    "settings.json が読めない(不存在ではない)",
			prepare: func(s *fakeSettingsStore) { s.readErr = errors.New("読めない") },
			want:    "読めない",
		},
		{
			name:    "settings.json が壊れている",
			prepare: func(s *fakeSettingsStore) { s.content = `{"hooks":` },
			want:    "JSON",
		},
		{
			name:    "書き込みに失敗",
			prepare: func(s *fakeSettingsStore) { s.writeErr = errors.New("書けない") },
			want:    "書けない",
		},
		{
			name: "バックアップの探索に失敗",
			prepare: func(s *fakeSettingsStore) {
				s.readErr = notExistErr()
				s.latestErr = errors.New("一覧できない")
			},
			want: "一覧できない",
		},
		{
			name: "バックアップの書き戻しに失敗",
			prepare: func(s *fakeSettingsStore) {
				s.readErr = notExistErr()
				s.writeErr = errors.New("書けない")
			},
			want: "書けない",
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

	// キー順とインデントを含めて元のバイト列に戻ること
	// (バックアップを介さず、逆向きの置換だけで戻る)。
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
