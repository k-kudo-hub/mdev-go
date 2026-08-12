package domain

import (
	"strconv"
	"strings"
	"time"
)

// ps の出力から zellij のプロセスを見分けるための目印。
const (
	// zellijServerFlag は zellij のサーバプロセスを表す引数である。
	// 実出力: `zellij --server /var/folders/.../zellij-501/.../<session>`
	zellijServerFlag = "--server"
	// zellijActionSubcommand はタブ操作を行うサブコマンドである。
	//
	// 実際の呼び出しは `zellij action ...` と
	// `zellij --session <名前> action ...` の 2 通りある(後者はペインが
	// 行う attach 確認の形)。前置きの文字列で照合すると後者を取りこぼす
	// ので、語の並びとして探す。
	zellijActionSubcommand = "action"
	// zellijArgsSeparator は「これより後ろは zellij ではなく利用者の
	// コマンド」を表す区切りである(`zellij run -- npm test` の `--`)。
	zellijArgsSeparator = "--"
	// mdevPaneMarker は mdev が管理するセッションであることの目印である。
	// ペインの中身が `<CONDUCTOR_HOME>/bin/mdev pane <name>` で起動される。
	mdevPaneMarker = "/bin/mdev pane"
	// zellijBinaryName は zellij の実行ファイル名である。
	// 絶対パスで出ることもあるため、パスの最後の要素と比べる。
	zellijBinaryName = "zellij"
)

// orphanPPID は親を失ったプロセスの親 PID である(init に引き取られた状態)。
const orphanPPID = 1

// CleanupMinAge は掃除の対象とみなすまでに必要な経過時間である。
//
// **作られたばかりのものを掴まないための猶予である。** 2 か所で効く。
//
//   - zellij のサーバは起動してから list-sessions に載るまでに一瞬の間が
//     あり、その隙に見ると「一覧に出ないのに動いているサーバ」に見える。
//     実機でも、走査した瞬間だけ見えて次には消えている短命なサーバを観測した
//   - セッションも、作られてからペインが起動して attach されるまでの間は
//     「誰も開いていない mdev セッション」に見える
//
// どちらもここで撃つと、利用者が今まさに開こうとしているセッションを落とす。
// 取りこぼしても次回の掃除で拾えるので、長めに取って構わない。
const CleanupMinAge = 60 * time.Second

// ProcessEntry は `ps -axo pid,ppid,etime,command` の 1 行である。
type ProcessEntry struct {
	PID  int
	PPID int
	// Elapsed は起動からの経過時間である。
	//
	// 掃除は「起動しかけのものを掴まない」ために使う。ps が出す値なので、
	// domain が時計を持つ必要はない。読めなかった場合は 0 になる
	// (= 起動直後の扱いになり、掃除の対象から外れる)。
	Elapsed time.Duration
	Command string
}

// ParseProcessList は `ps -axo pid,ppid,etime,command` の出力を読む。
//
// 見出し行と、先頭 2 列が数値でない行は読み飛ばす。command は 4 列目以降を
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
		// command は元の空白のまま残したいので、先頭 3 列を順に落とす。
		command := strings.TrimSpace(line)
		for _, column := range fields[:3] {
			command = strings.TrimSpace(strings.TrimPrefix(command, column))
		}
		entries = append(entries, ProcessEntry{
			PID:     pid,
			PPID:    ppid,
			Elapsed: parseElapsed(fields[2]),
			Command: command,
		})
	}
	return entries
}

// parseElapsed は ps の ELAPSED 列を読む。
//
// 書式は `[[DD-]HH:]MM:SS` である(実機: `01:00:59` / `54-10:00:41`)。
// 読めない場合は 0 を返す。0 は「起動直後」と同じ扱いになり、掃除の対象から
// 外れるので安全側である。
func parseElapsed(field string) time.Duration {
	days := 0
	rest := field
	if i := strings.Index(field, "-"); i >= 0 {
		parsed, err := strconv.Atoi(field[:i])
		if err != nil {
			return 0
		}
		days, rest = parsed, field[i+1:]
	}

	parts := strings.Split(rest, ":")
	if len(parts) < 2 || len(parts) > 3 {
		return 0
	}
	units := []time.Duration{time.Hour, time.Minute, time.Second}
	// MM:SS のときは時間の桁が無い。
	units = units[len(units)-len(parts):]

	total := time.Duration(days) * 24 * time.Hour
	for i, part := range parts {
		value, err := strconv.Atoi(part)
		if err != nil {
			return 0
		}
		total += time.Duration(value) * units[i]
	}
	return total
}

// ZellijServer は動いている zellij サーバ 1 つである。
type ZellijServer struct {
	// PID はサーバのプロセス ID。
	PID int
	// PPID は親 PID。シグナルを送る直前の照合に使う。
	PPID int
	// Elapsed は起動からの経過時間。ゾンビ判定の猶予に使う。
	Elapsed time.Duration
	// Command は ps が出したコマンド行である。
	//
	// シグナルを送る直前に引き直して照合するために持つ。PID は使い回される
	// ので、選んだときと同じ PID が別のプロセスになっていることがある。
	Command string
	// Socket はサーバが使っているソケットのパスである。
	// 掃除の対象を自分の見える範囲へ絞るために使う。
	Socket string
	// Session はサーバが持つセッション名。
	//
	// サーバのコマンド行の末尾がソケットのパスで、その最後の要素が
	// セッション名になる(実出力の
	// `--server /var/folders/.../zellij-501/contract_version_1/mdev-go-224042`)。
	Session string
}

