package domain

import (
	"regexp"
	"strings"
)

// FilterSecrets が置き換えに使う印。
const (
	redactedMarker    = "***REDACTED***"
	redactedKeyMarker = "***REDACTED PRIVATE KEY***"
)

// pemBeginPattern / pemEndPattern は PEM の秘密鍵ブロックの境界である。
// 現行 upload-log.sh の awk が使う正規表現をそのまま写した(行頭・行末の
// 固定は無く、行のどこかに現れれば境界とみなす)。
var (
	pemBeginPattern = regexp.MustCompile(`-----BEGIN[ A-Za-z]*PRIVATE KEY-----`)
	pemEndPattern   = regexp.MustCompile(`-----END[ A-Za-z]*PRIVATE KEY-----`)
)

// base64LinePattern は「その行が丸ごと base64 の文字だけでできている」ことを
// 表す。長さ 40 以上との AND で、印の無い鍵の本体を落とす受け皿になる。
var base64LinePattern = regexp.MustCompile(`^[A-Za-z0-9+/=]+$`)

// base64LineMinLength は base64 行とみなす最小の長さ(現行版の length($0) >= 40)。
const base64LineMinLength = 40

// secretPattern は 1 行に収まるトークンの置き換え規則 1 つである。
type secretPattern struct {
	re   *regexp.Regexp
	repl string
}

// secretPatterns は現行 upload-log.sh の sed -E が持つ 7 つの規則である。
//
// **順序に意味がある**。sed は -e を書いた順に同じ行へ適用するため、先に
// sk-ant- を潰しておかないと、後ろの sk- の規則が sk-ant- の途中から
// マッチして別の切り方になる。ここでも同じ順で上から適用する。
//
// 置換後の文字列は最後の 1 つだけ前置きを残す。Bearer の後ろだけが秘密で、
// "Bearer " まで消すと文脈が読めなくなるためである。
var secretPatterns = []secretPattern{
	{regexp.MustCompile(`sk-ant-[A-Za-z0-9_-]{10,}`), redactedMarker},
	{regexp.MustCompile(`sk-[A-Za-z0-9_-]{20,}`), redactedMarker},
	{regexp.MustCompile(`gh[posur]_[A-Za-z0-9]{20,}`), redactedMarker},
	{regexp.MustCompile(`github_pat_[A-Za-z0-9_]{20,}`), redactedMarker},
	{regexp.MustCompile(`AKIA[0-9A-Z]{16}`), redactedMarker},
	{regexp.MustCompile(`xox[baprs]-[A-Za-z0-9-]{10,}`), redactedMarker},
	{regexp.MustCompile(`([Bb]earer )[A-Za-z0-9._-]{10,}`), "${1}" + redactedMarker},
}

// FilterSecrets は既知の秘密情報を伏せた文字列を返す。
//
// 現行 upload-log.sh の filter_secrets(awk | sed のパイプ)と同じ 2 段構えで、
// 出力も awk|sed に合わせて **行ごとに改行を付ける**(入力の末尾に改行が
// 無くても、出力の最後の行には改行が付く)。Shell 側は結果をコマンド置換で
// 受けるため末尾の改行が落ちるが、その始末は呼び出し側で行う。
//
// 第 1 段(行単位の状態機械):
//
//   - PEM の BEGIN 行を見たら 1 行の印に置き換え、END 行までを **1 行も出さない**
//     (END 行自体も出さない)。鍵本体の折り返し最終行は 40 文字未満のことが
//     あり、長さで判定する第 3 段では抜けてしまうためである。
//   - BEGIN が閉じないまま入力が終わった場合は、そこから末尾まで全部落ちる。
//     内容を失うより漏らさないほうを採る(over-mask)。
//   - ブロックの外で、40 文字以上かつ base64 の文字だけの行は印に置き換える。
//
// 第 2 段: 1 行に収まるトークンを secretPatterns の順に置き換える。
func FilterSecrets(input string) string {
	var out strings.Builder
	inKey := false
	for _, line := range splitLines(input) {
		switch {
		case pemBeginPattern.MatchString(line):
			out.WriteString(redactedKeyMarker)
			out.WriteByte('\n')
			inKey = true
			continue
		case inKey:
			if pemEndPattern.MatchString(line) {
				inKey = false
			}
			continue
		case len(line) >= base64LineMinLength && base64LinePattern.MatchString(line):
			out.WriteString(redactedMarker)
			out.WriteByte('\n')
			continue
		}
		out.WriteString(maskTokens(line))
		out.WriteByte('\n')
	}
	return out.String()
}

// maskTokens は 1 行へ secretPatterns を順に適用する。
func maskTokens(line string) string {
	for _, pattern := range secretPatterns {
		line = pattern.re.ReplaceAllString(line, pattern.repl)
	}
	return line
}

// splitLines は入力を awk のレコードと同じ単位に切る。
//
// 末尾の改行はレコードの区切りであって空のレコードではないため、その 1 つだけを
// 落とす。空文字はレコード 0 件になる(awk が何も読まない状態)。
func splitLines(input string) []string {
	if input == "" {
		return nil
	}
	// strings.Split("", "\n") は空のレコード 1 件を返すため、入力が "\n" だけの
	// ときも awk と同じ「空行 1 件」になる。
	return strings.Split(strings.TrimSuffix(input, "\n"), "\n")
}
