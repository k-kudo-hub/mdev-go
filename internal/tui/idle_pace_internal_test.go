package tui

import (
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/k-kudo-hub/mdev-go/internal/app"
)

// 未アタッチ減速のテスト。
//
// 最も大事なのは「減速を足してもポーリングのチェーンが 1 本のままである」
// ことである(pane.go の不変条件)。attach の確認はポーリングの着弾に
// 相乗りし、その合図は何も予約しない。

// testPollInterval はテスト用の通常の間隔である。
//
// 短くしてあるのは、合図のコマンドを実際に走らせて中身を見るためである
// (tea.Batch の中身は走らせないと区別できない)。実際のペインは
// 2〜5 秒で回る。
const testPollInterval = 10 * time.Millisecond

// fakeAttachChecker は list-clients の代役である。
type fakeAttachChecker struct {
	attached bool
	calls    []string
}

func (c *fakeAttachChecker) IsAttached(session string) bool {
	c.calls = append(c.calls, session)
	return c.attached
}

// newPacedPoller は attach の見張り付きの poller を、時刻を固定して返す。
func newPacedPoller(checker *fakeAttachChecker, now *time.Time) poller {
	p := newPoller(testPollInterval).withAttachWatch(AttachWatch{Checker: checker, Session: "s1"})
	p.now = func() time.Time { return *now }
	return p
}

// TestPollerChecksAttachOnArrival は着弾のたびに(頃合いなら)確認を
// 出すことを確かめる。
func TestPollerChecksAttachOnArrival(t *testing.T) {
	t.Parallel()

	base := time.Date(2026, 8, 11, 10, 0, 0, 0, time.UTC)
	now := base
	checker := &fakeAttachChecker{attached: true}
	p := newPacedPoller(checker, &now)

	// 1 回目の着弾: まだ一度も確かめていないので確認が出る。
	cmd := p.arrive(true)
	if cmd == nil {
		t.Fatal("次の合図が張られていません")
	}
	runAttachChecks(t, cmd)
	if len(checker.calls) != 1 {
		t.Fatalf("確認 = %d 回, want 1", len(checker.calls))
	}

	// 30 秒経つ前の着弾では確認を重ねて出さない。
	now = base.Add(29 * time.Second)
	runAttachChecks(t, p.arrive(true))
	if len(checker.calls) != 1 {
		t.Errorf("確認 = %d 回, want 1(30 秒未満は出さない)", len(checker.calls))
	}

	// 30 秒経てばまた確認する。
	now = base.Add(30 * time.Second)
	runAttachChecks(t, p.arrive(true))
	if len(checker.calls) != 2 {
		t.Errorf("確認 = %d 回, want 2", len(checker.calls))
	}
}

// TestPollerSlowsDownWhenDetached は未アタッチが分かったら間隔が
// 落ちること、attach で戻ることを確かめる。
func TestPollerSlowsDownWhenDetached(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 8, 11, 10, 0, 0, 0, time.UTC)
	checker := &fakeAttachChecker{}
	p := newPacedPoller(checker, &now)

	if got := p.pollInterval(); got != testPollInterval {
		t.Errorf("確認前の間隔 = %v, want %v", got, testPollInterval)
	}

	observeNow(&p, false, nil)
	if got := p.pollInterval(); got != app.AttachCheckInterval {
		t.Errorf("未アタッチの間隔 = %v, want %v", got, app.AttachCheckInterval)
	}

	observeNow(&p, true, nil)
	if got := p.pollInterval(); got != testPollInterval {
		t.Errorf("attach 復帰後の間隔 = %v, want %v(即座に通常へ戻る)", got, testPollInterval)
	}
}

