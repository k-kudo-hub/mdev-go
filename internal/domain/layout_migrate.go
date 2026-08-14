package domain

import (
	"regexp"
	"strings"
)

// レイアウト(*.kdl)に残った Shell スクリプト呼び出しを mdev の呼び出しへ
// 書き換える。
//
// # なぜ上書きではなく書き換えか
//
// 資産の配置は「不在時のみ書く」(利用者が手を入れたレイアウトを install の
// たびに消さない)。しかし移行では `scripts/` を消すため、そこを指したままの
// レイアウトは**ペインが起動しなくなる**。上書きするとカスタマイズが消え、
// 放置すると壊れる。そこで hooks と同じく、**壊れる箇所だけを外科的に
// 書き換える**。それ以外のバイトは 1 つも動かない。

// layoutPaneNames は書き換える 5 つのペインである。
//
// 総称パターン(`(.*)-loop\.sh`)にまとめないのは、mdev 側に実装の無いペインまで
// 将来自動で巻き込むためである。明示列挙なら未対応のペインは書き換わらずに
// 残り、その事実が警告として表に出る(現行 install.sh の注記と同じ判断)。
var layoutPaneNames = []string{"dashboard", "waiting", "done", "news", "task-create"}

// layoutPathChars はコマンド行に現れるパスの文字である。
// 引用符と空白は含まない(KDL の文字列の切れ目と語の切れ目になる)。
const layoutPathChars = `[^"'\s]+`

// layoutAgentLaunch は dev.kdl の Agent ペインの呼び出しを捉える。
//
// 現行版は `bash \"<PREFIX>/scripts/agent-launch.sh\"` の形で、KDL の文字列の
// 中なので引用符が `\"` に逃がしてある。
var layoutAgentLaunch = regexp.MustCompile(
	`bash \\"(` + layoutPathChars + `)/scripts/agent-launch\.sh\\"`)

// layoutPanePattern は multi.kdl のペイン呼び出しを捉える。
var layoutPanePattern = regexp.MustCompile(
	`(` + layoutPathChars + `)/scripts/(` + strings.Join(layoutPaneNames, "|") + `)-loop\.sh`)

// layoutBareMdev は引用符で囲まれていない mdev の呼び出しを捉える。
//
// 6-2 より前の install が書いたレイアウトがこの形で、HOME に空白があると
// bash が語分割してペインが起動しない。移行のついでに囲む(既に囲まれて
// いるものは `\"` が手前に付くので当たらない = 冪等)。
var layoutBareMdev = regexp.MustCompile(
	`([^\\"'\s]` + layoutPathChars + `)/bin/mdev ((?:pane [a-z-]+|agent launch))`)

// conductorBinPattern は CONDUCTOR_HOME 経由の mdev 呼び出しを捉える。
// テスト用のレイアウトで絶対パスへ差し替えるために使う。
var conductorBinPattern = regexp.MustCompile(`(?:\\")?\$\{CONDUCTOR_HOME:-[^}]*\}/bin/mdev(?:\\")?`)

// LayoutChange は書き換えた呼び出し 1 件である。表示用に使う。
type LayoutChange struct {
	Before string
	After  string
}

// MigrateLayout はレイアウトの Shell 呼び出しを mdev の呼び出しへ書き換える。
//
// 書き換え後のコマンドはパスを引用符で囲む。囲まないと HOME に空白がある
// 環境で bash が語分割し、コマンドが見つからなくなる(6-2 で実測)。
// 既に mdev を指しているが囲まれていない呼び出しも、このときに囲む
// (6-2 より前の install が書いたレイアウトがこれに当たる)。
//
// どの規則にも当てはまらなければ入力をそのまま返す(冪等)。既に mdev を
// 指しているレイアウトは 2 回目以降も変わらない。
func MigrateLayout(content string) (string, []LayoutChange) {
	var changes []LayoutChange

	out := layoutAgentLaunch.ReplaceAllStringFunc(content, func(match string) string {
		m := layoutAgentLaunch.FindStringSubmatch(match)
		after := `\"` + m[1] + `/bin/mdev\" agent launch`
		changes = append(changes, LayoutChange{Before: match, After: after})
		return after
	})

	out = layoutPanePattern.ReplaceAllStringFunc(out, func(match string) string {
		m := layoutPanePattern.FindStringSubmatch(match)
		after := `\"` + m[1] + `/bin/mdev\" pane ` + m[2]
		changes = append(changes, LayoutChange{Before: match, After: after})
		return after
	})

	out = layoutBareMdev.ReplaceAllStringFunc(out, func(match string) string {
		m := layoutBareMdev.FindStringSubmatch(match)
		after := `\"` + m[1] + `/bin/mdev\" ` + m[2]
		changes = append(changes, LayoutChange{Before: match, After: after})
		return after
	})
	return out, changes
}

// RemainingLayoutScripts はレイアウトに残った conductor スクリプトの呼び出しを
// 返す。
//
// 書き換えた後にこれが空でなければ、規則に無い呼び出し(利用者が足した別の
// スクリプト)が残っているということである。`scripts/` を消すとそのペインだけが
// 起動しなくなるため、呼び出し側は警告として提示する。
func RemainingLayoutScripts(content string) []string {
	var out []string
	for _, line := range strings.Split(content, "\n") {
		if strings.Contains(line, "/scripts/") {
			out = append(out, strings.TrimSpace(line))
		}
	}
	return out
}
