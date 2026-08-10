package store_test

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/k-kudo-hub/mdev-go/internal/infra/store"
)

// mkdirs は相対パスの並びをディレクトリとして作る。
func mkdirs(t *testing.T, root string, paths ...string) {
	t.Helper()
	for _, p := range paths {
		if err := os.MkdirAll(filepath.Join(root, p), 0o755); err != nil {
			t.Fatalf("ディレクトリの作成に失敗: %v", err)
		}
	}
}

func TestPaneStoreListDirs(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	mkdirs(t, root,
		"projects/alpha/sub",
		"projects/beta",
		"projects/.hidden/inside",
		"works/gamma",
	)
	// ファイルは候補にしない。
	if err := os.WriteFile(filepath.Join(root, "projects", "note.txt"), nil, 0o600); err != nil {
		t.Fatalf("ファイルの作成に失敗: %v", err)
	}

	s := store.NewPaneStore(filepath.Join(root, "pending"), root)
	projects := filepath.Join(root, "projects")
	works := filepath.Join(root, "works")

	t.Run("深さ 1 は直下だけ", func(t *testing.T) {
		t.Parallel()
		want := []string{
			filepath.Join(projects, "alpha"),
			filepath.Join(projects, "beta"),
			filepath.Join(works, "gamma"),
		}
		if got := s.ListDirs([]string{projects, works}, 1); !reflect.DeepEqual(got, want) {
			t.Errorf("ListDirs(depth=1) = %v\nwant %v", got, want)
		}
	})

	t.Run("深さ 2 は孫まで(fd の --max-depth と同じ)", func(t *testing.T) {
		t.Parallel()
		want := []string{
			filepath.Join(projects, "alpha"),
			filepath.Join(projects, "alpha", "sub"),
			filepath.Join(projects, "beta"),
		}
		if got := s.ListDirs([]string{projects}, 2); !reflect.DeepEqual(got, want) {
			t.Errorf("ListDirs(depth=2) = %v\nwant %v", got, want)
		}
	})

	t.Run("読めない root は飛ばす", func(t *testing.T) {
		t.Parallel()
		want := []string{filepath.Join(works, "gamma")}
		got := s.ListDirs([]string{filepath.Join(root, "missing"), works}, 1)
		if !reflect.DeepEqual(got, want) {
			t.Errorf("ListDirs() = %v, want %v", got, want)
		}
	})

	t.Run("root が 1 つも無ければ空", func(t *testing.T) {
		t.Parallel()
		if got := s.ListDirs(nil, 1); len(got) != 0 {
			t.Errorf("ListDirs(nil) = %v, want 空", got)
		}
	})
}

func TestPaneStoreListDirsSkipsSymlinks(t *testing.T) {
	t.Parallel()

	// fd は既定でシンボリックリンクを辿らない(-L なし)。
	root := t.TempDir()
	mkdirs(t, root, "projects/real")
	link := filepath.Join(root, "projects", "link")
	if err := os.Symlink(filepath.Join(root, "projects", "real"), link); err != nil {
		t.Skipf("シンボリックリンクを作れない環境: %v", err)
	}

	s := store.NewPaneStore(filepath.Join(root, "pending"), root)
	want := []string{filepath.Join(root, "projects", "real")}
	if got := s.ListDirs([]string{filepath.Join(root, "projects")}, 1); !reflect.DeepEqual(got, want) {
		t.Errorf("ListDirs() = %v, want %v", got, want)
	}
}
