package tui

import (
	"errors"
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
}

var _ DashboardService = (*tickDashboard)(nil)

func (s *tickDashboard) Startup() {}

func (s *tickDashboard) Refresh(app.PaneEnv) (app.DashboardSnapshot, error) {
	s.refreshes++
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

// update はモデルにメッセージを 1 つ渡し、進んだモデルとコマンドを返す。
func update(t *testing.T, m tea.Model, msg tea.Msg) (tea.Model, tea.Cmd) {
	t.Helper()
	next, cmd := m.Update(msg)
	if next == nil {
		t.Fatal("Update がモデルを返していない")
	}
	return next, cmd
}

// wantRefreshIssued は tick が読み直しと次の合図を束ねて返したことを確かめ、
// 束ねられた読み直しを実際に走らせる(発行はしたが着弾はしていない状態)。
func wantRefreshIssued(t *testing.T, cmd tea.Cmd) {
	t.Helper()
	batch, ok := immediate(cmd).(tea.BatchMsg)
	if !ok {
		t.Fatal("読み直しと次の合図を束ねていない")
	}
	if len(batch) != 2 {
		t.Fatalf("コマンド数 = %d, want 2", len(batch))
	}
	batch[0]()
}

// wantOnlyRearm は tick が次の合図だけを予約したことを確かめる。
//
// 読み直しを束ねていれば tea.Batch がその場でメッセージを返すのに対し、
// タイマー(tea.Tick)だけなら間隔ぶん待たないと返らない。この差で判別する。
func wantOnlyRearm(t *testing.T, cmd tea.Cmd) {
	t.Helper()
	if cmd == nil {
		t.Fatal("ポーリングが止まっている")
	}
	if msg := immediate(cmd); msg != nil {
		t.Errorf("読み直しを重ねて発行している: %T", msg)
	}
}

// ---- 逐次化(4 ペイン共通) -----------------------------------------------

// 前回の読み直し(コマンドの発行から着弾まで)が終わるまで、tick は次の
// 読み直しを発行しない。Dashboard の読み直しは zellij の CLI 呼び出しを含み
// 間隔(2 秒)を超えることがあり、重ねて出すと CLI が並行に走り続ける。

// inflightCase は 4 ペインを同じ形で回すためのテーブルの 1 行である。
type inflightCase struct {
	name string
	// model は生成直後のモデル。Init が最初の読み直しを発行済みの状態にあたる。
	model tea.Model
	// landed は読み直しの着弾メッセージ。
	landed tea.Msg
	// refreshes はユースケースが読み直された回数を返す。
	refreshes func() int
}

func inflightCases() []inflightCase {
	dashboard := &tickDashboard{snapshot: app.DashboardSnapshot{Text: "画面"}}
	waiting := &tickWaiting{text: "待ち画面"}
	done := &tickDone{snapshot: app.DoneSnapshot{Text: "完了画面"}}
	news := &tickNews{snapshot: app.NewsSnapshot{Text: "ニュース画面"}}

	return []inflightCase{
		{"dashboard", NewDashboardModel(dashboard, tickEnv), dashboardRefreshedMsg{}, func() int { return dashboard.refreshes }},
		{"waiting", NewWaitingModel(waiting, tickEnv), waitingRefreshedMsg{}, func() int { return waiting.refreshes }},
		{"done", NewDoneModel(done), doneRefreshedMsg{}, func() int { return done.refreshes }},
		{"news", NewNewsModel(news), newsRefreshedMsg{}, func() int { return news.refreshes }},
	}
}

func TestPaneTickSkipsRefreshWhileInFlight(t *testing.T) {
	t.Parallel()

	for _, tt := range inflightCases() {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			// 生成直後は Init が出した読み直しが走っている。着弾するまでは
			// tick が来ても重ねて出さず、次の合図だけを予約する。
			m, cmd := update(t, tt.model, tickMsg{})
			wantOnlyRearm(t, cmd)
			if tt.refreshes() != 0 {
				t.Fatalf("起動時の読み直しに重ねている: %d 回", tt.refreshes())
			}

			// 着弾したら、次の tick は通常どおり読み直しを発行する。
			m, _ = update(t, m, tt.landed)
			m, cmd = update(t, m, tickMsg{})
			wantRefreshIssued(t, cmd)
			if tt.refreshes() != 1 {
				t.Fatalf("読み直しの回数 = %d, want 1", tt.refreshes())
			}

			// その読み直しが着弾する前の tick も、次の合図だけを予約する。
			m, cmd = update(t, m, tickMsg{})
			wantOnlyRearm(t, cmd)
			if tt.refreshes() != 1 {
				t.Fatalf("読み直しが重なった: %d 回", tt.refreshes())
			}

			// 着弾すれば元どおり発行できる(印の下ろし忘れが無い)。
			m, _ = update(t, m, tt.landed)
			_, cmd = update(t, m, tickMsg{})
			wantRefreshIssued(t, cmd)
			if tt.refreshes() != 2 {
				t.Errorf("読み直しの回数 = %d, want 2", tt.refreshes())
			}
		})
	}
}

