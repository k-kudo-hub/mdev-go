package store_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/k-kudo-hub/mdev-go/internal/app"
	"github.com/k-kudo-hub/mdev-go/internal/infra/store"
)

// settingsClock は Backup のファイル名を固定するための時計。
// UTC ではないゾーンを与え、ファイル名が UTC に正規化されることを確かめる。
type settingsClock struct{ now time.Time }

func (c settingsClock) Now() time.Time { return c.now }

var testSettingsClock = settingsClock{
	now: time.Date(2026, 8, 9, 22, 33, 44, 0, time.FixedZone("JST", 9*60*60)),
}

// newSettingsFile は一時ディレクトリに settings.json を作り、その store を返す。
func newSettingsFile(t *testing.T, content string) (*store.SettingsStore, string) {
	t.Helper()

	dir := filepath.Join(t.TempDir(), ".claude")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("MkdirAll() = %v", err)
	}
	path := filepath.Join(dir, "settings.json")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil { //nolint:gosec // 実環境と同じ見え方にする
		t.Fatalf("WriteFile() = %v", err)
	}
	return store.NewSettingsStore(path, testSettingsClock), path
}

func TestClaudeSettingsPath(t *testing.T) {
	t.Parallel()

	want := filepath.Join("/Users/x", ".claude", "settings.json")
	if got := store.ClaudeSettingsPath("/Users/x"); got != want {
		t.Errorf("ClaudeSettingsPath() = %q, want %q", got, want)
	}
}

func TestSettingsStoreReadAndPath(t *testing.T) {
	t.Parallel()

	s, path := newSettingsFile(t, `{"hooks":{}}`)
	if s.Path() != path {
		t.Errorf("Path() = %q, want %q", s.Path(), path)
	}
	got, err := s.Read()
	if err != nil {
		t.Fatalf("Read() = %v", err)
	}
	if string(got) != `{"hooks":{}}` {
		t.Errorf("Read() = %q", got)
	}
}

func TestSettingsStoreReadFailsWhenAbsent(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), ".claude", "settings.json")
	_, err := store.NewSettingsStore(path, testSettingsClock).Read()
	if err == nil {
		t.Fatal("Read() = nil, want エラー")
	}
	// どのファイルが無いのか分からないと利用者が直せない。
	if !strings.Contains(err.Error(), path) {
		t.Errorf("エラー = %v, want パスを含む", err)
	}
}

func TestSettingsStoreWriteReplacesAtomically(t *testing.T) {
	t.Parallel()

	s, path := newSettingsFile(t, `{"a":1}`)
	if err := s.Write([]byte(`{"b":2}`)); err != nil {
		t.Fatalf("Write() = %v", err)
	}

	got, err := os.ReadFile(path) //nolint:gosec // テストの一時ファイル
	if err != nil {
		t.Fatalf("ReadFile() = %v", err)
	}
	if string(got) != `{"b":2}` {
		t.Errorf("内容 = %q", got)
	}
	// 一時ファイルが残っていないこと(書きかけが settings.json の隣に残ると
	// 利用者が settings.json.tmp-xxx を消してよいのか判断できない)。
	entries, err := os.ReadDir(filepath.Dir(path))
	if err != nil {
		t.Fatalf("ReadDir() = %v", err)
	}
	if len(entries) != 1 || entries[0].Name() != "settings.json" {
		names := make([]string, 0, len(entries))
		for _, e := range entries {
			names = append(names, e.Name())
		}
		t.Errorf("ディレクトリの中身 = %v, want settings.json のみ", names)
	}
}

func TestSettingsStoreWritePreservesFileMode(t *testing.T) {
	t.Parallel()

	// 利用者が権限を絞っている settings.json を勝手に緩めない。
	s, path := newSettingsFile(t, `{"a":1}`)
	if err := os.Chmod(path, 0o600); err != nil {
		t.Fatalf("Chmod() = %v", err)
	}
	if err := s.Write([]byte(`{"b":2}`)); err != nil {
		t.Fatalf("Write() = %v", err)
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("Stat() = %v", err)
	}
	if got := info.Mode().Perm(); got != 0o600 {
		t.Errorf("パーミッション = %o, want 600", got)
	}
}

