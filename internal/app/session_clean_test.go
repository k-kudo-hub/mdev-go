package app_test

import (
	"errors"
	"fmt"
	"slices"
	"testing"
	"time"

	"github.com/k-kudo-hub/mdev-go/internal/app"
)

// fakeSessionStore は zellij のセッション操作の代役である。
type fakeSessionStore struct {
	sessionsOut string
	sessionsErr error
	// clients はセッション名ごとの list-clients の出力。
	clients map[string]string
	// clientErrs はセッション名ごとの list-clients の失敗。
	clientErrs map[string]error

	// calls は行った操作を順に記録する。
	calls []string
}

func (s *fakeSessionStore) ListSessions() (string, error) {
	return s.sessionsOut, s.sessionsErr
}

func (s *fakeSessionStore) ListClients(session string) (string, error) {
	if err := s.clientErrs[session]; err != nil {
		return "", err
	}
	return s.clients[session], nil
}

func (s *fakeSessionStore) KillSession(name string) error {
	s.calls = append(s.calls, "kill "+name)
	return nil
}

func (s *fakeSessionStore) DeleteSession(name string) error {
	s.calls = append(s.calls, "delete "+name)
	return nil
}

// fakeProcessStore はプロセスの一覧とシグナルの代役である。
type fakeProcessStore struct {
	out string
	err error
	// aliveAfterTerm は TERM の後もまだ居る PID。
	aliveAfterTerm map[int]bool

	calls []string
}

func (p *fakeProcessStore) ListProcesses() (string, error) { return p.out, p.err }

func (p *fakeProcessStore) Terminate(pid int) error {
	p.calls = append(p.calls, fmt.Sprintf("term %d", pid))
	return nil
}

func (p *fakeProcessStore) Kill(pid int) error {
	p.calls = append(p.calls, fmt.Sprintf("kill %d", pid))
	return nil
}

func (p *fakeProcessStore) IsAlive(pid int) bool { return p.aliveAfterTerm[pid] }

// fakeSleeper は待ちを記録するだけで実際には待たない。
type fakeSleeper struct{ slept []time.Duration }

func (s *fakeSleeper) Sleep(d time.Duration) { s.slept = append(s.slept, d) }

// 掃除のテストで使う実機由来の出力。使用中の 1 つ(in-use)と、
// 誰も開いていない mdev セッション(idle)、終了済み、手動セッションを含む。
const (
	cleanSessionList = "in-use [Created 40m ago] \n" +
		"idle [Created 30m ago] \n" +
		"manual [Created 20m ago] \n" +
		"gone-1 [Created 12m ago] (EXITED - attach to resurrect)\n" +
		"gone-2 [Created 12m ago] (EXITED - attach to resurrect)\n"

	cleanProcessList = "  PID  PPID     ELAPSED COMMAND\n" +
		"100     1 40:00 /opt/homebrew/bin/zellij --server /tmp/z/in-use\n" +
		"101   100 40:00 /home/u/.claude-conductor/bin/mdev pane dashboard\n" +
		"200     1 30:00 /opt/homebrew/bin/zellij --server /tmp/z/idle\n" +
		"201   200 30:00 /home/u/.claude-conductor/bin/mdev pane dashboard\n" +
		"300     1 20:00 /opt/homebrew/bin/zellij --server /tmp/z/manual\n" +
		"301   300 20:00 -zsh\n" +
		"400     1 12:00 /opt/homebrew/bin/zellij --server /tmp/z/zombie\n" +
		"500     1 12:00 zellij action list-tabs\n"
)

// newCleaner は既定の状況(in-use に 1 つ attach、idle は誰も居ない)の
// 掃除ユースケースを返す。
func newCleaner() (*app.SessionCleaner, *fakeSessionStore, *fakeProcessStore, *fakeSleeper) {
	sessions := &fakeSessionStore{
		sessionsOut: cleanSessionList,
		clients: map[string]string{
			"in-use": "CLIENT_ID ZELLIJ_PANE_ID RUNNING_COMMAND\n1  terminal_3  mdev pane news\n",
			"idle":   "CLIENT_ID ZELLIJ_PANE_ID RUNNING_COMMAND\n",
			"manual": "CLIENT_ID ZELLIJ_PANE_ID RUNNING_COMMAND\n",
		},
		clientErrs: map[string]error{},
	}
	processes := &fakeProcessStore{out: cleanProcessList, aliveAfterTerm: map[int]bool{}}
	sleeper := &fakeSleeper{}
	return &app.SessionCleaner{
		Sessions:  sessions,
		Clients:   sessions,
		Remover:   sessions,
		Processes: processes,
		Signaler:  processes,
		Sleeper:   sleeper,
	}, sessions, processes, sleeper
}

