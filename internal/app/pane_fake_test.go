package app_test

import (
	"errors"
	"strings"
	"time"

	"github.com/k-kudo-hub/mdev-go/internal/app"
	"github.com/k-kudo-hub/mdev-go/internal/domain"
)

// ペイン用 port の fake 実装。副作用の有無と順序を観測するために使う。
var (
	_ app.TabLister          = (*fakeTabLister)(nil)
	_ app.TabCloser          = (*fakeTabCloser)(nil)
	_ app.PendingLister      = (*fakePendingLister)(nil)
	_ app.PendingRemover     = (*fakePendingRemover)(nil)
	_ app.RegistryRemover    = (*fakeRegistryRemover)(nil)
	_ app.ScreenStateRemover = (*fakeScreenStateRemover)(nil)
	_ app.DailyReader        = (*fakeDailyReader)(nil)
	_ app.NewsReader         = (*fakeNewsReader)(nil)
	_ app.ConfigLoader       = (*fakeConfigLoader)(nil)
	_ app.URLOpener          = (*fakeURLOpener)(nil)
	_ app.NewsFetcher        = (*fakeNewsFetcher)(nil)
	_ app.LogUploadRunner    = (*fakeLogUploader)(nil)
	_ app.Focuser            = (*fakePaneFocuser)(nil)
)

// paneJournal は複数の port にまたがる副作用の順序を 1 本の並びで記録する。
//
// 削除フローは「upload が失敗したら何も消さない」「消す順番は pending →
// registry → screen-state → close-tab」という順序の契約を持つ。port ごとに
// 呼び出し回数を数えるだけでは順序が固定できないため、共有の記録先を持たせる。
type paneJournal struct {
	entries []string
}

func (j *paneJournal) add(entry string) {
	if j == nil {
		return
	}
	j.entries = append(j.entries, entry)
}

type fakeTabLister struct {
	output string
	calls  int
}

func (f *fakeTabLister) ListTabs() string {
	f.calls++
	return f.output
}

type fakeTabCloser struct {
	journal      *paneJournal
	ids          []string
	closedActive int
}

func (f *fakeTabCloser) CloseTabByID(id string) {
	f.ids = append(f.ids, id)
	f.journal.add("close-tab-by-id " + id)
}

func (f *fakeTabCloser) CloseActiveTab() {
	f.closedActive++
	f.journal.add("close-tab")
}

type fakePendingLister struct {
	views map[string][]domain.PendingView
	err   error
}

func (f *fakePendingLister) List(session string) ([]domain.PendingView, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.views[session], nil
}

type fakePendingRemover struct {
	journal      *paneJournal
	deletedTabs  []string
	deletedNames []string
	err          error
}

func (f *fakePendingRemover) DeleteByTab(session, tab string) error {
	if f.err != nil {
		return f.err
	}
	f.deletedTabs = append(f.deletedTabs, session+"/"+tab)
	f.journal.add("pending-delete-by-tab " + tab)
	return nil
}

func (f *fakePendingRemover) DeleteByName(session, name string) error {
	if f.err != nil {
		return f.err
	}
	f.deletedNames = append(f.deletedNames, session+"/"+name)
	f.journal.add("pending-delete-by-name " + name)
	return nil
}

type fakeRegistryRemover struct {
	journal *paneJournal
	removed []string
	err     error
}

func (f *fakeRegistryRemover) RemoveByTab(session, tab string) error {
	if f.err != nil {
		return f.err
	}
	f.removed = append(f.removed, session+"/"+tab)
	f.journal.add("registry-remove " + tab)
	return nil
}

type fakeScreenStateRemover struct {
	journal *paneJournal
	removed []string
	err     error
}

func (f *fakeScreenStateRemover) Remove(session, slug string) error {
	if f.err != nil {
		return f.err
	}
	f.removed = append(f.removed, session+"/"+slug)
	f.journal.add("screen-state-remove " + slug)
	return nil
}

type fakeDailyReader struct {
	lines [][]byte
	dates []string
}

func (f *fakeDailyReader) ReadToday(date string) [][]byte {
	f.dates = append(f.dates, date)
	return f.lines
}

type fakeNewsReader struct {
	data  []byte
	dates []string
}

func (f *fakeNewsReader) Read(date string) []byte {
	f.dates = append(f.dates, date)
	return f.data
}

type fakeConfigLoader struct {
	config domain.Config
	// failed は設定を読めなかった状況を表す(ファイルが無い・壊れている)。
	failed bool
}

func (f *fakeConfigLoader) Load() (domain.Config, bool) { return f.config, !f.failed }

type fakeURLOpener struct {
	opened []string
}

func (f *fakeURLOpener) Open(url string) { f.opened = append(f.opened, url) }

type fakePaneFocuser struct {
	journal *paneJournal
	focused []string
}

