package tui

import (
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/k-kudo-hub/mdev-go/internal/app"
)

// ポーリングの合図(tickMsg)は非公開型なので、外部テスト(tui_test.go)から
// 流し込めない。「2 打鍵目を待っている間はポーリングを止める」という
// 取り決めはここで内部から確かめる。表示とキー操作の検証は外部テストが持つ。

var tickEnv = app.PaneEnv{ZellijSession: "s1"}

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

// ---- Dashboard ------------------------------------------------------------

func TestDashboardTickRefreshesAndRearms(t *testing.T) {
	t.Parallel()

	service := &tickDashboard{snapshot: app.DashboardSnapshot{Text: "画面"}}
	m := NewDashboardModel(service, tickEnv)

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
	m := NewDashboardModel(service, tickEnv)
	m.awaiting = true

	_, cmd := m.Update(tickMsg{})
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
}

func TestDashboardTickIsFrozenWhileBusy(t *testing.T) {
	t.Parallel()

	service := &tickDashboard{snapshot: app.DashboardSnapshot{Text: "画面"}}
	m := NewDashboardModel(service, tickEnv)
	m.busy = true

	_, cmd := m.Update(tickMsg{})
	if cmd == nil {
		t.Fatal("ポーリングが止まっている")
	}
	if msg := immediate(cmd); msg != nil {
		t.Errorf("削除の途中に %T を予約している", msg)
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

func TestDoneTickRefreshesAndRearms(t *testing.T) {
	t.Parallel()

	service := &tickDone{snapshot: app.DoneSnapshot{Text: "完了画面"}}
	m := NewDoneModel(service)

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
	m := NewDoneModel(service)
	m.awaiting = true

	_, cmd := m.Update(tickMsg{})
	if cmd == nil {
		t.Fatal("ポーリングが止まっている")
	}
	if msg := immediate(cmd); msg != nil {
		t.Errorf("待ち受け中に %T を予約している", msg)
	}
	if service.refreshes != 0 {
		t.Errorf("待ち受け中に読み直している: %d 回", service.refreshes)
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
