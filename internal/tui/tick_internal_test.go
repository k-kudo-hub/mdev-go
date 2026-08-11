package tui

import (
	"errors"
	"reflect"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/k-kudo-hub/mdev-go/internal/app"
)

// ポーリングの合図(tickMsg)は非公開型なので、外部テスト(tui_test.go)から
// 流し込めない。「2 打鍵目を待っている間はポーリングを止める」「前回の
// 読み直しが終わるまで次を出さない」という取り決めはここで内部から確かめる。
// 表示とキー操作の検証は外部テストが持つ。

var tickEnv = app.PaneEnv{ZellijSession: "s1"}

// errTickRefresh は読み直しが失敗した状況を表す。
var errTickRefresh = errors.New("読み直しに失敗した")

// ---- 最小限のスタブ -------------------------------------------------------

type tickDashboard struct {
	snapshot  app.DashboardSnapshot
	refreshes int
	// calls は Startup と Refresh の呼び出し順。
	calls []string
}

var _ DashboardService = (*tickDashboard)(nil)

func (s *tickDashboard) Startup(app.PaneEnv) { s.calls = append(s.calls, "startup") }

func (s *tickDashboard) Refresh(app.PaneEnv) (app.DashboardSnapshot, error) {
	s.refreshes++
	s.calls = append(s.calls, "refresh")
	return s.snapshot, nil
}

func (s *tickDashboard) Jump(app.PaneEnv, app.DashboardSnapshot, int) error { return nil }

func (s *tickDashboard) PrepareDelete(app.PaneEnv, string) (app.DeletePreparation, error) {
	return app.DeletePreparation{}, nil
}

func (s *tickDashboard) CommitDelete(app.PaneEnv, string) error { return nil }

type tickDone struct {
	snapshot  app.DoneSnapshot
	refreshes int
}

var _ DoneService = (*tickDone)(nil)

func (s *tickDone) Refresh() app.DoneSnapshot {
	s.refreshes++
	return s.snapshot
}

func (s *tickDone) Restore(app.DoneSnapshot, int) {}

type tickWaiting struct {
	text      string
	refreshes int
}

var _ WaitingService = (*tickWaiting)(nil)

func (s *tickWaiting) Refresh(app.PaneEnv) (string, error) {
	s.refreshes++
	return s.text, nil
}

type tickNews struct {
	snapshot  app.NewsSnapshot
	refreshes int
}

var _ NewsService = (*tickNews)(nil)

func (s *tickNews) Refresh() app.NewsSnapshot {
	s.refreshes++
	return s.snapshot
}

func (s *tickNews) Reload()                    {}
func (s *tickNews) Open(app.NewsSnapshot, int) {}

// ---- ヘルパ ---------------------------------------------------------------

// immediate はコマンドを別 goroutine で実行し、すぐに返ってきたメッセージを
// 返す。返ってこなければ nil を返す。
//
// 読み直しのコマンドと tea.Batch はその場でメッセージを返すのに対し、
// ポーリングのタイマー(tea.Tick)は間隔ぶん待たないと返らない。この差で
// 「予約されたのがタイマーだけかどうか」を実時間を待たずに判別する。
func immediate(cmd tea.Cmd) tea.Msg {
	if cmd == nil {
		return nil
	}
	got := make(chan tea.Msg, 1)
	go func() { got <- cmd() }()
	select {
	case msg := <-got:
		return msg
	case <-time.After(100 * time.Millisecond):
		return nil
	}
}

// tickKey は 1 文字のキー押下メッセージを作る。
func tickKey(r rune) tea.KeyPressMsg { return tea.KeyPressMsg{Code: r, Text: string(r)} }

// update はモデルにメッセージを 1 つ渡し、進んだモデルとコマンドを返す。
func update(t *testing.T, m tea.Model, msg tea.Msg) (tea.Model, tea.Cmd) {
	t.Helper()
	next, cmd := m.Update(msg)
	if next == nil {
		t.Fatal("Update がモデルを返していない")
	}
	return next, cmd
}