// ZellijServers は動いている zellij サーバを返す。
//
// セッション名を取り出せない行は落とす。名前が分からないサーバを掃除の
// 対象にすると、使用中のものを巻き込む。
func ZellijServers(entries []ProcessEntry) []ZellijServer {
	var servers []ZellijServer
	for _, entry := range entries {
		socket, ok := zellijServerSocket(entry.Command)
		if !ok {
			continue
		}
		name := socketSessionName(socket)
		if name == "" {
			continue
		}
		servers = append(servers, ZellijServer{
			PID: entry.PID, PPID: entry.PPID, Elapsed: entry.Elapsed, Session: name,
			Command: entry.Command, Socket: socket,
		})
	}
	return servers
}

// zellijServerSocket はコマンド行が zellij サーバのものなら、そのソケットの
// パスを返す。
//
// **形をきっちり見る。** 判定に使うのは次の 3 つで、どれか 1 つでも
// 合わなければサーバとみなさない。
//
//   - 実行ファイルの名前(パスの最後の要素)が "zellij" であること
//   - 2 つ目の語がちょうど `--server` であること
//   - その後ろにソケットのパスがあること
//
// 部分一致で見ていたときは、`nvim --server /tmp/zellij-x` のような
// 無関係のプロセスまでサーバとして拾い、掃除が kill しにいく恐れがあった。
//
// パスは **`--server` の後ろから行末まで** を丸ごと採る。最後の語だけを
// 見ると、空白を含むパス(macOS の一時ディレクトリは既定では含まないが、
// TMPDIR は利用者が変えられる)で名前を取り違える。
//
// 受容している穴が 1 つある。**zellij の実行ファイル自身のパスに空白が
// 含まれる環境では、サーバとして検出できない**(1 つ目の語がパスの途中で
// 切れるため)。これは意図的に検出漏れの側へ倒したものである。ここを緩めて
// 語の並びを推測で繋ぐと、無関係のプロセスをサーバと誤認して kill しうる。
// 検出できなければ掃除が 1 回見送られるだけだが、誤認すれば動いている
// セッションを落とす。害の大きさが釣り合わない。
func zellijServerSocket(command string) (string, bool) {
	trimmed := strings.TrimSpace(command)
	fields := strings.Fields(trimmed)
	if len(fields) < 3 {
		return "", false
	}
	if socketBase(fields[0]) != zellijBinaryName || fields[1] != zellijServerFlag {
		return "", false
	}
	rest := strings.TrimSpace(strings.TrimPrefix(trimmed, fields[0]))
	rest = strings.TrimSpace(strings.TrimPrefix(rest, zellijServerFlag))
	if rest == "" {
		return "", false
	}
	return rest, true
}

// socketSessionName はソケットのパスからセッション名(最後の要素)を返す。
func socketSessionName(socket string) string {
	return socketBase(socket)
}

