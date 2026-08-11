package store_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/k-kudo-hub/mdev-go/internal/domain"
	"github.com/k-kudo-hub/mdev-go/internal/infra/store"
)

// TestFlavorStorePath は印の置き場所が CONDUCTOR_HOME 直下の FLAVOR で
// あることを固定する。conductor の install.sh がこのパスを読むため、
// 場所が変わると機構ごと動かなくなる。
func TestFlavorStorePath(t *testing.T) {
	t.Parallel()

	home := "/tmp/conductor"
	want := filepath.Join(home, "FLAVOR")
	if got := store.NewFlavorStore(home).Path(); got != want {
		t.Errorf("Path() = %q, want %q", got, want)
	}
}

// TestFlavorStoreWrite は印が install.sh の読み方で "go" と読める形で
// 書かれることを確かめる。
//
// install.sh は `head -n 1 | sed で前後の空白を落とす` で読むため、
// 末尾の改行があってもよいが、余計な行や空白があってはならない。
func TestFlavorStoreWrite(t *testing.T) {
	t.Parallel()

	home := filepath.Join(t.TempDir(), "conductor")
	s := store.NewFlavorStore(home)

	if err := s.WriteFlavor(domain.FlavorGo); err != nil {
		t.Fatalf("WriteFlavor() = %v", err)
	}
	got, err := os.ReadFile(filepath.Join(home, "FLAVOR")) //nolint:gosec // テストの一時ディレクトリ
	if err != nil {
		t.Fatalf("印が読めません: %v", err)
	}
	if string(got) != "go\n" {
		t.Errorf("印の中身 = %q, want %q", got, "go\n")
	}
}

// TestFlavorStoreWriteIsIdempotent は繰り返し書いても中身が増えない
// ことを確かめる(追記ではなく置き換え)。
func TestFlavorStoreWriteIsIdempotent(t *testing.T) {
	t.Parallel()

	home := filepath.Join(t.TempDir(), "conductor")
	s := store.NewFlavorStore(home)

	for range 3 {
		if err := s.WriteFlavor(domain.FlavorGo); err != nil {
			t.Fatalf("WriteFlavor() = %v", err)
		}
	}
	got, err := os.ReadFile(s.Path()) //nolint:gosec // テストの一時ディレクトリ
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "go\n" {
		t.Errorf("印の中身 = %q, want %q", got, "go\n")
	}
}

// TestFlavorStoreWriteLeavesNoTempFile は原子的な置き換えの痕跡が
// 残らないことを確かめる。install.sh は同時にこのファイルを読みうる。
func TestFlavorStoreWriteLeavesNoTempFile(t *testing.T) {
	t.Parallel()

	home := filepath.Join(t.TempDir(), "conductor")
	if err := store.NewFlavorStore(home).WriteFlavor(domain.FlavorGo); err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(home)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Name() != "FLAVOR" {
		var names []string
		for _, entry := range entries {
			names = append(names, entry.Name())
		}
		t.Errorf("CONDUCTOR_HOME の中身 = %v, want [FLAVOR]", names)
	}
}

// TestFlavorStoreRemove は印を消せることを確かめる。
func TestFlavorStoreRemove(t *testing.T) {
	t.Parallel()

	home := filepath.Join(t.TempDir(), "conductor")
	s := store.NewFlavorStore(home)
	if err := s.WriteFlavor(domain.FlavorGo); err != nil {
		t.Fatal(err)
	}

	if err := s.RemoveFlavor(); err != nil {
		t.Fatalf("RemoveFlavor() = %v", err)
	}
	if _, err := os.Stat(s.Path()); !os.IsNotExist(err) {
		t.Errorf("印が残っています: %v", err)
	}
}

// TestFlavorStoreRemoveMissingIsSuccess は印が無くても成功することを
// 確かめる。目的は「印が無い状態にする」ことなので、既にそうなっているのは
// 失敗ではない(Shell 版のまま使っている環境で restore を叩いた場合)。
func TestFlavorStoreRemoveMissingIsSuccess(t *testing.T) {
	t.Parallel()

	home := filepath.Join(t.TempDir(), "conductor")
	if err := store.NewFlavorStore(home).RemoveFlavor(); err != nil {
		t.Errorf("印が無いのに error になりました: %v", err)
	}
}

// TestFlavorStoreWriteFailsWhenHomeIsFile は置き場所を作れないときに
// error になることを確かめる(握り潰して「書けたつもり」にしない)。
func TestFlavorStoreWriteFailsWhenHomeIsFile(t *testing.T) {
	t.Parallel()

	blocker := filepath.Join(t.TempDir(), "conductor")
	if err := os.WriteFile(blocker, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := store.NewFlavorStore(blocker).WriteFlavor(domain.FlavorGo); err == nil {
		t.Error("CONDUCTOR_HOME がファイルなのに error になりませんでした")
	}
}
