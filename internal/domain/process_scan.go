package domain

import (
	"strconv"
	"strings"
)

// ps の出力から zellij のプロセスを見分けるための目印。
const (
	// zellijServerMarker は zellij のサーバプロセスの目印である。
	// 実出力: `zellij --server /var/folders/.../zellij-501/.../<session>`
	zellijServerMarker = "--server"
	// zellijActionMarker は `zellij action ...` のクライアントの目印である。
	zellijActionMarker = "zellij action"
	// mdevPaneMarker は mdev が管理するセッションであることの目印である。
	// ペインの中身が `<CONDUCTOR_HOME>/bin/mdev pane <name>` で起動される。
	mdevPaneMarker = "/bin/mdev pane"
	// zellijBinaryMarker は zellij の実行ファイルであることの目印である。
	// 絶対パスで出ることもあるため、部分一致で見る。
	zellijBinaryMarker = "zellij"
)

// orphanPPID は親を失ったプロセスの親 PID である(init に引き取られた状態)。
const orphanPPID = 1

// ProcessEntry は `ps -axo pid,ppid,command` の 1 行である。
type ProcessEntry struct {
	PID     int
	PPID    int
	Command string
}

// ParseProcessList は `ps -axo pid,ppid,command` の出力を読む。
//
// 見出し行と、先頭 2 列が数値でない行は読み飛ばす。command は 3 列目以降を
// そのまま(空白を含めて)持つ。
//
// 読めない行を error にしないのは、掃除が最善努力であるためと、判断できない
// プロセスを対象から外すほうが安全側に倒れるためである。
func ParseProcessList(out string) []ProcessEntry {
	var entries []ProcessEntry
	for _, line := range strings.Split(out, "\n") {
		fields := strings.Fields(line)
		if len(fields) < 3 {
			continue
		}
		pid, err := strconv.Atoi(fields[0])
		if err != nil {
			continue
		}
		ppid, err := strconv.Atoi(fields[1])
		if err != nil {
			continue
		}
		// command は元の空白のまま残したいので、3 列目の位置から切り出す。
		command := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(line), fields[0]))
		command = strings.TrimSpace(strings.TrimPrefix(command, fields[1]))
		entries = append(entries, ProcessEntry{PID: pid, PPID: ppid, Command: command})
	}
	return entries
}

// ZellijServer は動いている zellij サーバ 1 つである。
type ZellijServer struct {
	// PID はサーバのプロセス ID。
	PID int
	// Session はサーバが持つセッション名。
	//
	// サーバのコマンド行の末尾がソケットのパスで、その最後の要素が
	// セッション名になる(実出力の
	// `--server /var/folders/.../zellij-501/contract_version_1/mdev-go-224042`)。
	Session string
}

// ZellijServers は動いている zellij サーバを返す。
//
// セッション名を取り出せない行(ソケットのパスが無いなど)は落とす。
// 名前が分からないサーバを掃除の対象にすると、使用中のものを巻き込む。
func ZellijServers(entries []ProcessEntry) []ZellijServer {
	var servers []ZellijServer
	for _, entry := range entries {
		if !strings.Contains(entry.Command, zellijBinaryMarker) ||
			!strings.Contains(entry.Command, zellijServerMarker) {
			continue
		}
		fields := strings.Fields(entry.Command)
		socket := fields[len(fields)-1]
		// `--server` の直後が無い(パスを取れない)場合は諦める。
		if socket == zellijServerMarker {
			continue
		}
		name := socket
		if i := strings.LastIndex(socket, "/"); i >= 0 {
			name = socket[i+1:]
		}
		if name == "" {
			continue
		}
		servers = append(servers, ZellijServer{PID: entry.PID, Session: name})
	}
	return servers
}

// MdevManagedSessions は mdev が管理しているセッション名の集合を返す。
//
// 判定は「そのセッションのサーバの子に `bin/mdev pane` が居るか」で行う。
// レジストリを引かずに済むうえ、手で作った `dev` のようなセッションを
// 巻き込まない。実機ではペインのプロセスがサーバの直接の子になる。
//
// この判定を外すと、掃除が利用者の手動セッションまで kill しうる。
// 判定できないもの(サーバが見つからない等)は「mdev のものではない」に
// 倒れるため、掃除の対象から外れる。
func MdevManagedSessions(entries []ProcessEntry) map[string]bool {
	byPID := make(map[int]string, len(entries))
	for _, server := range ZellijServers(entries) {
		byPID[server.PID] = server.Session
	}

	managed := map[string]bool{}
	for _, entry := range entries {
		if !strings.Contains(entry.Command, mdevPaneMarker) {
			continue
		}
		if session, ok := byPID[entry.PPID]; ok {
			managed[session] = true
		}
	}
	return managed
}

// ZombieServers は一覧に出てこないのに動いている zellij サーバを返す。
//
// zellij が把握していない(list-sessions に出ない)サーバや、EXITED と
// 表示されているのにプロセスが残っているサーバが該当する。放っておくと
// CPU を食い続け、他のセッションのタブ操作まで遅くする。
//
// **一覧に生きていると出ているセッションのサーバは決して含めない。**
// 使用中のセッションを落とす唯一の経路になりうるためである。
func ZombieServers(servers []ZellijServer, sessions []SessionEntry) []ZellijServer {
	alive := map[string]bool{}
	for _, name := range AliveSessionNames(sessions) {
		alive[name] = true
	}

	var zombies []ZellijServer
	for _, server := range servers {
		if alive[server.Session] {
			continue
		}
		zombies = append(zombies, server)
	}
	return zombies
}

// OrphanZellijClients は親を失った `zellij action ...` のプロセスを返す。
//
// タブ操作の呼び出しが返らなくなり、呼び出し元だけが先に終わると
// この形で残る。実環境では 200 個超まで積み上がり、うち数個が 100% CPU で
// 空転してマシン全体を劣化させた(internal/infra/proc の説明を参照)。
//
// PPID が 1 であることを条件に含めるのは、動いている呼び出し(親が生きて
// いるもの)を巻き込まないためである。
func OrphanZellijClients(entries []ProcessEntry) []ProcessEntry {
	var orphans []ProcessEntry
	for _, entry := range entries {
		if entry.PPID != orphanPPID {
			continue
		}
		if !strings.Contains(entry.Command, zellijActionMarker) {
			continue
		}
		orphans = append(orphans, entry)
	}
	return orphans
}