func TestSettingsStoreBackupNamingAndContent(t *testing.T) {
	t.Parallel()

	s, path := newSettingsFile(t, `{"a":1}`)
	backupPath, err := s.Backup([]byte(`{"a":1}`))
	if err != nil {
		t.Fatalf("Backup() = %v", err)
	}

	// settings.json と同じディレクトリに、UTC のタイムスタンプ付きで作る。
	// JST 2026-08-09 22:33:44 は UTC では 13:33:44 である。
	want := filepath.Join(filepath.Dir(path), "settings.json.mdev-backup-20260809T133344Z")
	if backupPath != want {
		t.Errorf("Backup() = %q, want %q", backupPath, want)
	}
	got, err := os.ReadFile(backupPath) //nolint:gosec // テストの一時ファイル
	if err != nil {
		t.Fatalf("ReadFile() = %v", err)
	}
	if string(got) != `{"a":1}` {
		t.Errorf("バックアップの内容 = %q", got)
	}
}

func TestSettingsStoreLatestBackupWithoutBackup(t *testing.T) {
	t.Parallel()

	s, _ := newSettingsFile(t, `{"a":1}`)
	path, data, found, err := s.LatestBackup()
	if err != nil {
		t.Fatalf("LatestBackup() = %v", err)
	}
	if found {
		t.Errorf("found = true (%q, %q), want false", path, data)
	}
}

func TestSettingsStoreLatestBackupPicksNewest(t *testing.T) {
	t.Parallel()

	s, path := newSettingsFile(t, `{"a":1}`)
	dir := filepath.Dir(path)

	// タイムスタンプの文字列は辞書順と時系列が一致する形式である。
	// 無関係なファイル(他ツールのバックアップ・ディレクトリ)は無視する。
	files := map[string]string{
		"settings.json.mdev-backup-20260809T000000Z": `{"old":true}`,
		"settings.json.mdev-backup-20260810T000000Z": `{"new":true}`,
		"settings.json.mdev-backup-20260801T000000Z": `{"older":true}`,
		"settings.json.bak":                          `{"other":true}`,
		"settings.local.json":                        `{"local":true}`,
	}
	for name, content := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o600); err != nil {
			t.Fatalf("WriteFile(%s) = %v", name, err)
		}
	}
	if err := os.Mkdir(filepath.Join(dir, "settings.json.mdev-backup-20260811T000000Z"), 0o755); err != nil {
		t.Fatalf("Mkdir() = %v", err)
	}

	gotPath, data, found, err := s.LatestBackup()
	if err != nil {
		t.Fatalf("LatestBackup() = %v", err)
	}
	if !found {
		t.Fatal("found = false, want true")
	}
	want := filepath.Join(dir, "settings.json.mdev-backup-20260810T000000Z")
	if gotPath != want {
		t.Errorf("パス = %q, want %q", gotPath, want)
	}
	if string(data) != `{"new":true}` {
		t.Errorf("内容 = %q", data)
	}
}

func TestSettingsStoreLatestBackupWithoutDirectory(t *testing.T) {
	t.Parallel()

	// settings.json のディレクトリごと無い場合もエラーにしない。
	path := filepath.Join(t.TempDir(), ".claude", "settings.json")
	_, _, found, err := store.NewSettingsStore(path, testSettingsClock).LatestBackup()
	if err != nil {
		t.Fatalf("LatestBackup() = %v", err)
	}
	if found {
		t.Error("found = true, want false")
	}
}