// wantTimerOnly は予約されたのが次の合図のタイマーだけであることを確かめる。
//
// 読み直しのコマンドはその場でメッセージを返すのに対し、タイマー(tea.Tick)は
// 間隔ぶん待たないと返らない。この差で判別する。
func wantTimerOnly(t *testing.T, cmd tea.Cmd) {
	t.Helper()
	if cmd == nil {
		t.Fatal("ポーリングのチェーンが切れている")
	}
	if msg := immediate(cmd); msg != nil {
		t.Errorf("タイマー以外に %T を予約している", msg)
	}
}

// wantRefreshOnly は予約されたのが読み直しだけであることを確かめ、その着弾
// メッセージを返す。
//
// 次の合図を一緒に張っていれば tea.Batch がその場で tea.BatchMsg を返すため、
// 「発行と同時にタイマーを張っていない」ことまでここで見える。
func wantRefreshOnly(t *testing.T, cmd tea.Cmd) tea.Msg {
	t.Helper()
	if cmd == nil {
		t.Fatal("読み直しを発行していない")
	}
	msg := immediate(cmd)
	if msg == nil {
		t.Fatal("読み直しではなくタイマーを予約している")
	}
	if batch, ok := msg.(tea.BatchMsg); ok {
		t.Fatalf("読み直しと一緒に %d 個のコマンドを束ねている(次の合図は着弾で張る)", len(batch))
	}
	return msg
}

// wantNoContinuation は何も予約していないことを確かめる。
func wantNoContinuation(t *testing.T, cmd tea.Cmd) {
	t.Helper()
	if cmd != nil {
		t.Errorf("予約すべきでない場面で %T を予約している", immediate(cmd))
	}
}

// ---- 完了起点のポーリング(4 ペイン共通)---------------------------------

// 不変条件: ポーリングのチェーンは常にちょうど 1 本である。
// 「未着弾のポーリング読み直し」と「予約済みのタイマー」のどちらか一方だけが
// 存在し、着弾と発火で互いに入れ替わる。
//
//	Init ─→ [読み直し] ─着弾→ [タイマー] ─発火→ [読み直し] ─着弾→ …
//
// これで 1 周期が「読み直しにかかった時間 + 間隔」になり、現行 Shell 版の
// 「処理 → sleep」と同じ自己抑制が働く。キー操作で出した読み直し(force 起源)は
// チェーンの一部ではなく、着弾しても何も予約しない。

// chainCase は 4 ペインを同じ形で回すためのテーブルの 1 行である。
type chainCase struct {
	name string
	// model は生成直後のモデル(Init が最初の読み直しを発行した状態にあたる)。
	model tea.Model
	// landed は着弾メッセージを作る。poll はポーリング起源かどうか。
	landed func(poll bool) tea.Msg
	// refreshes はユースケースが読み直された回数を返す。
	refreshes func() int
}

func chainCases() []chainCase {
	dashboard := &tickDashboard{snapshot: app.DashboardSnapshot{Text: "画面"}}
	waiting := &tickWaiting{text: "待ち画面"}
	done := &tickDone{snapshot: app.DoneSnapshot{Text: "完了画面"}}
	news := &tickNews{snapshot: app.NewsSnapshot{Text: "ニュース画面"}}

	return []chainCase{
		{
			name:      "dashboard",
			model:     NewDashboardModel(dashboard, tickEnv),
			landed:    func(poll bool) tea.Msg { return dashboardRefreshedMsg{poll: poll} },
			refreshes: func() int { return dashboard.refreshes },
		},
		{
			name:      "waiting",
			model:     NewWaitingModel(waiting, tickEnv),
			landed:    func(poll bool) tea.Msg { return waitingRefreshedMsg{poll: poll} },
			refreshes: func() int { return waiting.refreshes },
		},
		{
			name:      "done",
			model:     NewDoneModel(done),
			landed:    func(poll bool) tea.Msg { return doneRefreshedMsg{poll: poll} },
			refreshes: func() int { return done.refreshes },
		},
		{
			name:      "news",
			model:     NewNewsModel(news),
			landed:    func(poll bool) tea.Msg { return newsRefreshedMsg{poll: poll} },
			refreshes: func() int { return news.refreshes },
		},
	}
}