// TestPollerKeepsSingleChain は **減速を足してもチェーンが 1 本のまま**
// であることを確かめる。
//
// attach の確認の合図(attachCheckedMsg)は何も予約しない。予約すると
// チェーンが 1 本ずつ増え、この設計が防いでいる「重なり」を自分で作る。
func TestPollerKeepsSingleChain(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 8, 11, 10, 0, 0, 0, time.UTC)
	checker := &fakeAttachChecker{attached: false}
	p := newPacedPoller(checker, &now)

	// 着弾 → 次の合図 1 本 + 確認 1 本。
	msgs, timers := collectMsgs(t, p.arrive(true))
	ticks, checks := countMsgs(msgs, timers)
	if ticks != 1 {
		t.Errorf("合図 = %d 本, want 1 本", ticks)
	}
	if checks != 1 {
		t.Errorf("確認 = %d 本, want 1 本", checks)
	}

	// 確認の結果を取り込んでも、合図は 1 本も増えない。
	before := p.inFlight
	observeNow(&p, false, nil)
	if p.inFlight != before {
		t.Errorf("確認で実行中の数が変わりました: %d → %d", before, p.inFlight)
	}
}

// TestPollerWithoutWatchDoesNotSlowDown は見張りが無い設定
// (zellij の外・--once)で減速も確認も起きないことを確かめる。
func TestPollerWithoutWatchDoesNotSlowDown(t *testing.T) {
	t.Parallel()

	p := newPoller(testPollInterval)
	msgs, timers := collectMsgs(t, p.arrive(true))
	ticks, checks := countMsgs(msgs, timers)
	if ticks != 1 || checks != 0 {
		t.Errorf("合図 = %d 本 / 確認 = %d 本, want 1 / 0", ticks, checks)
	}
	if got := p.pollInterval(); got != testPollInterval {
		t.Errorf("間隔 = %v, want %v", got, testPollInterval)
	}
}

// TestPollerRearmUsesPacedInterval は凍結中の再予約にも減速が効くことを
// 確かめる。ここだけ通常の間隔のままだと、減速したはずのペインが
// 2 秒間隔で回り続ける。
func TestPollerRearmUsesPacedInterval(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 8, 11, 10, 0, 0, 0, time.UTC)
	p := newPacedPoller(&fakeAttachChecker{}, &now)
	observeNow(&p, false, nil)

	if got := p.pollInterval(); got != app.AttachCheckInterval {
		t.Errorf("rearm の間隔 = %v, want %v", got, app.AttachCheckInterval)
	}
	if p.rearm() == nil {
		t.Error("再予約されていません")
	}
}

// collectDeadline は合図が返るのを待つ上限である。
//
// 予約されたタイマーは期限が来るまで返らない。未アタッチ中の合図は 30 秒
// 先なので、実際に待つとテストがその時間だけ止まる。待たずに「タイマーが
// 1 本予約された」と数えるための短い期限である。
const collectDeadline = 200 * time.Millisecond

// runAttachChecks は cmd を実行し、確認のコマンドがあれば走らせる。
func runAttachChecks(t *testing.T, cmd tea.Cmd) {
	t.Helper()
	collectMsgs(t, cmd)
}

// collectMsgs は cmd(tea.Batch を含む)を期限つきで実行し、返った合図と
// 期限内に返らなかったコマンド(= 予約されたタイマー)の数を返す。
func collectMsgs(t *testing.T, cmd tea.Cmd) ([]tea.Msg, int) {
	t.Helper()
	if cmd == nil {
		return nil, 0
	}

	// tea.Batch そのものは即座に BatchMsg を返す。
	first := make(chan tea.Msg, 1)
	go func() { first <- cmd() }()
	var msg tea.Msg
	select {
	case msg = <-first:
	case <-time.After(collectDeadline):
		// 単独のタイマーだった。
		return nil, 1
	}

	batch, ok := msg.(tea.BatchMsg)
	if !ok {
		return []tea.Msg{msg}, 0
	}

	results := make(chan tea.Msg, len(batch))
	pending := 0
	for _, inner := range batch {
		if inner == nil {
			continue
		}
		pending++
		go func(c tea.Cmd) { results <- c() }(inner)
	}

	var msgs []tea.Msg
	timers := 0
	deadline := time.After(collectDeadline)
	for range pending {
		select {
		case got := <-results:
			msgs = append(msgs, got)
		case <-deadline:
			// 残りはすべて期限内に返らなかった = タイマーである。
			return msgs, pending - len(msgs)
		}
	}
	return msgs, timers
}

