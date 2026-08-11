package store_test

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/k-kudo-hub/mdev-go/internal/infra/store"
)

// writePaneFile はテスト用にファイルを 1 つ書く。
func writePaneFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("ディレクトリの作成に失敗: %v", err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("ファイルの書き込みに失敗: %v", err)
	}
}

// newPaneStore は一時ディレクトリの上に PaneStore を組み立てる。
func newPaneStore(t *testing.T) (*store.PaneStore, string, string) {
	t.Helper()
	root := t.TempDir()
	pendingRoot := filepath.Join(root, "pending")
	conductorHome := filepath.Join(root, "conductor")
	return store.NewPaneStore(pendingRoot, conductorHome), pendingRoot, conductorHome
}

func TestPaneStoreListSortsByFileName(t *testing.T) {
	t.Parallel()

	s, pendingRoot, _ := newPaneStore(t)
	writePaneFile(t, filepath.Join(pendingRoot, "s1", "b.json"), `{"tab":"beta"}`)
	writePaneFile(t, filepath.Join(pendingRoot, "s1", "a.json"), `{"tab":"alpha"}`)
	// 拡張子が違うものは glob `*.json` に入らない。
	writePaneFile(t, filepath.Join(pendingRoot, "s1", "c.txt"), `{"tab":"ignored"}`)

	views, err := s.List("s1")
	if err != nil {
		t.Fatalf("List() = %v", err)
	}

	want := []string{"a.json", "b.json"}
	got := make([]string, 0, len(views))
	for _, v := range views {
		got = append(got, v.Name)
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("並び = %v, want %v", got, want)
	}
	if views[0].Tab != "alpha" || views[1].Tab != "beta" {
		t.Errorf("中身が読めていない: %+v", views)
	}
}

func TestPaneStoreListWithoutDirectory(t *testing.T) {
	t.Parallel()

	s, _, _ := newPaneStore(t)
	views, err := s.List("missing")
	if err != nil {
		t.Fatalf("List() = %v", err)
	}
	if len(views) != 0 {
		t.Errorf("List() = %+v, want 空", views)
	}
}

func TestPaneStoreListKeepsBrokenJSON(t *testing.T) {
	t.Parallel()

	// 壊れたファイルも要素としては残る(全フィールドが空文字になり、
	// タブ名の一致判定から外れて表示されない)。
	s, pendingRoot, _ := newPaneStore(t)
	writePaneFile(t, filepath.Join(pendingRoot, "s1", "broken.json"), `{broken`)

	views, err := s.List("s1")
	if err != nil {
		t.Fatalf("List() = %v", err)
	}
	if len(views) != 1 || views[0].Tab != "" {
		t.Errorf("List() = %+v, want タブ名が空の 1 件", views)
	}
}

func TestPaneStoreDeleteByTabRemovesAllMatches(t *testing.T) {
	t.Parallel()

	// --resume で再開するとセッション ID が変わるため、同じタブに複数の
	// pending が残ることがある。タブを消すときは全部消す。
	s, pendingRoot, _ := newPaneStore(t)
	dir := filepath.Join(pendingRoot, "s1")
	writePaneFile(t, filepath.Join(dir, "a.json"), `{"tab":"alpha"}`)
	writePaneFile(t, filepath.Join(dir, "b.json"), `{"tab":"alpha"}`)
	writePaneFile(t, filepath.Join(dir, "c.json"), `{"tab":"beta"}`)

	if err := s.DeleteByTab("s1", "alpha"); err != nil {
		t.Fatalf("DeleteByTab() = %v", err)
	}

	views, err := s.List("s1")
	if err != nil {
		t.Fatalf("List() = %v", err)
	}
	if len(views) != 1 || views[0].Name != "c.json" {
		t.Errorf("残った pending = %+v, want c.json のみ", views)
	}
}

func TestPaneStoreDeleteByNameIsIdempotent(t *testing.T) {
	t.Parallel()

	s, pendingRoot, _ := newPaneStore(t)
	writePaneFile(t, filepath.Join(pendingRoot, "s1", "a.json"), `{"tab":"alpha"}`)

	for range 2 {
		// 2 回目は既に無いが、rm -f と同じくエラーにしない。
		if err := s.DeleteByName("s1", "a.json"); err != nil {
			t.Fatalf("DeleteByName() = %v", err)
		}
	}
}

