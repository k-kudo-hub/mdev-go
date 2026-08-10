// Package shell は移行期に Shell のまま残るスクリプトの呼び出しを担当する。
// internal/app が定義する port の実装(adapter)である(ADR-0002)。
//
// ここにあるものはいずれ Go 化する予定の暫定実装である(スクリーン検出と
// セッション復元はフェーズ 4、ログのアップロードはフェーズ 5)。
package shell

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/k-kudo-hub/mdev-go/internal/app"
	"github.com/k-kudo-hub/mdev-go/internal/infra/proc"
)

// scriptsDirName は CONDUCTOR_HOME 直下のスクリプト置き場。
const scriptsDirName = "scripts"

// ポーリングと起動の経路に付ける実行時間の上限。
//
// zellij サーバが劣化して CLI が返らなくなると、この呼び出しは永久に返らない。
// ポーリングの読み直しは着弾して初めて次の合図を張るため(完了起点。
// internal/tui の poller を参照)、返らない呼び出しはポーリングをそこで
// 止めてしまう。上限を付けておけばタイムアウトがエラーとして着弾し、
// 表示が 1 回ぶん古くなるだけでポーリングは回り続ける。
//
// 値は「正常時の実測(スクリーン検出の内部で走る list-panes が 1.1〜1.5 秒)より
// 十分長く、利用者が固まったと感じるより短い」ところに置いている。
const (
	// screenDetectTimeout はスクリーン検出 1 回の上限。
	screenDetectTimeout = 15 * time.Second
	// restoreSessionTimeout は起動時のセッション復元の上限。
	// 登録済みのタスクぶんタブを作り直すため、他より長く取る。
	restoreSessionTimeout = 60 * time.Second
	// noTimeout は上限を設けないことを表す。
	//
	// 利用者が起こす長時間処理(UploadLog / RestoreTask / FetchNews)に使う。
	// 途中で切ると作業ログを失う・復元が中途半端に終わるなど、待つほうが安全で、
	// いずれもポーリングのチェーンを止めない経路である。
	noTimeout = 0
)

// uploadLogPrefix は upload-log.sh が結果行の頭に付ける印。
// 表示するときはこれを取り除く(現行版の `${upload_out#upload-log: }`)。
const uploadLogPrefix = "upload-log: "

// Runner は conductor の Shell スクリプトを同期で呼ぶ。
type Runner struct {
	conductorHome string
	// env は子プロセスへ渡す環境変数。テストで差し替える。
	env []string
	// run はコマンドを実行して標準出力を返す。テストで差し替える。
	// timeout が 0 以下なら上限を設けない。
	run func(timeout time.Duration, env []string, name string, args ...string) (string, error)
}

var _ app.ShellRunner = (*Runner)(nil)

// NewRunner は conductorHome 配下のスクリプトを呼ぶ Runner を返す。
//
// 子プロセスには呼び出し元の環境をそのまま渡したうえで CONDUCTOR_HOME を
// 上書きする。スクリプト側も `${CONDUCTOR_HOME:-$HOME/.claude-conductor}` で
// 同じ既定を持つが、mdev 側が別の値に解決している場合(worktree での試験など)に
// 食い違わないよう明示する。ZELLIJ_SESSION_NAME は継承されたものをそのまま使う。
func NewRunner(conductorHome string) *Runner {
	return &Runner{
		conductorHome: conductorHome,
		env:           append(os.Environ(), "CONDUCTOR_HOME="+conductorHome),
		run:           runCommand,
	}
}

// script は conductorHome 配下のスクリプトのパスを返す。
func (r *Runner) script(name string) string {
	return filepath.Join(r.conductorHome, scriptsDirName, name)
}