func TestPaneInitStartsChainWithRefreshOnly(t *testing.T) {
	t.Parallel()

	for _, tt := range chainCases() {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			// Init が張るのは最初の読み直しだけである。ここでタイマーも
			// 一緒に張るとチェーンが 2 本になり、以後ずっと 2 本で回る。
			wantRefreshOnly(t, tt.model.Init())
			if tt.refreshes() != 1 {
				t.Errorf("読み直しの回数 = %d, want 1", tt.refreshes())
			}
		})
	}
}

func TestPaneChainAlternatesRefreshAndTimer(t *testing.T) {
	t.Parallel()

	for _, tt := range chainCases() {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			m := tt.model
			// 2 周ぶん回して、毎回ちょうど 1 本だけが次へ渡ることを見る。
			for round := 1; round <= 2; round++ {
				// 着弾 → 次の合図(タイマー)だけを張る。
				next, cmd := update(t, m, tt.landed(true))
				wantTimerOnly(t, cmd)

				// タイマーの発火 → 読み直しだけを発行する(合図は張らない)。
				after, cmd := update(t, next, tickMsg{})
				wantRefreshOnly(t, cmd)
				if tt.refreshes() != round {
					t.Fatalf("%d 周目の読み直しの回数 = %d, want %d", round, tt.refreshes(), round)
				}
				m = after
			}
		})
	}
}

func TestPaneForcedRefreshDoesNotRearmPolling(t *testing.T) {
	t.Parallel()

	for _, tt := range chainCases() {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			// キー操作で出した読み直しの着弾は、何度来てもチェーンを増やさない。
			// ここで合図を張ると、押した回数だけポーリングが増殖する。
			m := tt.model
			for i := range 3 {
				next, cmd := update(t, m, tt.landed(false))
				if cmd != nil {
					t.Fatalf("%d 回目の force 起源の着弾がポーリングを張り直している", i+1)
				}
				m = next
			}
		})
	}
}

func TestPaneTickSkipsRefreshWhileInFlight(t *testing.T) {
	t.Parallel()

	for _, tt := range chainCases() {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			// 生成直後は Init が出した読み直しが走っている。着弾する前に
			// tick が来ても重ねて出さず、次の合図だけを予約する。
			// (完了起点なのでこの状況ではタイマーが存在せず、通常は tick 自体
			// 来ない。キー操作の読み直しが走っている場合への備えである)
			m, cmd := update(t, tt.model, tickMsg{})
			wantTimerOnly(t, cmd)
			if tt.refreshes() != 0 {
				t.Fatalf("実行中の読み直しに重ねている: %d 回", tt.refreshes())
			}

			// 着弾したら、次の tick は通常どおり読み直しを発行する
			// (印の下ろし忘れが無い)。
			m, _ = update(t, m, tt.landed(true))
			_, cmd = update(t, m, tickMsg{})
			wantRefreshOnly(t, cmd)
			if tt.refreshes() != 1 {
				t.Errorf("読み直しの回数 = %d, want 1", tt.refreshes())
			}
		})
	}
}

// forceCase はキー操作起源の読み直しを起こせるペインのテーブルの 1 行である。
// Waiting はキー入力を受け付けないためこの経路を持たない。
type forceCase struct {
	name  string
	model tea.Model
	// trigger はキー操作起源の読み直しを発行させるメッセージ。
	trigger tea.Msg
	// landed は着弾メッセージを作る。
	landed    func(poll bool) tea.Msg
	refreshes func() int
}

func forceCases() []forceCase {
	dashboard := &tickDashboard{snapshot: app.DashboardSnapshot{Text: "画面"}}
	done := &tickDone{snapshot: app.DoneSnapshot{Text: "完了画面"}}
	news := &tickNews{snapshot: app.NewsSnapshot{Text: "ニュース画面"}}

	return []forceCase{
		{
			// 2 打鍵目の待ち受けが時間切れになると、止めていた間の変化に
			// 追いつくために読み直す。
			name:      "dashboard",
			model:     NewDashboardModel(dashboard, tickEnv),
			trigger:   promptExpiredMsg{},
			landed:    func(poll bool) tea.Msg { return dashboardRefreshedMsg{poll: poll} },
			refreshes: func() int { return dashboard.refreshes },
		},
		{
			name:      "done",
			model:     NewDoneModel(done),
			trigger:   promptExpiredMsg{},
			landed:    func(poll bool) tea.Msg { return doneRefreshedMsg{poll: poll} },
			refreshes: func() int { return done.refreshes },
		},
		{
			// r を押した後の取得。取得が終わったら読み直して通常の画面へ戻る。
			name:      "news",
			model:     NewNewsModel(news),
			trigger:   newsReloadMsg{},
			landed:    func(poll bool) tea.Msg { return newsRefreshedMsg{poll: poll} },
			refreshes: func() int { return news.refreshes },
		},
	}
}

