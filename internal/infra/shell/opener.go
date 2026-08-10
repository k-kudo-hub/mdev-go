package shell

import (
	"os/exec"
	"time"

	"github.com/k-kudo-hub/mdev-go/internal/app"
)

// openCommands は URL を開くコマンドの候補。先に見つかったものを使う
// (現行 news-loop.sh の `command -v open` → `command -v xdg-open` の順)。
var openCommands = []string{"open", "xdg-open"}

// openTimeout は URL を開くコマンド 1 回の実行時間の上限である。
//
// `open` は LaunchServices に依頼して即座に返るコマンドで、ブラウザ自体は
// launchd が起こすためこのプロセスの子孫にはならない。つまり正常時は
// ミリ秒で返り、10 秒かかる時点で異常である。
//
// 上限が要るのは、返らない場合に後始末の手が無いためである。呼び出しは
// ニュース画面のキー操作が出す tea.Cmd(= goroutine)の中にあり、画面は
// 固まらないものの、goroutine と子プロセスが mdev の終了まで残り続ける。
const openTimeout = 10 * time.Second

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
//
// 上限を持つので runCommand と同じくプロセスグループごと切る(command を参照)。
// 上限に達したときに道連れになるのは `open` 自身とその子孫だけで、開いた
// ブラウザは launchd の下にあるため巻き込まれない。
func runOpen(name string, args ...string) error {
	cmd, cancel := command(openTimeout, name, args...)
	defer cancel()
	return cmd.Run()
}
