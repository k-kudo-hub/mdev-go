// Package shell は移行期に Shell のまま残るスクリプトの呼び出しを担当する。
// internal/app が定義する port の実装(adapter)である(ADR-0002)。
//
// ここにあるものはいずれ Go 化する予定の暫定実装である(スクリーン検出と
// セッション復元はフェーズ 4、ログのアップロードはフェーズ 5)。
package shell

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/k-kudo-hub/mdev-go/internal/app"
)

// scriptsDirName は CONDUCTOR_HOME 直下のスクリプト置き場。
const scriptsDirName = "scripts"

// uploadLogPrefix は upload-log.sh が結果行の頭に付ける印。
// 表示するときはこれを取り除く(現行版の `${upload_out#upload-log: }`)。
const uploadLogPrefix = "upload-log: "

// Runner は conductor の Shell スクリプトを同期で呼ぶ。
type Runner struct {
	conductorHome string
	// env は子プロセスへ渡す環境変数。テストで差し替える。
	env []string
	// run はコマンドを実行して標準出力を返す。テストで差し替える。
	run func(env []string, name string, args ...string) (string, error)
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
	out, err := r.run(r.env, "bash", r.script("upload-log.sh"), tab)
	if err != nil {
		return "", fmt.Errorf("upload-log.sh が失敗しました: %w", err)
	}
	// コマンド置換と同じく末尾の改行を落としてから印を外す。
	return strings.TrimPrefix(strings.TrimRight(out, "\n"), uploadLogPrefix), nil
}

// RestoreTask は Done のタスクをダッシュボードへ戻す。
// 終了コードは見ない(現行版も `2>/dev/null` で握り潰している)。
func (r *Runner) RestoreTask(tab, session, completedAt string) {
	_, _ = r.run(r.env, "bash", r.script("restore-task.sh"), tab, session, completedAt)
}

// FetchNews は当日のニュースを取り直す。
func (r *Runner) FetchNews() {
	_, _ = r.run(r.env, "bash", r.script("fetch-news.sh"), "--force")
}

// RestoreSession は登録済みタスクのタブを作り直す。
func (r *Runner) RestoreSession() {
	_, _ = r.run(r.env, "bash", r.script("restore-session.sh"))
}

// ScreenDetectTick はスクリーン検出を 1 回走らせる。
//
// screen-detect-lib.sh は関数を定義するだけのライブラリなので、source して
// から関数を呼ぶ。省略すると screen 方式のエージェント(codex)のタスクが
// ダッシュボードに出てこない。
func (r *Runner) ScreenDetectTick(session string) {
	script := fmt.Sprintf(". %q; screen_detect_tick \"$1\"", r.script("screen-detect-lib.sh"))
	_, _ = r.run(r.env, "bash", "-c", script, "_", session)
}

// runCommand は外部コマンドを実行して標準出力を返す。
// 標準エラー出力は捨てる(現行版の `2>/dev/null` に対応する)。
func runCommand(env []string, name string, args ...string) (string, error) {
	cmd := exec.Command(name, args...)
	cmd.Env = env
	out, err := cmd.Output()
	return string(out), err
}
