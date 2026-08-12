package app

import (
	"sort"
	"time"

	"github.com/k-kudo-hub/mdev-go/internal/domain"
)

// cleanupBudget は掃除 1 回に使ってよい時間の合計である。
//
// この掃除はセッションの起動前に走る。zellij の呼び出しは 1 回あたり最大
// 10 秒かかりうるので、対象が増えるとそのぶん起動が待たされる。**選ぶ側と
// 実行する側で別々の予算を持たせない。** 別々にすると合計は 2 倍になり、
// さらにゾンビの猶予(3 秒)が上に乗る。ここで切った 1 本の締切を全体で
// 共有し、超えた分は今回見送る(次回の掃除が拾う)。
const cleanupBudget = 10 * time.Second

// zombieGrace は TERM を送ってから KILL へ進むまでの猶予である。
//
// zellij サーバには後始末(ソケットの削除)の機会を与えたい。一方で掃除は
// セッションの起動前に走るため、待ちすぎると起動が遅れる。
const zombieGrace = 3 * time.Second

// ZombieServer は止める対象の zellij サーバ 1 つである。
//
// 中身は domain.ZellijServer と同じだが、cli / tui は app にしか依存
// できない(ADR-0002)ため、境界に出す型は app が持つ。
type ZombieServer struct {
	// PID はサーバのプロセス ID。
	PID int
	// Session はサーバが持つセッション名。
	Session string
	// Command は選んだ時点のコマンド行。送る直前の照合に使う。
	Command string
}

// CleanupPlan は掃除で何をするかである。
//
// dry-run はこれを組み立てて表示するだけで実行しない。実行時も同じものを
// 組み立ててから動くので、表示と実際の対象が食い違わない。
type CleanupPlan struct {
	// ExitedSessions は削除する終了済みセッションの名前。
	ExitedSessions []string
	// DetachedSessions は終了させて削除する、誰も開いていない
	// mdev 管理セッションの名前。
	DetachedSessions []string
	// ZombieServers は止める zellij サーバ。
	ZombieServers []ZombieServer
	// OrphanClients は止める孤児 `zellij action`。
	OrphanClients []OrphanClient
}

// OrphanClient は親を失った `zellij action` 1 つである。
type OrphanClient struct {
	// PID はプロセス ID。
	PID int
	// Command は選んだ時点のコマンド行。送る直前の照合に使う。
	Command string
}

// CleanupCounts は実際に片付けた件数である。
//
// 計画の件数とは別に持つ。**予定と実績は食い違う。** 消す直前の再確認で
// 飛ばしたもの、予算切れで見送ったもの、失敗したものがあるためで、
// 計画の件数をそのまま報告すると「掃除した」と嘘をつくことになる。
type CleanupCounts struct {
	ExitedSessions   int
	DetachedSessions int
	ZombieServers    int
	OrphanClients    int
}

// IsEmpty は 1 件も片付けなかったかを返す。
func (c CleanupCounts) IsEmpty() bool {
	return c.ExitedSessions == 0 && c.DetachedSessions == 0 &&
		c.ZombieServers == 0 && c.OrphanClients == 0
}

// IsEmpty は掃除する対象が 1 つも無いかを返す。
func (p CleanupPlan) IsEmpty() bool {
	return len(p.ExitedSessions) == 0 && len(p.DetachedSessions) == 0 &&
		len(p.ZombieServers) == 0 && len(p.OrphanClients) == 0
}

// CleanupResult は掃除の結果である。
type CleanupResult struct {
	// Plan は掃除の対象。
	Plan CleanupPlan
	// Done は実際に片付けた件数である。dry-run では 0 のままになる。
	Done CleanupCounts
	// DryRun は実行しなかったことを表す。
	DryRun bool
}

