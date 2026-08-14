package domain

import (
	"encoding/json"
	"regexp"
	"strings"
)

// codex は notify を **ユーザー全体の設定からしか読まない**(プロジェクト
// 直下の notify は無視される)。conductor の pending へターン完了を橋渡し
// しているのがこの設定で、これが無いと codex のタスクは Dashboard /
// Waiting / Done のどれにも出てこない。

// CodexNotifyMarker は現行 Shell 版の notify を見分ける目印である。
const CodexNotifyMarker = "codex-notify.sh"

// codexNotifyArgs は mdev を呼ぶときの残りの引数。
var codexNotifyArgs = []string{"codex", "notify"}

// CodexNotifyComment は追記した行に付ける印(現行 install.sh と同じ)。
const CodexNotifyComment = " # claude-conductor"

// CodexNotifyStatus は config.toml に対して何をしたかである。
type CodexNotifyStatus int

const (
	// CodexNotifyUnchanged は既に mdev を指していて何もしなかったことを表す。
	CodexNotifyUnchanged CodexNotifyStatus = iota
	// CodexNotifyAdded は notify が無かったので追記したことを表す。
	CodexNotifyAdded
	// CodexNotifyMigrated は Shell 版の呼び出しを mdev へ差し替えたことを表す。
	CodexNotifyMigrated
	// CodexNotifyForeign は他のツールが notify を使っていて触らなかったことを表す。
	CodexNotifyForeign
)

// notifyAssignment は `notify = ...` の行を見つける正規表現である。
var notifyAssignment = regexp.MustCompile(`(?m)^[ \t]*notify[ \t]*=[ \t]*`)

// tableHeader は `[table]` / `[[array]]` の見出し行である。
var tableHeader = regexp.MustCompile(`(?m)^[ \t]*\[`)

// findTopLevelNotify は **トップレベルの** notify の代入を探す。
//
// TOML では `[table]` より後ろに書いたキーはその table のものになり、codex は
// それを読まない(現行 install.sh がわざわざ先頭へ足しているのはこのため)。
// 探索を最初の見出しより前に限れば、次の 2 つを同時に防げる。
//
//   - table 配下の `notify`(別ツールがその table で使っているもの)を
//     conductor のものと取り違えて書き換える
//   - table 配下にしか notify が無い設定を「他ツールが使っている」と誤判定し、
//     トップレベルへ足すべきときに何もしない
//
// 見つからなければ ok=false を返す。
func findTopLevelNotify(content string) (start, end int, ok bool) {
	limit := len(content)
	if header := tableHeader.FindStringIndex(content); header != nil {
		limit = header[0]
	}
	loc := notifyAssignment.FindStringIndex(content[:limit])
	if loc == nil {
		return 0, 0, false
	}
	return loc[0], loc[1], true
}

// RewriteCodexNotify は codex の設定を mdev の notify へ揃える。
//
// mdevPath には `<CONDUCTOR_HOME>/bin/mdev` の絶対パスを渡す。戻り値は
// 書き換え後の内容と、何をしたかである。
//
// 扱う形は 3 つある。
//
//   - notify が無い: 先頭へ 1 行足す(`[table]` より前でなければ codex が
//     読まないため、必ず先頭に置く)
//   - notify が Shell 版を指している: mdev の 3 語へ差し替える。**Codex
//     Computer Use のような別ツールが `--previous-notify` の JSON 文字列と
//     して conductor を包んでいる場合も、その入れ子の中を差し替える**
//   - notify が他ツールのもので conductor がどこにも出てこない: 触らない
//
// 既に mdev を指していれば何も変えない(冪等)。
func RewriteCodexNotify(content, mdevPath string) (string, CodexNotifyStatus) {
	_, valueStart, ok := findTopLevelNotify(content)
	if !ok {
		return addCodexNotify(content, mdevPath), CodexNotifyAdded
	}

	span := codexNotifyValueSpan(content, valueStart)
	value := content[valueStart:span]
	rewritten, changed := rewriteNotifyValue(value, mdevPath)
	if changed {
		return content[:valueStart] + rewritten + content[span:], CodexNotifyMigrated
	}
	if notifyCallsMdev(value, mdevPath) {
		return content, CodexNotifyUnchanged
	}
	return content, CodexNotifyForeign
}

// notifyCallsMdev は notify のどこかで mdev を呼んでいるかを返す。
//
// 生の文字列に含まれるかではなく、**引用符とエスケープを解いた値**が
// mdev のパスと一致するかを見る。入れ子の JSON はスラッシュが `\/` で
// 逃がしてあることがあり、そのままの照合では見つからない。
func notifyCallsMdev(value, mdevPath string) bool {
	for _, e := range scanNotifyElements(value) {
		if e.value == mdevPath {
			return true
		}
		var inner []string
		if err := json.Unmarshal([]byte(strings.TrimSpace(e.value)), &inner); err != nil {
			continue
		}
		for _, v := range inner {
			if v == mdevPath {
				return true
			}
		}
	}
	return false
}