func TestPaneTickSkipsRefreshWhileForcedRefreshInFlight(t *testing.T) {
	t.Parallel()

	for _, tt := range forceCases() {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			// 起動時の読み直しが着弾し、チェーンはタイマーになっている。
			m, cmd := update(t, tt.model, tt.landed(true))
			wantTimerOnly(t, cmd)

			// キー操作起源の読み直しが走り出す。着弾しても合図は張らない
			// 約束なので、この間チェーンはタイマーのまま 1 本である。
			m, cmd = update(t, m, tt.trigger)
			wantRefreshOnly(t, cmd)
			if tt.refreshes() != 1 {
				t.Fatalf("キー操作の読み直しの回数 = %d, want 1", tt.refreshes())
			}

			// その着弾前にタイマーが発火した。読み直しを重ねず、次の合図
			// だけを予約し直す。
			m, cmd = update(t, m, tickMsg{})
			wantTimerOnly(t, cmd)
			if tt.refreshes() != 1 {
				t.Fatalf("読み直しが重なった: %d 回", tt.refreshes())
			}

			// キー操作の読み直しが着弾する。ここで合図を張るとチェーンが
			// 2 本になる。
			m, cmd = update(t, m, tt.landed(false))
			wantNoContinuation(t, cmd)

			// 印は下りているので、次のタイマーの発火では読み直しを発行する。
			_, cmd = update(t, m, tickMsg{})
			wantRefreshOnly(t, cmd)
			if tt.refreshes() != 2 {
				t.Errorf("読み直しの回数 = %d, want 2", tt.refreshes())
			}
		})
	}
}

func TestPaneTickWaitsForForcedRefreshAfterPollArrives(t *testing.T) {
	t.Parallel()

	// キー操作の読み直しとポーリングの読み直しが同時に走り、**ポーリングの
	// ほうが先に着弾する**交錯ケース。実行中かどうかを真偽値で持つと、先に
	// 着弾したポーリングが印を下ろしてしまい、まだ走っているキー操作の
	// 読み直しへ次のポーリングが重なる(CLI が 2 本並行する)。本数で数えて
	// いるのでそうならないことを固定する。
	for _, tt := range forceCases() {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			// 起動時の読み直しが着弾 → チェーンはタイマー。
			m, cmd := update(t, tt.model, tt.landed(true))
			wantTimerOnly(t, cmd)

			// タイマーが発火してポーリングの読み直しが走り出す(1 本目)。
			m, cmd = update(t, m, tickMsg{})
			wantRefreshOnly(t, cmd)

			// その最中にキー操作の読み直しも走り出す(2 本目)。
			m, cmd = update(t, m, tt.trigger)
			wantRefreshOnly(t, cmd)
			if tt.refreshes() != 2 {
				t.Fatalf("読み直しの回数 = %d, want 2", tt.refreshes())
			}

			// 先にポーリングのほうが着弾する。次の合図は張られるが、キー操作の
			// 読み直しはまだ走っている。
			m, cmd = update(t, m, tt.landed(true))
			wantTimerOnly(t, cmd)

			// その合図で読み直しを出してはいけない(まだ 1 本走っている)。
			m, cmd = update(t, m, tickMsg{})
			wantTimerOnly(t, cmd)
			if tt.refreshes() != 2 {
				t.Fatalf("走っている読み直しに重ねた: %d 回", tt.refreshes())
			}

			// キー操作のほうが着弾して初めて 0 本になる。
			m, cmd = update(t, m, tt.landed(false))
			wantNoContinuation(t, cmd)

			_, cmd = update(t, m, tickMsg{})
			wantRefreshOnly(t, cmd)
			if tt.refreshes() != 3 {
				t.Errorf("読み直しの回数 = %d, want 3", tt.refreshes())
			}
		})
	}
}