// SessionCleaner は溜まった zellij セッションとプロセスを片付ける。
//
// zellij はウィンドウを閉じてもセッションを残す。mdev は毎回新しい名前で
// セッションを作るため、閉じるたびに detached なセッションが 1 つ増え、
// その中では 5 つのペインがポーリングを続ける。これが積み上がると zellij
// サーバが劣化し、タブ作成の遅延・分割の崩れとして表に出る。
//
// **使用中(アタッチあり)のセッションには絶対に触れない。** 判断に必要な
// 情報が取れなかった場合も触れない側へ倒す。掃除は最善努力であり、
// 取りこぼしは次回また拾えるが、使用中のものを落とすと作業が飛ぶ。
//
// detached なセッションを終了させてよいのは、mdev のタスクがレジストリから
// --resume 付きで復元されるためである。レジストリと pending には一切
// 触れない(掃除の対象はプロセスとセッションのメタデータだけ)。
type SessionCleaner struct {
	Sessions  SessionLister
	Clients   SessionClientLister
	Remover   SessionRemover
	Processes ProcessLister
	Signaler  ProcessSignaler
	Sleeper   Sleeper
	Clock     Clock
}

// Clean は掃除を行い、何をした(する)かを返す。
//
// dryRun が true のときは対象を数えるだけで、一切実行しない。
//
// 判断材料(セッション一覧・プロセス一覧)が取れなかった場合は error を
// 返す。この error で呼び出し側を止めてはならない用途(--auto)があるため、
// 握り潰すかどうかは呼び出し側が決める。
func (c *SessionCleaner) Clean(dryRun bool) (CleanupResult, error) {
	// 締切は選ぶ前に 1 本だけ決め、実行まで通して使う(cleanupBudget)。
	deadline := c.Clock.Now().Add(cleanupBudget)

	plan, err := c.plan(deadline)
	if err != nil {
		return CleanupResult{}, err
	}
	result := CleanupResult{Plan: plan, DryRun: dryRun}
	if dryRun {
		return result, nil
	}
	result.Done = c.apply(plan, deadline)
	return result, nil
}

// expired は締切を過ぎたかを返す。
func (c *SessionCleaner) expired(deadline time.Time) bool {
	return !c.Clock.Now().Before(deadline)
}

// plan は掃除の対象を組み立てる。
func (c *SessionCleaner) plan(deadline time.Time) (CleanupPlan, error) {
	sessionsOut, err := c.Sessions.ListSessions()
	if err != nil {
		return CleanupPlan{}, err
	}
	processesOut, err := c.Processes.ListProcesses()
	if err != nil {
		return CleanupPlan{}, err
	}

	sessions := domain.ParseSessionList(sessionsOut)
	processes := domain.ParseProcessList(processesOut)
	managed := domain.MdevManagedSessions(processes)

	return CleanupPlan{
		ExitedSessions:   domain.ExitedSessionNames(sessions),
		DetachedSessions: c.detachedSessions(sessions, managed, deadline),
		ZombieServers:    toZombieServers(domain.ZombieServers(domain.ZellijServers(processes), sessions)),
		OrphanClients:    toOrphanClients(domain.OrphanZellijClients(processes)),
	}, nil
}

// detachedSessions は誰も開いていない mdev 管理セッションを返す。
//
// 対象を mdev 管理のものに絞るのは、手で作ったセッション(`dev` など)を
// 巻き込まないためである。それらは終了済みでない限り触れない。
//
// 作られてから CleanupMinAge に満たないセッションも外す。ペインが起動して
// attach されるまでの間は「誰も開いていない」ように見えるためで、ここで
// 撃つと今まさに開こうとしているセッションを落とす。
//
// 確認できなかったセッションは **アタッチありとして飛ばす**。誰も居ないと
// 誤って判断すると、使用中のセッションを終了させてしまう。飛ばす条件は
// 3 つある。list-clients が失敗した、応答の形が想定と違う、そして
// 確認の予算を使い切った、である。
func (c *SessionCleaner) detachedSessions(
	sessions []domain.SessionEntry, managed map[string]bool, deadline time.Time,
) []string {
	// **古いものから確かめる。** 予算が尽きると後ろは見送られるので、
	// 順番が固定だと末尾のセッションが毎回あぶれて永久に残る。放置が長い
	// ものほど片付ける価値が高いので、古い順に予算を使う。
	candidates := make([]domain.SessionEntry, 0, len(sessions))
	for _, session := range sessions {
		if session.Exited || !managed[session.Name] || session.Age < domain.CleanupMinAge {
			continue
		}
		candidates = append(candidates, session)
	}
	sort.SliceStable(candidates, func(i, j int) bool {
		return candidates[i].Age > candidates[j].Age
	})

	var detached []string
	for _, session := range candidates {
		// 予算を使い切ったら、残りは次回の掃除に任せる。
		if c.expired(deadline) {
			break
		}
		if c.attached(session.Name) {
			continue
		}
		detached = append(detached, session.Name)
	}
	return detached
}

