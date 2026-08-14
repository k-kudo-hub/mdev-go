package shell

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"github.com/k-kudo-hub/mdev-go/internal/app"
)

// Worktree は worktree の場所を調べる app.WorktreeLocator の実装である。
type Worktree struct{}

var _ app.WorktreeLocator = Worktree{}

// NewWorktree は Worktree を返す。
func NewWorktree() Worktree { return Worktree{} }

// MainRoot は主リポジトリの root を返す。worktree の中からでも引ける。
//
// `git rev-parse --git-common-dir` は worktree でも **主リポジトリの** .git を
// 指すため、その親が主リポジトリの root になる(現行 mdev-test と同じ引き方)。
func (Worktree) MainRoot() string {
	out, err := exec.Command("git", "rev-parse", "--git-common-dir").Output()
	if err != nil {
		return ""
	}
	common := strings.TrimSpace(string(out))
	if common == "" {
		return ""
	}
	if !filepath.IsAbs(common) {
		cwd, err := os.Getwd()
		if err != nil {
			return ""
		}
		common = filepath.Join(cwd, common)
	}
	root, err := filepath.Abs(filepath.Dir(common))
	if err != nil {
		return ""
	}
	return root
}

// IsDir はディレクトリとして在るかを返す。
func (Worktree) IsDir(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}

// Abs は絶対パスへ直す。
func (Worktree) Abs(path string) (string, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", fmt.Errorf("%s の絶対パスを求められません: %w", path, err)
	}
	return abs, nil
}

// List は dir 直下のディレクトリ名を昇順で返す。
func (Worktree) List(dir string) []string {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var names []string
	for _, e := range entries {
		if e.IsDir() {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)
	return names
}

// GoBuilder は worktree のソースからバイナリを組む app.GoBuilder の実装である。
type GoBuilder struct {
	// run はビルドを実行する。テストで差し替える。
	run func(worktree, out string) ([]byte, error)
}

var _ app.GoBuilder = GoBuilder{}

// NewGoBuilder は go build を使う GoBuilder を返す。
func NewGoBuilder() GoBuilder { return GoBuilder{run: runGoBuild} }

// Build は worktree の cmd/mdev を out へ組む。
//
// 失敗したときは go の出力をそのまま説明に載せる。ビルドの失敗は利用者が
// 直すもので、要約すると直せる情報が落ちる。
func (b GoBuilder) Build(worktree, out string) error {
	output, err := b.run(worktree, out)
	if err != nil {
		return fmt.Errorf("%w\n%s", err, output)
	}
	return nil
}

// runGoBuild は実際に go build を走らせる。
//
// 上限は設けない。ビルドは worktree の大きさに応じて時間がかかるもので、
// 途中で切ると中途半端なバイナリが残りうる。
func runGoBuild(worktree, out string) ([]byte, error) {
	cmd := exec.Command("go", "build", "-o", out, "./cmd/mdev")
	cmd.Dir = worktree
	return cmd.CombinedOutput()
}
