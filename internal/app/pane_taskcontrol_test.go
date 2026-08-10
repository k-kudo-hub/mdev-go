package app_test

import (
	"encoding/json"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/k-kudo-hub/mdev-go/internal/app"
	"github.com/k-kudo-hub/mdev-go/internal/domain"
)

// task-control.sh(m / w / dd)の移植を固定する。
// 期待値の根拠は test.sh 22b(dd でレジストリが消える)と 36(waiting-toggle)。

// fakePendingRaw は pending の生読み書きを覚える。
type fakePendingRaw struct {
	journal *paneJournal
	// files は session/name をキーにした中身。
	files map[string][]byte
	// order は FindRawByTab が返す順(ファイル名の昇順を模す)。
	order    []string
	findErr  error
	writeErr error
	// rawOverride を設定すると、見つけた pending の中身としてこれを返す。
	rawOverride []byte
}

var _ app.PendingRawStore = (*fakePendingRaw)(nil)

func (f *fakePendingRaw) FindRawByTab(session, tab string) (string, []byte, bool, error) {
	if f.findErr != nil {
		return "", nil, false, f.findErr
	}
	for _, name := range f.order {
		data, ok := f.files[session+"/"+name]
		if !ok {
			continue
		}
		var probe struct {
			Tab string `json:"tab"`
		}
		if json.Unmarshal(data, &probe) != nil || probe.Tab != tab {
			continue
		}
		if f.rawOverride != nil {
			return name, f.rawOverride, true, nil
		}
		return name, data, true, nil
	}
	return "", nil, false, nil
}

func (f *fakePendingRaw) WriteRaw(session, name string, data []byte) error {
	if f.writeErr != nil {
		return f.writeErr
	}
	f.files[session+"/"+name] = data
	f.journal.add("pending-write " + name)
	return nil
}

// fakePendingFinder はタブ名で pending を引く。
type fakePendingFinder struct {
	pending domain.Pending
	found   bool
	err     error
}

var _ app.PendingFinder = (*fakePendingFinder)(nil)

func (f *fakePendingFinder) FindByTab(string, string) (domain.Pending, bool, error) {
	return f.pending, f.found, f.err
}

// taskControlFixture は task-control の 1 回ぶんの実行環境である。
type taskControlFixture struct {
	pane    *app.TaskControlPane
	journal *paneJournal
	raw     *fakePendingRaw
	finder  *fakePendingFinder
	focuser *fakePaneFocuser
	closer  *fakeTabCloser
	shell   *fakeShellRunner
}

func newTaskControlFixture(tabOutput string) *taskControlFixture {
	journal := &paneJournal{}
	raw := &fakePendingRaw{journal: journal, files: map[string][]byte{}}
	finder := &fakePendingFinder{}
	focuser := &fakePaneFocuser{journal: journal}
	closer := &fakeTabCloser{journal: journal}
	shell := &fakeShellRunner{journal: journal}

	return &taskControlFixture{
		pane: &app.TaskControlPane{
			Pending: finder,
			Raw:     raw,
			Focuser: focuser,
			Clock:   paneClock{now: time.Date(2026, 8, 10, 11, 22, 33, 0, time.UTC)},
			Deleter: &app.TaskDeleter{
				Remover:                &fakePendingRemover{journal: journal},
				Registry:               &fakeRegistryRemover{journal: journal},
				ScreenState:            &fakeScreenStateRemover{journal: journal},
				Tabs:                   &fakeTabLister{output: tabOutput},
				Closer:                 closer,
				Recorder:               &fakeRecorder{journal: journal},
				Shell:                  shell,
				CloseActiveOnMissingID: true,
			},
		},
		journal: journal,
		raw:     raw,
		finder:  finder,
		focuser: focuser,
		closer:  closer,
		shell:   shell,
	}
}

var taskControlEnv = app.PaneEnv{ZellijSession: "s1"}

// ---- 表示 ----------------------------------------------------------------