// countMsgs は合図の種類ごとの数を返す。
// timers は「返らなかった = 予約された合図」で、tick として数える。
func countMsgs(msgs []tea.Msg, timers int) (ticks, checks int) {
	ticks = timers
	for _, msg := range msgs {
		switch msg.(type) {
		case tickMsg:
			ticks++
		case attachCheckedMsg:
			checks++
		}
	}
	return ticks, checks
}

// countingRefresh は読み直しの発行回数を数える。
type countingRefresh struct {
	calls []bool // poll の値を順に記録する
}

func (r *countingRefresh) cmd(poll bool) tea.Cmd {
	r.calls = append(r.calls, poll)
	return func() tea.Msg { return tickMsg{} }
}

// TestPollerStopsRefreshingWhenDetached は未アタッチ中に **読み直しを
// 一切出さない** ことを確かめる。
//
// 誰も見ていない画面を描き直しても意味が無く、Dashboard の読み直しは
// zellij の CLI を 2 回叩く一番重い処理である。止めれば放置された
// セッションはほぼ無害になる。
func TestPollerStopsRefreshingWhenDetached(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 8, 11, 10, 0, 0, 0, time.UTC)
	checker := &fakeAttachChecker{attached: false}
	p := newPacedPoller(checker, &now)
	observeNow(&p, false, nil)

	// 生成直後の 1 本を着弾させ、「読み直しを出せる状態」にしておく。
	// ここを 1 のままにすると、減速ではなく「重なり防止」で読み直しが
	// 出ないだけになり、テストが素通りする。
	p.inFlight = 0

	refresh := &countingRefresh{}
	for i := range 3 {
		now = now.Add(app.AttachCheckInterval)
		cmd := p.tick(refresh.cmd)
		if cmd == nil {
			t.Fatalf("%d 回目: 次の合図が張られていません", i+1)
		}
		msgs, timers := collectMsgs(t, cmd)
		ticks, checks := countMsgs(msgs, timers)
		if ticks != 1 {
			t.Errorf("%d 回目: 合図 = %d 本, want 1 本", i+1, ticks)
		}
		// 読み直しの代わりに attach の確認だけを出す。
		if checks != 1 {
			t.Errorf("%d 回目: 確認 = %d 本, want 1 本", i+1, checks)
		}
	}
	if len(refresh.calls) != 0 {
		t.Errorf("未アタッチ中に読み直しを出しました: %v", refresh.calls)
	}
	if p.inFlight != 0 {
		// 読み直しを出していないので実行中は増えない。
		t.Errorf("実行中 = %d, want 0", p.inFlight)
	}
}

// TestPollerRecoversOnAttach は attach を検知したら読み直しを 1 本出して
// 通常の速さへ戻ることを確かめる。
//
// 出す読み直しは poll=false である。着弾しても次の合図を張らないので、
// 止まっていたチェーン(30 秒の合図)と合わせて 1 本のままになる。
func TestPollerRecoversOnAttach(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 8, 11, 10, 0, 0, 0, time.UTC)
	p := newPacedPoller(&fakeAttachChecker{}, &now)
	observeNow(&p, false, nil)

	refresh := &countingRefresh{}
	cmd := observeNow(&p, true, refresh.cmd)
	if cmd == nil {
		t.Fatal("復帰の読み直しが出ていません")
	}
	if want := []bool{false}; len(refresh.calls) != 1 || refresh.calls[0] != want[0] {
		t.Errorf("読み直し = %v, want %v(poll=false = チェーンの外)", refresh.calls, want)
	}
	if got := p.pollInterval(); got != testPollInterval {
		t.Errorf("復帰後の間隔 = %v, want %v", got, testPollInterval)
	}
	// 復帰の読み直しは着弾しても何も予約しない。
	if next := p.arrive(false); next != nil {
		t.Error("復帰の読み直しの着弾で合図が張られました(チェーンが増える)")
	}
}

