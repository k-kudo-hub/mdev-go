package store_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/k-kudo-hub/mdev-go/internal/app"
	"github.com/k-kudo-hub/mdev-go/internal/domain"
	"github.com/k-kudo-hub/mdev-go/internal/infra/store"
)

var _ app.RegistryStore = (*store.RegistryStore)(nil)

func TestRegistryRoot(t *testing.T) {
	t.Parallel()

	// 現行 registry-lib.sh の `$CONDUCTOR_HOME/tasks/<session>`。
	if got, want := store.RegistryRoot("/Users/x/.claude-conductor"), "/Users/x/.claude-conductor/tasks"; got != want {
		t.Errorf("RegistryRoot() = %q, want %q", got, want)
	}
}

func newTestEntry() domain.RegistryEntry {
	return domain.RegistryEntry{
		Tab:             "tab-one",
		Session:         "reg-sess",
		ClaudeSessionID: "sid-1",
		UpdatedAt:       "2026-08-08T10:11:12+0900",
		Dir:             "/tmp/dir1",
		TaskType:        "dev",
		Agent:           "claude",
		TranscriptPath:  "/tmp/t1.jsonl",
	}
}

func TestRegistryStoreUpsert(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	s := store.NewRegistryStore(root)

	if err := s.Upsert(newTestEntry()); err != nil {
		t.Fatalf("Upsert() = %v", err)
	}

	path := filepath.Join(root, "reg-sess", "sid-1.json")
	b, err := os.ReadFile(path) //nolint:gosec // テスト内で組み立てた一時パス
	if err != nil {
		t.Fatalf("エントリが %s に無い: %v", path, err)
	}
	var got map[string]any
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatalf("Unmarshal(%s) = %v", b, err)
	}
	want := map[string]any{
		"tab":               "tab-one",
		"session":           "reg-sess",
		"claude_session_id": "sid-1",
		"updated_at":        "2026-08-08T10:11:12+0900",
		"dir":               "/tmp/dir1",
		"task_type":         "dev",
		"agent":             "claude",
		"transcript_path":   "/tmp/t1.jsonl",
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("保存された JSON = %v, want %v", got, want)
	}
}

func TestRegistryStoreUpsertOverwritesAndIsolatesSessions(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	s := store.NewRegistryStore(root)

	first := newTestEntry()
	if err := s.Upsert(first); err != nil {
		t.Fatalf("Upsert() = %v", err)
	}

	// 同じ sid への upsert は完全上書き(タブ名の変更が反映される)。
	renamed := first
	renamed.Tab = "tab-renamed"
	if err := s.Upsert(renamed); err != nil {
		t.Fatalf("Upsert() = %v", err)
	}

	// セッションが違えば別ディレクトリに分かれ、既存のエントリを壊さない。
	other := first
	other.Session = "reg-other"
	other.Tab = "other-tab"
	if err := s.Upsert(other); err != nil {
		t.Fatalf("Upsert() = %v", err)
	}

	entries, err := s.List("reg-sess")
	if err != nil {
		t.Fatalf("List() = %v", err)
	}
	if !reflect.DeepEqual(entries, []domain.RegistryEntry{renamed}) {
		t.Errorf("List(reg-sess) = %+v, want %+v", entries, []domain.RegistryEntry{renamed})
	}

	otherEntries, err := s.List("reg-other")
	if err != nil {
		t.Fatalf("List() = %v", err)
	}
	if !reflect.DeepEqual(otherEntries, []domain.RegistryEntry{other}) {
		t.Errorf("List(reg-other) = %+v, want %+v", otherEntries, []domain.RegistryEntry{other})
	}
}

func TestRegistryStoreUpsertOmitsEmptyOptionalFields(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	s := store.NewRegistryStore(root)
	if err := s.Upsert(domain.RegistryEntry{
		Tab: "t", Session: "s", ClaudeSessionID: "sid", UpdatedAt: "2026-08-08T10:11:12+0900",
	}); err != nil {
		t.Fatalf("Upsert() = %v", err)
	}

	b, err := os.ReadFile(filepath.Join(root, "s", "sid.json")) //nolint:gosec // テスト内で組み立てた一時パス
	if err != nil {
		t.Fatalf("ReadFile() = %v", err)
	}
	var got map[string]any
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatalf("Unmarshal() = %v", err)
	}
	for _, key := range []string{"dir", "task_type", "agent", "transcript_path"} {
		if _, ok := got[key]; ok {
			t.Errorf("空の %q がキーとして残っている: %v", key, got)
		}
	}
}