func TestPaneRefreshErrorReleasesInFlightAndRearms(t *testing.T) {
	t.Parallel()

	// エラーで返ってきた着弾でも、実行中の数を減らして次の合図を張る。
	// どちらかを忘れると、一度失敗しただけでポーリングが二度と回らなくなる。
	tests := []struct {
		name   string
		model  tea.Model
		failed tea.Msg
	}{
		{
			name:   "dashboard",
			model:  NewDashboardModel(&tickDashboard{}, tickEnv),
			failed: dashboardRefreshedMsg{err: errTickRefresh, poll: true},
		},
		{
			name:   "waiting",
			model:  NewWaitingModel(&tickWaiting{}, tickEnv),
			failed: waitingRefreshedMsg{err: errTickRefresh, poll: true},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			m, cmd := update(t, tt.model, tt.failed)
			wantTimerOnly(t, cmd)

			_, cmd = update(t, m, tickMsg{})
			wantRefreshOnly(t, cmd)
		})
	}
}

// ---- Dashboard ------------------------------------------------------------

// settledDashboard は起動時の読み直しが着弾済みの Dashboard を返す
// (= 逐次化の印が下りていて、tick が読み直しを発行できる状態)。
func settledDashboard(t *testing.T, service DashboardService) DashboardModel {
	t.Helper()
	next, _ := NewDashboardModel(service, tickEnv).Update(dashboardRefreshedMsg{})
	m, ok := next.(DashboardModel)
	if !ok {
		t.Fatalf("モデルの型 = %T", next)
	}
	return m
}

func TestDashboardInitProceedsToRefreshAfterStartup(t *testing.T) {
	t.Parallel()

	// Init は起動時の復元 → 最初の読み直し、の順に進む。復元(restore-session.sh)は
	// 上限で切られることがある(internal/infra/shell の restoreSessionTimeout)が、
	// 切られても Startup は戻るので読み直しには必ず進み、その着弾が
	// ポーリングのチェーンを張る。
	service := &tickDashboard{snapshot: app.DashboardSnapshot{Text: "画面"}}
	m := NewDashboardModel(service, tickEnv)

	landed, ok := wantRefreshOnly(t, m.Init()).(dashboardRefreshedMsg)
	if !ok {
		t.Fatal("Init が読み直しを返していない")
	}
	if !landed.poll {
		t.Error("ポーリング起源として発行していない")
	}
	if want := []string{"startup", "refresh"}; !reflect.DeepEqual(service.calls, want) {
		t.Errorf("呼び出し = %v, want %v", service.calls, want)
	}

	// その着弾が次の合図を張り、ポーリングが回り始める。
	_, cmd := update(t, m, landed)
	wantTimerOnly(t, cmd)
}

func TestDashboardTickRefreshesAndRearms(t *testing.T) {
	t.Parallel()

	service := &tickDashboard{snapshot: app.DashboardSnapshot{Text: "画面"}}
	m := settledDashboard(t, service)

	// tick が出すのは読み直しだけで、次の合図はその着弾で張る。
	_, cmd := m.Update(tickMsg{})
	landed, ok := wantRefreshOnly(t, cmd).(dashboardRefreshedMsg)
	if !ok {
		t.Fatal("発行したのが読み直しではない")
	}
	if !landed.poll {
		t.Error("ポーリング起源として発行していない")
	}
	if service.refreshes != 1 {
		t.Errorf("読み直しの回数 = %d, want 1", service.refreshes)
	}

	// その着弾が次の合図を張る。
	_, cmd = m.Update(landed)
	wantTimerOnly(t, cmd)
}