// addCodexNotify は notify の行を先頭へ足す。
//
// 現行 install.sh と同じく、既存の内容の前に 1 行と空行を置く。
func addCodexNotify(content, mdevPath string) string {
	line := "notify = " + encodeTOMLArray(append([]string{mdevPath}, codexNotifyArgs...)) +
		CodexNotifyComment
	if content == "" {
		return line + "\n"
	}
	return line + "\n\n" + content
}

// codexNotifyValueSpan は代入の値が終わる位置を返す。
//
// 配列なら対応する `]` の次まで、そうでなければ行末までである。文字列の中に
// 現れる `]` や改行は数えない(パスに `]` が入っていても壊れないため)。
func codexNotifyValueSpan(content string, start int) int {
	if start >= len(content) || content[start] != '[' {
		if nl := strings.IndexByte(content[start:], '\n'); nl >= 0 {
			return start + nl
		}
		return len(content)
	}

	depth := 0
	for i := start; i < len(content); i++ {
		switch content[i] {
		case '"', '\'':
			end, ok := scanTOMLString(content, i)
			if !ok {
				return len(content)
			}
			i = end - 1
		case '[':
			depth++
		case ']':
			depth--
			if depth == 0 {
				return i + 1
			}
		}
	}
	return len(content)
}

// scanTOMLString は content[start] から始まる文字列リテラルの終端(閉じ引用符の
// 次の位置)を返す。開始位置の文字が引用符でない場合は ok=false。
//
// 扱うのは 1 行の基本文字列("...", バックスラッシュのエスケープあり)と
// リテラル文字列('...', エスケープなし)の 2 つである。複数行文字列は
// notify では使われないため、見つけたら解釈をあきらめる(呼び出し側は
// 「他ツールのもの」として触らない側へ倒れる)。
func scanTOMLString(content string, start int) (int, bool) {
	if start >= len(content) {
		return 0, false
	}
	quote := content[start]
	if quote != '"' && quote != '\'' {
		return 0, false
	}
	for i := start + 1; i < len(content); i++ {
		switch content[i] {
		case '\\':
			if quote == '"' {
				i++
			}
		case quote:
			return i + 1, true
		case '\n':
			return 0, false
		}
	}
	return 0, false
}

// notifyElement は notify 配列の要素 1 つである。
type notifyElement struct {
	// start / end は元の文字列でこの要素が占める範囲。
	start int
	end   int
	// value は引用符を外した中身。
	value string
	// quote は使われている引用符(`"` か `'`)。
	quote byte
}

// rewriteNotifyValue は notify の値を書き換える。変えるものが無ければ changed=false。
func rewriteNotifyValue(value, mdevPath string) (string, bool) {
	elements := scanNotifyElements(value)
	if len(elements) == 0 {
		return value, false
	}

	// 入れ子(別ツールが `--previous-notify` の JSON 文字列として包んでいる)を先に見る。
	for _, e := range elements {
		inner, ok := rewriteNotifyJSON(e.value, mdevPath)
		if !ok {
			continue
		}
		return value[:e.start] + quoteTOML(inner, e.quote) + value[e.end:], true
	}

	from, to, ok := conductorElementRange(elements)
	if !ok {
		return value, false
	}
	replacement := strings.Join(quoteAll(mdevCommand(mdevPath), elements[to].quote), ", ")
	return value[:elements[from].start] + replacement + value[elements[to].end:], true
}

// scanNotifyElements は値の中の文字列リテラルを順に集める。
func scanNotifyElements(value string) []notifyElement {
	var out []notifyElement
	for i := 0; i < len(value); i++ {
		if value[i] != '"' && value[i] != '\'' {
			continue
		}
		end, ok := scanTOMLString(value, i)
		if !ok {
			return nil
		}
		out = append(out, notifyElement{
			start: i,
			end:   end,
			value: decodeTOMLString(value[i:end]),
			quote: value[i],
		})
		i = end - 1
	}
	return out
}

// conductorElementRange は差し替える要素の範囲を返す。
//
// 現行版が書くのは `["bash", "<...>/codex-notify.sh"]` の 2 要素なので、
// 手前の "bash" ごと差し替える。"bash" が無い書き方(直接実行できるように
// してある場合)ではその 1 要素だけを差し替える。
func conductorElementRange(elements []notifyElement) (int, int, bool) {
	for i, e := range elements {
		if !strings.HasSuffix(e.value, CodexNotifyMarker) {
			continue
		}
		if i > 0 && elements[i-1].value == "bash" {
			return i - 1, i, true
		}
		return i, i, true
	}
	return 0, 0, false
}

