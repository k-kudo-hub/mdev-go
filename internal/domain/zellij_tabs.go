package domain

import (
	"strings"
)

// ParseTabNames は `zellij action list-tabs` の出力からタブ名を表示順に取り出す。
//
// 現行版の `tail -n +2 | awk '{print $3}'` を再現する。1 行目は見出しなので
// 捨て、残りの行から空白区切りの 3 列目だけを取る。
//
// このため**スペースを含むタブ名は 3 列目の断片しか取れない**。その断片は
// pending の .tab と一致しないので、そのタスクは Dashboard に出てこない。
// 現行版の既知バグで、移行中の挙動を揃えるためそのまま再現している
// (削除時の id 解決だけは ResolveTabID がスペース入りに対応しており非対称)。
func ParseTabNames(output string) []string {
	names := []string{}
	for _, line := range tabLinesAfterHeader(output) {
		fields := strings.Fields(line)
		if len(fields) < 3 {
			continue
		}
		names = append(names, fields[2])
	}
	return names
}

// ResolveTabID は `zellij action list-tabs` の出力からタブ名に対応する id を返す。
//
// 現行版の awk を再現する。
//
//	NR>1 { line=$0; sub(/^[^ ]+ +[^ ]+ +/, "", line); if (line == name) print $1 }
//
// 「先頭 2 列を落とした残り」をタブ名として比較するため、スペースを含む名前も
// 正しく解決できる。該当が無ければ空文字を返す。
//
// 同じ名前のタブが複数ある場合は**先頭の一致だけ**を返す(意図的な差異)。
// 現行版はコマンド置換の結果が改行で連結された "1\n3" のような文字列になり、
// `zellij action close-tab` がそれを id として解釈できずタブが閉じ残る。
// pending とレジストリは先に消えているので、画面から消えたのにタブだけが
// 残るという最も分かりにくい壊れ方をする。先頭だけを返せば少なくとも 1 つは
// 閉じられ、残りは次の削除操作で閉じられる。
func ResolveTabID(output, tab string) string {
	for _, line := range tabLinesAfterHeader(output) {
		if stripTwoColumns(line) != tab {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) == 0 {
			continue
		}
		return fields[0]
	}
	return ""
}

// tabLinesAfterHeader は見出しの 1 行目を除いた行を返す。
func tabLinesAfterHeader(output string) []string {
	lines := strings.Split(strings.TrimRight(output, "\n"), "\n")
	if len(lines) <= 1 {
		return nil
	}
	return lines[1:]
}

// stripTwoColumns は行頭の 2 列(とその後ろの空白)を落とす。
// 現行版の `sub(/^[^ ]+ +[^ ]+ +/, "", line)` に対応し、区切りはスペースのみで
// タブは含まない。パターンに一致しない行はそのまま返す。
func stripTwoColumns(line string) string {
	rest := line
	for range 2 {
		end := strings.IndexByte(rest, ' ')
		if end < 0 {
			return line
		}
		next := end
		for next < len(rest) && rest[next] == ' ' {
			next++
		}
		if next == len(rest) {
			return line
		}
		rest = rest[next:]
	}
	return rest
}

// screenSlugSafe はスラグでそのまま残す文字の集合(`tr -c 'A-Za-z0-9_.-'`)。
func screenSlugSafe(r rune) bool {
	switch {
	case r >= 'A' && r <= 'Z', r >= 'a' && r <= 'z', r >= '0' && r <= '9':
		return true
	case r == '_', r == '.', r == '-':
		return true
	default:
		return false
	}
}

// ScreenTabSlug はタブ名からスクリーン検出用のファイル名を作る。
//
// 現行 task-lib.sh の `_screen_tab_slug` と同じ結果を返す。
//
//	safe=$(printf '%s' "$1" | tr -c 'A-Za-z0-9_.-' '_')
//	hash=$(printf '%s' "$1" | cksum | awk '{print $1}')
//	printf '%s-%s' "$safe" "$hash"
//
// 置換は文字単位で行う。BSD の tr はロケールに従うため、実行環境
// (LC_CTYPE=UTF-8)では日本語 1 文字が `_` 1 つになる。LC_ALL=C で動かすと
// バイト単位になって結果が変わる点は evidence に記録している。
func ScreenTabSlug(tab string) string {
	var safe strings.Builder
	for _, r := range tab {
		if screenSlugSafe(r) {
			safe.WriteRune(r)
			continue
		}
		safe.WriteByte('_')
	}
	return safe.String() + "-" + itoa32(posixCksum([]byte(tab)))
}

// itoa32 は uint32 を 10 進表記にする。
func itoa32(v uint32) string {
	if v == 0 {
		return "0"
	}
	var buf [10]byte
	i := len(buf)
	for v > 0 {
		i--
		buf[i] = byte('0' + v%10)
		v /= 10
	}
	return string(buf[i:])
}

// posixCksumTable は POSIX の cksum が使う CRC-32 の表(多項式 0x04C11DB7、
// ビット反転なし)である。
var posixCksumTable = func() [256]uint32 {
	var table [256]uint32
	for i := range table {
		crc := uint32(i) << 24
		for range 8 {
			if crc&0x80000000 != 0 {
				crc = crc<<1 ^ 0x04C11DB7
			} else {
				crc <<= 1
			}
		}
		table[i] = crc
	}
	return table
}()

// posixCksum は `cksum` コマンドが出す第 1 フィールドの値を返す。
//
// 標準ライブラリの hash/crc32 は IEEE 多項式でビット反転する別物なので使えない。
// POSIX の定義どおり、データを通したあとに長さのオクテット列を流し込み、
// 最後に全ビットを反転する。実際の cksum コマンドとの一致は evidence の表を
// 参照(7 通りで確認済み)。
func posixCksum(data []byte) uint32 {
	var crc uint32
	for _, b := range data {
		crc = crc<<8 ^ posixCksumTable[byte(crc>>24)^b]
	}
	for n := len(data); n > 0; n >>= 8 {
		crc = crc<<8 ^ posixCksumTable[byte(crc>>24)^byte(n)]
	}
	return ^crc
}
