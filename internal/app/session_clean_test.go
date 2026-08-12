package app_test

import (
	"errors"
	"fmt"
	"slices"
	"strings"
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
	// clientsOnRecheck は 2 回目以降の list-clients で返す出力である。
	// 「選んだ後に誰かが開いた」状況を作るために使う。
	clientsOnRecheck map[string]string
	// clientCalls はセッションごとの list-clients の呼び出し回数。
	clientCalls map[string]int
	// sessionsOnRecheck は 2 回目以降の list-sessions で返す出力である。
	sessionsOnRecheck string
	// sessionCalls は list-sessions の呼び出し回数。
	sessionCalls int

	// calls は行った操作を順に記録する。
	calls []string
}

func (s *fakeSessionStore) ListSessions() (string, error) {
	s.sessionCalls++
	if s.sessionCalls > 1 && s.sessionsOnRecheck != "" {
		return s.sessionsOnRecheck, s.sessionsErr
	}
	return s.sessionsOut, s.sessionsErr
}

func (s *fakeSessionStore) ListClients(session string) (string, error) {
	if s.clientCalls == nil {
		s.clientCalls = map[string]int{}
	}
	s.clientCalls[session]++
	if err := s.clientErrs[session]; err != nil {
		return "", err
	}
	if s.clientCalls[session] > 1 {
		if out, ok := s.clientsOnRecheck[session]; ok {
			return out, nil
		}
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
	cleanSessionList = "in-use [Created 40m 0s ago] \n" +
		"idle [Created 30m 0s ago] \n" +
		"manual [Created 20m 0s ago] \n" +
		"gone-1 [Created 12m 0s ago] (EXITED - attach to resurrect)\n" +
		"gone-2 [Created 12m 0s ago] (EXITED - attach to resurrect)\n"

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
		clientErrs:       map[string]error{},
		clientsOnRecheck: map[string]string{},
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
		Clock:     testClock,
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
		sessionsOut: "in-use [Created 1m 0s ago] \n",
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
		Clock: testClock,
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

// attachedClients は attach 中のクライアントが 1 つある応答である。
const attachedClients = "CLIENT_ID ZELLIJ_PANE_ID RUNNING_COMMAND\n1  terminal_1  x\n"

// TestCleanRechecksClientsBeforeKill は **消す直前にもう一度確かめる**
// ことを確かめる(指摘 3)。
//
// 掃除はセッションの起動前に走るので、対象を選んでから実行するまでの間に
// 利用者がそのセッションを開くことはありうる。選んだ時点の判断だけで消すと、
// 開いたばかりのセッションを落とす。
func TestCleanRechecksClientsBeforeKill(t *testing.T) {
	t.Parallel()

	cleaner, sessions, _, _ := newCleaner()
	// 選ぶときは誰も居ないが、消す直前には開かれている。
	sessions.clientsOnRecheck["idle"] = attachedClients

	got, err := cleaner.Clean(false)
	if err != nil {
		t.Fatalf("Clean() = %v", err)
	}
	// 計画には載る(選んだ時点では誰も居なかった)。
	if want := []string{"idle"}; !slices.Equal(got.Plan.DetachedSessions, want) {
		t.Errorf("計画 = %v, want %v", got.Plan.DetachedSessions, want)
	}
	// しかし実行はしない。
	for _, call := range sessions.calls {
		if call == "kill idle" || call == "delete idle" {
			t.Errorf("直前に開かれたセッションを消しました: %v", sessions.calls)
		}
	}
}

// TestCleanRechecksExitedBeforeDelete は終了済みセッションの削除前に
// 一覧を引き直すことを確かめる(指摘 3)。
//
// 終了済みは「attach すると復活する」状態でもある。選んでから実行するまでの
// 間に復活していたら、それは使用中のセッションであって消してはならない。
func TestCleanRechecksExitedBeforeDelete(t *testing.T) {
	t.Parallel()

	cleaner, sessions, _, _ := newCleaner()
	// gone-1 だけが復活した(EXITED でなくなった)。
	sessions.sessionsOnRecheck = "in-use [Created 40m 0s ago] \n" +
		"gone-1 [Created 12m 0s ago] \n" +
		"gone-2 [Created 12m 0s ago] (EXITED - attach to resurrect)\n"

	if _, err := cleaner.Clean(false); err != nil {
		t.Fatalf("Clean() = %v", err)
	}
	for _, call := range sessions.calls {
		if call == "delete gone-1" {
			t.Errorf("復活したセッションを消しました: %v", sessions.calls)
		}
	}
	if !slices.Contains(sessions.calls, "delete gone-2") {
		t.Errorf("終了済みのままのものが消えていません: %v", sessions.calls)
	}
}

// TestCleanSkipsDeleteWhenRecheckFails は引き直しに失敗したら終了済みを
// 1 件も消さないことを確かめる(確かめられないなら消さない)。
func TestCleanSkipsDeleteWhenRecheckFails(t *testing.T) {
	t.Parallel()

	cleaner, sessions, _, _ := newCleaner()
	sessions.sessionsOnRecheck = "  " // 空 = 引き直しても何も分からない

	if _, err := cleaner.Clean(false); err != nil {
		t.Fatalf("Clean() = %v", err)
	}
	for _, call := range sessions.calls {
		if strings.HasPrefix(call, "delete gone-") {
			t.Errorf("確かめられないのに消しました: %v", sessions.calls)
		}
	}
}

// TestCleanSparesYoungSessions は作られたばかりのセッションを掴まない
// ことを確かめる(指摘 4)。
//
// ペインが起動して attach されるまでの間は「誰も開いていない」ように
// 見える。ここで撃つと、今まさに開こうとしているセッションを落とす。
func TestCleanSparesYoungSessions(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		age  string
		want []string
	}{
		{name: "作成直後", age: "3s", want: nil},
		{name: "59 秒", age: "59s", want: nil},
		{name: "60 秒ちょうど", age: "1m 0s", want: []string{"idle"}},
		{name: "十分に古い", age: "30m 0s", want: []string{"idle"}},
		// 経過時間を読めない場合は 0 = 作成直後の扱い(安全側)。
		{name: "読めない書式", age: "しばらく", want: nil},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			cleaner, sessions, _, _ := newCleaner()
			sessions.sessionsOut = "idle [Created " + tt.age + " ago] \n"

			got, err := cleaner.Clean(false)
			if err != nil {
				t.Fatalf("Clean() = %v", err)
			}
			if !slices.Equal(got.Plan.DetachedSessions, tt.want) {
				t.Errorf("detached = %v, want %v", got.Plan.DetachedSessions, tt.want)
			}
		})
	}
}