func TestTaskControlRefresh(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		event string
		found bool
		want  string
	}{
		{"pending が無ければ通常表示", "", false, domain.RenderTaskControlBar(false)},
		{"Notification は通常表示", domain.EventNotification, true, domain.RenderTaskControlBar(false)},
		{"Waiting は WAITING 表示", domain.EventWaiting, true, domain.RenderTaskControlBar(true)},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			f := newTaskControlFixture("")
			f.finder.pending = domain.Pending{Tab: "t", Event: tc.event}
			f.finder.found = tc.found

			got, err := f.pane.Refresh(taskControlEnv, "t")
			if err != nil {
				t.Fatalf("Refresh() = %v", err)
			}
			if got != tc.want {
				t.Errorf("Refresh() = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestTaskControlRefreshReportsReadFailure(t *testing.T) {
	t.Parallel()

	f := newTaskControlFixture("")
	f.finder.err = errors.New("読めない")
	if _, err := f.pane.Refresh(taskControlEnv, "t"); err == nil {
		t.Fatal("Refresh() が成功してしまった")
	}
}

// ---- m キー --------------------------------------------------------------

func TestTaskControlGoToMain(t *testing.T) {
	t.Parallel()

	f := newTaskControlFixture("")
	if err := f.pane.GoToMain(); err != nil {
		t.Fatalf("GoToMain() = %v", err)
	}
	if want := []string{domain.MainTabName}; !reflect.DeepEqual(f.focuser.focused, want) {
		t.Errorf("移動先 = %v, want %v", f.focuser.focused, want)
	}
}

// ---- w キー --------------------------------------------------------------

func TestTaskControlToggleWaiting(t *testing.T) {
	t.Parallel()

	f := newTaskControlFixture("")
	f.raw.order = []string{"a.json"}
	f.raw.files["s1/a.json"] = []byte(`{"tab":"t","event":"Notification","time":"10:00:00"}`)

	if err := f.pane.ToggleWaiting(taskControlEnv, "t"); err != nil {
		t.Fatalf("ToggleWaiting() = %v", err)
	}
	want := `{"tab":"t","event":"Waiting","time":"11:22:33","prev_event":"Notification"}`
	if got := string(f.raw.files["s1/a.json"]); got != want {
		t.Errorf("書き込み結果 = %s\nwant %s", got, want)
	}

	// もう一度で元へ戻る。
	if err := f.pane.ToggleWaiting(taskControlEnv, "t"); err != nil {
		t.Fatalf("ToggleWaiting() = %v", err)
	}
	want = `{"tab":"t","event":"Notification","time":"11:22:33"}`
	if got := string(f.raw.files["s1/a.json"]); got != want {
		t.Errorf("戻した結果 = %s\nwant %s", got, want)
	}
}

func TestTaskControlToggleWaitingIsNoOpWithoutPending(t *testing.T) {
	t.Parallel()

	// pending がまだ無いタスクでは何もしない(新しく作らない)。
	// Waiting はエージェントが Notification / Stop を出したタスクにだけ
	// 意味があり、勝手に作るとセッション ID を鍵にする解決 hook が
	// 後片付けできなくなる。
	f := newTaskControlFixture("")
	if err := f.pane.ToggleWaiting(taskControlEnv, "fresh-task"); err != nil {
		t.Fatalf("ToggleWaiting() = %v", err)
	}
	if len(f.raw.files) != 0 {
		t.Errorf("pending を作ってしまった: %v", f.raw.files)
	}
	if f.journal.indexOf("pending-write") >= 0 {
		t.Error("書き込みが起きている")
	}
}

func TestTaskControlToggleWaitingTouchesOnlyTheFirstMatch(t *testing.T) {
	t.Parallel()

	// タブ一致の pending が複数ある(--resume でセッション ID が変わった)
	// 場合、現行版は glob の展開順で最初の 1 件だけを書き換える。
	f := newTaskControlFixture("")
	f.raw.order = []string{"a.json", "b.json"}
	f.raw.files["s1/a.json"] = []byte(`{"tab":"t","event":"Stop"}`)
	f.raw.files["s1/b.json"] = []byte(`{"tab":"t","event":"Stop"}`)

	if err := f.pane.ToggleWaiting(taskControlEnv, "t"); err != nil {
		t.Fatalf("ToggleWaiting() = %v", err)
	}
	if got := string(f.raw.files["s1/b.json"]); got != `{"tab":"t","event":"Stop"}` {
		t.Errorf("2 件目まで書き換えている: %s", got)
	}
	if f.journal.count("pending-write") != 1 {
		t.Errorf("書き込み回数 = %d, want 1", f.journal.count("pending-write"))
	}
}

func TestTaskControlToggleWaitingReportsWriteFailure(t *testing.T) {
	t.Parallel()

	f := newTaskControlFixture("")
	f.raw.order = []string{"a.json"}
	f.raw.files["s1/a.json"] = []byte(`{"tab":"t","event":"Stop"}`)
	f.raw.writeErr = errors.New("書けない")

	if err := f.pane.ToggleWaiting(taskControlEnv, "t"); err == nil {
		t.Fatal("書き込み失敗が握り潰されている")
	}
}

func TestTaskControlToggleWaitingKeepsUnconvertiblePending(t *testing.T) {
	t.Parallel()

	// 変換できない中身は書き戻さない(現行版も jq が失敗したら一時ファイルを
	// 捨てて元のファイルを残す)。実際の store は JSON オブジェクトでない
	// ファイルをそもそも選ばないため、この枝は fake からしか通らない。
	f := newTaskControlFixture("")
	f.raw.order = []string{"a.json"}
	f.raw.rawOverride = []byte(`[1,2]`)
	f.raw.files["s1/a.json"] = []byte(`{"tab":"t","event":"Stop"}`)

	if err := f.pane.ToggleWaiting(taskControlEnv, "t"); err != nil {
		t.Fatalf("ToggleWaiting() = %v", err)
	}
	if f.journal.indexOf("pending-write") >= 0 {
		t.Error("変換できない中身を書き戻している")
	}
}

func TestTaskControlToggleWaitingReportsReadFailure(t *testing.T) {
	t.Parallel()

	f := newTaskControlFixture("")
	f.raw.findErr = errors.New("読めない")
	if err := f.pane.ToggleWaiting(taskControlEnv, "t"); err == nil {
		t.Fatal("読み取り失敗が握り潰されている")
	}
}

// ---- dd キー -------------------------------------------------------------

func TestTaskControlDeleteOrder(t *testing.T) {
	t.Parallel()

	f := newTaskControlFixture("ID POS NAME\n1 x Main\n3 x my task\n")

	prep, err := f.pane.PrepareDelete(taskControlEnv, "my task")
	if err != nil {
		t.Fatalf("PrepareDelete() = %v", err)
	}
	if prep.Cancelled {
		t.Fatal("中止になってしまった")
	}
	if err := f.pane.CommitDelete(taskControlEnv, "my task"); err != nil {
		t.Fatalf("CommitDelete() = %v", err)
	}

	want := []string{
		"record my task",
		"upload-log my task",
		"pending-delete-by-tab my task",
		"registry-remove my task",
		// Shell 版の task-control は消していなかった。統合により付与した
		// (同名タブを作り直したときに前の状態を引き継がせない)。
		"screen-state-remove " + domain.ScreenTabSlug("my task"),
		// 空白を含むタブ名でも id が引ける(ResolveTabID は先頭 2 列を落とす)。
		"close-tab-by-id 3",
	}
	if !reflect.DeepEqual(f.journal.entries, want) {
		t.Errorf("副作用の並び = %v\nwant %v", f.journal.entries, want)
	}
}

func TestTaskControlDeleteFallsBackToCloseTab(t *testing.T) {
	t.Parallel()

	// id が引けないときは close-tab へ落ちる。task-control は自分のタブの
	// 中で動いているので「今のタブ」を閉じれば概ね正しい。
	f := newTaskControlFixture("ID POS NAME\n1 x Main\n")

	if _, err := f.pane.PrepareDelete(taskControlEnv, "gone"); err != nil {
		t.Fatalf("PrepareDelete() = %v", err)
	}
	if err := f.pane.CommitDelete(taskControlEnv, "gone"); err != nil {
		t.Fatalf("CommitDelete() = %v", err)
	}
	if f.closer.closedActive != 1 {
		t.Errorf("close-tab へ落ちていない(closedActive=%d)", f.closer.closedActive)
	}
	if len(f.closer.ids) != 0 {
		t.Errorf("id 指定で閉じている: %v", f.closer.ids)
	}
}

func TestTaskControlDeleteCancelsOnUploadFailure(t *testing.T) {
	t.Parallel()

	// アップロードが失敗したら**何も消さない**。タブを消すと作業ログを
	// 永久に失うため、これがこのフローで最も重要な契約である。
	f := newTaskControlFixture("ID POS NAME\n1 x t\n")
	f.shell.uploadErr = errors.New("送信に失敗")

	prep, err := f.pane.PrepareDelete(taskControlEnv, "t")
	if err != nil {
		t.Fatalf("PrepareDelete() = %v", err)
	}
	if !prep.Cancelled {
		t.Fatal("中止になっていない")
	}
	for _, prefix := range []string{"pending-delete", "registry-remove", "screen-state-remove", "close-tab"} {
		if f.journal.indexOf(prefix) >= 0 {
			t.Errorf("%s が起きている(何も消してはいけない): %v", prefix, f.journal.entries)
		}
	}
}

// ---- Dashboard 側は close-tab へ落ちないこと ------------------------------

func TestDashboardDeleteDoesNotFallBackToCloseTab(t *testing.T) {
	t.Parallel()

	// Dashboard は Main タブの中で動いているため、close-tab へ落ちると
	// Main を閉じてしまう。現行版もフォールバックを持たない。
	f := newDashboardFixture(nil, "ID POS NAME\n1 x Main\n")

	if _, err := f.pane.PrepareDelete(dashboardEnv, "gone"); err != nil {
		t.Fatalf("PrepareDelete() = %v", err)
	}
	if err := f.pane.CommitDelete(dashboardEnv, "gone"); err != nil {
		t.Fatalf("CommitDelete() = %v", err)
	}
	if f.closer.closedActive != 0 {
		t.Errorf("Dashboard が close-tab を撃っている(Main を閉じうる)")
	}
}
