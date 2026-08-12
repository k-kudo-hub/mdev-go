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
	// outOnRecheck は 2 回目以降の ps で返す出力である。
	// 「選んだ後に PID が別のプロセスへ回った」状況を作るために使う。
	outOnRecheck string
	// listCalls は ps の呼び出し回数。
	listCalls int

	calls []string
}

func (p *fakeProcessStore) ListProcesses() (string, error) {
	p.listCalls++
	if p.listCalls > 1 && p.outOnRecheck != "" {
		return p.outOnRecheck, p.err
	}
	return p.out, p.err
}

func (p *fakeProcessStore) Terminate(pid int) error {
	p.calls = append(p.calls, fmt.Sprintf("term %d", pid))
	return nil
}

func (p *fakeProcessStore) Kill(pid int) error {
	p.calls = append(p.calls, fmt.Sprintf("kill %d", pid))
	return nil
}

func (p *fakeProcessStore) IsAlive(pid int) bool { return p.aliveAfterTerm[pid] }

// fakeTraces は mdev の痕跡の有無を返す代役である。
type fakeTraces struct {
	// has は痕跡があるセッション名。nil ならすべて痕跡ありとして扱う。
	has map[string]bool
}

func (t fakeTraces) HasTrace(session string) bool {
	if t.has == nil {
		return true
	}
	return t.has[session]
}

// fakeSockets は自分から見えるソケット置き場を返す代役である。
type fakeSockets struct{ dir string }

func (s fakeSockets) SocketDir() string { return s.dir }

// testSocketDir は fixture のソケットが置かれている場所である。
const testSocketDir = "/tmp/z"

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
func newCleanFakes() (*fakeSessionStore, *fakeProcessStore) {
	sessions := &fakeSessionStore{
		sessionsOut: cleanSessionList,
		clients: map[string]string{
			"in-use": "CLIENT_ID ZELLIJ_PANE_ID RUNNING_COMMAND\n1  terminal_3  mdev pane news\n",
			"idle":   "CLIENT_ID ZELLIJ_PANE_ID RUNNING_COMMAND\n",
			"manual": "CLIENT_ID ZELLIJ_PANE_ID RUNNING_COMMAND\n",
			// ゾンビにも「誰も居ない」ことを確かめさせる。
			"zombie": "CLIENT_ID ZELLIJ_PANE_ID RUNNING_COMMAND\n",
		},
		clientErrs:       map[string]error{},
		clientsOnRecheck: map[string]string{},
	}
	processes := &fakeProcessStore{out: cleanProcessList, aliveAfterTerm: map[int]bool{}}
	return sessions, processes
}