func (f *fakePaneFocuser) FocusTab(name string) error {
	f.focused = append(f.focused, name)
	f.journal.add("go-to-tab-name " + name)
	return nil
}

type fakeNewsFetcher struct {
	journal *paneJournal

	fetchNewsCalls int
	// dates は FetchNews に渡された日付。
	dates []string
}

func (f *fakeNewsFetcher) FetchNews(date string) {
	f.fetchNewsCalls++
	f.dates = append(f.dates, date)
	if f.journal != nil {
		f.journal.add("fetch-news")
	}
}

// fakeLogUploader は作業ログのアップロードの代役である。
//
// 削除フローが見ているのは戻り値の 3 通り(飛ばした・アップロードした・
// 失敗した)だけなので、それを直接指定できるようにしている。
type fakeLogUploader struct {
	journal *paneJournal

	// output / err は UploadLog の戻り値。
	output string
	err    error

	uploadedTabs []string
}

func (f *fakeLogUploader) UploadLog(_ app.PaneEnv, tab string) (string, error) {
	f.uploadedTabs = append(f.uploadedTabs, tab)
	f.journal.add("upload-log " + tab)
	return f.output, f.err
}

// --- スクリーン検出用の fake ---

var (
	_ app.PaneLister        = (*fakePaneLister)(nil)
	_ app.ScreenDumper      = (*fakeScreenDumper)(nil)
	_ app.ScreenStateStore  = (*fakeScreenStateStore)(nil)
	_ app.PendingSaver      = (*fakePendingSaver)(nil)
	_ app.RegistryTabLookup = (*fakeRegistryLookup)(nil)
)

type fakePaneLister struct {
	panes []app.AgentPane
	calls int
}

func (f *fakePaneLister) ListAgentPanes() []app.AgentPane {
	f.calls++
	return f.panes
}

type fakeScreenDumper struct {
	journal *paneJournal
	dumps   map[string]string
	ids     []string
	// clock と spend を入れると、1 枚の取得が時間を食ったことにできる。
	clock *advancingClock
	spend time.Duration
}

func (f *fakeScreenDumper) DumpScreen(paneID string) string {
	f.ids = append(f.ids, paneID)
	f.journal.add("dump-screen " + paneID)
	if f.clock != nil {
		f.clock.advance(f.spend)
	}
	return f.dumps[paneID]
}

// fakeScreenStateStore は状態ファイルの中身を "<session>/<slug>" 鍵で持つ。
type fakeScreenStateStore struct {
	journal *paneJournal
	lines   map[string]string
	err     error
}

func (f *fakeScreenStateStore) ReadScreenState(session, slug string) string {
	return f.lines[session+"/"+slug]
}

func (f *fakeScreenStateStore) WriteScreenState(session, slug, line string) error {
	if f.err != nil {
		return f.err
	}
	f.lines[session+"/"+slug] = line
	f.journal.add("screen-state-write " + slug + " " + line)
	return nil
}

type fakePendingSaver struct {
	journal  *paneJournal
	saved    []domain.Pending
	sessions []string
	err      error
}

func (f *fakePendingSaver) Save(session, sessionID string, pending domain.Pending) error {
	if f.err != nil {
		return f.err
	}
	f.saved = append(f.saved, pending)
	f.sessions = append(f.sessions, session)
	f.journal.add("pending-save " + sessionID)
	return nil
}

// fakeRegistryLookup は "<session>/<tab>" 鍵でエントリを返す。
type fakeRegistryLookup struct {
	entries map[string]domain.RegistryEntry
}

func (f *fakeRegistryLookup) LatestByTabMtime(session, tab string) (domain.RegistryEntry, bool) {
	entry, ok := f.entries[session+"/"+tab]
	return entry, ok
}

// errScreenDetect はスクリーン検出が失敗した状況を表す。
var errScreenDetect = errors.New("スクリーン検出が失敗した")

var _ app.ScreenTicker = (*fakeScreenTicker)(nil)

// fakeScreenTicker は Dashboard から見たスクリーン検出の呼び出しを記録する。
type fakeScreenTicker struct {
	journal  *paneJournal
	sessions []string
	err      error
}

func (f *fakeScreenTicker) Tick(env app.PaneEnv) error {
	f.sessions = append(f.sessions, env.Session())
	f.journal.add("screen-detect-tick " + env.Session())
	return f.err
}

// --- 復元用の fake ---

var (
	_ app.RegistryLister = (*fakeRegistryReader)(nil)
	_ app.TabNameQuerier = (*fakeTabNames)(nil)
	_ app.PathChecker    = (*fakePathChecker)(nil)
	_ app.TaskMaker      = (*fakeTaskMaker)(nil)
)

// errCreateFailed はタブそのものが作られなかった状況を表す
// (現行 create_task が new-tab の rc をそのまま返す枝)。
var errCreateFailed = errors.New("タブを作れなかった")

