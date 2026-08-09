package store_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/k-kudo-hub/mdev-go/internal/app"
	"github.com/k-kudo-hub/mdev-go/internal/domain"
	"github.com/k-kudo-hub/mdev-go/internal/infra/store"
)

var _ app.PendingStore = (*store.PendingStore)(nil)

func TestPendingRoot(t *testing.T) {
	t.Parallel()

	// pending の置き場所は CONDUCTOR_HOME に依存せず $HOME 固定である
	// (現行版の `$HOME/.claude-pending/$SESSION_NAME`)。
	if got, want := store.PendingRoot("/Users/x"), "/Users/x/.claude-pending"; got != want {
		t.Errorf("PendingRoot() = %q, want %q", got, want)
	}
}

func TestPendingStoreSaveUsesSessionScopedPath(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	s := store.NewPendingStore(root)

	pending := domain.Pending{
		Tab:             "api-feature",
		Session:         "test-session",
		ClaudeSessionID: "sess-aaa",
		Message:         "Permission needed",
		Event:           domain.EventNotification,
		Time:            "10:11:12",
		Agent:           "claude",
		Dir:             "/tmp/myapp",
	}
	if err := s.Save("test-session", "sess-aaa", pending); err != nil {
		t.Fatalf("Save() = %v", err)
	}

	path := filepath.Join(root, "test-session", "sess-aaa.json")
	b, err := os.ReadFile(path) //nolint:gosec // テスト内で組み立てた一時パス
	if err != nil {
		t.Fatalf("pending が %s に無い: %v", path, err)
	}

	var got map[string]any
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatalf("Unmarshal(%s) = %v", b, err)
	}
	want := map[string]any{
		"tab":               "api-feature",
		"session":           "test-session",
		"claude_session_id": "sess-aaa",
		"message":           "Permission needed",
		"event":             "Notification",
		"time":              "10:11:12",
		"agent":             "claude",
		"dir":               "/tmp/myapp",
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("保存された JSON = %v, want %v", got, want)
	}
}

func TestPendingStoreSaveOverwrites(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	s := store.NewPendingStore(root)

	if err := s.Save("s", "sid", domain.Pending{Event: domain.EventStop}); err != nil {
		t.Fatalf("Save() = %v", err)
	}
	if err := s.Save("s", "sid", domain.Pending{Event: domain.EventNotification}); err != nil {
		t.Fatalf("Save() = %v", err)
	}

	if got := s.Event("s", "sid"); got != domain.EventNotification {
		t.Errorf("Event() = %q, want %q", got, domain.EventNotification)
	}
	// 一時ファイルが残っていないこと(temp+rename の後始末)。
	entries, err := os.ReadDir(filepath.Join(root, "s"))
	if err != nil {
		t.Fatalf("ReadDir() = %v", err)
	}
	if len(entries) != 1 || entries[0].Name() != "sid.json" {
		names := make([]string, 0, len(entries))
		for _, e := range entries {
			names = append(names, e.Name())
		}
		t.Errorf("ディレクトリの中身 = %v, want [sid.json]", names)
	}
}

func TestPendingStoreSaveWritesTempInSameDirectory(t *testing.T) {
	t.Parallel()

	// 別ファイルシステムをまたぐ rename は原子的にならないため、一時ファイルは
	// 必ず書き込み先と同じディレクトリに作る。書き込み先だけを読み取り専用に
	// すると保存が失敗することで、$TMPDIR ではなくそこを使っていると分かる。
	root := t.TempDir()
	dir := filepath.Join(root, "s")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("MkdirAll() = %v", err)
	}
	if err := os.Chmod(dir, 0o555); err != nil {
		t.Fatalf("Chmod() = %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0o755) })

	if err := store.NewPendingStore(root).Save("s", "sid", domain.Pending{Event: domain.EventStop}); err == nil {
		t.Error("Save() = nil, want エラー(一時ファイルが書き込み先ディレクトリに作られていない)")
	}
}

func TestPendingStoreEvent(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		content string // 空文字はファイルを作らないことを表す
		create  bool
		want    string
	}{
		{name: "Notification", content: `{"event":"Notification"}`, create: true, want: "Notification"},
		{name: "Stop", content: `{"event":"Stop"}`, create: true, want: "Stop"},
		{name: "Waiting", content: `{"event":"Waiting"}`, create: true, want: "Waiting"},
		{name: "pending 無しは空文字", create: false, want: ""},
		// 現行版は jq のエラーを握り潰して空文字にしていた。壊れた JSON は
		// 「event 空」として扱われ、Stop に上書きされる / PostToolUse では消えない。
		{name: "壊れた JSON は空文字", content: `{"event":`, create: true, want: ""},
		{name: "空ファイルは空文字", content: ``, create: true, want: ""},
		{name: "event キーが無ければ空文字", content: `{"tab":"t"}`, create: true, want: ""},
		{name: "JSON でない内容は空文字", content: "not json at all", create: true, want: ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			root := t.TempDir()
			if tt.create {
				dir := filepath.Join(root, "s")
				if err := os.MkdirAll(dir, 0o755); err != nil {
					t.Fatalf("MkdirAll() = %v", err)
				}
				if err := os.WriteFile(filepath.Join(dir, "sid.json"), []byte(tt.content), 0o600); err != nil {
					t.Fatalf("WriteFile() = %v", err)
				}
			}
			if got := store.NewPendingStore(root).Event("s", "sid"); got != tt.want {
				t.Errorf("Event() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestPendingStoreDelete(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	s := store.NewPendingStore(root)
	if err := s.Save("s", "sid", domain.Pending{Event: domain.EventStop}); err != nil {
		t.Fatalf("Save() = %v", err)
	}

	if err := s.Delete("s", "sid"); err != nil {
		t.Fatalf("Delete() = %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "s", "sid.json")); !os.IsNotExist(err) {
		t.Errorf("Stat() = %v, want IsNotExist", err)
	}

	// 存在しない pending の削除は成功として扱う(現行版の `rm -f` 相当)。
	if err := s.Delete("s", "sid"); err != nil {
		t.Errorf("2 回目の Delete() = %v, want nil", err)
	}
	if err := s.Delete("no-such-session", "sid"); err != nil {
		t.Errorf("存在しないセッションの Delete() = %v, want nil", err)
	}
}

func TestPendingStoreDeleteReportsUnexpectedFailure(t *testing.T) {
	t.Parallel()

	// 「存在しない」以外の失敗は握り潰さずに返す。
	root := t.TempDir()
	s := store.NewPendingStore(root)
	if err := s.Save("s", "sid", domain.Pending{Event: domain.EventStop}); err != nil {
		t.Fatalf("Save() = %v", err)
	}

	dir := filepath.Join(root, "s")
	if err := os.Chmod(dir, 0o555); err != nil {
		t.Fatalf("Chmod() = %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0o755) })

	if err := s.Delete("s", "sid"); err == nil {
		t.Error("Delete() = nil, want エラー")
	}
}

func TestPendingStoreSaveFailsOnUnwritableRoot(t *testing.T) {
	t.Parallel()

	// ルートをファイルにして、ディレクトリを作れない状況を作る。
	root := filepath.Join(t.TempDir(), "root")
	if err := os.WriteFile(root, []byte("x"), 0o600); err != nil {
		t.Fatalf("WriteFile() = %v", err)
	}
	if err := store.NewPendingStore(root).Save("s", "sid", domain.Pending{}); err == nil {
		t.Error("Save() = nil, want エラー")
	}
}