// TestPollerObserveAttachIsIdempotent は既にアタッチ済みと分かっている
// ときに読み直しを重ねて出さないことを確かめる。
func TestPollerObserveAttachIsIdempotent(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 8, 11, 10, 0, 0, 0, time.UTC)
	p := newPacedPoller(&fakeAttachChecker{attached: true}, &now)

	refresh := &countingRefresh{}
	if cmd := observeNow(&p, true, refresh.cmd); cmd != nil {
		t.Error("減速していないのに復帰の読み直しを出しました")
	}
	if len(refresh.calls) != 0 {
		t.Errorf("読み直し = %v, want 空", refresh.calls)
	}
}

// TestPollerForceCountsAsAttached はキー操作を attach の証拠として扱う
// ことを確かめる(A-1)。
//
// キー操作が来たということは、誰かがその画面を開いて触っている。
// 確認を待たずに通常の速さへ戻してよい。
func TestPollerForceCountsAsAttached(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 8, 11, 10, 0, 0, 0, time.UTC)
	p := newPacedPoller(&fakeAttachChecker{}, &now)
	observeNow(&p, false, nil)
	if got := p.pollInterval(); got != app.AttachCheckInterval {
		t.Fatalf("減速していません: %v", got)
	}

	p.force()

	if got := p.pollInterval(); got != testPollInterval {
		t.Errorf("キー操作後の間隔 = %v, want %v(即座に通常へ)", got, testPollInterval)
	}
	// 読み直しも通常どおり出るようになる。
	refresh := &countingRefresh{}
	p.inFlight = 0
	if cmd := p.tick(refresh.cmd); cmd == nil {
		t.Error("読み直しが出ていません")
	}
	if len(refresh.calls) != 1 {
		t.Errorf("読み直し = %v, want 1 本", refresh.calls)
	}
}

// observeNow は「今出ている確認の結果が返った」ことを模す。
//
// 実際の確認は armWithAttachCheck が番号を進めてから出す。テストでは
// その番号をそのまま使い、古い結果として捨てられないようにする。
func observeNow(p *poller, attached bool, refresh func(poll bool) tea.Cmd) tea.Cmd {
	return p.observeAttach(attachCheckedMsg{attached: attached, token: p.attachToken}, refresh)
}

// TestPollerIgnoresStaleAttachResult は古い確認の結果を捨てることを
// 確かめる(指摘 8)。
//
// キー操作で通常へ戻した直後に、それ以前に出した「誰も居ない」が遅れて
// 着くことがある。そのまま取り込むと、触っているのに画面がまた止まる。
func TestPollerIgnoresStaleAttachResult(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 8, 12, 10, 0, 0, 0, time.UTC)
	p := newPacedPoller(&fakeAttachChecker{attached: false}, &now)

	// 確認を 1 本出す(番号が進む)。
	runAttachChecks(t, p.arrive(true))
	stale := p.attachToken

	// 結果が返る前にキーを押した = 誰かが開いている。
	p.force()
	if p.pace.Detached() {
		t.Fatal("キー操作で減速が解けていません")
	}

	// ここで古い「誰も居ない」が着く。
	if cmd := p.observeAttach(attachCheckedMsg{attached: false, token: stale}, nil); cmd != nil {
		t.Error("古い結果で読み直しを出しました")
	}
	if p.pace.Detached() {
		t.Error("古い結果で再び減速しました(触っているのに画面が止まる)")
	}
	if got := p.pollInterval(); got != testPollInterval {
		t.Errorf("間隔 = %v, want %v", got, testPollInterval)
	}
}

// TestPollerAcceptsLatestAttachResult は最新の確認の結果は取り込むことを
// 確かめる(古い結果を捨てる仕組みが、正しい結果まで捨てていないこと)。
func TestPollerAcceptsLatestAttachResult(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 8, 12, 10, 0, 0, 0, time.UTC)
	p := newPacedPoller(&fakeAttachChecker{attached: false}, &now)

	runAttachChecks(t, p.arrive(true))
	if cmd := p.observeAttach(attachCheckedMsg{attached: false, token: p.attachToken}, nil); cmd != nil {
		t.Error("減速へ入るときに読み直しを出しました")
	}
	if !p.pace.Detached() {
		t.Error("最新の結果が取り込まれていません")
	}
}

