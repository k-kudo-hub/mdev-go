package store_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/k-kudo-hub/mdev-go/internal/infra/store"
)

// writeHomeFile は CONDUCTOR_HOME 直下にファイルを置く。
func writeHomeFile(t *testing.T, home, name, body string) {
	t.Helper()
	if err := os.MkdirAll(home, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, name), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

// TestUpdateStateStoreReads は状態ファイルの読み取りを確かめる。
func TestUpdateStateStoreReads(t *testing.T) {
	home := t.TempDir()
	// install.sh はどちらも末尾に改行を付けて書く。
	writeHomeFile(t, home, "REPO_URL", "https://github.com/o/r.git\n")
	writeHomeFile(t, home, "VERSION", "v0.1.0\n")

	s := store.NewUpdateStateStore(home)
	if got := s.RepoURL(); got != "https://github.com/o/r.git" {
		t.Errorf("RepoURL = %q", got)
	}
	if got := s.InstalledVersion(); got != "v0.1.0" {
		t.Errorf("InstalledVersion = %q", got)
	}
}

// TestUpdateStateStoreMissingFiles はファイルが無い環境で空が返ることを
// 確かめる(更新確認は黙って諦める)。
func TestUpdateStateStoreMissingFiles(t *testing.T) {
	s := store.NewUpdateStateStore(t.TempDir())
	if got := s.RepoURL(); got != "" {
		t.Errorf("RepoURL = %q, want 空", got)
	}
	if got := s.InstalledVersion(); got != "" {
		t.Errorf("InstalledVersion = %q, want 空", got)
	}
	if date, tag := s.ReadUpdateCache(); date != "" || tag != "" {
		t.Errorf("ReadUpdateCache = (%q, %q), want 空", date, tag)
	}
}

// TestUpdateStateStoreCache はキャッシュの読み書きを確かめる。
func TestUpdateStateStoreCache(t *testing.T) {
	tests := []struct {
		name      string
		body      string
		date, tag string
	}{
		{name: "日付とタグ", body: "2026-08-08 v0.2.0\n", date: "2026-08-08", tag: "v0.2.0"},
		{name: "2 行目は見ない", body: "2026-08-08 v0.2.0\nゴミ\n", date: "2026-08-08", tag: "v0.2.0"},
		{name: "列が足りなければ空", body: "2026-08-08\n"},
		{name: "空ファイルは空", body: ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			home := t.TempDir()
			writeHomeFile(t, home, ".update-check", tt.body)
			date, tag := store.NewUpdateStateStore(home).ReadUpdateCache()
			if date != tt.date || tag != tt.tag {
				t.Errorf("ReadUpdateCache = (%q, %q), want (%q, %q)", date, tag, tt.date, tt.tag)
			}
		})
	}
}

// TestUpdateStateStoreWriteCache は書いたものを読み戻せることを確かめる。
func TestUpdateStateStoreWriteCache(t *testing.T) {
	home := filepath.Join(t.TempDir(), "conductor")
	s := store.NewUpdateStateStore(home)

	if err := s.WriteUpdateCache("2026-08-08", "v0.2.0"); err != nil {
		t.Fatalf("WriteUpdateCache が失敗しました: %v", err)
	}
	// 現行版は `echo "$TODAY $LATEST" >` で書くので末尾に改行が付く。
	got, err := os.ReadFile(filepath.Join(home, ".update-check"))
	if err != nil {
		t.Fatalf("キャッシュがありません: %v", err)
	}
	if string(got) != "2026-08-08 v0.2.0\n" {
		t.Errorf("キャッシュ = %q", got)
	}
	if date, tag := s.ReadUpdateCache(); date != "2026-08-08" || tag != "v0.2.0" {
		t.Errorf("読み戻し = (%q, %q)", date, tag)
	}
}