func TestSettingsStoreReportsWriteFailure(t *testing.T) {
	t.Parallel()

	s, path := newSettingsFile(t, `{"a":1}`)
	dir := filepath.Dir(path)
	if err := os.Chmod(dir, 0o500); err != nil {
		t.Fatalf("Chmod() = %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0o755) })

	if err := s.Write([]byte(`{"b":2}`)); err == nil {
		t.Error("Write() = nil, want エラー")
	}
	if _, err := s.Backup([]byte(`{"a":1}`)); err == nil {
		t.Error("Backup() = nil, want エラー")
	}
}

// TestHookSwitchRoundTripOnRealFiles は一時ディレクトリの実ファイルに対して
// 切り替え → 復元の往復を行う。app のユースケースと infra の store を
// つないだ状態で、settings.json が 1 バイトも変わらずに戻ることを確認する。
//
// 実環境の ~/.claude/settings.json には一切触れない。
func TestHookSwitchRoundTripOnRealFiles(t *testing.T) {
	t.Parallel()

	original, err := os.ReadFile(filepath.Join("testdata", "settings-conductor-merged.json"))
	if err != nil {
		t.Fatalf("ReadFile() = %v", err)
	}
	s, path := newSettingsFile(t, string(original))
	switcher := &app.HookSwitcher{Settings: s}

	switched, err := switcher.Switch(false)
	if err != nil {
		t.Fatalf("Switch() = %v", err)
	}
	if len(switched.Changes) != 4 {
		t.Fatalf("Changes = %+v, want 4 件", switched.Changes)
	}
	afterSwitch, err := os.ReadFile(path) //nolint:gosec // テストの一時ファイル
	if err != nil {
		t.Fatalf("ReadFile() = %v", err)
	}
	if strings.Contains(string(afterSwitch), "/scripts/pending-") {
		t.Errorf("切り替え後にスクリプト呼び出しが残っている:\n%s", afterSwitch)
	}
	if !strings.Contains(string(afterSwitch), `"Bash(git:*)"`) {
		t.Error("hooks 以外のキー(permissions)が失われている")
	}
	// バックアップは切り替え前の内容と完全一致する。
	backup, err := os.ReadFile(switched.BackupPath) //nolint:gosec // テストの一時ファイル
	if err != nil {
		t.Fatalf("ReadFile(backup) = %v", err)
	}
	if string(backup) != string(original) {
		t.Error("バックアップが切り替え前の内容と一致しない")
	}

	// 2 回目の切り替えは何もしない(冪等)。
	again, err := switcher.Switch(false)
	if err != nil {
		t.Fatalf("2 回目の Switch() = %v", err)
	}
	if len(again.Changes) != 0 || again.BackupPath != "" {
		t.Errorf("2 回目 = %+v, want 変更なし", again)
	}

	restored, err := switcher.Restore(false)
	if err != nil {
		t.Fatalf("Restore() = %v", err)
	}
	if !restored.Found || !restored.Changed {
		t.Errorf("Restore() = %+v, want Found と Changed が true", restored)
	}
	final, err := os.ReadFile(path) //nolint:gosec // テストの一時ファイル
	if err != nil {
		t.Fatalf("ReadFile() = %v", err)
	}
	if string(final) != string(original) {
		t.Errorf("往復後の settings.json が元と異なる:\n--- got ---\n%s\n--- want ---\n%s", final, original)
	}

	// 復元済みの状態でもう一度 restore してもエラーにならず、状態を報告する。
	twice, err := switcher.Restore(false)
	if err != nil {
		t.Fatalf("2 回目の Restore() = %v", err)
	}
	if !twice.Found || twice.Changed {
		t.Errorf("2 回目の Restore() = %+v, want Found=true / Changed=false", twice)
	}
}

func TestSettingsPathHonorsOverride(t *testing.T) {
	t.Parallel()

	// 実環境へ適用する前にコピーで試せるよう、パスを差し替えられる。
	tests := []struct {
		name     string
		envValue string
		want     string
	}{
		{name: "MDEV_SETTINGS_FILE を優先", envValue: "/tmp/copy.json", want: "/tmp/copy.json"},
		{name: "空ならホーム直下", envValue: "", want: filepath.Join("/Users/x", ".claude", "settings.json")},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := store.SettingsPath("/Users/x", tt.envValue); got != tt.want {
				t.Errorf("SettingsPath() = %q, want %q", got, tt.want)
			}
		})
	}
}
