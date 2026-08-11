package fsutil_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/k-kudo-hub/mdev-go/internal/infra/fsutil"
)

// TestWriteFileCreatesParents は置き場所ごと作って書けることを確かめる。
func TestWriteFileCreatesParents(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "a", "b", "out.json")
	if err := fsutil.WriteFile(path, []byte("body\n")); err != nil {
		t.Fatalf("WriteFile が失敗しました: %v", err)
	}
	got, err := os.ReadFile(path) //nolint:gosec // テストの一時ディレクトリ
	if err != nil {
		t.Fatalf("書いたファイルが読めません: %v", err)
	}
	if string(got) != "body\n" {
		t.Errorf("中身 = %q, want %q", got, "body\n")
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != fsutil.FilePerm {
		t.Errorf("権限 = %v, want %v", info.Mode().Perm(), fsutil.FilePerm)
	}
}

// TestWriteFileReplaces は既存の内容を置き換えることを確かめる。
func TestWriteFileReplaces(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "out.json")
	if err := fsutil.WriteFile(path, []byte("old")); err != nil {
		t.Fatal(err)
	}
	if err := fsutil.WriteFile(path, []byte("new")); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(path) //nolint:gosec // テストの一時ディレクトリ
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "new" {
		t.Errorf("中身 = %q, want new", got)
	}
}

// TestWriteFileLeavesNoTempFile は一時ファイルを残さないことを確かめる。
//
// 読み手(ペインのポーリング)が中途半端なファイルを拾わないための書き方
// なので、痕跡が残ると目的を果たしていない。
func TestWriteFileLeavesNoTempFile(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	if err := fsutil.WriteFile(filepath.Join(dir, "out.json"), []byte("body")); err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if strings.Contains(entry.Name(), ".tmp") {
			t.Errorf("一時ファイルが残っています: %s", entry.Name())
		}
	}
	if len(entries) != 1 {
		t.Errorf("ディレクトリの中身 = %d 件, want 1 件", len(entries))
	}
}

// TestWriteFileMode は権限を指定して書けることを確かめる。
// settings.json の書き換えが既存の権限を引き継ぐために使う。
func TestWriteFileMode(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "out.json")
	if err := fsutil.WriteFileMode(path, []byte("body"), 0o600); err != nil {
		t.Fatalf("WriteFileMode が失敗しました: %v", err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Errorf("権限 = %v, want 0600", info.Mode().Perm())
	}
}

// TestWriteFileFailsWhenParentIsFile は置き場所を作れないときに error に
// なることを確かめる(握り潰して「書けたつもり」にしない)。
func TestWriteFileFailsWhenParentIsFile(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	blocker := filepath.Join(dir, "blocker")
	if err := os.WriteFile(blocker, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := fsutil.WriteFile(filepath.Join(blocker, "out.json"), []byte("body")); err == nil {
		t.Error("親がファイルなのに error になりませんでした")
	}
}
