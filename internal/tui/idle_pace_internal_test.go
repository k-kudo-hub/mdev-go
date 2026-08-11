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

	p.observeAttach(false)
	if got := p.pollInterval(); got != app.IdlePollInterval {
		t.Errorf("未アタッチの間隔 = %v, want %v", got, app.IdlePollInterval)
	}

	p.observeAttach(true)
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
	msgs := collectMsgs(t, p.arrive(true))
	ticks, checks := countMsgs(msgs)
	if ticks != 1 {
		t.Errorf("合図 = %d 本, want 1 本", ticks)
	}
	if checks != 1 {
		t.Errorf("確認 = %d 本, want 1 本", checks)
	}

	// 確認の結果を取り込んでも、合図は 1 本も増えない。
	before := p.inFlight
	p.observeAttach(false)
	if p.inFlight != before {
		t.Errorf("確認で実行中の数が変わりました: %d → %d", before, p.inFlight)
	}
}

// TestPollerWithoutWatchDoesNotSlowDown は見張りが無い設定
// (zellij の外・--once)で減速も確認も起きないことを確かめる。
func TestPollerWithoutWatchDoesNotSlowDown(t *testing.T) {
	t.Parallel()

	p := newPoller(testPollInterval)
	msgs := collectMsgs(t, p.arrive(true))
	ticks, checks := countMsgs(msgs)
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
	p.observeAttach(false)

	if got := p.pollInterval(); got != app.IdlePollInterval {
		t.Errorf("rearm の間隔 = %v, want %v", got, app.IdlePollInterval)
	}
	if p.rearm() == nil {
		t.Error("再予約されていません")
	}
}

// runAttachChecks は cmd を実行し、確認のコマンドがあれば走らせる。
func runAttachChecks(t *testing.T, cmd tea.Cmd) {
	t.Helper()
	collectMsgs(t, cmd)
}

// collectMsgs は cmd(tea.Batch を含む)を実行して出た合図を集める。
func collectMsgs(t *testing.T, cmd tea.Cmd) []tea.Msg {
	t.Helper()
	if cmd == nil {
		return nil
	}
	msg := cmd()
	batch, ok := msg.(tea.BatchMsg)
	if !ok {
		return []tea.Msg{msg}
	}
	msgs := make([]tea.Msg, 0, len(batch))
	for _, inner := range batch {
		if inner == nil {
			continue
		}
		msgs = append(msgs, inner())
	}
	return msgs
}

// countMsgs は合図の種類ごとの数を返す。
func countMsgs(msgs []tea.Msg) (ticks, checks int) {
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