// TestPollingPanesWatchAttach はポーリングで回るペインすべてに attach の
// 見張りが配線されていることを確かめる(指摘 6)。
//
// task-control はタブごとに 1 つ常駐して 2 秒で回るため、放置された
// セッションではタブの数だけ積み上がる。ここが漏れると減速の意味が薄れる。
func TestPollingPanesWatchAttach(t *testing.T) {
	t.Parallel()

	panes := Panes{
		Dashboard:   &tickDashboard{},
		Waiting:     &tickWaiting{},
		Done:        &tickDone{},
		News:        &tickNews{},
		TaskCreate:  tickTaskCreate{},
		TaskControl: tickTaskControl{},
		Env:         tickEnv,
		Attach:      AttachWatch{Checker: &fakeAttachChecker{}, Session: "s1"},
	}

	// ポーリングを持つペインはすべて見張りを受け取る。task-create だけは
	// ポーリングを持たないので対象外である。
	tests := []struct {
		name string
		get  func(tea.Model) (poller, bool)
	}{
		{name: NameDashboard, get: func(m tea.Model) (poller, bool) {
			v, ok := m.(DashboardModel)
			return v.polling, ok
		}},
		{name: NameWaiting, get: func(m tea.Model) (poller, bool) {
			v, ok := m.(WaitingModel)
			return v.polling, ok
		}},
		{name: NameDone, get: func(m tea.Model) (poller, bool) {
			v, ok := m.(DoneModel)
			return v.polling, ok
		}},
		{name: NameNews, get: func(m tea.Model) (poller, bool) {
			v, ok := m.(NewsModel)
			return v.polling, ok
		}},
		{name: NameTaskControl, get: func(m tea.Model) (poller, bool) {
			v, ok := m.(TaskControlModel)
			return v.polling, ok
		}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			model, err := panes.model(tt.name, "tab")
			if err != nil {
				t.Fatalf("model(%q) = %v", tt.name, err)
			}
			p, ok := tt.get(model)
			if !ok {
				t.Fatalf("%s のモデルの型が違います: %T", tt.name, model)
			}
			if p.watch.Checker == nil || p.watch.Session == "" {
				t.Errorf("%s に attach の見張りが配線されていません", tt.name)
			}
		})
	}
}

// tickTaskCreate / tickTaskControl は配線の確認だけに使う代役である。
// 呼ばれない前提なのでゼロ値を返す。
type tickTaskCreate struct{}

func (tickTaskCreate) Menu(app.PaneEnv) string           { return "" }
func (tickTaskCreate) Directories() ([]string, bool)     { return nil, false }
func (tickTaskCreate) TaskTypes() []app.TaskTypeChoice   { return nil }
func (tickTaskCreate) Agents() []string                  { return nil }
func (tickTaskCreate) SkipNameInput() bool               { return false }
func (tickTaskCreate) DefaultName(string, string) string { return "" }
func (tickTaskCreate) ResolveName(string, string) string { return "" }
func (tickTaskCreate) UniqueName(base string) string     { return base }
func (tickTaskCreate) Create(app.PaneEnv, string, string, string, string) (string, error) {
	return "", nil
}

type tickTaskControl struct{}

func (tickTaskControl) Refresh(app.PaneEnv, string) (string, error) { return "", nil }
func (tickTaskControl) GoToMain() error                             { return nil }
func (tickTaskControl) ToggleWaiting(app.PaneEnv, string) error     { return nil }
func (tickTaskControl) PrepareDelete(app.PaneEnv, string) (app.DeletePreparation, error) {
	return app.DeletePreparation{}, nil
}
func (tickTaskControl) CommitDelete(app.PaneEnv, string) error { return nil }
