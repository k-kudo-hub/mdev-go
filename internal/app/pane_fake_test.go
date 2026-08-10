package app_test

import (
	"strings"

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
	_ app.ShellRunner        = (*fakeShellRunner)(nil)
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
	journal *paneJournal
	ids     []string
}

func (f *fakeTabCloser) CloseTabByID(id string) {
	f.ids = append(f.ids, id)
	f.journal.add("close-tab-by-id " + id)
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

type fakeShellRunner struct {
	journal *paneJournal

	// uploadOutput / uploadErr は UploadLog の戻り値。
	uploadOutput string
	uploadErr    error

	uploadedTabs   []string
	restoredTasks  []string
	fetchNewsCalls int
	restoreCalls   int
	detectSessions []string
}

func (f *fakeShellRunner) UploadLog(tab string) (string, error) {
	f.uploadedTabs = append(f.uploadedTabs, tab)
	f.journal.add("upload-log " + tab)
	return f.uploadOutput, f.uploadErr
}

func (f *fakeShellRunner) RestoreTask(tab, session, completedAt string) {
	f.restoredTasks = append(f.restoredTasks, strings.Join([]string{tab, session, completedAt}, " "))
	f.journal.add("restore-task " + tab)
}

func (f *fakeShellRunner) FetchNews() {
	f.fetchNewsCalls++
	f.journal.add("fetch-news")
}

func (f *fakeShellRunner) RestoreSession() {
	f.restoreCalls++
	f.journal.add("restore-session")
}

func (f *fakeShellRunner) ScreenDetectTick(session string) {
	f.detectSessions = append(f.detectSessions, session)
	f.journal.add("screen-detect-tick " + session)
}