func TestRegistryStoreUpsertNoopWithoutSessionOrID(t *testing.T) {
	t.Parallel()

	// 現行 registry_upsert は session / sid が空なら何もしない。
	tests := []struct {
		name  string
		entry domain.RegistryEntry
	}{
		{name: "session が空", entry: domain.RegistryEntry{ClaudeSessionID: "sid", Tab: "t"}},
		{name: "sid が空", entry: domain.RegistryEntry{Session: "s", Tab: "t"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			root := t.TempDir()
			if err := store.NewRegistryStore(root).Upsert(tt.entry); err != nil {
				t.Fatalf("Upsert() = %v", err)
			}
			entries, err := os.ReadDir(root)
			if err != nil {
				t.Fatalf("ReadDir() = %v", err)
			}
			if len(entries) != 0 {
				t.Errorf("ルート配下に %d 件作られた, want 0 件", len(entries))
			}
		})
	}
}

func TestRegistryStoreListSkipsBrokenEntries(t *testing.T) {
	t.Parallel()

	// jq -s は全件まとめて読むため 1 件でも壊れていると復元全体が止まる。
	// 現行版は 1 ファイルずつ検証しており、その挙動を引き継ぐ。
	root := t.TempDir()
	dir := filepath.Join(root, "s")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("MkdirAll() = %v", err)
	}
	files := map[string]string{
		"good-1.json":  `{"tab":"alpha","session":"s","claude_session_id":"good-1","updated_at":"2026-08-08T10:00:00+0900"}`,
		"broken.json":  `{"tab":`,
		"empty.json":   ``,
		"good-2.json":  `{"tab":"beta","session":"s","claude_session_id":"good-2","updated_at":"2026-08-08T11:00:00+0900"}`,
		"not-json.txt": `無視されるべき拡張子`,
	}
	for name, content := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o600); err != nil {
			t.Fatalf("WriteFile(%s) = %v", name, err)
		}
	}

	got, err := store.NewRegistryStore(root).List("s")
	if err != nil {
		t.Fatalf("List() = %v", err)
	}
	want := []domain.RegistryEntry{
		{Tab: "alpha", Session: "s", ClaudeSessionID: "good-1", UpdatedAt: "2026-08-08T10:00:00+0900"},
		{Tab: "beta", Session: "s", ClaudeSessionID: "good-2", UpdatedAt: "2026-08-08T11:00:00+0900"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("List() = %+v, want %+v", got, want)
	}
}

func TestRegistryStoreListReturnsEmptyForMissingSession(t *testing.T) {
	t.Parallel()

	got, err := store.NewRegistryStore(t.TempDir()).List("no-such-session")
	if err != nil {
		t.Fatalf("List() = %v", err)
	}
	if len(got) != 0 {
		t.Errorf("List() = %+v, want 空", got)
	}
}

func TestRegistryStoreRemove(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	s := store.NewRegistryStore(root)
	if err := s.Upsert(newTestEntry()); err != nil {
		t.Fatalf("Upsert() = %v", err)
	}
	other := newTestEntry()
	other.Session = "reg-other"
	if err := s.Upsert(other); err != nil {
		t.Fatalf("Upsert() = %v", err)
	}

	if err := s.Remove("reg-sess", "sid-1"); err != nil {
		t.Fatalf("Remove() = %v", err)
	}
	if entries, err := s.List("reg-sess"); err != nil || len(entries) != 0 {
		t.Errorf("List(reg-sess) = %+v, %v, want 空", entries, err)
	}
	if entries, err := s.List("reg-other"); err != nil || len(entries) != 1 {
		t.Errorf("List(reg-other) = %+v, %v, want 1 件", entries, err)
	}

	// 存在しないエントリの削除は成功として扱う。
	if err := s.Remove("reg-sess", "sid-1"); err != nil {
		t.Errorf("2 回目の Remove() = %v, want nil", err)
	}
	// session / sid が空なら何もしない。
	if err := s.Remove("", "sid"); err != nil {
		t.Errorf("Remove(\"\", ...) = %v, want nil", err)
	}
	if err := s.Remove("reg-sess", ""); err != nil {
		t.Errorf("Remove(..., \"\") = %v, want nil", err)
	}
}

