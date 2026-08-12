package zellij

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/k-kudo-hub/mdev-go/internal/app"
	"github.com/k-kudo-hub/mdev-go/internal/domain"
)

// SessionController は zellij のセッション操作を担当する。
//
// どの呼び出しにも commandTimeout(10 秒)の上限を付ける。掃除はセッションの
// 起動前に走るため、zellij が返らなくなると起動そのものが止まってしまう。
// socketDirPrefix は zellij のソケット置き場の名前の頭である。
// 実体は `<一時ディレクトリ>/zellij-<uid>`。
const socketDirPrefix = "zellij-"

type SessionController struct {
	// output はコマンドの標準出力と実行の成否を返す。テストで差し替える。
	output func(timeout time.Duration, name string, args ...string) (string, error)
	// outputBoth は標準出力と標準エラー出力の両方を返す。テストで差し替える。
	//
	// list-sessions だけがこちらを使う。「セッションが 1 つも無い」ことを
	// 失敗と区別するには標準エラーの文言が要る(ListSessions を参照)。
	outputBoth func(timeout time.Duration, name string, args ...string) (string, string, error)
	// run はコマンドを実行する。テストで差し替える。
	run func(timeout time.Duration, name string, args ...string) error
}

var (
	_ app.SessionLister        = (*SessionController)(nil)
	_ app.SessionClientLister  = (*SessionController)(nil)
	_ app.SessionRemover       = (*SessionController)(nil)
	_ app.SessionAttachChecker = (*SessionController)(nil)
	_ app.SessionSocketLocator = (*SessionController)(nil)
)

// NewSessionController は zellij コマンドを実行する SessionController を返す。
func NewSessionController() *SessionController {
	return &SessionController{
		output:     commandOutput,
		outputBoth: commandOutputBoth,
		run:        runCommand,
	}
}

// ListSessions は `zellij list-sessions --no-formatting` の出力を返す。
//
// **「0 件」と認めるのは、標準エラーに明示の文言があるときだけである。**
// zellij はセッションが 1 つも無いと rc=1・標準出力は空・標準エラーへ
// 「No active zellij sessions found.」を出す(実機で確認)。
//
// rc=0 で何も返さない応答は **判断不能として error にする。** ここを
// 「0 件」として通すと、生きているセッションのサーバがすべて
// 「一覧に出ないサーバ」= ゾンビに見え、掃除が撃ちにいく。実際に、PATH に
// 何もしない zellij スタブ(rc=0・無出力)が入った状態で使用中セッションの
// サーバを落とす事故が起きた。zellij CLI の rc=0 は「やり遂げた」ことを
// 意味しないので、成功したという申告だけを根拠にしてはならない。
func (c *SessionController) ListSessions() (string, error) {
	stdout, stderr, err := c.outputBoth(commandTimeout, binaryName, "list-sessions", "--no-formatting")
	if err == nil {
		if strings.TrimSpace(stdout) != "" {
			return stdout, nil
		}
		return "", errors.New(
			"zellij list-sessions が何も返しませんでした(セッションの有無を判断できません)")
	}
	if domain.IsNoSessionsOutput(stdout, stderr) {
		return "", nil
	}
	return "", err //nolint:wrapcheck // 呼び出し側が用途に応じて包む
}

// SocketDir は自分が見ている zellij のソケット置き場を返す。
//
// zellij はソケットを `<一時ディレクトリ>/zellij-<uid>/contract_version_N/<名前>`
// に置く(実機で確認)。一時ディレクトリは TMPDIR 由来なので、**同じ
// 一時ディレクトリを見ているサーバだけが list-sessions の対象になる。**
// 別の TMPDIR で起動されたサーバは一覧に出ないが、それは「ゾンビ」では
// なく「こちらから見えていないだけ」である。掃除の対象を自分の見える
// 範囲へ絞るために使う。
func (c *SessionController) SocketDir() string {
	return filepath.Join(os.TempDir(), socketDirPrefix+strconv.Itoa(os.Getuid()))
}

// ListClients はセッションにアタッチしているクライアントの一覧を返す。
//
// 失敗をそのまま error として返す。呼び出し側は「アタッチあり」に倒して
// そのセッションへ触れない(誰も居ないと誤って判断すると使用中のセッションを
// kill するため)。
func (c *SessionController) ListClients(session string) (string, error) {
	out, err := c.output(commandTimeout, binaryName, "--session", session, "action", "list-clients")
	if err != nil {
		return "", err //nolint:wrapcheck // 呼び出し側が用途に応じて包む
	}
	return out, nil
}

// KillSession は動いているセッションを終了させる。
//
// delete-session と違い、zellij 側に「誰か開いていたら拒む」防御は無い。
// つまり呼んだ時点で本当に誰も居ないかは、こちらが確かめるしかない。
// 掃除は消す直前に list-clients を引き直しているが、そこから実際に
// kill が届くまでのごく短い間(数百ミリ秒)は残る。zellij の CLI に
// 「誰も居なければ kill」を原子的に行う手段が無いため、これ以上は
// 縮められない(SessionCleaner.apply のコメントを参照)。
func (c *SessionController) KillSession(name string) error {
	return c.run(commandTimeout, binaryName, "kill-session", name)
}

// DeleteSession はセッションのメタデータを削除する。
//
// **--force は付けない。** 付けないと zellij は動いているセッションの削除を
// 拒む(実機で確認: 「exists and is active, use --force to delete it」で
// rc=1)。これは最後の砦として効く。掃除は「誰も開いていない」ことを
// 確かめてから消しに来るが、その判断と実行の間に誰かが開くことはありうる。
// --force はその守りを自分から外す指定になる。
//
// 代わりに、kill の直後で zellij がまだ動いていると見なす間は削除が失敗する。
// そのセッションは EXITED として残り、次回の掃除が拾う。
func (c *SessionController) DeleteSession(name string) error {
	return c.run(commandTimeout, binaryName, "delete-session", name)
}

// IsAttached はセッションを誰か開いているかを返す。
//
// **判断できない場合は true を返す。** 誰も居ないと誤って判断すると、
// 実際には見ている画面のポーリングが落ちて固まって見える。判断できないのは
// 次の 2 つで、どちらも開いている扱いにする。
//
//   - list-clients が失敗した(セッションが応答しない)
//   - 応答の形が想定と違う(見出し行が無い)。rc=0 で完全に空の応答が
//     これに当たる
func (c *SessionController) IsAttached(session string) bool {
	out, err := c.ListClients(session)
	if err != nil {
		return true
	}
	count, ok := domain.ParseClientList(out)
	if !ok {
		return true
	}
	return count > 0
}

// commandOutputBoth は外部コマンドを実行して標準出力と標準エラー出力を返す。
//
// 標準エラーまで見るのは list-sessions だけである。「セッションが 1 つも
// 無い」ことを他の失敗と区別するには、その文言が要る。
func commandOutputBoth(timeout time.Duration, name string, args ...string) (string, string, error) {
	cmd, cancel := command(timeout, name, args...)
	defer cancel()
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	return stdout.String(), stderr.String(), err //nolint:wrapcheck // 呼び出し側が用途に応じて包む
}
