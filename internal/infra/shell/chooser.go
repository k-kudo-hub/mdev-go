package shell

import (
	"bufio"
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/k-kudo-hub/mdev-go/internal/app"
)

// Chooser は候補を番号付きで出して 1 つ選ばせる app.SessionChooser の
// 実装である。
//
// 現行版は fzf を呼んでいた。番号を読むだけにしたのは、この選択が
// 「セッション一覧から 1 つ」という数件の場面に限られるためである。
// 絞り込みが要る場面(タスク作成のディレクトリ選択)は tui が受け持つ。
// これで fzf への依存が消え、依存チェックからも外せる。
type Chooser struct {
	in  io.Reader
	out io.Writer
}

var _ app.SessionChooser = Chooser{}

// NewChooser は in から選択を読み、out へ一覧を出す Chooser を返す。
func NewChooser(in io.Reader, out io.Writer) Chooser {
	return Chooser{in: in, out: out}
}

// Choose は prompt を出して 1 つ選ばせる。何も選ばなければ空を返す。
//
// 候補が 1 件しか無いときは尋ねずにそれを返す。選ぶ余地が無いのに
// 番号を打たせる意味が無い。
func (c Chooser) Choose(prompt string, options []string) (string, error) {
	if len(options) == 0 {
		return "", nil
	}
	if len(options) == 1 {
		return options[0], nil
	}

	// 出力先へ書けない状況で追加の報告先は無いため失敗は無視する。
	_, _ = fmt.Fprintf(c.out, "%s:\n", prompt)
	for i, name := range options {
		_, _ = fmt.Fprintf(c.out, "  %d) %s\n", i+1, name)
	}
	_, _ = fmt.Fprintf(c.out, "番号 [1-%d] (何も入れなければ中止): ", len(options))

	line, err := bufio.NewReader(c.in).ReadString('\n')
	if err != nil && line == "" {
		// 入力が閉じている(端末でない)場合は中止として扱う。
		return "", nil
	}
	choice := strings.TrimSpace(line)
	if choice == "" {
		return "", nil
	}
	n, err := strconv.Atoi(choice)
	if err != nil || n < 1 || n > len(options) {
		return "", fmt.Errorf("選べない番号です: %s", choice)
	}
	return options[n-1], nil
}
