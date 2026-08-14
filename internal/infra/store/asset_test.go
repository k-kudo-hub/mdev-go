package store_test

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/k-kudo-hub/mdev-go/internal/infra/store"
)

// TestReadAssetPrefersRealFile は実ファイルを優先することを確かめる。
//
// 利用者が手を入れたレイアウトや既定設定が、更新のたびに埋め込みで
// 上書きされて見えるのでは、手を入れる意味が無い。
func TestReadAssetPrefersRealFile(t *testing.T) {
	t.Parallel()

	home := t.TempDir()
	path := filepath.Join(home, "layouts", "dev.kdl")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("ディレクトリを作れない: %v", err)
	}
	if err := os.WriteFile(path, []byte("layout { 利用者の版 }\n"), 0o644); err != nil {
		t.Fatalf("ファイルを作れない: %v", err)
	}

	got, err := store.ReadAsset(home, "layouts/dev.kdl")
	if err != nil {
		t.Fatalf("ReadAsset = %v", err)
	}
	if want := "layout { 利用者の版 }\n"; string(got) != want {
		t.Errorf("中身 = %q, want %q", got, want)
	}
}

// TestReadAssetFallsBackToEmbedded は実ファイルが無いときに埋め込みを返す
// ことを確かめる。初回のインストール前がこの状態である。
func TestReadAssetFallsBackToEmbedded(t *testing.T) {
	t.Parallel()

	home := t.TempDir()
	for _, name := range store.AssetNames() {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			got, err := store.ReadAsset(home, name)
			if err != nil {
				t.Fatalf("ReadAsset = %v", err)
			}
			if len(got) == 0 {
				t.Error("埋め込みが空")
			}
		})
	}
}

// TestReadAssetUnknownName は覚えの無い名前を弾くことを確かめる。
//
// CONDUCTOR_HOME 配下の任意のファイルを読み出す口にしないための線引きでも
// ある。
func TestReadAssetUnknownName(t *testing.T) {
	t.Parallel()

	home := t.TempDir()
	if err := os.WriteFile(filepath.Join(home, "secret"), []byte("x"), 0o600); err != nil {
		t.Fatalf("ファイルを作れない: %v", err)
	}

	for _, name := range []string{"secret", "config.json", "", "../assets.go"} {
		if _, err := store.ReadAsset(home, name); !errors.Is(err, store.ErrUnknownAsset) {
			t.Errorf("ReadAsset(%q) = %v, want %v", name, err, store.ErrUnknownAsset)
		}
	}
}

// TestReadAssetReportsUnreadableFile は実ファイルを読めないときにエラーを
// 返すことを確かめる。
//
// 埋め込みで埋めてしまうと、利用者が置いたはずの内容と食い違ったまま動く。
func TestReadAssetReportsUnreadableFile(t *testing.T) {
	t.Parallel()

	home := t.TempDir()
	// ディレクトリを同じ名前で置くと、読み取りは「無い」ではなく失敗になる。
	if err := os.Mkdir(filepath.Join(home, "hooks.json"), 0o755); err != nil {
		t.Fatalf("ディレクトリを作れない: %v", err)
	}

	if _, err := store.ReadAsset(home, "hooks.json"); err == nil {
		t.Error("失敗を返すはず")
	}
}

// TestAssetNames は資産の一覧が埋め込みと一致することを確かめる。
func TestAssetNames(t *testing.T) {
	t.Parallel()

	want := []string{
		"config.default.json",
		"hooks.json",
		"layouts/dev.kdl",
		"layouts/multi.kdl",
	}
	if got := store.AssetNames(); !reflect.DeepEqual(got, want) {
		t.Errorf("AssetNames = %q, want %q", got, want)
	}
}
