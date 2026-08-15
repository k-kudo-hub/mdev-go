package store_test

import (
	"os"
	"path/filepath"
	"testing"
	"time"

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

func TestSettingsStoreBackupNameFollowsTargetFile(t *testing.T) {
	t.Parallel()

	// MDEV_SETTINGS_FILE で同じディレクトリ内のコピーを対象にできるため、
	// バックアップ名は対象ファイル名から導く。固定名にすると、実ファイルと
	// 予行演習の退避が互いを上書きしてしまう。
	dir := t.TempDir()
	real := store.NewSettingsStore(filepath.Join(dir, "settings.json"), testSettingsClock)
	copied := store.NewSettingsStore(filepath.Join(dir, "settings.copy.json"), testSettingsClock)

	realBackup, err := real.Backup([]byte(`{"real":true}`))
	if err != nil {
		t.Fatalf("Backup() = %v", err)
	}
	copyBackup, err := copied.Backup([]byte(`{"copy":true}`))
	if err != nil {
		t.Fatalf("Backup() = %v", err)
	}

	if want := filepath.Join(dir, "settings.json.mdev-backup-20260809T133344Z"); realBackup != want {
		t.Errorf("実ファイルのバックアップ = %q, want %q", realBackup, want)
	}
	if want := filepath.Join(dir, "settings.copy.json.mdev-backup-20260809T133344Z"); copyBackup != want {
		t.Errorf("コピーのバックアップ = %q, want %q", copyBackup, want)
	}

	// 中身も取り違えない。
	for _, tt := range []struct{ path, want string }{
		{path: realBackup, want: `{"real":true}`},
		{path: copyBackup, want: `{"copy":true}`},
	} {
		data, err := os.ReadFile(tt.path)
		if err != nil {
			t.Fatalf("ReadFile(%s) = %v", tt.path, err)
		}
		if string(data) != tt.want {
			t.Errorf("%s の中身 = %q, want %q", tt.path, data, tt.want)
		}
	}
}