// TestCleanPlan は掃除の対象の選び方を固定する。
func TestCleanPlan(t *testing.T) {
	t.Parallel()

	cleaner, _, _, _ := newCleaner()
	got, err := cleaner.Clean(true)
	if err != nil {
		t.Fatalf("Clean() = %v", err)
	}

	if want := []string{"gone-1", "gone-2"}; !slices.Equal(got.Plan.ExitedSessions, want) {
		t.Errorf("終了済み = %v, want %v", got.Plan.ExitedSessions, want)
	}
	// idle だけ。in-use は attach あり、manual は mdev 管理ではない。
	if want := []string{"idle"}; !slices.Equal(got.Plan.DetachedSessions, want) {
		t.Errorf("detached = %v, want %v", got.Plan.DetachedSessions, want)
	}
	want := []app.ZombieServer{{PID: 400, Session: "zombie"}}
	if !slices.Equal(got.Plan.ZombieServers, want) {
		t.Errorf("ゾンビ = %v, want %v", got.Plan.ZombieServers, want)
	}
	if want := []int{500}; !slices.Equal(got.Plan.OrphanClients, want) {
		t.Errorf("孤児 = %v, want %v", got.Plan.OrphanClients, want)
	}
}

// TestCleanDryRunDoesNothing は dry-run が一切実行しないことを確かめる。
func TestCleanDryRunDoesNothing(t *testing.T) {
	t.Parallel()

	cleaner, sessions, processes, sleeper := newCleaner()
	got, err := cleaner.Clean(true)
	if err != nil {
		t.Fatalf("Clean() = %v", err)
	}

	if !got.DryRun {
		t.Error("DryRun = false, want true")
	}
	if len(sessions.calls) != 0 {
		t.Errorf("セッションを操作しました: %v", sessions.calls)
	}
	if len(processes.calls) != 0 {
		t.Errorf("シグナルを送りました: %v", processes.calls)
	}
	if len(sleeper.slept) != 0 {
		t.Errorf("待ちました: %v", sleeper.slept)
	}
}

// TestCleanApplies は実行時の操作と順序を固定する。
//
// detached は kill してから delete する。kill だけでは EXITED として残り、
// 次の掃除でまた拾うことになる。
func TestCleanApplies(t *testing.T) {
	t.Parallel()

	cleaner, sessions, processes, sleeper := newCleaner()
	if _, err := cleaner.Clean(false); err != nil {
		t.Fatalf("Clean() = %v", err)
	}

	want := []string{"kill idle", "delete idle", "delete gone-1", "delete gone-2"}
	if !slices.Equal(sessions.calls, want) {
		t.Errorf("セッション操作 = %v, want %v", sessions.calls, want)
	}
	// ゾンビは TERM → 猶予 → 生きていれば KILL。ここでは TERM で消える。
	if want := []string{"term 400", "kill 500"}; !slices.Equal(processes.calls, want) {
		t.Errorf("シグナル = %v, want %v", processes.calls, want)
	}
	if want := []time.Duration{3 * time.Second}; !slices.Equal(sleeper.slept, want) {
		t.Errorf("猶予 = %v, want %v", sleeper.slept, want)
	}
}

// TestCleanKillsStubbornZombie は TERM で終わらないサーバへ KILL を送る
// ことを確かめる。
func TestCleanKillsStubbornZombie(t *testing.T) {
	t.Parallel()

	cleaner, _, processes, _ := newCleaner()
	processes.aliveAfterTerm[400] = true

	if _, err := cleaner.Clean(false); err != nil {
		t.Fatalf("Clean() = %v", err)
	}
	if want := []string{"term 400", "kill 400", "kill 500"}; !slices.Equal(processes.calls, want) {
		t.Errorf("シグナル = %v, want %v", processes.calls, want)
	}
}

// TestCleanNeverTouchesAttachedSession は **この機能で最も重要な不変条件**
// を固定する。アタッチしているクライアントが 1 つでもあるセッションと、
// そのサーバには一切触れない。
func TestCleanNeverTouchesAttachedSession(t *testing.T) {
	t.Parallel()

	cleaner, sessions, processes, _ := newCleaner()
	if _, err := cleaner.Clean(false); err != nil {
		t.Fatalf("Clean() = %v", err)
	}

	for _, call := range sessions.calls {
		if call == "kill in-use" || call == "delete in-use" {
			t.Errorf("使用中のセッションを操作しました: %v", sessions.calls)
		}
	}
	// in-use のサーバ(PID 100)とそのペイン(101)へシグナルを送らない。
	for _, call := range processes.calls {
		for _, forbidden := range []string{"100", "101"} {
			if call == "term "+forbidden || call == "kill "+forbidden {
				t.Errorf("使用中セッションのプロセスへシグナルを送りました: %v", processes.calls)
			}
		}
	}
}

