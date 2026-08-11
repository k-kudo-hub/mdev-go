package store_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/k-kudo-hub/mdev-go/internal/infra/store"
)

// TestDailyStoreReadSession は daily ファイルをファイル名の昇順で全部読むことを
// 確かめる。upload はこの並びを「後のファイルが勝つ」判断に使うため、
// 並びが崩れると古い記録で上書きされてしまう。
func TestDailyStoreReadSession(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "sess")
	if err := os.MkdirAll(filepath.Join(dir, "subdir"), 0o755); err != nil {
		t.Fatal(err)
	}
	// 作る順をわざと逆にして、並びが os.ReadDir の昇順になることを見る。
	for name, body := range map[string]string{
		"2026-08-02.jsonl": "second\n",
		"2026-08-01.jsonl": "first\n",
		"notes.txt":        "無視される\n",
	} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	got := store.NewDailyStore(root, nil).ReadSession("sess")
	want := []string{"first\n", "second\n"}
	if len(got) != len(want) {
		t.Fatalf("読み込んだファイル数 = %d, want %d (%q)", len(got), len(want), got)
	}
	for i := range want {
		if string(got[i]) != want[i] {
			t.Errorf("ファイル %d = %q, want %q", i, got[i], want[i])
		}
	}
}

// TestDailyStoreReadSessionMissingDir はディレクトリが無いセッションで
// 空が返ることを確かめる(記録なし = プレースホルダへ落ちる経路)。
func TestDailyStoreReadSessionMissingDir(t *testing.T) {
	if got := store.NewDailyStore(t.TempDir(), nil).ReadSession("none"); len(got) != 0 {
		t.Errorf("ReadSession = %q, want 空", got)
	}
}