// TestCleanTreatsUnreadableClientListAsAttached は応答の形が想定と違う
// セッションへ触れないことを確かめる(指摘 7 と同じ安全側の倒し方)。
func TestCleanTreatsUnreadableClientListAsAttached(t *testing.T) {
	t.Parallel()

	cleaner, sessions, _, _ := newCleaner()
	// 見出し行が無い = 誰も居ないとは断定できない。
	sessions.clients["idle"] = ""

	got, err := cleaner.Clean(false)
	if err != nil {
		t.Fatalf("Clean() = %v", err)
	}
	if len(got.Plan.DetachedSessions) != 0 {
		t.Errorf("detached = %v, want 空", got.Plan.DetachedSessions)
	}
}

// TestCleanStopsCheckingWhenBudgetIsSpent は確認の予算を使い切ったら
// 残りを次回へ回すことを確かめる(指摘 10)。
//
// この掃除はセッションの起動前に走る。確認は 1 回あたり最大 10 秒かかり
// うるので、対象が増えると起動がそのぶん待たされる。
func TestCleanStopsCheckingWhenBudgetIsSpent(t *testing.T) {
	t.Parallel()

	sessions := &fakeSessionStore{
		sessionsOut: "a [Created 10m 0s ago] \nb [Created 10m 0s ago] \nc [Created 10m 0s ago] \n",
		clients: map[string]string{
			"a": emptyClients, "b": emptyClients, "c": emptyClients,
		},
		clientErrs:       map[string]error{},
		clientsOnRecheck: map[string]string{},
	}
	processes := &fakeProcessStore{
		out: "1 1 40:00 /opt/homebrew/bin/zellij --server /tmp/z/a\n" +
			"2 1 40:00 /home/u/.claude-conductor/bin/mdev pane dashboard\n" +
			"3 1 40:00 /opt/homebrew/bin/zellij --server /tmp/z/b\n" +
			"4 3 40:00 /home/u/.claude-conductor/bin/mdev pane dashboard\n" +
			"5 1 40:00 /opt/homebrew/bin/zellij --server /tmp/z/c\n" +
			"6 5 40:00 /home/u/.claude-conductor/bin/mdev pane dashboard\n",
		aliveAfterTerm: map[int]bool{},
	}
	// 1 回の確認で予算(10 秒)の半分を使う時計。
	clock := &steppingClock{now: testClock.Now(), step: 6 * time.Second}
	cleaner := &app.SessionCleaner{
		Sessions: sessions, Clients: sessions, Remover: sessions,
		Processes: processes, Signaler: processes, Sleeper: &fakeSleeper{},
		Clock: clock,
	}

	got, err := cleaner.Clean(true)
	if err != nil {
		t.Fatalf("Clean() = %v", err)
	}
	// 予算内に収まった分だけが対象になる。全部見ようとして起動を
	// 待たせてはならない。
	if len(got.Plan.DetachedSessions) >= 3 {
		t.Errorf("detached = %v, want 予算内の一部のみ", got.Plan.DetachedSessions)
	}
	if len(got.Plan.DetachedSessions) == 0 {
		t.Errorf("detached = 空, want 1 件以上(予算内は確認する)")
	}
}

// emptyClients は誰も開いていない応答である。
const emptyClients = "CLIENT_ID ZELLIJ_PANE_ID RUNNING_COMMAND\n"

// steppingClock は読むたびに step だけ進む時計である。予算の消費を模す。
type steppingClock struct {
	now  time.Time
	step time.Duration
	// reads は Now が呼ばれた回数。
	reads int
}

func (c *steppingClock) Now() time.Time {
	c.reads++
	// 最初の 1 回(締切の計算)では進めない。
	if c.reads == 1 {
		return c.now
	}
	c.now = c.now.Add(c.step)
	return c.now
}
