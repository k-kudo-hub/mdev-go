package git

import (
	"errors"
	"fmt"
	"os/exec"
	"strings"
)

// runGit は git を実行して標準出力を返す。
//
// env が nil なら呼び出し元の環境をそのまま引き継ぎ、非 nil ならそれで
// 置き換える(更新確認だけが待ち時間を抑える変数を足すために使う)。
// dir が空でなければ `git -C <dir>` として実行する。
//
// 標準エラー出力は捨てる(現行版も 2>/dev/null)。上限は設けない。
// clone と push は回線とリポジトリの大きさ次第で時間がかかり、途中で切ると
// 作業ログを失うためで、これは現行版と同じ判断である。
//
// 異常終了(ExitError)では、そこまでに出ていた標準出力を error と一緒に
// 返す。git は失敗しても途中まで出力していることがあり、呼び出し側が
// 「何が返ってきたか」を見て判断できるようにするためである。起動そのものに
// 失敗した場合は出力が存在しないので空を返す。
func runGit(env []string, dir string, args ...string) (string, error) {
	full := args
	if dir != "" {
		full = append([]string{"-C", dir}, args...)
	}
	cmd := exec.Command("git", full...) //nolint:gosec // 引数は呼び出し側が組み立てた固定の並び
	cmd.Env = env
	out, err := cmd.Output()
	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			return string(out), fmt.Errorf("git %s: %w", strings.Join(args, " "), err)
		}
		return "", fmt.Errorf("git の起動に失敗しました: %w", err)
	}
	return string(out), nil
}

// runGitIn は dir の中で git を実行する(環境は呼び出し元のまま)。
// ログリポジトリの操作はすべてこちらを使う。
func runGitIn(dir string, args ...string) (string, error) {
	return runGit(nil, dir, args...)
}

// runGitWithEnv は環境変数を差し替えて git を実行する。
// 更新確認が待ち時間を抑える変数を渡すために使う。
func runGitWithEnv(env []string, args ...string) (string, error) {
	return runGit(env, "", args...)
}