// attached は今アタッチしているクライアントが居るかを返す。
// **判断できない場合は真を返す**(触れない側へ倒す)。
func (c *SessionCleaner) attached(name string) bool {
	out, err := c.Clients.ListClients(name)
	if err != nil {
		return true
	}
	count, ok := domain.ParseClientList(out)
	if !ok {
		return true
	}
	return count > 0
}

// apply は掃除を実行し、実際に片付けた件数を返す。
//
// **消す直前にもう一度確かめる。** 対象を選んでから実行するまでの間に、
// 利用者がセッションを開くことはありうる(掃除はセッションの起動前に走るので、
// まさにその瞬間が重なりやすい)。選んだ時点の判断だけで消すと、開いた
// ばかりのセッションを落とす。
//
// **締切を過ぎたら残りは次回に回す。** ここで粘っても起動が待たされるだけで、
// 取りこぼしは次の掃除が拾う。
//
// **個別の失敗は無視して先へ進む。** 1 件消せないことより、掃除全体が
// 途中で止まって蓄積が残るほうが害が大きい。消せなかったものは次回また
// 対象になる。
func (c *SessionCleaner) apply(plan CleanupPlan, deadline time.Time) CleanupCounts {
	var done CleanupCounts

	for _, name := range plan.DetachedSessions {
		if c.expired(deadline) {
			return done
		}
		// 選んでから今までの間に誰かが開いていないか、もう一度見る。
		//
		// ここから kill までのごく短い間(数百ミリ秒)は、依然として
		// 取りこぼしうる窓である。zellij の kill-session には
		// delete-session のような「動いていたら拒む」防御が無いため、
		// これ以上は縮められない。窓を最小にすることまでが打てる手で、
		// 万一巻き込んでもタスクはレジストリから復元される。
		if c.attached(name) {
			continue
		}
		// 終了させてからメタデータを消す。kill だけではセッションが
		// EXITED として残り、次の掃除でまた拾うことになる。
		//
		// 消すほうは --force を付けない(zellij が動作中の削除を拒む。
		// SessionController.DeleteSession を参照)。kill の直後でまだ
		// 動いていると見なされた場合は削除に失敗し、EXITED として残って
		// 次回の掃除が拾う。
		if err := c.Remover.KillSession(name); err != nil {
			continue
		}
		_ = c.Remover.DeleteSession(name)
		done.DetachedSessions++
	}

	for _, name := range c.stillExited(plan.ExitedSessions, deadline) {
		if c.expired(deadline) {
			return done
		}
		if err := c.Remover.DeleteSession(name); err != nil {
			continue
		}
		done.ExitedSessions++
	}

	// プロセスへ送る前に ps を引き直す。PID は使い回されるので、選んだ
	// ときと同じ PID が別のプロセスになっていることがある。
	commands := c.currentCommands()
	done.ZombieServers = c.stopZombies(plan.ZombieServers, commands, deadline)
	for _, orphan := range plan.OrphanClients {
		if c.expired(deadline) {
			return done
		}
		if !sameProcess(commands, orphan.PID, orphan.Command) {
			continue
		}
		if err := c.Signaler.Kill(orphan.PID); err != nil {
			continue
		}
		done.OrphanClients++
	}
	return done
}

