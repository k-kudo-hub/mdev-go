package shell

import (
	"os/exec"

	"github.com/k-kudo-hub/mdev-go/internal/app"
)

// openCommands は URL を開くコマンドの候補。先に見つかったものを使う
// (現行 news-loop.sh の `command -v open` → `command -v xdg-open` の順)。
var openCommands = []string{"open", "xdg-open"}

// Opener は既定のブラウザで URL を開く app.URLOpener の実装である。
type Opener struct {
	// lookPath はコマンドの所在を調べる。テストで差し替える。
	lookPath func(string) (string, error)
	// run はコマンドを実行する。テストで差し替える。
	run func(name string, args ...string) error
}

var _ app.URLOpener = (*Opener)(nil)

// NewOpener は open / xdg-open を使う Opener を返す。
func NewOpener() *Opener {
	return &Opener{lookPath: exec.LookPath, run: runOpen}
}

// Open は URL を開く。使えるコマンドが無ければ何もしない。
// 失敗しても何も返さない(現行版も `2>/dev/null` で握り潰している)。
func (o *Opener) Open(url string) {
	for _, name := range openCommands {
		if _, err := o.lookPath(name); err != nil {
			continue
		}
		_ = o.run(name, url)
		return
	}
}

// runOpen は実際に外部コマンドを実行する。
func runOpen(name string, args ...string) error {
	return exec.Command(name, args...).Run()
}
