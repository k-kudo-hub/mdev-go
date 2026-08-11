package tui

import (
	"strings"

	"github.com/k-kudo-hub/mdev-go/internal/app"
)

// 選択 UI と入力欄の表示に使う ANSI。
//
// この画面は現行版では fzf が描いていたもので、突き合わせるバイト列が無い。
// それでも他のペインと同じ見た目にするため、色は同じものを使う。
const (
	ansiCyan  = "\033[0;36m"
	ansiBold  = "\033[1m"
	ansiDim   = "\033[2m"
	ansiRed   = "\033[0;31m"
	ansiReset = "\033[0m"
)

// selectVisible は選択 UI に一度に出す行数である。
//
// タスク作成ペインはレイアウト上 30% の高さ(実測 10 行前後)しかない。
// 見出しと入力欄で 2 行使うので、残りに収まる数にしている。
const selectVisible = 6

// selectList は入力で絞り込みながら 1 つ選ぶ一覧である。
//
// 現行版が fzf にやらせていたことのうち、実際に使われている機能だけを持つ。
// 絞り込みは部分列マッチ(app.FilterCandidates)で、スコアによる並べ替えは
// しない。並びが入力のたびに動くと、方向キーで選んでいる最中に対象が
// 入れ替わるためである。
type selectList struct {
	// prompt は入力欄の見出し(例 "Directory: ")。
	prompt string
	// items は絞り込み前の全候補。
	items []string
	// labels は表示だけを差し替えたいときの見出し(空なら items をそのまま出す)。
	// タスク種別の「キー + 説明」のように、選ぶ値と見せる文字列が違う場合に使う。
	labels []string

	query    string
	filtered []string
	// indexes は filtered の各要素が items の何番目かを覚える。
	indexes []int
	cursor  int
}

// newSelectList は候補一覧を作る。
func newSelectList(prompt string, items, labels []string) selectList {
	list := selectList{prompt: prompt, items: items, labels: labels}
	list.refilter()
	return list
}

// refilter は今の入力で候補を絞り直し、カーソルを範囲内へ収める。
//
// 絞り込みは位置で受け取る。値だけを受け取ると、元の何番目だったかを
// 突き合わせで引き直すことになり、同じ文字列が 2 つある一覧では最初の
// 1 つに寄ってしまう(ディレクトリ名は重複しうる)。
func (l *selectList) refilter() {
	l.indexes = app.FilterCandidateIndexes(l.items, l.query)
	l.filtered = l.filtered[:0]
	for _, i := range l.indexes {
		l.filtered = append(l.filtered, l.items[i])
	}
	if l.cursor >= len(l.filtered) {
		l.cursor = max(len(l.filtered)-1, 0)
	}
}

// step は 1 打鍵ぶんの入力を処理する。
//
// 受け付けるキーは fzf の基本操作に合わせてある。
//
//	↑ / ctrl+p     1 つ上へ
//	↓ / ctrl+n     1 つ下へ
//	enter          確定
//	esc            取り消し
//	backspace      入力を 1 文字消す
//	それ以外の 1 文字  入力へ足して絞り込む
//
// 戻り値は次のとおりである。
//
//	done=false             まだ選んでいる途中(絞り込み・カーソル移動)
//	done=true, index >= 0  確定した。value は選ばれた値、index は items の位置
//	done=true, index = -1  ESC で取り消された
func (l *selectList) step(key string) (value string, index int, done bool) {
	switch key {
	case "up", "ctrl+p":
		if l.cursor > 0 {
			l.cursor--
		}
		return "", 0, false
	case "down", "ctrl+n":
		if l.cursor < len(l.filtered)-1 {
			l.cursor++
		}
		return "", 0, false
	case "esc":
		return "", -1, true
	case "enter":
		if len(l.filtered) == 0 {
			// 候補が無いときの Enter は何もしない(現行版も fzf が
			// 空を返し、呼び出し側が continue でメニューへ戻る)。
			return "", 0, false
		}
		return l.filtered[l.cursor], l.indexes[l.cursor], true
	case "backspace":
		if l.query != "" {
			l.query = l.query[:len(l.query)-len(lastRune(l.query))]
			l.refilter()
		}
		return "", 0, false
	}

	// 1 文字ぶんの入力だけを絞り込みに使う。修飾キー付き("ctrl+a" など)は
	// 複数文字の名前で届くため、ここで自然に弾かれる。
	if text, ok := typedText(key); ok {
		l.query += text
		l.refilter()
	}
	return "", 0, false
}

// View は選択 UI の表示を返す。
//
// 見出し(入力欄)のあと、カーソルの周りだけを selectVisible 行ぶん出す。
// 候補が 1 件も無い場合はその旨を出す(fzf の空表示に相当)。
func (l *selectList) View() string {
	var b strings.Builder
	b.WriteString("  " + ansiBold + l.prompt + ansiReset + l.query + ansiCyan + "▏" + ansiReset + "\n")

	if len(l.filtered) == 0 {
		b.WriteString("  " + ansiDim + "(候補なし)" + ansiReset + "\n")
		return b.String()
	}

	start := l.windowStart()
	for i := start; i < len(l.filtered) && i < start+selectVisible; i++ {
		if i == l.cursor {
			b.WriteString("  " + ansiCyan + ansiBold + "> " + l.label(i) + ansiReset + "\n")
			continue
		}
		b.WriteString("    " + l.label(i) + "\n")
	}
	if rest := len(l.filtered) - (start + selectVisible); rest > 0 {
		b.WriteString("  " + ansiDim + "… 他 " + itoa(rest) + " 件" + ansiReset + "\n")
	}
	return b.String()
}

// label は絞り込み後の i 番目の表示文字列を返す。
func (l *selectList) label(i int) string {
	if l.labels == nil {
		return l.filtered[i]
	}
	return l.labels[l.indexes[i]]
}

// windowStart はカーソルが見える位置まで表示範囲を送る。
func (l *selectList) windowStart() int {
	if l.cursor < selectVisible {
		return 0
	}
	return l.cursor - selectVisible + 1
}

// lastRune は文字列の末尾 1 文字を返す。
func lastRune(s string) string {
	runes := []rune(s)
	if len(runes) == 0 {
		return ""
	}
	return string(runes[len(runes)-1:])
}

// spaceKeyName は Bubble Tea がスペースキーに付ける名前である。
//
// Bubble Tea v2 は印字可能な 1 文字をその文字自身の名前で報告するが、
// スペースだけは例外で `Key.String()` が "space" を返す(v2.0.8 実測)。
// 名前の長さで入力かどうかを判断すると、スペースだけが無言で捨てられる。
// タブ名にもディレクトリ名にもスペースは入りうるので、明示的に受ける。
const spaceKeyName = "space"

// typedText は key が文字入力なら、その文字を返す。
//
// 修飾キー付き("ctrl+a" など)や名前付きキー("enter" など)は複数文字の
// 名前で届くため、1 文字であることを条件にすれば自然に弾かれる。
// 唯一の例外がスペースで、名前が "space" になるため個別に受ける。
func typedText(key string) (string, bool) {
	if key == spaceKeyName {
		return " ", true
	}
	if len([]rune(key)) == 1 {
		return key, true
	}
	return "", false
}

// itoa は小さな非負整数を 10 進表記にする。
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var buf []byte
	for n > 0 {
		buf = append([]byte{byte('0' + n%10)}, buf...)
		n /= 10
	}
	return string(buf)
}