func TestDashboardIgnoresPromptTimeoutAfterNumberKey(t *testing.T) {
	t.Parallel()

	// d を押した時点で 3 秒の打ち切りタイマーが仕掛かる。2 打鍵目を押して削除へ
	// 進んだ後にそれが発火しても、待ち受けはもう終わっているので無視しなければ
	// ならない。無視しないと、削除の処理中に余計な読み直しが 1 本走る。
	service := &tickDashboard{snapshot: app.DashboardSnapshot{Text: "画面", Tabs: []string{"alpha"}}}
	m := settledDashboard(t, service)
	m.snapshot = service.snapshot

	prompted, cmd := update(t, m, tickKey('d'))
	if cmd == nil {
		t.Fatal("待ち受けに入っていない")
	}
	// 2 打鍵目。削除の準備(record と upload)へ進む。
	deleting, cmd := update(t, prompted, tickKey('1'))
	if cmd == nil {
		t.Fatal("削除の準備に進んでいない")
	}

	// d を押したときに仕掛けた世代(1)の打ち切りが遅れて着弾する。
	_, cmd = update(t, deleting, promptExpiredMsg{token: 1})
	if cmd != nil {
		t.Errorf("削除の途中に %T を予約している", immediate(cmd))
	}
	if service.refreshes != 0 {
		t.Errorf("削除の途中に読み直している: %d 回", service.refreshes)
	}
}

func TestDoneIgnoresPromptTimeoutAfterNumberKey(t *testing.T) {
	t.Parallel()

	// Dashboard と同じ。restore へ進んだ後に古い打ち切りが発火しても集計しない。
	service := &tickDone{snapshot: app.DoneSnapshot{Text: "完了画面", Count: 1}}
	m := settledDone(t, service)
	m.snapshot = service.snapshot

	prompted, cmd := update(t, m, tickKey('r'))
	if cmd == nil {
		t.Fatal("待ち受けに入っていない")
	}
	restoring, cmd := update(t, prompted, tickKey('1'))
	if cmd == nil {
		t.Fatal("restore に進んでいない")
	}

	_, cmd = update(t, restoring, promptExpiredMsg{token: 1})
	if cmd != nil {
		t.Errorf("restore の直後に %T を予約している", immediate(cmd))
	}
	if service.refreshes != 0 {
		t.Errorf("restore の直後に集計している: %d 回", service.refreshes)
	}
}

func TestDashboardTickIsFrozenWhileAwaiting(t *testing.T) {
	t.Parallel()

	service := &tickDashboard{snapshot: app.DashboardSnapshot{Text: "画面", Tabs: []string{"alpha"}}}
	m := settledDashboard(t, service)
	m.awaiting = true

	next, cmd := m.Update(tickMsg{})
	if cmd == nil {
		t.Fatal("ポーリングが止まっている")
	}
	// 予約されたのは次の合図のタイマーだけである。読み直しを束ねていれば
	// その場でメッセージが返ってしまう。
	if msg := immediate(cmd); msg != nil {
		t.Errorf("待ち受け中に %T を予約している", msg)
	}
	if service.refreshes != 0 {
		t.Errorf("待ち受け中に読み直している: %d 回", service.refreshes)
	}

	// 凍結中の tick は読み直しを発行しないので、逐次化の印を立ててもいけない。
	// 立ててしまうと、凍結が解けた後の tick が二度と読み直せなくなる。
	after, ok := next.(DashboardModel)
	if !ok {
		t.Fatalf("モデルの型 = %T", next)
	}
	after.awaiting = false
	_, cmd = after.Update(tickMsg{})
	wantRefreshOnly(t, cmd)
	if service.refreshes != 1 {
		t.Errorf("凍結が解けた後の読み直しの回数 = %d, want 1", service.refreshes)
	}
}

func TestDashboardTickIsFrozenWhileBusy(t *testing.T) {
	t.Parallel()

	service := &tickDashboard{snapshot: app.DashboardSnapshot{Text: "画面"}}
	m := settledDashboard(t, service)
	m.busy = true

	next, cmd := m.Update(tickMsg{})
	if cmd == nil {
		t.Fatal("ポーリングが止まっている")
	}
	if msg := immediate(cmd); msg != nil {
		t.Errorf("削除の途中に %T を予約している", msg)
	}

	// 削除の途中の tick も逐次化の印を立てない(awaiting と同じ理由)。
	after, ok := next.(DashboardModel)
	if !ok {
		t.Fatalf("モデルの型 = %T", next)
	}
	after.busy = false
	_, cmd = after.Update(tickMsg{})
	wantRefreshOnly(t, cmd)
	if service.refreshes != 1 {
		t.Errorf("削除の後の読み直しの回数 = %d, want 1", service.refreshes)
	}
}

