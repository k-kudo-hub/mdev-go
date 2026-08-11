package app

import (
	"time"

	"github.com/k-kudo-hub/mdev-go/internal/domain"
)

// zombieGrace は TERM を送ってから KILL へ進むまでの猶予である。
//
// zellij サーバには後始末(ソケットの削除)の機会を与えたい。一方で掃除は
// セッションの起動前に走るため、待ちすぎると起動が遅れる。
const zombieGrace = 3 * time.Second

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
	ZombieServers []domain.ZellijServer
	// OrphanClients は止める孤児 `zellij action` の PID。
	OrphanClients []int
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
}

// Clean は掃除を行い、何をした(する)かを返す。
//
// dryRun が true のときは対象を数えるだけで、一切実行しない。
//
// 判断材料(セッション一覧・プロセス一覧)が取れなかった場合は error を
// 返す。この error で呼び出し側を止めてはならない用途(--auto)があるため、
// 握り潰すかどうかは呼び出し側が決める。
func (c *SessionCleaner) Clean(dryRun bool) (CleanupResult, error) {
	plan, err := c.plan()
	if err != nil {
		return CleanupResult{}, err
	}
	result := CleanupResult{Plan: plan, DryRun: dryRun}
	if dryRun {
		return result, nil
	}
	c.apply(plan)
	return result, nil
}

// plan は掃除の対象を組み立てる。
func (c *SessionCleaner) plan() (CleanupPlan, error) {
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
		DetachedSessions: c.detachedSessions(sessions, managed),
		ZombieServers:    domain.ZombieServers(domain.ZellijServers(processes), sessions),
		OrphanClients:    orphanPIDs(domain.OrphanZellijClients(processes)),
	}, nil
}

// detachedSessions は誰も開いていない mdev 管理セッションを返す。
//
// 対象を mdev 管理のものに絞るのは、手で作ったセッション(`dev` など)を
// 巻き込まないためである。それらは終了済みでない限り触れない。
//
// list-clients が失敗したセッションは **アタッチありとして飛ばす**。
// 誰も居ないと誤って判断すると、使用中のセッションを終了させてしまう。
func (c *SessionCleaner) detachedSessions(sessions []domain.SessionEntry, managed map[string]bool) []string {
	var detached []string
	for _, name := range domain.AliveSessionNames(sessions) {
		if !managed[name] {
			continue
		}
		out, err := c.Clients.ListClients(name)
		if err != nil {
			continue
		}
		if domain.AttachedClientCount(out) == 0 {
			detached = append(detached, name)
		}
	}
	return detached
}

// apply は掃除を実行する。
//
// **個別の失敗は無視して先へ進む。** 1 件消せないことより、掃除全体が
// 途中で止まって蓄積が残るほうが害が大きい。消せなかったものは次回また
// 対象になる。
func (c *SessionCleaner) apply(plan CleanupPlan) {
	for _, name := range plan.DetachedSessions {
		// 終了させてからメタデータを消す。kill だけではセッションが
		// EXITED として残り、次の掃除でまた拾うことになる。
		_ = c.Remover.KillSession(name)
		_ = c.Remover.DeleteSession(name)
	}
	for _, name := range plan.ExitedSessions {
		_ = c.Remover.DeleteSession(name)
	}
	c.stopZombies(plan.ZombieServers)
	for _, pid := range plan.OrphanClients {
		_ = c.Signaler.Kill(pid)
	}
}

// stopZombies はゾンビサーバを止める。
//
// まず TERM を送って後始末の機会を与え、猶予の後にまだ居るものだけ KILL する。
// いきなり KILL するとソケットのファイルが残り、zellij が次に同じ名前の
// セッションを作るときに失敗しうる。
func (c *SessionCleaner) stopZombies(servers []domain.ZellijServer) {
	if len(servers) == 0 {
		return
	}
	for _, server := range servers {
		_ = c.Signaler.Terminate(server.PID)
	}
	// 全部まとめて待つ。1 件ずつ待つと台数ぶん猶予が積み上がる。
	c.Sleeper.Sleep(zombieGrace)
	for _, server := range servers {
		if c.Signaler.IsAlive(server.PID) {
			_ = c.Signaler.Kill(server.PID)
		}
	}
}

// orphanPIDs は孤児クライアントの PID だけを取り出す。
func orphanPIDs(entries []domain.ProcessEntry) []int {
	if len(entries) == 0 {
		return nil
	}
	pids := make([]int, 0, len(entries))
	for _, entry := range entries {
		pids = append(pids, entry.PID)
	}
	return pids
}