// TestCleanTreatsClientFailureAsAttached は list-clients が失敗した
// セッションへ触れないことを確かめる(安全側)。
//
// 誰も居ないと誤って判断すると、使用中のセッションを終了させてしまう。
func TestCleanTreatsClientFailureAsAttached(t *testing.T) {
	t.Parallel()

	cleaner, sessions, _, _ := newCleaner()
	sessions.clientErrs["idle"] = errors.New("応答しない")

	got, err := cleaner.Clean(false)
	if err != nil {
		t.Fatalf("Clean() = %v", err)
	}
	if len(got.Plan.DetachedSessions) != 0 {
		t.Errorf("detached = %v, want 空", got.Plan.DetachedSessions)
	}
	for _, call := range sessions.calls {
		if call == "kill idle" {
			t.Errorf("判断できないセッションを終了させました: %v", sessions.calls)
		}
	}
}

// TestCleanLeavesManualSessions は手で作ったセッション(mdev 管理でない)を
// 終了させないことを確かめる。終了済みのものだけは消す。
func TestCleanLeavesManualSessions(t *testing.T) {
	t.Parallel()

	cleaner, sessions, _, _ := newCleaner()
	if _, err := cleaner.Clean(false); err != nil {
		t.Fatalf("Clean() = %v", err)
	}
	for _, call := range sessions.calls {
		if call == "kill manual" || call == "delete manual" {
			t.Errorf("手動セッションを操作しました: %v", sessions.calls)
		}
	}
}

// TestCleanReportsListFailures は判断材料が取れないときに error を返し、
// 何も実行しないことを確かめる。
func TestCleanReportsListFailures(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		setup func(*fakeSessionStore, *fakeProcessStore)
	}{
		{
			name: "セッション一覧が取れない",
			setup: func(s *fakeSessionStore, _ *fakeProcessStore) {
				s.sessionsErr = errors.New("zellij が無い")
			},
		},
		{
			name: "プロセス一覧が取れない",
			setup: func(_ *fakeSessionStore, p *fakeProcessStore) {
				p.err = errors.New("ps が無い")
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			cleaner, sessions, processes, _ := newCleaner()
			tt.setup(sessions, processes)

			if _, err := cleaner.Clean(false); err == nil {
				t.Fatal("error になりませんでした")
			}
			if len(sessions.calls) != 0 || len(processes.calls) != 0 {
				t.Errorf("判断材料が無いのに実行しました: %v / %v", sessions.calls, processes.calls)
			}
		})
	}
}

// TestCleanupPlanIsEmpty は「掃除するものが無い」の判定を確かめる。
// --auto はこれが真なら何も出さない。
func TestCleanupPlanIsEmpty(t *testing.T) {
	t.Parallel()

	if !(app.CleanupPlan{}).IsEmpty() {
		t.Error("空の計画が空でないと判定されました")
	}
	filled := []app.CleanupPlan{
		{ExitedSessions: []string{"a"}},
		{DetachedSessions: []string{"a"}},
		{ZombieServers: []app.ZombieServer{{PID: 1}}},
		{OrphanClients: []int{1}},
	}
	for _, plan := range filled {
		if plan.IsEmpty() {
			t.Errorf("対象があるのに空と判定されました: %+v", plan)
		}
	}
}

// TestCleanWithNothingToDo は掃除する対象が無い環境で何も実行しないことを
// 確かめる。
func TestCleanWithNothingToDo(t *testing.T) {
	t.Parallel()

	sessions := &fakeSessionStore{
		sessionsOut: "in-use [Created 1m ago] \n",
		clients: map[string]string{
			"in-use": "CLIENT_ID ZELLIJ_PANE_ID RUNNING_COMMAND\n1  terminal_1  x\n",
		},
		clientErrs: map[string]error{},
	}
	processes := &fakeProcessStore{
		out: "100     1 40:00 /opt/homebrew/bin/zellij --server /tmp/z/in-use\n" +
			"101   100 40:00 /home/u/.claude-conductor/bin/mdev pane dashboard\n",
		aliveAfterTerm: map[int]bool{},
	}
	cleaner := &app.SessionCleaner{
		Sessions: sessions, Clients: sessions, Remover: sessions,
		Processes: processes, Signaler: processes, Sleeper: &fakeSleeper{},
	}

	got, err := cleaner.Clean(false)
	if err != nil {
		t.Fatalf("Clean() = %v", err)
	}
	if !got.Plan.IsEmpty() {
		t.Errorf("掃除の対象 = %+v, want 空", got.Plan)
	}
	if len(sessions.calls) != 0 || len(processes.calls) != 0 {
		t.Errorf("何もしないはずが操作しました: %v / %v", sessions.calls, processes.calls)
	}
}