// currentCommands は今動いているプロセスの PID とコマンド行の対応を返す。
// 引けなかった場合は nil を返し、そのときは何にもシグナルを送らない。
func (c *SessionCleaner) currentCommands() map[int]string {
	out, err := c.Processes.ListProcesses()
	if err != nil {
		return nil
	}
	return domain.ProcessCommands(domain.ParseProcessList(out))
}

// sameProcess は pid が今も同じコマンドで動いているかを返す。
//
// **一致を確かめられないものへは送らない。** PID の使い回しで無関係の
// プロセス(利用者のエディタや別のセッションのペイン)を殺しうる。
func sameProcess(commands map[int]string, pid int, command string) bool {
	current, ok := commands[pid]
	return ok && current == command
}

// stillExited は今も終了済みであるものだけを返す。
//
// 終了済みのセッションは「attach すると復活する」状態でもある。選んでから
// 実行するまでの間に利用者が復活させていたら、それは使用中のセッションで
// あって消してはならない。
//
// 一覧を引き直せなかったときは 1 件も返さない(確かめられないなら消さない)。
func (c *SessionCleaner) stillExited(names []string, deadline time.Time) []string {
	if len(names) == 0 || c.expired(deadline) {
		return nil
	}
	out, err := c.Sessions.ListSessions()
	if err != nil {
		return nil
	}
	exited := map[string]bool{}
	for _, name := range domain.ExitedSessionNames(domain.ParseSessionList(out)) {
		exited[name] = true
	}

	var still []string
	for _, name := range names {
		if exited[name] {
			still = append(still, name)
		}
	}
	return still
}

// stopZombies はゾンビサーバを止め、止めた件数を返す。
//
// まず TERM を送って後始末の機会を与え、猶予の後にまだ居るものだけ KILL する。
// いきなり KILL するとソケットのファイルが残り、zellij が次に同じ名前の
// セッションを作るときに失敗しうる。
//
// 送る前に PID とコマンド行の一致を確かめる。PID は使い回されるので、
// 選んだときと同じ PID が別のプロセスになっていることがある。
func (c *SessionCleaner) stopZombies(
	servers []ZombieServer, commands map[int]string, deadline time.Time,
) int {
	targets := make([]ZombieServer, 0, len(servers))
	for _, server := range servers {
		if sameProcess(commands, server.PID, server.Command) {
			targets = append(targets, server)
		}
	}
	if len(targets) == 0 || c.expired(deadline) {
		return 0
	}

	stopped := 0
	for _, server := range targets {
		if err := c.Signaler.Terminate(server.PID); err == nil {
			stopped++
		}
	}
	// 全部まとめて待つ。1 件ずつ待つと台数ぶん猶予が積み上がる。
	c.Sleeper.Sleep(zombieGrace)
	for _, server := range targets {
		if c.Signaler.IsAlive(server.PID) {
			_ = c.Signaler.Kill(server.PID)
		}
	}
	return stopped
}

// toZombieServers は domain の型を境界の型へ移し替える。
func toZombieServers(servers []domain.ZellijServer) []ZombieServer {
	if len(servers) == 0 {
		return nil
	}
	out := make([]ZombieServer, 0, len(servers))
	for _, server := range servers {
		out = append(out, ZombieServer{
			PID: server.PID, Session: server.Session, Command: server.Command,
		})
	}
	return out
}

// toOrphanClients は孤児クライアントを境界の型へ移し替える。
func toOrphanClients(entries []domain.ProcessEntry) []OrphanClient {
	if len(entries) == 0 {
		return nil
	}
	out := make([]OrphanClient, 0, len(entries))
	for _, entry := range entries {
		out = append(out, OrphanClient{PID: entry.PID, Command: entry.Command})
	}
	return out
}