// UploadLog は作業ログをアップロードする。
//
// 終了コード 0 は成功か意図的なスキップ(アップロード無効・対象なし)で、
// 非 0 は失敗である。呼び出し側は非 0 のときタブの削除を中止しなければ
// ならない。戻り値の文字列は標準出力から `upload-log: ` を取り除いたもので、
// 空なら表示するものが無い。
func (r *Runner) UploadLog(tab string) (string, error) {
	out, err := r.run(noTimeout, r.env, "bash", r.script("upload-log.sh"), tab)
	if err != nil {
		return "", fmt.Errorf("upload-log.sh が失敗しました: %w", err)
	}
	// コマンド置換と同じく末尾の改行を落としてから印を外す。
	return strings.TrimPrefix(strings.TrimRight(out, "\n"), uploadLogPrefix), nil
}

// RestoreTask は Done のタスクをダッシュボードへ戻す。
// 終了コードは見ない(現行版も `2>/dev/null` で握り潰している)。
func (r *Runner) RestoreTask(tab, session, completedAt string) {
	_, _ = r.run(noTimeout, r.env, "bash", r.script("restore-task.sh"), tab, session, completedAt)
}

// FetchNews は当日のニュースを取り直す。
func (r *Runner) FetchNews() {
	_, _ = r.run(noTimeout, r.env, "bash", r.script("fetch-news.sh"), "--force")
}

// RestoreSession は登録済みタスクのタブを作り直す。
//
// 起動時に 1 度だけ走る。ここが返らないと最初の描画に進めないため上限を付ける
// (restoreSessionTimeout)。切られた場合もタブが作り直されないだけで、
// 呼び出し側はそのまま最初の読み直しへ進む。
func (r *Runner) RestoreSession() {
	_, _ = r.run(restoreSessionTimeout, r.env, "bash", r.script("restore-session.sh"))
}

// screenDetectScript はスクリーン検出を 1 回走らせる bash の本文である。
//
// パスもセッション名も位置パラメータで受け取り、本文には一切埋め込まない。
// fmt の %q は Go の文字列リテラルの引用であって shell のエスケープではない
// ため、`$` やバッククォートを含む CONDUCTOR_HOME を埋め込むと bash が
// コマンド置換として実行してしまう(source 先のパスも別物に化ける)。
const screenDetectScript = `. "$1"; screen_detect_tick "$2"`

// ScreenDetectTick はスクリーン検出を 1 回走らせる。
//
// screen-detect-lib.sh は関数を定義するだけのライブラリなので、source して
// から関数を呼ぶ。省略すると screen 方式のエージェント(codex)のタスクが
// ダッシュボードに出てこない。
func (r *Runner) ScreenDetectTick(session string) {
	// bash -c の第 1 引数は $0 になるので、$1 の手前に置き場所が要る。
	_, _ = r.run(screenDetectTimeout, r.env, "bash", "-c", screenDetectScript, "_", r.script("screen-detect-lib.sh"), session)
}

// runCommand は外部コマンドを実行して標準出力を返す。
// 標準エラー出力は捨てる(現行版の `2>/dev/null` に対応する)。
// 上限で切られた場合はエラーが返る。
func runCommand(timeout time.Duration, env []string, name string, args ...string) (string, error) {
	cmd, cancel := command(timeout, name, args...)
	defer cancel()
	cmd.Env = env
	out, err := cmd.Output()
	return string(out), err
}

// command は timeout の有無で起動方法を変えた exec.Cmd を返す。
//
// timeout が正の値なら proc.Command を使い、その時間でプロセスグループごと切る。
// ここで呼ぶのは bash スクリプトで、スクリプトはさらに `zellij action ...` を
// 起こすため、直接の子だけを切ると孫が残ってしまう(internal/infra/proc を参照)。
//
// timeout が無いときは素の exec.Command を使う。プロセスグループを分けると、
// 端末が閉じたときにカーネルが送る SIGHUP が子へ連鎖しなくなるためである。
// 上限のある呼び出しは高々その上限で自分から片付くが、上限の無い呼び出し
// (UploadLog / RestoreTask / FetchNews)は連鎖が切れると端末が消えた後も
// 残り続ける。
func command(timeout time.Duration, name string, args ...string) (*exec.Cmd, context.CancelFunc) {
	if timeout <= 0 {
		return exec.Command(name, args...), func() {}
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	return proc.Command(ctx, name, args...), cancel
}