func TestDashboardDroppedRefreshReleasesInFlight(t *testing.T) {
	t.Parallel()

	// 待ち受け中に着弾した読み直しは捨てる(番号の対応を変えないため)が、
	// 逐次化の印は下ろす。捨てたぶんを数えたままにすると、待ち受けが解けた
	// 後のポーリングが読み直せなくなる。
	service := &tickDashboard{snapshot: app.DashboardSnapshot{Text: "画面"}}
	m := NewDashboardModel(service, tickEnv)
	m.awaiting = true

	next, _ := m.Update(dashboardRefreshedMsg{snapshot: app.DashboardSnapshot{Text: "新しい画面"}})
	after, ok := next.(DashboardModel)
	if !ok {
		t.Fatalf("モデルの型 = %T", next)
	}
	after.awaiting = false

	_, cmd := after.Update(tickMsg{})
	wantRefreshOnly(t, cmd)
	if service.refreshes != 1 {
		t.Errorf("読み直しの回数 = %d, want 1", service.refreshes)
	}
}

func TestDashboardRefreshIsDroppedWhileAwaiting(t *testing.T) {
	t.Parallel()

	// 待ち受けに入る前に発行された読み直しが遅れて着弾しても、番号の対応が
	// 変わってはいけない。
	service := &tickDashboard{}
	m := NewDashboardModel(service, tickEnv)
	m.snapshot = app.DashboardSnapshot{Text: "元の画面", Tabs: []string{"alpha", "beta"}}
	m.awaiting = true

	next, cmd := m.Update(dashboardRefreshedMsg{
		snapshot: app.DashboardSnapshot{Text: "新しい画面", Tabs: []string{"gamma"}},
	})
	if cmd != nil {
		t.Error("在庫の読み直しがコマンドを返している")
	}
	after, ok := next.(DashboardModel)
	if !ok {
		t.Fatalf("モデルの型 = %T", next)
	}
	if len(after.snapshot.Tabs) != 2 || after.snapshot.Tabs[0] != "alpha" {
		t.Errorf("待ち受け中にタブの対応が変わった: %v", after.snapshot.Tabs)
	}
}

// ---- Done -----------------------------------------------------------------

// settledDone は起動時の集計が着弾済みの Done を返す。
func settledDone(t *testing.T, service DoneService) DoneModel {
	t.Helper()
	next, _ := NewDoneModel(service).Update(doneRefreshedMsg{})
	m, ok := next.(DoneModel)
	if !ok {
		t.Fatalf("モデルの型 = %T", next)
	}
	return m
}

func TestDoneTickRefreshesAndRearms(t *testing.T) {
	t.Parallel()

	service := &tickDone{snapshot: app.DoneSnapshot{Text: "完了画面"}}
	m := settledDone(t, service)

	// tick が出すのは集計だけで、次の合図はその着弾で張る。
	_, cmd := m.Update(tickMsg{})
	landed, ok := wantRefreshOnly(t, cmd).(doneRefreshedMsg)
	if !ok {
		t.Fatal("発行したのが集計ではない")
	}
	if !landed.poll {
		t.Error("ポーリング起源として発行していない")
	}

	_, cmd = m.Update(landed)
	wantTimerOnly(t, cmd)
}

func TestDoneTickIsFrozenWhileAwaiting(t *testing.T) {
	t.Parallel()

	service := &tickDone{snapshot: app.DoneSnapshot{Text: "完了画面", Count: 3}}
	m := settledDone(t, service)
	m.awaiting = true

	next, cmd := m.Update(tickMsg{})
	if cmd == nil {
		t.Fatal("ポーリングが止まっている")
	}
	if msg := immediate(cmd); msg != nil {
		t.Errorf("待ち受け中に %T を予約している", msg)
	}
	if service.refreshes != 0 {
		t.Errorf("待ち受け中に読み直している: %d 回", service.refreshes)
	}

	// 凍結中の tick は逐次化の印を立てない。
	after, ok := next.(DoneModel)
	if !ok {
		t.Fatalf("モデルの型 = %T", next)
	}
	after.awaiting = false
	_, cmd = after.Update(tickMsg{})
	wantRefreshOnly(t, cmd)
	if service.refreshes != 1 {
		t.Errorf("凍結が解けた後の読み直しの回数 = %d, want 1", service.refreshes)
	}
}