func TestPaneStoreRemoveScreenState(t *testing.T) {
	t.Parallel()

	s, pendingRoot, _ := newPaneStore(t)
	path := filepath.Join(pendingRoot, "s1", ".screen-state", "alpha-123")
	writePaneFile(t, path, "working")

	if err := s.Remove("s1", "alpha-123"); err != nil {
		t.Fatalf("Remove() = %v", err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Errorf("状態ファイルが残っている: %v", err)
	}
	// 無いものを消しても成功する。
	if err := s.Remove("s1", "alpha-123"); err != nil {
		t.Errorf("Remove() 2 回目 = %v", err)
	}
}

func TestPaneStoreReadTodayAcrossSessions(t *testing.T) {
	t.Parallel()

	s, _, conductorHome := newPaneStore(t)
	daily := filepath.Join(conductorHome, "daily")
	writePaneFile(t, filepath.Join(daily, "s2", "2026-08-09.jsonl"), "{\"tab\":\"b\"}\n")
	writePaneFile(t, filepath.Join(daily, "s1", "2026-08-09.jsonl"), "{\"tab\":\"a\"}\n\n")
	// 別の日付は対象外。
	writePaneFile(t, filepath.Join(daily, "s1", "2026-08-08.jsonl"), "{\"tab\":\"old\"}\n")

	lines := s.ReadToday("2026-08-09")

	// セッション名の昇順。空行は読み飛ばす。
	want := []string{`{"tab":"a"}`, `{"tab":"b"}`}
	got := make([]string, 0, len(lines))
	for _, line := range lines {
		got = append(got, string(line))
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("ReadToday() = %v, want %v", got, want)
	}
}

func TestPaneStoreReadTodayWithoutDailyDir(t *testing.T) {
	t.Parallel()

	s, _, _ := newPaneStore(t)
	if lines := s.ReadToday("2026-08-09"); len(lines) != 0 {
		t.Errorf("ReadToday() = %v, want 空", lines)
	}
}

func TestPaneStoreReadNews(t *testing.T) {
	t.Parallel()

	s, _, conductorHome := newPaneStore(t)
	writePaneFile(t, filepath.Join(conductorHome, "news", "2026-08-09.json"), `{"items":[]}`)

	if got := string(s.Read("2026-08-09")); got != `{"items":[]}` {
		t.Errorf("Read() = %q", got)
	}
	if got := s.Read("2026-08-08"); got != nil {
		t.Errorf("無い日付の Read() = %q, want nil", got)
	}
}

func TestPaneStoreLoadConfig(t *testing.T) {
	t.Parallel()

	s, _, conductorHome := newPaneStore(t)
	writePaneFile(t, filepath.Join(conductorHome, "config.json"),
		`{"agents":{"codex":{"detection":"screen"}}}`)

	config, ok := s.Load()
	if !ok {
		t.Fatal("読めた設定なのに ok=false が返った")
	}
	if got := config.AgentDetection("codex"); got != "screen" {
		t.Errorf("AgentDetection() = %q, want screen", got)
	}
}

func TestPaneStoreLoadBrokenConfigFallsBackToHooks(t *testing.T) {
	t.Parallel()

	// 設定が壊れていても画面が出なくなるより既定へ落ちるほうがよい。
	// ただし「読めなかった」ことは ok で伝える。中身が分からない状態を
	// 「エージェントが 1 つも無い」と取り違えさせないためである。
	s, _, conductorHome := newPaneStore(t)
	writePaneFile(t, filepath.Join(conductorHome, "config.json"), `{broken`)

	config, ok := s.Load()
	if ok {
		t.Error("壊れた設定なのに ok=true が返った")
	}
	if got := config.AgentDetection("codex"); got != "hooks" {
		t.Errorf("AgentDetection() = %q, want hooks", got)
	}
}

func TestPaneStoreLoadWithoutConfigFileReportsNotOK(t *testing.T) {
	t.Parallel()

	// config.json も config.default.json も無い(CONDUCTOR_HOME の指し先が
	// 違う)。設定が空なのか読めないのか区別が付かないので ok=false にする。
	s, _, _ := newPaneStore(t)

	config, ok := s.Load()
	if ok {
		t.Error("設定ファイルが無いのに ok=true が返った")
	}
	if len(config.Agents) != 0 {
		t.Errorf("agents = %v, want 空", config.Agents)
	}
}

func TestPaneStoreScreenStateReadWrite(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	s := store.NewPaneStore(store.PendingRoot(root), root)

	// 一度も書いていないタブは空文字(現行版の `cat 2>/dev/null || true`)。
	if got := s.ReadScreenState("s1", "slug"); got != "" {
		t.Errorf("ReadScreenState() = %q, want 空", got)
	}

	if err := s.WriteScreenState("s1", "slug", "idle_pending 1754870400"); err != nil {
		t.Fatalf("WriteScreenState() = %v", err)
	}
	// 現行版の `echo` と同じく末尾に改行が付く。
	path := filepath.Join(store.PendingRoot(root), "s1", ".screen-state", "slug")
	b, err := os.ReadFile(path) //nolint:gosec // テストの一時ディレクトリ
	if err != nil {
		t.Fatalf("状態ファイルが読めない: %v", err)
	}
	if string(b) != "idle_pending 1754870400\n" {
		t.Errorf("ファイルの中身 = %q", string(b))
	}
	if got := s.ReadScreenState("s1", "slug"); got != "idle_pending 1754870400\n" {
		t.Errorf("ReadScreenState() = %q", got)
	}

	// 書き直しは完全に置き換える。
	if err := s.WriteScreenState("s1", "slug", "working"); err != nil {
		t.Fatalf("WriteScreenState() = %v", err)
	}
	if got := s.ReadScreenState("s1", "slug"); got != "working\n" {
		t.Errorf("上書き後の ReadScreenState() = %q", got)
	}
}

func TestPaneStoreIsFile(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	s := store.NewPaneStore(store.PendingRoot(root), root)

	path := filepath.Join(root, "transcript.jsonl")
	if err := os.WriteFile(path, []byte("{}"), 0o600); err != nil {
		t.Fatalf("fixture の作成に失敗: %v", err)
	}

	if !s.IsFile(path) {
		t.Error("実在するファイルが偽になった")
	}
	if s.IsFile(filepath.Join(root, "missing.jsonl")) {
		t.Error("存在しないファイルが真になった")
	}
	if s.IsFile(root) {
		t.Error("ディレクトリが真になった")
	}
}
