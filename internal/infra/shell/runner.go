// Package shell は移行期に Shell のまま残るスクリプトの呼び出しを担当する。
// internal/app が定義する port の実装(adapter)である(ADR-0002)。
//
// ここに残っているものはいずれ Go 化する予定の暫定実装である
// (ログのアップロードとニュース取得はフェーズ 5)。
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

// noTimeout は上限を設けないことを表す。
//
// ここに残っているのは利用者が起こす長時間処理(UploadLog / FetchNews)だけで、
// どちらも途中で切ると作業ログを失う・取得が中途半端に終わるため、待つほうが
// 安全である。いずれもポーリングのチェーンを止めない経路でもある。
const noTimeout = 0

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
// 同じ既定を持つが、mdev 側が別の値に解決している場合(worktree での試験)に
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

// FetchNews は当日のニュースを取り直す。
func (r *Runner) FetchNews() {
	_, _ = r.run(noTimeout, r.env, "bash", r.script("fetch-news.sh"), "--force")
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
// (UploadLog / FetchNews)は連鎖が切れると端末が消えた後も残り続ける。
func command(timeout time.Duration, name string, args ...string) (*exec.Cmd, context.CancelFunc) {
	if timeout <= 0 {
		return exec.Command(name, args...), func() {}
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	return proc.Command(ctx, name, args...), cancel
}