func newCleaner() (*app.SessionCleaner, *fakeSessionStore, *fakeProcessStore, *fakeSleeper) {
	sessions, processes := newCleanFakes()
	sleeper := &fakeSleeper{}
	return &app.SessionCleaner{
		Sessions:  sessions,
		Clients:   sessions,
		Remover:   sessions,
		Processes: processes,
		Signaler:  processes,
		Sockets:   fakeSockets{dir: testSocketDir},
		Traces:    fakeTraces{},
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
	want := []app.ZombieServer{{
		PID: 400, Session: "zombie",
		Command: "/opt/homebrew/bin/zellij --server /tmp/z/zombie",
	}}
	if !slices.Equal(got.Plan.ZombieServers, want) {
		t.Errorf("ゾンビ = %v, want %v", got.Plan.ZombieServers, want)
	}
	want500 := []app.OrphanClient{{PID: 500, Command: "zellij action list-tabs"}}
	if !slices.Equal(got.Plan.OrphanClients, want500) {
		t.Errorf("孤児 = %v, want %v", got.Plan.OrphanClients, want500)
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
		{OrphanClients: []app.OrphanClient{{PID: 1}}},
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
		Sockets: fakeSockets{dir: testSocketDir}, Traces: fakeTraces{}, Clock: testClock,
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
		Sockets: fakeSockets{dir: testSocketDir}, Traces: fakeTraces{}, Clock: clock,
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

// TestCleanVerifiesPIDIdentityBeforeSignal は PID の使い回しで無関係の
// プロセスへシグナルを送らないことを確かめる(指摘 5)。
//
// 選んでから送るまでの間にプロセスが終わり、同じ PID が別のプロセス
// (利用者のエディタや別セッションのペイン)に割り当たることはありうる。
func TestCleanVerifiesPIDIdentityBeforeSignal(t *testing.T) {
	t.Parallel()

	cleaner, _, processes, _ := newCleaner()
	// 送る直前に引き直すと、ゾンビ(400)も孤児(500)も別のプロセスに
	// 変わっている。
	processes.outOnRecheck = "  PID  PPID     ELAPSED COMMAND\n" +
		"100     1 40:00 /opt/homebrew/bin/zellij --server /tmp/z/in-use\n" +
		"400     1 00:10 nvim /Users/kazuto/memo.md\n" +
		"500     1 00:10 -zsh\n"

	got, err := cleaner.Clean(false)
	if err != nil {
		t.Fatalf("Clean() = %v", err)
	}
	// 計画には載る(選んだ時点では確かにゾンビと孤児だった)。
	if len(got.Plan.ZombieServers) != 1 || len(got.Plan.OrphanClients) != 1 {
		t.Fatalf("計画 = %+v", got.Plan)
	}
	// しかし送らない。
	if len(processes.calls) != 0 {
		t.Errorf("別のプロセスへシグナルを送りました: %v", processes.calls)
	}
	if got.Done.ZombieServers != 0 || got.Done.OrphanClients != 0 {
		t.Errorf("片付けた件数 = %+v, want 0 件", got.Done)
	}
}

// TestCleanSignalsWhenIdentityMatches は同じプロセスのままなら送ることを
// 確かめる(照合が正しいものまで止めていないこと)。
func TestCleanSignalsWhenIdentityMatches(t *testing.T) {
	t.Parallel()

	cleaner, _, processes, _ := newCleaner()
	got, err := cleaner.Clean(false)
	if err != nil {
		t.Fatalf("Clean() = %v", err)
	}
	if want := []string{"term 400", "kill 500"}; !slices.Equal(processes.calls, want) {
		t.Errorf("シグナル = %v, want %v", processes.calls, want)
	}
	if got.Done.ZombieServers != 1 || got.Done.OrphanClients != 1 {
		t.Errorf("片付けた件数 = %+v", got.Done)
	}
}

// TestCleanReportsExecutedCounts は「予定」ではなく「実績」を返すことを
// 確かめる(指摘 7)。
//
// 消す直前の再確認で飛ばしたものを数に入れると、--auto が「掃除した」と
// 嘘をつくことになる。
func TestCleanReportsExecutedCounts(t *testing.T) {
	t.Parallel()

	cleaner, sessions, _, _ := newCleaner()
	// detached は直前に開かれ、終了済みの 1 つは復活した。
	sessions.clientsOnRecheck["idle"] = attachedClients
	sessions.sessionsOnRecheck = "in-use [Created 40m 0s ago] \n" +
		"gone-1 [Created 12m 0s ago] \n" +
		"gone-2 [Created 12m 0s ago] (EXITED - attach to resurrect)\n"

	got, err := cleaner.Clean(false)
	if err != nil {
		t.Fatalf("Clean() = %v", err)
	}
	// 計画は 2 件 + 1 件、実績は 1 件 + 0 件。
	if len(got.Plan.ExitedSessions) != 2 || len(got.Plan.DetachedSessions) != 1 {
		t.Fatalf("計画 = %+v", got.Plan)
	}
	if got.Done.ExitedSessions != 1 {
		t.Errorf("終了済みの実績 = %d, want 1", got.Done.ExitedSessions)
	}
	if got.Done.DetachedSessions != 0 {
		t.Errorf("detached の実績 = %d, want 0", got.Done.DetachedSessions)
	}
}

// TestCleanChecksOldestSessionsFirst は古いものから確かめることを
// 確かめる(指摘 6)。
//
// 予算が尽きると後ろは見送られるので、順番が固定だと末尾のセッションが
// 毎回あぶれて永久に残る。放置が長いものほど片付ける価値が高い。
func TestCleanChecksOldestSessionsFirst(t *testing.T) {
	t.Parallel()

	sessions := &fakeSessionStore{
		sessionsOut: "young [Created 2m 0s ago] \n" +
			"oldest [Created 3days 0s ago] \n" +
			"middle [Created 5h 0m 0s ago] \n",
		clients: map[string]string{
			"young": emptyClients, "oldest": emptyClients, "middle": emptyClients,
		},
		clientErrs:       map[string]error{},
		clientsOnRecheck: map[string]string{},
	}
	processes := &fakeProcessStore{
		out: "1 1 40:00 /opt/homebrew/bin/zellij --server /tmp/z/young\n" +
			"2 1 40:00 /home/u/.claude-conductor/bin/mdev pane dashboard\n" +
			"3 1 40:00 /opt/homebrew/bin/zellij --server /tmp/z/oldest\n" +
			"4 3 40:00 /home/u/.claude-conductor/bin/mdev pane dashboard\n" +
			"5 1 40:00 /opt/homebrew/bin/zellij --server /tmp/z/middle\n" +
			"6 5 40:00 /home/u/.claude-conductor/bin/mdev pane dashboard\n",
		aliveAfterTerm: map[int]bool{},
	}
	cleaner := &app.SessionCleaner{
		Sessions: sessions, Clients: sessions, Remover: sessions,
		Processes: processes, Signaler: processes, Sleeper: &fakeSleeper{},
		Sockets: fakeSockets{dir: testSocketDir}, Traces: fakeTraces{}, Clock: testClock,
	}

	got, err := cleaner.Clean(true)
	if err != nil {
		t.Fatalf("Clean() = %v", err)
	}
	want := []string{"oldest", "middle", "young"}
	if !slices.Equal(got.Plan.DetachedSessions, want) {
		t.Errorf("確かめる順 = %v, want %v(古い順)", got.Plan.DetachedSessions, want)
	}
}

// TestCleanAbortsWhenSessionListIsUnreadable は **実際に起きた事故の再現**
// である。
//
// PATH に何もしない zellij スタブ(rc=0・無出力)が入った状態で --auto が
// 走り、使用中セッションのサーバを TERM → KILL した。セッション一覧が
// 空に見えると、生きているセッションのサーバがすべて「一覧に出ないサーバ」
// = ゾンビに見えるためである。
//
// 判断材料が空のときは掃除全体を中止し、**1 件もシグナルを送らない**。
func TestCleanAbortsWhenSessionListIsUnreadable(t *testing.T) {
	t.Parallel()

	cleaner, sessions, processes, _ := newCleaner()
	// スタブの zellij が返す状態(adapter が判断不能として error にする)。
	sessions.sessionsOut = ""
	sessions.sessionsErr = errors.New("セッションの有無を判断できません")

	if _, err := cleaner.Clean(false); err == nil {
		t.Fatal("判断材料が無いのに掃除を続けました")
	}
	if len(processes.calls) != 0 {
		t.Errorf("シグナルを送りました: %v", processes.calls)
	}
	if len(sessions.calls) != 0 {
		t.Errorf("セッションを操作しました: %v", sessions.calls)
	}
}

// TestCleanNeverKillsAttachedZombieCandidate は、一覧に出ないサーバでも
// クライアントが居るなら撃たないことを確かめる。
//
// detached 経路と同じ「触れない側へ倒す」原則をゾンビ経路にも通す。
func TestCleanNeverKillsAttachedZombieCandidate(t *testing.T) {
	t.Parallel()

	cleaner, sessions, processes, _ := newCleaner()
	// 一覧には出ないが、クライアントが 1 つ居る。
	sessions.clients["zombie"] = attachedClients

	got, err := cleaner.Clean(false)
	if err != nil {
		t.Fatalf("Clean() = %v", err)
	}
	if len(got.Plan.ZombieServers) != 0 {
		t.Errorf("ゾンビ = %+v, want 空(誰か開いている)", got.Plan.ZombieServers)
	}
	for _, call := range processes.calls {
		if call == "term 400" || call == "kill 400" {
			t.Errorf("開かれているサーバへシグナルを送りました: %v", processes.calls)
		}
	}
}

// TestCleanSkipsZombieWhenClientsUnknown は応答しないセッションのサーバへ
// 撃たないことを確かめる。
//
// この条件により、ソケットごと死んだサーバは掃除できなくなる。それでも
// 撃たないほうを選ぶ(使用中セッションを落とす事故と、取りこぼしとでは
// 害の大きさが釣り合わない)。
func TestCleanSkipsZombieWhenClientsUnknown(t *testing.T) {
	t.Parallel()

	cleaner, sessions, processes, _ := newCleaner()
	sessions.clientErrs["zombie"] = errors.New("session not found")

	got, err := cleaner.Clean(false)
	if err != nil {
		t.Fatalf("Clean() = %v", err)
	}
	if len(got.Plan.ZombieServers) != 0 {
		t.Errorf("ゾンビ = %+v, want 空(確かめられない)", got.Plan.ZombieServers)
	}
	// 孤児クライアント(500)の掃除はセッションと無関係なので続く。
	for _, call := range processes.calls {
		if call == "term 400" || call == "kill 400" {
			t.Errorf("確かめられないサーバへシグナルを送りました: %v", processes.calls)
		}
	}
}

// TestCleanScopesZombiesToOwnSocketDir は自分から見える範囲の外にある
// サーバを候補にしないことを確かめる。
//
// zellij の list-sessions は自分の一時ディレクトリ配下しか見ないため、
// 別の一時ディレクトリで起動されたサーバは必ず「一覧に出ない」ように
// 見える。それはゾンビではなく、こちらから見えていないだけである。
func TestCleanScopesZombiesToOwnSocketDir(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		socketDir string
		wantCount int
	}{
		{name: "同じ置き場なら候補になる", socketDir: testSocketDir, wantCount: 1},
		{name: "別の置き場は候補にしない", socketDir: "/var/folders/other/T/zellij-501", wantCount: 0},
		// 範囲を決められないなら 1 件も撃たない。
		{name: "置き場が分からなければ候補にしない", socketDir: "", wantCount: 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			sessions, processes := newCleanFakes()
			cleaner := &app.SessionCleaner{
				Sessions: sessions, Clients: sessions, Remover: sessions,
				Processes: processes, Signaler: processes, Sleeper: &fakeSleeper{},
				Sockets: fakeSockets{dir: tt.socketDir}, Traces: fakeTraces{}, Clock: testClock,
			}

			got, err := cleaner.Clean(true)
			if err != nil {
				t.Fatalf("Clean() = %v", err)
			}
			if len(got.Plan.ZombieServers) != tt.wantCount {
				t.Errorf("ゾンビ = %+v, want %d 件", got.Plan.ZombieServers, tt.wantCount)
			}
		})
	}
}

// TestCleanOnlyDeletesExitedSessionsMdevTouched は、mdev が扱った跡の無い
// 終了済みセッションに触れないことを確かめる。
//
// 終了済みセッションのメタデータは attach で復活させるための資産である。
// mdev がそれを使わないのは mdev 自身の設計判断であって、利用者が手で
// 作ったセッションにまで押し付けてよいものではない。この掃除はセッションを
// 開くたびに走るので、無条件に消すと利用者の `dev` のようなセッションの
// 復活先が毎回失われる。
func TestCleanOnlyDeletesExitedSessionsMdevTouched(t *testing.T) {
	t.Parallel()

	sessions, processes := newCleanFakes()
	cleaner := &app.SessionCleaner{
		Sessions: sessions, Clients: sessions, Remover: sessions,
		Processes: processes, Signaler: processes, Sleeper: &fakeSleeper{},
		Sockets: fakeSockets{dir: testSocketDir},
		// gone-1 は mdev が扱った跡があるが、gone-2 は利用者のセッション。
		Traces: fakeTraces{has: map[string]bool{"gone-1": true}},
		Clock:  testClock,
	}

	got, err := cleaner.Clean(false)
	if err != nil {
		t.Fatalf("Clean() = %v", err)
	}
	if want := []string{"gone-1"}; !slices.Equal(got.Plan.ExitedSessions, want) {
		t.Errorf("終了済み = %v, want %v(痕跡のあるものだけ)", got.Plan.ExitedSessions, want)
	}
	for _, call := range sessions.calls {
		if call == "delete gone-2" {
			t.Errorf("利用者のセッションを消しました: %v", sessions.calls)
		}
	}
	if !slices.Contains(sessions.calls, "delete gone-1") {
		t.Errorf("mdev のセッションが消えていません: %v", sessions.calls)
	}
}

// TestCleanLeavesAllExitedWithoutTraces は痕跡が 1 つも無ければ終了済みを
// 1 件も消さないことを確かめる(mdev を入れたばかりの環境など)。
func TestCleanLeavesAllExitedWithoutTraces(t *testing.T) {
	t.Parallel()

	sessions, processes := newCleanFakes()
	cleaner := &app.SessionCleaner{
		Sessions: sessions, Clients: sessions, Remover: sessions,
		Processes: processes, Signaler: processes, Sleeper: &fakeSleeper{},
		Sockets: fakeSockets{dir: testSocketDir},
		Traces:  fakeTraces{has: map[string]bool{}},
		Clock:   testClock,
	}

	got, err := cleaner.Clean(false)
	if err != nil {
		t.Fatalf("Clean() = %v", err)
	}
	if len(got.Plan.ExitedSessions) != 0 {
		t.Errorf("終了済み = %v, want 空", got.Plan.ExitedSessions)
	}
	for _, call := range sessions.calls {
		if strings.HasPrefix(call, "delete gone-") {
			t.Errorf("痕跡が無いのに消しました: %v", sessions.calls)
		}
	}
}