// errRegistryRead はレジストリを読めなかった状況を表す。
var errRegistryRead = errors.New("レジストリを読めない")

// errTabQuery は既存タブの問い合わせが失敗した状況を表す。
var errTabQuery = errors.New("タブ名の一覧を取得できない")

// fakeRegistryReader はレジストリの読み取りと削除を記録する。
type fakeRegistryReader struct {
	journal *paneJournal
	entries map[string][]domain.RegistryEntry
	removed []string
	err     error
}

func (f *fakeRegistryReader) List(session string) ([]domain.RegistryEntry, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.entries[session], nil
}

func (f *fakeRegistryReader) RemoveByTab(session, tab string) error {
	f.removed = append(f.removed, session+"/"+tab)
	return nil
}

// fakeTabNames は既存タブ名の問い合わせを記録する。
type fakeTabNames struct {
	names []string
	calls int
	err   error
}

func (f *fakeTabNames) QueryTabNames(time.Duration) ([]string, error) {
	f.calls++
	if f.err != nil {
		return nil, f.err
	}
	return f.names, nil
}

// fakePathChecker は dir と transcript の実在を差し替える。
type fakePathChecker struct {
	dirs  map[string]bool
	files map[string]bool
}

func (f *fakePathChecker) IsDir(path string) bool  { return f.dirs[path] }
func (f *fakePathChecker) IsFile(path string) bool { return f.files[path] }

// fakeTaskMaker は TaskCreator の代わりに呼び出しだけを記録する。
type fakeTaskMaker struct {
	journal *paneJournal
	specs   []app.TaskSpec
	result  app.TaskCreateResult
	err     error
	// clock と spend を入れると、1 回の作成が時間を食ったことにできる。
	clock *advancingClock
	spend time.Duration
}

func (f *fakeTaskMaker) Execute(_ app.PaneEnv, spec app.TaskSpec) (app.TaskCreateResult, error) {
	f.specs = append(f.specs, spec)
	f.journal.add("create-task " + spec.Name)
	if f.clock != nil {
		f.clock.advance(f.spend)
	}
	return f.result, f.err
}

// advancingClock は明示的に進めた分だけ時刻が動く時計である。
//
// 予算のテストで「この呼び出しが何秒かかったか」を組み立てるために使う。
// 実時間で待つと予算(15 秒・60 秒)ぶんテストが止まってしまう。
type advancingClock struct{ now time.Time }

func newAdvancingClock() *advancingClock {
	return &advancingClock{now: time.Date(2026, 8, 11, 10, 0, 0, 0, time.UTC)}
}

func (c *advancingClock) Now() time.Time { return c.now }

func (c *advancingClock) advance(d time.Duration) { c.now = c.now.Add(d) }

var _ app.SessionStarter = (*fakeSessionStarter)(nil)

// fakeSessionStarter は Dashboard から見たセッション復元の呼び出しを記録する。
type fakeSessionStarter struct {
	journal  *paneJournal
	sessions []string
	warnings []string
}

func (f *fakeSessionStarter) Restore(env app.PaneEnv) []string {
	f.sessions = append(f.sessions, env.Session())
	f.journal.add("restore-session " + env.Session())
	return f.warnings
}

var _ app.DailyRestoreStore = (*fakeDailyRestore)(nil)

// errDailyWrite は daily ログを更新できなかった状況を表す。
var errDailyWrite = errors.New("daily ログを書けない")

// fakeDailyRestore は daily ログの検索と restored の書き込みを記録する。
type fakeDailyRestore struct {
	journal *paneJournal
	target  domain.DailyRestoreTarget
	found   bool
	dates   []string
	// marked は MarkRestored を試みた回数(失敗した回も数える)。
	marked  int
	markErr error
}

func (f *fakeDailyRestore) FindRestorable(session, date, tab, _ string) (domain.DailyRestoreTarget, bool) {
	f.dates = append(f.dates, date)
	f.journal.add("daily-find " + session + " " + date + " " + tab)
	return f.target, f.found
}

func (f *fakeDailyRestore) MarkRestored(session, date, tab, _ string) error {
	f.marked++
	f.journal.add("daily-mark " + session + " " + date + " " + tab)
	return f.markErr
}

var _ app.TaskRestoreRunner = (*fakeTaskRestoreRunner)(nil)

// fakeTaskRestoreRunner は Done ペインから見た復元の呼び出しを記録する。
type fakeTaskRestoreRunner struct {
	calls   []string
	warning string
	err     error
}

func (f *fakeTaskRestoreRunner) Restore(env app.PaneEnv, tab, session, completedAt string) (string, error) {
	f.calls = append(f.calls, strings.Join([]string{env.Session(), tab, session, completedAt}, " "))
	return f.warning, f.err
}