// socketBase はパスの最後の要素を返す。
func socketBase(path string) string {
	if i := strings.LastIndex(path, "/"); i >= 0 {
		return path[i+1:]
	}
	return path
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
//
// 起動から CleanupMinAge に満たないサーバも外す。起動しかけのサーバは
// まだ list-sessions に載っておらず、そのままでは「一覧に出ないのに
// 動いている」に見えるためである。
func ZombieServers(servers []ZellijServer, sessions []SessionEntry) []ZellijServer {
	alive := map[string]bool{}
	for _, name := range AliveSessionNames(sessions) {
		alive[name] = true
	}

	var zombies []ZellijServer
	for _, server := range servers {
		if alive[server.Session] || server.Elapsed < CleanupMinAge {
			continue
		}
		zombies = append(zombies, server)
	}
	return zombies
}

// OrphanZellijClients は親を失った zellij のタブ操作プロセスを返す。
//
// タブ操作の呼び出しが返らなくなり、呼び出し元だけが先に終わると
// この形で残る。実環境では 200 個超まで積み上がり、うち数個が 100% CPU で
// 空転してマシン全体を劣化させた(internal/infra/proc の説明を参照)。
//
// 条件は 2 つで、どちらも欠かせない。
//
//   - **PPID が 1 であること。** 呼び出し元が先に死んだ印である。実行中の
//     呼び出しを撃つと、タブ操作の途中で zellij の状態が壊れる
//   - **zellij の action サブコマンドであること。** 実行ファイル名と
//     サブコマンドの位置で見る(isZellijActionClient)
//   - **起動から CleanupMinAge 以上経っていること。** ゾンビサーバと同じ
//     猶予である。積み上がって困る孤児は数時間単位で残るのに対し、
//     `zellij action X &` のように起こして親が先に終わった直後のプロセスは
//     秒単位で PPID=1 になる。後者はまだ仕事の途中なので撃ってはいけない
//
// ゾンビサーバと違い、**ソケットの置き場による範囲の絞り込みはできない。**
// クライアントのコマンド行にはソケットのパスが出ないためである
// (出るのは `--session <名前>` までで、どの一時ディレクトリを見ているかは
// 分からない)。それでよいと判断しているのは、この形のプロセスが
// **どの環境から見てもゴミだから**である。親を失った `zellij action` は
// 結果を受け取る相手がもう居らず、生きているセッションの役に立つことは無い。
// 一方サーバのほうは、別の一時ディレクトリで動いていれば「こちらから
// 見えていないだけの正常なセッション」でありうるので、範囲を絞る必要がある。
//
// 送る直前には PID とコマンド行の一致を確かめる(SessionCleaner.apply)。
// PID は使い回されるため、ここで選んだ後に無関係のプロセスへ回っている
// ことがある。
func OrphanZellijClients(entries []ProcessEntry) []ProcessEntry {
	var orphans []ProcessEntry
	for _, entry := range entries {
		if entry.PPID != orphanPPID {
			continue
		}
		if entry.Elapsed < CleanupMinAge {
			continue
		}
		if !isZellijActionClient(entry.Command) {
			continue
		}
		orphans = append(orphans, entry)
	}
	return orphans
}

// ProcessesByPID は PID からプロセスを引ける表を返す。
//
// シグナルを送る直前の照合に使う。PID は使い回されるため、選んだときと
// 同じ PID が別のプロセスになっていることがある。
func ProcessesByPID(entries []ProcessEntry) map[int]ProcessEntry {
	byPID := make(map[int]ProcessEntry, len(entries))
	for _, entry := range entries {
		byPID[entry.PID] = entry
	}
	return byPID
}

// zellijValueFlags は値を 1 つ取る zellij のグローバルオプションである。
//
// サブコマンドの位置を数えるときに、値をオプションの一部として飛ばすために
// 使う。`zellij --session action attach` の "action" はセッション名であって
// サブコマンドではない。表に無いオプションで値が "action" だった場合は
// サブコマンドと読んでしまうため、zellij が持つ値付きオプションはすべて
// 挙げてある。
var zellijValueFlags = map[string]bool{
	"-c": true, "--config": true,
	"--config-dir": true,
	"--data-dir":   true,
	"-l":           true, "--layout": true,
	"--layout-string": true,
	"--max-panes":     true,
	"-n":              true, "--new-session-with-layout": true,
	"-s": true, "--session": true,
}

// isZellijActionClient はコマンド行が zellij のタブ操作かどうかを返す。
//
// 条件は次の 2 つである。
//
//   - 実行ファイルの名前(パスの最後の要素)が "zellij" であること。
//     `grep zellij action` のような無関係のプロセスを拾わないため
//   - オプションを除いた **最初の位置引数**(= サブコマンド)が "action"
//     であること
//
// サブコマンドの位置で見るのは、`action` という名前のセッションやレイアウトが
// あると語の並びのどこかに "action" が現れてしまうためである。
// `zellij attach action` はセッション名が action なだけで、タブ操作の
// 呼び出しではない。逆に `zellij --session action action list-clients` は
// 名前も action だがサブコマンドも action なので、これは拾う。
//
// `--` が現れたらそこで打ち切る。後ろは利用者のコマンドである
// (`zellij run -- nvim action.md` の action.md を取り違えない)。
func isZellijActionClient(command string) bool {
	fields := strings.Fields(command)
	if len(fields) < 2 || socketBase(fields[0]) != zellijBinaryName {
		return false
	}

	for i := 1; i < len(fields); i++ {
		field := fields[i]
		if field == zellijArgsSeparator {
			return false
		}
		if zellijValueFlags[field] {
			// 次の語はこのオプションの値なので飛ばす。
			i++
			continue
		}
		if strings.HasPrefix(field, "-") {
			// `--flag=値` と値を取らないオプション。1 語で完結する。
			continue
		}
		// 最初の位置引数がサブコマンドである。
		return field == zellijActionSubcommand
	}
	return false
}

// ServersInSocketDir は dir の配下にソケットを持つサーバだけを返す。
//
// **自分から見える範囲だけを掃除の候補にする。** zellij の `list-sessions`
// は自分の一時ディレクトリ配下しか見ないため、別の一時ディレクトリで
// 起動されたサーバは必ず「一覧に出ないサーバ」に見える。それはゾンビでは
// なく、こちらから見えていないだけである。範囲で絞らないと、別の環境で
// 動いている正常なセッションを撃つことになる。
//
// dir が空(範囲を決められない)場合は 1 件も返さない。
func ServersInSocketDir(servers []ZellijServer, dir string) []ZellijServer {
	if dir == "" {
		return nil
	}
	prefix := strings.TrimSuffix(dir, "/") + "/"

	var scoped []ZellijServer
	for _, server := range servers {
		if !strings.HasPrefix(server.Socket, prefix) {
			continue
		}
		scoped = append(scoped, server)
	}
	return scoped
}
