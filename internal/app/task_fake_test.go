package app_test

import (
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/k-kudo-hub/mdev-go/internal/app"
)

// タスク作成用 port の fake。zellij へ撃ったコマンドの並びを 1 本の記録に残す。
var (
	_ app.TabActor            = (*fakeTabActor)(nil)
	_ app.Clock               = (*fakeStopwatch)(nil)
	_ app.Sleeper             = (*fakeStopwatch)(nil)
	_ app.TaskControlLauncher = (*fakeLauncher)(nil)
)

// fakeStopwatch は時計と sleep を兼ねる。
//
// Sleep が実際に待つ代わりに仮想時刻を進めるので、ポーリングの回数と
// 予算の消費をテストの中で決められる。zellij の呼び出しにも時間を
// 使わせたい場合は spend を設定する。
type fakeStopwatch struct {
	mu     sync.Mutex
	now    time.Time
	slept  []time.Duration
	origin time.Time
}

func newStopwatch() *fakeStopwatch {
	base := time.Date(2026, 8, 10, 12, 0, 0, 0, time.UTC)
	return &fakeStopwatch{now: base, origin: base}
}

func (c *fakeStopwatch) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *fakeStopwatch) Sleep(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.slept = append(c.slept, d)
	c.now = c.now.Add(d)
}

// advance は zellij の呼び出しが時間を食ったことにする。
func (c *fakeStopwatch) advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = c.now.Add(d)
}

// elapsed は開始からの経過時間を返す。
func (c *fakeStopwatch) elapsed() time.Duration {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now.Sub(c.origin)
}

// fakeTabActor は zellij 操作を記録する。
type fakeTabActor struct {
	journal *paneJournal
	// caps は呼び出しごとに渡された上限。
	caps []time.Duration

	// tabNames は QueryTabNames が返す名前。registerAfter 回目の呼び出しから
	// tabToRegister が現れる(タブ登録の遅延を再現する)。
	tabNames      []string
	tabToRegister string
	registerAfter int
	queryCalls    int
	// queryErr は問い合わせが失敗する状況を作る。
	queryErr error

	// focusEmptyUntil 回目までの FocusTabVerified は偽を返す。
	focusEmptyUntil int
	focusCalls      int

	// newTabErr は new-tab の失敗を再現する。
	newTabErr error
	// spend は 1 回の呼び出しが消費する時間。劣化サーバの再現に使う。
	spend time.Duration
	// overrunOn はアクション名ごとの「上限を超えてかかる時間」である。
	//
	// 実際の打ち切りは上限ぴったりでは終わらない。proc.Command は期限のあとも
	// WaitDelay(2 秒)ぶん後始末を待ち、SIGKILL の配送と reap にも時間がかかる。
	// この超過ぶんだけ経過時間は予算を追い越し、残り予算は**負**になる。
	// 上限で頭打ちにする spend では負にならないため、別の口として持つ。
	overrunOn map[string]time.Duration
	clock     *fakeStopwatch
}

func (f *fakeTabActor) tick(action string, limit time.Duration) {
	f.caps = append(f.caps, limit)
	if f.clock == nil {
		return
	}
	// 打ち切りは上限までしか待たない。
	spent := f.spend
	if limit > 0 && spent > limit {
		spent = limit
	}
	// 超過ぶんは上限で頭打ちにしない(後始末は期限のあとに起きるため)。
	if spent += f.overrunOn[action]; spent > 0 {
		f.clock.advance(spent)
	}
}

func (f *fakeTabActor) QueryTabNames(limit time.Duration) ([]string, error) {
	f.tick("query-tab-names", limit)
	f.queryCalls++
	f.journal.add("query-tab-names")
	if f.queryErr != nil {
		return nil, f.queryErr
	}
	names := f.tabNames
	if f.tabToRegister != "" && f.queryCalls >= f.registerAfter {
		names = append(append([]string{}, names...), f.tabToRegister)
	}
	return names, nil
}

func (f *fakeTabActor) FocusTabVerified(limit time.Duration, name string) bool {
	f.tick("go-to-tab-name", limit)
	f.focusCalls++
	f.journal.add("go-to-tab-name " + name)
	return f.focusCalls > f.focusEmptyUntil
}

func (f *fakeTabActor) NewTab(limit time.Duration, name, cwd string, command []string) error {
	f.tick("new-tab", limit)
	f.journal.add(fmt.Sprintf("new-tab %s %s -- %s", name, cwd, strings.Join(command, " ")))
	return f.newTabErr
}

func (f *fakeTabActor) NewPane(limit time.Duration, direction, cwd string, command []string) error {
	f.tick("new-pane", limit)
	entry := fmt.Sprintf("new-pane %s %s", direction, cwd)
	if len(command) > 0 {
		entry += " -- " + strings.Join(command, " ")
	}
	f.journal.add(entry)
	return nil
}

func (f *fakeTabActor) MoveFocus(limit time.Duration, direction string) error {
	f.tick("move-focus", limit)
	f.journal.add("move-focus " + direction)
	return nil
}

func (f *fakeTabActor) FocusPreviousPane(limit time.Duration) error {
	f.tick("focus-previous-pane", limit)
	f.journal.add("focus-previous-pane")
	return nil
}

func (f *fakeTabActor) Resize(limit time.Duration, args ...string) error {
	f.tick("resize", limit)
	f.journal.add("resize " + strings.Join(args, " "))
	return nil
}

// count は記録のうち prefix で始まるものの数を返す。
func (j *paneJournal) count(prefix string) int {
	n := 0
	for _, e := range j.entries {
		if strings.HasPrefix(e, prefix) {
			n++
		}
	}
	return n
}

// indexOf は記録のうち prefix で始まる最初のものの位置を返す(無ければ -1)。
func (j *paneJournal) indexOf(prefix string) int {
	for i, e := range j.entries {
		if strings.HasPrefix(e, prefix) {
			return i
		}
	}
	return -1
}

// fakeLauncher は task-control ペインの起動コマンドを返す。
type fakeLauncher struct{}

func (fakeLauncher) TaskControlCommand(tab string) []string {
	return []string{"/x/bin/mdev", "pane", "task-control", tab}
}
