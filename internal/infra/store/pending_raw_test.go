package store_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/k-kudo-hub/mdev-go/internal/infra/store"
)

// waiting-toggle が使う「生のまま読んで生のまま書き戻す」経路を固定する。
//
// 構造体を経由しないのは、jq が未知のキーをそのまま持ち越すためである
// (現行 waiting-toggle.sh は pending の中身を解釈せず event / prev_event /
// time だけを書き換える)。

// writePending はテスト用に pending を 1 件置く。
func writePending(t *testing.T, root, session, name, content string) string {
	t.Helper()
	dir := filepath.Join(root, session)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("ディレクトリの作成に失敗: %v", err)
	}
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("pending の作成に失敗: %v", err)
	}
	return path
}

func TestPendingStoreFindRawByTab(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	// ファイル名の昇順で最初に一致したものを採る(現行の glob の展開順)。
	writePending(t, root, "s1", "a-broken.json", "{ not json")
	writePending(t, root, "s1", "b.json", `{"tab":"alpha","event":"Notification"}`)
	writePending(t, root, "s1", "c.json", `{"tab":"alpha","event":"Stop"}`)
	writePending(t, root, "s1", "d.json", `{"tab":"beta","event":"Stop"}`)

	s := store.NewPendingStore(root)

	name, data, found, err := s.FindRawByTab("s1", "alpha")
	if err != nil {
		t.Fatalf("FindRawByTab() = %v", err)
	}
	if !found {
		t.Fatal("見つからなかった")
	}
	if name != "b.json" {
		t.Errorf("採ったファイル = %q, want b.json(昇順で最初の一致)", name)
	}
	if want := `{"tab":"alpha","event":"Notification"}`; string(data) != want {
		t.Errorf("中身 = %q, want %q", data, want)
	}
}

func TestPendingStoreFindRawByTabNotFound(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	writePending(t, root, "s1", "a.json", `{"tab":"alpha"}`)
	s := store.NewPendingStore(root)

	tests := []struct{ name, session, tab string }{
		{"タブが一致しない", "s1", "nope"},
		{"セッションのディレクトリが無い", "missing", "alpha"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			_, _, found, err := s.FindRawByTab(tc.session, tc.tab)
			if err != nil {
				t.Fatalf("FindRawByTab() = %v", err)
			}
			if found {
				t.Error("見つからないはずが見つかった")
			}
		})
	}
}

func TestPendingStoreWriteRaw(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	path := writePending(t, root, "s1", "a.json", `{"tab":"alpha","event":"Notification"}`)
	s := store.NewPendingStore(root)

	want := `{"tab":"alpha","event":"Waiting"}`
	if err := s.WriteRaw("s1", "a.json", []byte(want)); err != nil {
		t.Fatalf("WriteRaw() = %v", err)
	}

	// 現行版は jq の出力をリダイレクトするため末尾に改行が付く。
	got, err := os.ReadFile(path) //nolint:gosec // テストの一時ディレクトリ
	if err != nil {
		t.Fatalf("読み直しに失敗: %v", err)
	}
	if string(got) != want+"\n" {
		t.Errorf("書き込み結果 = %q, want %q", got, want+"\n")
	}
}

func TestPendingStoreWriteRawIsAtomic(t *testing.T) {
	t.Parallel()

	// 一時ファイルは同じディレクトリに作って rename する。書きかけの内容を
	// ダッシュボードのポーリングが読むことがないようにするためである。
	root := t.TempDir()
	writePending(t, root, "s1", "a.json", `{"tab":"alpha"}`)
	s := store.NewPendingStore(root)

	if err := s.WriteRaw("s1", "a.json", []byte(`{"tab":"alpha","event":"Waiting"}`)); err != nil {
		t.Fatalf("WriteRaw() = %v", err)
	}
	entries, err := os.ReadDir(filepath.Join(root, "s1"))
	if err != nil {
		t.Fatalf("読み取りに失敗: %v", err)
	}
	if len(entries) != 1 {
		t.Errorf("一時ファイルが残っている: %d 件", len(entries))
	}
}