// ---- News -----------------------------------------------------------------

func TestNewsKeepsFetchingScreenWhenPollArrives(t *testing.T) {
	t.Parallel()

	// 取得(r)に入る前に発行したポーリングの読み直しが、取得中に着弾する。
	// 表示を差し替えると取得中の画面が消え、まだ走っている取得が終わったように
	// 見える。さらに r が再び効くようになり、取得が二重に走る。
	service := &tickNews{snapshot: app.NewsSnapshot{
		Text: "ニュース画面", FetchingText: "取得中", Count: 1,
	}}
	next, _ := NewNewsModel(service).Update(newsRefreshedMsg{poll: true})
	m, ok := next.(NewsModel)
	if !ok {
		t.Fatalf("モデルの型 = %T", next)
	}
	m.snapshot = service.snapshot

	// r を押して取得中の画面へ入る。
	fetching, cmd := update(t, m, tickKey('r'))
	if cmd == nil {
		t.Fatal("取得へ進んでいない")
	}
	if got := fetching.View().Content; got != "取得中" {
		t.Fatalf("取得中の画面が出ていない: %q", got)
	}

	// 取得の裏で、先に出ていたポーリングの読み直しが着弾する。
	during, cmd := update(t, fetching, newsRefreshedMsg{
		snapshot: app.NewsSnapshot{Text: "新しいニュース", FetchingText: "取得中", Count: 1},
		poll:     true,
	})
	wantTimerOnly(t, cmd)
	if got := during.View().Content; got != "取得中" {
		t.Errorf("取得中の画面が差し替わった: %q", got)
	}

	// この間に r をもう一度押しても取得は始まらない。
	if _, cmd := update(t, during, tickKey('r')); cmd != nil {
		t.Errorf("取得中に %T を予約している(二重の取得)", immediate(cmd))
	}

	// 取得が終わった着弾で、はじめて通常の画面へ戻る。
	done, cmd := update(t, during, newsRefreshedMsg{snapshot: service.snapshot})
	wantNoContinuation(t, cmd)
	if got := done.View().Content; got != "ニュース画面" {
		t.Errorf("取得後に通常の画面へ戻っていない: %q", got)
	}
}

func TestNewsTickIsFrozenWhileFetching(t *testing.T) {
	t.Parallel()

	// 取得中の tick は読み直さない。ここでも逐次化の印は立てない。
	service := &tickNews{snapshot: app.NewsSnapshot{Text: "ニュース画面"}}
	next, _ := NewNewsModel(service).Update(newsRefreshedMsg{})
	m, ok := next.(NewsModel)
	if !ok {
		t.Fatalf("モデルの型 = %T", next)
	}
	m.fetching = true

	next, cmd := m.Update(tickMsg{})
	if cmd == nil {
		t.Fatal("ポーリングが止まっている")
	}
	if msg := immediate(cmd); msg != nil {
		t.Errorf("取得中に %T を予約している", msg)
	}

	after, ok := next.(NewsModel)
	if !ok {
		t.Fatalf("モデルの型 = %T", next)
	}
	after.fetching = false
	_, cmd = after.Update(tickMsg{})
	wantRefreshOnly(t, cmd)
	if service.refreshes != 1 {
		t.Errorf("取得の後の読み直しの回数 = %d, want 1", service.refreshes)
	}
}

func TestDoneRefreshIsDroppedWhileAwaiting(t *testing.T) {
	t.Parallel()

	service := &tickDone{}
	m := NewDoneModel(service)
	m.snapshot = app.DoneSnapshot{Text: "元の完了画面", Count: 3}
	m.awaiting = true

	next, cmd := m.Update(doneRefreshedMsg{
		snapshot: app.DoneSnapshot{Text: "新しい完了画面", Count: 1},
	})
	if cmd != nil {
		t.Error("在庫の読み直しがコマンドを返している")
	}
	after, ok := next.(DoneModel)
	if !ok {
		t.Fatalf("モデルの型 = %T", next)
	}
	if after.snapshot.Count != 3 {
		t.Errorf("待ち受け中に件数が変わった: %d", after.snapshot.Count)
	}
}
