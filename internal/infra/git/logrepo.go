// Package git は git バイナリを起動してログ用リポジトリを操作する。
// internal/app が定義する port の実装(adapter)である(ADR-0002)。
//
// go-git のような Go 実装ではなく git バイナリを exec するのは、認証を
// 利用者の設定にそのまま任せるためである。ログ用リポジトリは private な
// ことが多く、credential helper・SSH agent・ssh の config・企業の proxy 設定
// といった「その環境の git が既にできていること」を再実装せずに使える。
package git

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/k-kudo-hub/mdev-go/internal/app"
)

// uploadCacheDirName は CONDUCTOR_HOME 直下の clone 置き場。
const uploadCacheDirName = "upload-cache"

// commitAuthor は作業ログのコミットに使う名前とメールアドレスである。
// 利用者の git 設定を汚さないよう、コミットのたびに -c で渡す。
const (
	commitAuthorEmail = "conductor@local"
	commitAuthorName  = "claude-conductor"
)

// dirPerm はキャッシュとログの置き場所を作るときのパーミッション。
const dirPerm = 0o755

// filePerm は書き出すログファイルのパーミッション。
const filePerm = 0o644

// slugUnsafePattern はキャッシュのディレクトリ名に使えない文字の並びである。
// 現行 upload-log.sh の `sed -E 's#[^A-Za-z0-9._-]+#_#g'` に対応する。
var slugUnsafePattern = regexp.MustCompile(`[^A-Za-z0-9._-]+`)

// LogRepository は作業ログをログ用リポジトリへ push する。
type LogRepository struct {
	conductorHome string
	// run は git を実行して標準出力を返す。テストで差し替える。
	run func(dir string, args ...string) (string, error)
	// lookGit は git がインストールされているかを確かめる。テストで差し替える。
	lookGit func() error
}

var _ app.LogPusher = (*LogRepository)(nil)

// NewLogRepository は conductorHome 配下にキャッシュを持つ LogRepository を返す。
func NewLogRepository(conductorHome string) *LogRepository {
	return &LogRepository{
		conductorHome: conductorHome,
		run:           runGitIn,
		lookGit:       func() error { _, err := exec.LookPath("git"); return err },
	}
}

// Push は relPath へ content を書いて push し、ログの参照文字列を返す。
//
// 手順は現行 upload-log.sh の push_log と同じである。
//
//  1. キャッシュに clone が無ければ作る。branch を指定せず --depth 1 で clone
//     するのは、ブランチが 1 本も無い作りたての空リポジトリでも成功させるため
//  2. fetch の **終了コードで**分岐して対象ブランチを用意する。成功したら
//     FETCH_HEAD を土台にし、失敗したら今の HEAD から作る。古い FETCH_HEAD を
//     見て判断すると、通信の失敗時に前回の内容を土台にしてしまう
//  3. 書き込み → add → 差分があるときだけ commit。同じ内容の再アップロードを
//     「commit するものが無い」失敗にすると、そのたびに dd が止まってしまう
//  4. push して HEAD の sha を読む
//
// 途中のどこかで失敗したら error を返す。呼び出し側はタブの削除を中止する。
func (r *LogRepository) Push(repo, branch, relPath, content string) (string, error) {
	if err := r.lookGit(); err != nil {
		return "", fmt.Errorf("git が見つかりません: %w", err)
	}

	cache := filepath.Join(r.conductorHome, uploadCacheDirName, RepoSlug(repo))
	if err := r.ensureClone(repo, cache); err != nil {
		return "", err
	}
	if err := r.checkoutBranch(cache, branch); err != nil {
		return "", err
	}
	if err := writeLogFile(filepath.Join(cache, relPath), content); err != nil {
		return "", err
	}
	if err := r.commitAndPush(cache, branch, relPath); err != nil {
		return "", err
	}

	// **push が済んだ後の失敗は握り潰す。** ここまで来ればログは残っており、
	// sha が読めないことを理由に error を返すと、呼び出し側がアップロードを
	// 失敗と見なしてタブの削除を中止してしまう(次の dd でまた同じログを
	// push しようとする)。現行版も `sha=$(git rev-parse HEAD 2>/dev/null)` の
	// 失敗を無視し、空の sha を埋めた参照文字列を出して正常終了する。
	sha, _ := r.run(cache, "rev-parse", "HEAD")
	return LogReference(repo, branch, relPath, strings.TrimSpace(sha)), nil
}