func TestRegistryStoreRemoveByTab(t *testing.T) {
	t.Parallel()

	// pending が無い削除経路では sid が分からないため、タブ名で一致する
	// エントリをすべて消す。
	root := t.TempDir()
	s := store.NewRegistryStore(root)
	for _, e := range []domain.RegistryEntry{
		{Session: "s", ClaudeSessionID: "sid-2", Tab: "tab-x", UpdatedAt: "u"},
		{Session: "s", ClaudeSessionID: "sid-3", Tab: "tab-x", UpdatedAt: "u"},
		{Session: "s", ClaudeSessionID: "sid-4", Tab: "tab-keep", UpdatedAt: "u"},
	} {
		if err := s.Upsert(e); err != nil {
			t.Fatalf("Upsert() = %v", err)
		}
	}

	if err := s.RemoveByTab("s", "tab-x"); err != nil {
		t.Fatalf("RemoveByTab() = %v", err)
	}

	entries, err := s.List("s")
	if err != nil {
		t.Fatalf("List() = %v", err)
	}
	if len(entries) != 1 || entries[0].ClaudeSessionID != "sid-4" {
		t.Errorf("List() = %+v, want sid-4 のみ", entries)
	}

	// session / tab が空なら何もしない。
	if err := s.RemoveByTab("", "tab-keep"); err != nil {
		t.Errorf("RemoveByTab(\"\", ...) = %v, want nil", err)
	}
	if err := s.RemoveByTab("s", ""); err != nil {
		t.Errorf("RemoveByTab(..., \"\") = %v, want nil", err)
	}
	if entries, err := s.List("s"); err != nil || len(entries) != 1 {
		t.Errorf("List() = %+v, %v, want 1 件のまま", entries, err)
	}
}

func TestRegistryStoreUpsertFailsOnUnwritableRoot(t *testing.T) {
	t.Parallel()

	root := filepath.Join(t.TempDir(), "root")
	if err := os.WriteFile(root, []byte("x"), 0o600); err != nil {
		t.Fatalf("WriteFile() = %v", err)
	}
	if err := store.NewRegistryStore(root).Upsert(newTestEntry()); err == nil {
		t.Error("Upsert() = nil, want エラー")
	}
}

// TestRegistryStoreLatestByTabMtime は screen 由来 pending が 3 キーを借りる
// ときの選び方を固定する。
//
// 現行 screen-detect-lib.sh の `_screen_registry_lookup` は `stat -f %m` の
// **最大**でエントリを選ぶ。restore-session が `updated_at` で選ぶのとは
// キーが違い、この非対称は現行仕様としてそのまま維持する(evidence §2-6)。
func TestRegistryStoreLatestByTabMtime(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	s := store.NewRegistryStore(root)

	entries := []domain.RegistryEntry{
		{Session: "s1", ClaudeSessionID: "old", Tab: "cx", Dir: "/tmp/old", TaskType: "review",
			UpdatedAt: "2099-01-01T00:00:00+0000"},
		{Session: "s1", ClaudeSessionID: "new", Tab: "cx", Dir: "/tmp/new", TaskType: "dev",
			TranscriptPath: "/tmp/rollout.jsonl", UpdatedAt: "2020-01-01T00:00:00+0000"},
		{Session: "s1", ClaudeSessionID: "other", Tab: "another", Dir: "/tmp/another"},
	}
	for _, e := range entries {
		if err := s.Upsert(e); err != nil {
			t.Fatalf("Upsert() = %v", err)
		}
	}
	// updated_at では old が最新に見えるが、mtime では new が新しい。
	old := time.Now().Add(-time.Hour)
	if err := os.Chtimes(filepath.Join(root, "s1", "old.json"), old, old); err != nil {
		t.Fatalf("mtime の変更に失敗: %v", err)
	}

	got, ok := s.LatestByTabMtime("s1", "cx")
	if !ok {
		t.Fatal("LatestByTabMtime() が見つからないと返した")
	}
	if got.Dir != "/tmp/new" || got.TaskType != "dev" || got.TranscriptPath != "/tmp/rollout.jsonl" {
		t.Errorf("LatestByTabMtime() = %+v, want mtime が最新の new", got)
	}

	if _, ok := s.LatestByTabMtime("s1", "unknown"); ok {
		t.Error("一致しないタブで見つかったと返した")
	}
	if _, ok := s.LatestByTabMtime("missing", "cx"); ok {
		t.Error("存在しないセッションで見つかったと返した")
	}
}