// rewriteNotifyJSON は要素の中身が JSON の文字列配列で、そこに conductor の
// 呼び出しが含まれる場合に、それを差し替えた JSON を返す。
//
// Codex Computer Use は `--previous-notify` の後ろへ「元の notify」を JSON の
// 文字列として畳んで置く。外側だけを見ていると conductor を見つけられず、
// 入れ子の中で Shell 版が呼ばれ続ける(6-2 の申し送り)。
func rewriteNotifyJSON(value, mdevPath string) (string, bool) {
	trimmed := strings.TrimSpace(value)
	if !strings.HasPrefix(trimmed, "[") {
		return "", false
	}
	var inner []string
	if err := json.Unmarshal([]byte(trimmed), &inner); err != nil {
		return "", false
	}

	elements := make([]notifyElement, len(inner))
	for i, v := range inner {
		elements[i] = notifyElement{value: v}
	}
	from, to, ok := conductorElementRange(elements)
	if !ok {
		return "", false
	}

	rewritten := append([]string{}, inner[:from]...)
	rewritten = append(rewritten, mdevCommand(mdevPath)...)
	rewritten = append(rewritten, inner[to+1:]...)

	encoded, err := json.Marshal(rewritten)
	if err != nil {
		return "", false
	}
	out := string(encoded)
	// 元が `\/` でスラッシュを逃がしていたら、その書き方に合わせる。
	// 意味は変わらないが、差分を読む人にとって表記が揺れないほうがよい。
	if strings.Contains(value, `\/`) {
		out = strings.ReplaceAll(out, "/", `\/`)
	}
	return out, true
}

// mdevCommand は notify から呼ぶコマンドの語を返す。
func mdevCommand(mdevPath string) []string {
	return append([]string{mdevPath}, codexNotifyArgs...)
}

// decodeTOMLString は引用符を外して中身を返す。
func decodeTOMLString(literal string) string {
	if len(literal) < 2 {
		return ""
	}
	body := literal[1 : len(literal)-1]
	if literal[0] == '\'' {
		// リテラル文字列はエスケープを解釈しない。
		return body
	}
	var sb strings.Builder
	for i := 0; i < len(body); i++ {
		if body[i] == '\\' && i+1 < len(body) {
			i++
			sb.WriteByte(unescapeTOMLByte(body[i]))
			continue
		}
		sb.WriteByte(body[i])
	}
	return sb.String()
}

// unescapeTOMLByte は基本文字列のエスケープ 1 文字を戻す。
// 表に無いものはその文字自身として扱う(`\/` を `/` に戻すのがこれ)。
func unescapeTOMLByte(c byte) byte {
	switch c {
	case 'n':
		return '\n'
	case 't':
		return '\t'
	case 'r':
		return '\r'
	default:
		return c
	}
}

// quoteTOML は s を quote の引用符で囲む。
//
// リテラル文字列(')はエスケープできないので、中に ' が現れる場合だけ
// 基本文字列(")へ倒す。notify に入る JSON は " しか使わないため、通常は
// リテラル文字列のまま保たれる。
func quoteTOML(s string, quote byte) string {
	if quote == '\'' && !strings.Contains(s, "'") {
		return "'" + s + "'"
	}
	escaped := strings.ReplaceAll(s, `\`, `\\`)
	escaped = strings.ReplaceAll(escaped, `"`, `\"`)
	return `"` + escaped + `"`
}

// quoteAll は語を並べて引用符で囲む。
func quoteAll(words []string, quote byte) []string {
	out := make([]string, len(words))
	for i, w := range words {
		out[i] = quoteTOML(w, quote)
	}
	return out
}

// encodeTOMLArray は語を TOML の配列表記にする。
func encodeTOMLArray(words []string) string {
	return "[" + strings.Join(quoteAll(words, '"'), ", ") + "]"
}

// RemoveCodexNotify は mdev を指す notify の行を取り除く(uninstall 用)。
//
// 現行 uninstall.sh は `grep -v codex-notify.sh` で行ごと落としていた。
// mdev の notify も同じく 1 行なので、行ごと落とす。**入れ子の中にある
// 場合は触らない**。別ツールの設定を壊すよりは、mdev への参照が残るほうが
// 害が小さい(呼ばれても mdev が消えていれば codex 側が失敗するだけである)。
func RemoveCodexNotify(content, mdevPath string) (string, bool) {
	lineStart, valueStart, ok := findTopLevelNotify(content)
	if !ok {
		return content, false
	}
	end := codexNotifyValueSpan(content, valueStart)
	if !notifyCallsMdev(content[valueStart:end], mdevPath) {
		return content, false
	}

	// 行ごと落とす。値の後ろにコメントが続く場合もあるので行末まで見る。
	if nl := strings.IndexByte(content[end:], '\n'); nl >= 0 {
		end += nl + 1
	} else {
		end = len(content)
	}
	out := content[:lineStart] + content[end:]
	// 追記時に入れた空行が残ると、実行のたびに空行が増える。
	return strings.TrimLeft(out, "\n"), true
}