// ensureClone はキャッシュに clone が無ければ作る。
//
// .git が無いディレクトリが残っていたら作り直す(中断された clone の残骸)。
func (r *LogRepository) ensureClone(repo, cache string) error {
	if info, err := os.Stat(filepath.Join(cache, ".git")); err == nil && info.IsDir() {
		return nil
	}
	if err := os.RemoveAll(cache); err != nil {
		return fmt.Errorf("キャッシュ %s の削除に失敗しました: %w", cache, err)
	}
	if err := os.MkdirAll(filepath.Dir(cache), dirPerm); err != nil {
		return fmt.Errorf("キャッシュの置き場所 %s の作成に失敗しました: %w", filepath.Dir(cache), err)
	}
	if _, err := r.run("", "clone", "--quiet", "--depth", "1", ResolveRepoURL(repo), cache); err != nil {
		return fmt.Errorf("ログリポジトリの clone に失敗しました: %w", err)
	}
	return nil
}

// checkoutBranch は対象ブランチを用意する。
func (r *LogRepository) checkoutBranch(cache, branch string) error {
	args := []string{"checkout", "--quiet", "-B", branch}
	if _, err := r.run(cache, "fetch", "--quiet", "--depth", "1", "origin", branch); err == nil {
		args = append(args, "FETCH_HEAD")
	}
	if _, err := r.run(cache, args...); err != nil {
		return fmt.Errorf("ブランチ %s の用意に失敗しました: %w", branch, err)
	}
	return nil
}

// commitAndPush は add → (差分があれば) commit → push を行う。
func (r *LogRepository) commitAndPush(cache, branch, relPath string) error {
	if _, err := r.run(cache, "add", relPath); err != nil {
		return fmt.Errorf("ログの add に失敗しました: %w", err)
	}
	// diff --cached --quiet は差分があると非 0 で戻る。同じ内容の再アップロード
	// (差分なし)は commit を飛ばして成功させる。
	if _, err := r.run(cache, "diff", "--cached", "--quiet"); err != nil {
		if _, err := r.run(cache,
			"-c", "user.email="+commitAuthorEmail,
			"-c", "user.name="+commitAuthorName,
			"commit", "--quiet", "-m", "chore: add work log "+relPath,
		); err != nil {
			return fmt.Errorf("ログの commit に失敗しました: %w", err)
		}
	}
	if _, err := r.run(cache, "push", "--quiet", "-u", "origin", branch); err != nil {
		return fmt.Errorf("ログリポジトリへの push に失敗しました: %w", err)
	}
	return nil
}

// writeLogFile はログ本文を末尾に改行を付けて書き出す。
// 現行版の `printf '%s\n' "$content" > "$target"` に対応する。
func writeLogFile(target, content string) error {
	if err := os.MkdirAll(filepath.Dir(target), dirPerm); err != nil {
		return fmt.Errorf("ログの置き場所 %s の作成に失敗しました: %w", filepath.Dir(target), err)
	}
	if err := os.WriteFile(target, []byte(content+"\n"), filePerm); err != nil {
		return fmt.Errorf("ログ %s の書き込みに失敗しました: %w", target, err)
	}
	return nil
}

// isPlainLocation は repo がそのまま git へ渡せる形かどうかを返す。
// 現行版の case パターン `*://*|git@*|/*|./*|../*` に対応する。
func isPlainLocation(repo string) bool {
	return strings.Contains(repo, "://") ||
		strings.HasPrefix(repo, "git@") ||
		strings.HasPrefix(repo, "/") ||
		strings.HasPrefix(repo, "./") ||
		strings.HasPrefix(repo, "../")
}

// ResolveRepoURL は設定の repo を git へ渡す URL にする。
// URL とローカルパスはそのまま、"owner/name" だけ GitHub の SSH URL にする。
func ResolveRepoURL(repo string) string {
	if isPlainLocation(repo) {
		return repo
	}
	return "git@github.com:" + repo + ".git"
}

// RepoSlug はキャッシュのディレクトリ名に使える形へ畳む。
func RepoSlug(repo string) string {
	return slugUnsafePattern.ReplaceAllString(repo, "_")
}

// LogReference は push したログの参照文字列を返す。
//
// "owner/name" で設定されている場合だけ GitHub の blob URL を組み立てる。
// それ以外(自前のホストやローカルパス)は URL の作り方が分からないので、
// 相対パスと sha を出して人が辿れるようにする。
func LogReference(repo, branch, relPath, sha string) string {
	if isPlainLocation(repo) {
		return relPath + " @ " + sha
	}
	return "https://github.com/" + repo + "/blob/" + branch + "/" + relPath
}
