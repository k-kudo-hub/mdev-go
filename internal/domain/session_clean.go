package domain

import "strings"

// zellij のセッション一覧・クライアント一覧を読むための目印。
const (
	// sessionExitedMarker は終了済みセッションに付く注記である。
	// 実出力: `name [Created 12m 50s ago] (EXITED - attach to resurrect)`
	sessionExitedMarker = "(EXITED"
	// sessionCreatedMarker は行がセッションを表すことの目印である。
	// これが無い行(「No active zellij sessions found.」など)は読み飛ばす。
	sessionCreatedMarker = "["
	// clientsHeaderPrefix は list-clients の見出し行の先頭である。
	// 実出力: `CLIENT_ID ZELLIJ_PANE_ID RUNNING_COMMAND`
	clientsHeaderPrefix = "CLIENT_ID"
)

// SessionEntry は `zellij list-sessions --no-formatting` の 1 行である。
type SessionEntry struct {
	// Name はセッション名。
	Name string
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
// 名前は行の最初の語である。終了済みかどうかは "(EXITED" の有無で決める。
// セッションを表さない行(「No active zellij sessions found.」など)は
// 読み飛ばす。判別は "[" の有無で行う。作成時刻の注記 `[Created ...]` は
// どのセッション行にも必ず付くためである。
//
// 出力を解釈できない場合でも error は返さない。掃除は最善努力で、読めない
// 行は「無かった」として飛ばすほうが安全側に倒れる(消す対象が減る)。
func ParseSessionList(out string) []SessionEntry {
	var entries []SessionEntry
	for _, line := range strings.Split(out, "\n") {
		if !strings.Contains(line, sessionCreatedMarker) {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) == 0 {
			continue
		}
		entries = append(entries, SessionEntry{
			Name:   fields[0],
			Exited: strings.Contains(line, sessionExitedMarker),
		})
	}
	return entries
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

// AttachedClientCount は `zellij action list-clients` の出力から
// アタッチ中のクライアント数を返す。
//
// 出力は見出し 1 行 + クライアント 1 行ずつである。実出力:
//
//	CLIENT_ID ZELLIJ_PANE_ID RUNNING_COMMAND
//	1         terminal_3     /Users/kazuto/.claude-conductor/bin/mdev pane news
//
// 0 は「誰も開いていない(detached)」を意味する。見出しだけの出力が
// これに当たる。
func AttachedClientCount(out string) int {
	count := 0
	for _, line := range strings.Split(out, "\n") {
		if strings.TrimSpace(line) == "" || strings.HasPrefix(line, clientsHeaderPrefix) {
			continue
		}
		count++
	}
	return count
}