func TestPaneRefreshErrorReleasesInFlight(t *testing.T) {
	t.Parallel()

	// エラーで返ってきた着弾でも印は下ろす。下ろし忘れると、一度失敗した
	// だけでポーリングが二度と読み直さなくなる。
	tests := []struct {
		name   string
		model  tea.Model
		failed tea.Msg
	}{
		{
			name:   "dashboard",
			model:  NewDashboardModel(&tickDashboard{}, tickEnv),
			failed: dashboardRefreshedMsg{err: errTickRefresh},
		},
		{
			name:   "waiting",
			model:  NewWaitingModel(&tickWaiting{}, tickEnv),
			failed: waitingRefreshedMsg{err: errTickRefresh},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			m, _ := update(t, tt.model, tt.failed)
			_, cmd := update(t, m, tickMsg{})
			wantRefreshIssued(t, cmd)
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

func TestDashboardTickRefreshesAndRearms(t *testing.T) {
	t.Parallel()

	service := &tickDashboard{snapshot: app.DashboardSnapshot{Text: "画面"}}
	m := settledDashboard(t, service)

	_, cmd := m.Update(tickMsg{})
	batch, ok := immediate(cmd).(tea.BatchMsg)
	if !ok {
		t.Fatal("読み直しと次の合図を束ねていない")
	}
	if len(batch) != 2 {
		t.Fatalf("コマンド数 = %d, want 2", len(batch))
	}
	// 先頭が読み直しである。後ろはタイマーなので実行しない。
	if _, ok := batch[0]().(dashboardRefreshedMsg); !ok {
		t.Error("束の先頭が読み直しではない")
	}
	if service.refreshes != 1 {
		t.Errorf("読み直しの回数 = %d, want 1", service.refreshes)
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
	wantRefreshIssued(t, cmd)
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
	wantRefreshIssued(t, cmd)
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
	wantRefreshIssued(t, cmd)
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

	_, cmd := m.Update(tickMsg{})
	batch, ok := immediate(cmd).(tea.BatchMsg)
	if !ok {
		t.Fatal("読み直しと次の合図を束ねていない")
	}
	if len(batch) != 2 {
		t.Fatalf("コマンド数 = %d, want 2", len(batch))
	}
	if _, ok := batch[0]().(doneRefreshedMsg); !ok {
		t.Error("束の先頭が読み直しではない")
	}
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
	wantRefreshIssued(t, cmd)
	if service.refreshes != 1 {
		t.Errorf("凍結が解けた後の読み直しの回数 = %d, want 1", service.refreshes)
	}
}

// ---- News -----------------------------------------------------------------

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
	wantRefreshIssued(t, cmd)
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
