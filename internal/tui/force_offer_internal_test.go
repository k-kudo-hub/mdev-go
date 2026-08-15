package tui

import (
	"strings"
	"testing"

	"github.com/k-kudo-hub/mdev-go/internal/app"
)

// 強制削除の受付の寿命を確かめる内部テスト。
//
// 外部テストから確かめられないのは、期限のタイマー(tea.Tick)を実行すると
// **本当にその時間だけ待つ**ためである。10 秒待つテストは書けない。ここでは
// 期限切れのメッセージを直接流して、受け手の振る舞いだけを見る。

// forceOfferPane は強制削除の呼び出しだけを記録する。
type forceOfferPane struct {
	calls []string
}

func (p *forceOfferPane) Startup(app.PaneEnv) []string { return nil }

func (p *forceOfferPane) Refresh(app.PaneEnv) (app.DashboardSnapshot, error) {
	return app.DashboardSnapshot{Text: "画面", Tabs: []string{"alpha"}}, nil
}

func (p *forceOfferPane) Jump(app.PaneEnv, app.DashboardSnapshot, int) error { return nil }

func (p *forceOfferPane) PrepareDelete(app.PaneEnv, string) (app.DeletePreparation, error) {
	return app.DeletePreparation{Cancelled: true, Reason: "だめでした"}, nil
}

func (p *forceOfferPane) CommitDelete(app.PaneEnv, string) error { return nil }

func (p *forceOfferPane) ForceDelete(_ app.PaneEnv, tab string) error {
	p.calls = append(p.calls, "force "+tab)
	return nil
}

// cancelledDashboard は「中止の提示が出ている」状態のモデルを返す。
func cancelledDashboard(t *testing.T, pane *forceOfferPane) DashboardModel {
	t.Helper()

	m := NewDashboardModel(pane, app.PaneEnv{ZellijSession: "s1"})
	snapshot, _ := pane.Refresh(app.PaneEnv{})
	m.snapshot = snapshot
	next, _ := m.handlePrepared(deletePreparedMsg{
		tab:  "alpha",
		prep: app.DeletePreparation{Cancelled: true, Reason: "だめでした"},
	})
	model, ok := next.(DashboardModel)
	if !ok {
		t.Fatalf("DashboardModel が返っていない: %T", next)
	}
	if model.forceTab != "alpha" {
		t.Fatalf("強制削除の提示が立っていない: %q", model.forceTab)
	}
	return model
}

// TestForceOfferExpiresWithNotice は期限切れで通知と受付が同時に解けることを
// 確かめる。
//
// **武装したまま放置されないことが要点である。** 通知が消えているのに `!` だけ
// が効く状態を作ると、後から押した 1 打鍵で作業ログを失う。
func TestForceOfferExpiresWithNotice(t *testing.T) {
	t.Parallel()

	pane := &forceOfferPane{}
	m := cancelledDashboard(t, pane)

	// 期限が来た。
	next, _ := m.Update(noticeExpiredMsg{token: m.token})
	expired, ok := next.(DashboardModel)
	if !ok {
		t.Fatalf("DashboardModel が返っていない: %T", next)
	}

	if expired.forceTab != "" {
		t.Errorf("受付が残っている: %q", expired.forceTab)
	}
	if expired.notice != "" {
		t.Errorf("通知が残っている: %q", expired.notice)
	}
	if expired.busy {
		t.Error("busy が解けていない")
	}

	// その後の `!` は何も起こさない。
	after, _ := expired.handleKey(forceDeleteKey)
	if model, ok := after.(DashboardModel); ok && model.busy {
		t.Error("期限切れ後に削除へ進んだ")
	}
	if len(pane.calls) != 0 {
		t.Errorf("期限切れ後に強制削除が走った: %v", pane.calls)
	}
}

// TestForceOfferArmedUntilExpiry は期限内なら `!` が効くことを確かめる。
func TestForceOfferArmedUntilExpiry(t *testing.T) {
	t.Parallel()

	pane := &forceOfferPane{}
	m := cancelledDashboard(t, pane)

	if !strings.Contains(m.notice, "アップロードせずに削除") {
		t.Errorf("案内が出ていない: %q", m.notice)
	}

	_, cmd := m.handleKey(forceDeleteKey)
	if cmd == nil {
		t.Fatal("強制削除のコマンドが出ていない")
	}
	if _, ok := cmd().(deleteFinishedMsg); !ok {
		t.Error("削除の完了メッセージが返っていない")
	}
	if len(pane.calls) != 1 || pane.calls[0] != "force alpha" {
		t.Errorf("強制削除が呼ばれていない: %v", pane.calls)
	}
}

// TestForceOfferStaleTimerIgnored は古い期限切れが後の表示を消さないことを
// 確かめる。
//
// 提示を解くときに世代を進めているので、張ってあったタイマーは無効になる。
// 進めないと、次の操作の最中に古い期限切れが届いて表示が消える。
func TestForceOfferStaleTimerIgnored(t *testing.T) {
	t.Parallel()

	m := cancelledDashboard(t, &forceOfferPane{})
	stale := m.token

	// 別のキーで提示を解く(世代が進む)。
	next, _ := m.handleKey("x")
	cleared, ok := next.(DashboardModel)
	if !ok {
		t.Fatalf("DashboardModel が返っていない: %T", next)
	}
	cleared.notice = "後から出した通知"

	after, _ := cleared.Update(noticeExpiredMsg{token: stale})
	model, ok := after.(DashboardModel)
	if !ok {
		t.Fatalf("DashboardModel が返っていない: %T", after)
	}
	if model.notice != "後から出した通知" {
		t.Errorf("古いタイマーが後の通知を消した: %q", model.notice)
	}
}

// TestForceOfferCmdUsesLongerDuration は受付の期限が通知より長いことを
// 確かめる。
//
// 通知(2 秒)は結果を知らせるだけで読み流してよいが、こちらは失敗の理由を
// 読んで判断するための時間である。
func TestForceOfferCmdUsesLongerDuration(t *testing.T) {
	t.Parallel()

	if forceOfferDuration <= noticeDuration {
		t.Errorf("forceOfferDuration = %v, want %v より長い", forceOfferDuration, noticeDuration)
	}
	// タイマーは張られるが、ここでは実行しない(実行すると本当に待つ)。
	if forceOfferCmd(1) == nil {
		t.Error("期限のコマンドが組み立てられていない")
	}
}
