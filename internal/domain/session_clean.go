package domain

import (
	"strconv"
	"strings"
	"time"
)

// zellij のセッション一覧・クライアント一覧を読むための目印。
const (
	// sessionExitedMarker は終了済みセッションに付く注記である。
	// 実出力: `name [Created 12m 50s ago] (EXITED - attach to resurrect)`
	sessionExitedMarker = "(EXITED"
	// sessionCreatedMarker は名前と注記の境目である。
	//
	// セッション名に空白が入りうるため、最初の語ではなく **この目印より前の
	// 全体** を名前として扱う。目印が無い行(「No active zellij sessions
	// found.」など)はセッション行ではないので読み飛ばす。
	sessionCreatedMarker = " [Created "
	// sessionAgeSuffix は作成からの経過時間の終わりを表す。
	sessionAgeSuffix = " ago]"
	// clientsHeaderPrefix は list-clients の見出し行の先頭である。
	// 実出力: `CLIENT_ID ZELLIJ_PANE_ID RUNNING_COMMAND`
	clientsHeaderPrefix = "CLIENT_ID"
)

// SessionEntry は `zellij list-sessions --no-formatting` の 1 行である。
type SessionEntry struct {
	// Name はセッション名。空白を含むことがある。
	Name string
	// Age は作成からの経過時間である。
	//
	// 掃除は「作られたばかりのセッションを掴まない」ために使う。読めなかった
	// 場合は 0 になり、作成直後と同じ扱い = 掃除の対象から外れる(安全側)。
	Age time.Duration
	// Exited は終了済み(EXITED)かどうか。
	//
	// zellij はウィンドウを閉じてもセッションのメタデータを残す。この状態が
	// 溜まると一覧が読みづらくなるうえ、resurrection の対象として蓄積し続ける。
	// mdev は resurrection を使わない(init.zsh が明示的に作り直す)ので、
	// 見つけたら消してよい。
	Exited bool
}

// ParseSessionList は `zellij list-sessions --no-formatting` の出力を読む。
//
// 実出力は `<名前> [Created 40m 42s ago]` で、終了済みなら末尾に
// `(EXITED - attach to resurrect)` が付く。
//
// **名前は最初の語ではなく、`[Created` より前の全体である。** セッション名には
// 空白を入れられるため、最初の語だけを見ると別の名前として扱ってしまう。
// 掃除は名前を指定して kill / delete するので、取り違えると無関係の
// セッションを消しかねない。
//
// 出力を解釈できない場合でも error は返さない。掃除は最善努力で、読めない
// 行は「無かった」として飛ばすほうが安全側に倒れる(消す対象が減る)。
func ParseSessionList(out string) []SessionEntry {
	var entries []SessionEntry
	for _, line := range strings.Split(out, "\n") {
		i := strings.Index(line, sessionCreatedMarker)
		if i < 0 {
			continue
		}
		name := strings.TrimSpace(line[:i])
		if name == "" {
			continue
		}
		entries = append(entries, SessionEntry{
			Name:   name,
			Age:    parseSessionAge(line[i:]),
			Exited: strings.Contains(line, sessionExitedMarker),
		})
	}
	return entries
}

// parseSessionAge は `[Created 16h 20m 11s ago]` から経過時間を読む。
//
// 読めない場合は 0 を返す。0 は作成直後と同じ扱いになり、掃除の対象から
// 外れるので安全側である。
func parseSessionAge(note string) time.Duration {
	start := strings.Index(note, sessionCreatedMarker)
	if start < 0 {
		return 0
	}
	rest := note[start+len(sessionCreatedMarker):]
	end := strings.Index(rest, sessionAgeSuffix)
	if end < 0 {
		return 0
	}
	return parseDurationWords(rest[:end])
}

// parseDurationWords は `16h 20m 11s` のような並びを読む。
//
// 単位は d / h / m / s を受ける。1 つでも読めない語があれば 0 を返す
// (中途半端に短い値を返すと、作られたばかりのセッションを古いものと
// 誤って掃除してしまう)。
func parseDurationWords(text string) time.Duration {
	units := map[byte]time.Duration{
		'd': 24 * time.Hour,
		'h': time.Hour,
		'm': time.Minute,
		's': time.Second,
	}
	words := strings.Fields(text)
	if len(words) == 0 {
		return 0
	}

	var total time.Duration
	for _, word := range words {
		unit, ok := units[word[len(word)-1]]
		if !ok {
			return 0
		}
		value, err := strconv.Atoi(word[:len(word)-1])
		if err != nil || value < 0 {
			return 0
		}
		total += time.Duration(value) * unit
	}
	return total
}

// AliveSessionNames は終了していないセッションの名前を返す。
func AliveSessionNames(entries []SessionEntry) []string {
	var names []string
	for _, entry := range entries {
		if !entry.Exited {
			names = append(names, entry.Name)
		}
	}
	return names
}

// ExitedSessionNames は終了済みセッションの名前を返す。
func ExitedSessionNames(entries []SessionEntry) []string {
	var names []string
	for _, entry := range entries {
		if entry.Exited {
			names = append(names, entry.Name)
		}
	}
	return names
}

// noSessionsMarker は「セッションが 1 つも無い」ときに zellij が標準エラーへ
// 出す文言の一部である。
//
// 実機(zellij 0.44.1 / macOS)ではこのとき rc=1・標準出力は空になる。
// 文言で見分けるのは、rc と空出力だけでは「本当に 0 件」と「zellij が
// 壊れている」を区別できないためである。区別を誤ると、生きている
// セッションのサーバを **すべてゾンビとみなして kill する** ことになる。
const noSessionsMarker = "No active zellij sessions"

// IsNoSessionsOutput は list-sessions の失敗が「0 件」を意味するかを返す。
//
// 0 件は掃除にとって正常な状態で、そのままプロセス側の掃除(ゾンビ・孤児)へ
// 進んでよい。それ以外の失敗は判断材料が無いということなので、呼び出し側は
// 何もしない。
func IsNoSessionsOutput(stdout, stderr string) bool {
	return strings.TrimSpace(stdout) == "" && strings.Contains(stderr, noSessionsMarker)
}

// ParseClientList は `zellij action list-clients` の出力から
// アタッチ中のクライアント数を返す。
//
// ok=false は「判断できなかった」を意味する。見出し行が無い出力がこれに
// 当たる(rc=0 で完全に空の応答など)。**呼び出し側はアタッチありへ倒すこと。**
// 誰も居ないと誤って判断すると、使用中のセッションを kill する。
func ParseClientList(out string) (count int, ok bool) {
	header := false
	for _, line := range strings.Split(out, "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		if strings.HasPrefix(line, clientsHeaderPrefix) {
			header = true
			continue
		}
		count++
	}
	if !header {
		return 0, false
	}
	return count, true
}
