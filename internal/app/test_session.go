package app

import (
	"errors"
	"fmt"
	"io"
	"path/filepath"

	"github.com/k-kudo-hub/mdev-go/internal/domain"
)

// GoBuilder は worktree のソースからバイナリを組む。
type GoBuilder interface {
	// Build は worktree の cmd/mdev を out へ組む。
	Build(worktree, out string) error
}

// TerminalLauncher は新しい端末の窓でコマンドを走らせる。
type TerminalLauncher interface {
	// Launch は title の窓を開いて command を走らせる。
	Launch(title, command string) error
}

// WorktreeLocator は worktree の場所を調べる。
type WorktreeLocator interface {
	// MainRoot は主リポジトリの root を返す(worktree の中からでも引ける)。
	MainRoot() string
	// IsDir はディレクトリとして在るかを返す。
	IsDir(path string) bool
	// Abs は絶対パスへ直す。
	Abs(path string) (string, error)
	// List は dir 直下のディレクトリ名を返す。
	List(dir string) []string
}

// TestSessionRunner は `mdev test <worktree>` のユースケースである
// (現行 init.zsh の mdev-test 相当)。
//
// worktree のソースからバイナリを組み、隔離したデータディレクトリで
// セッションを開く。**2 つの worktree を同時に試せる**のがこの作りの
// 目的で、設置済みの環境には一切触れない。
type TestSessionRunner struct {
	Locator  WorktreeLocator
	Builder  GoBuilder
	Terminal TerminalLauncher
	Chooser  SessionChooser
	// Installer は隔離したデータディレクトリを整える。組んだバイナリの
	// install を呼ぶのではなく、**今動いている実装**で用意する。組んだ
	// ばかりのバイナリを実行する前に、置き場所だけは確実に作っておきたい。
	Assets AssetReader
	Files  FileStore
}

// RunTest はテストセッションを起動する。dryRun なら内容を出すだけで何もしない。
func (r *TestSessionRunner) RunTest(out io.Writer, input string, dryRun bool) error {
	worktree, err := r.resolve(input)
	if err != nil {
		return err
	}
	spec := domain.NewTestSessionSpec(worktree)

	if dryRun {
		return r.report(out, spec)
	}

	_, _ = fmt.Fprintf(out, "%s のソースを組んでいます...\n", spec.Worktree)
	if err := r.Builder.Build(spec.Worktree, spec.Binary); err != nil {
		return fmt.Errorf("ビルドに失敗しました: %w", err)
	}
	if err := r.prepareHome(spec); err != nil {
		return err
	}
	_, _ = fmt.Fprintf(out, "テストセッション %s を開きます(データ: %s)\n",
		spec.Session, spec.ConductorHome)
	return r.Terminal.Launch(spec.Session, spec.LaunchCommand())
}

// resolve は指定から worktree の絶対パスを決める。
func (r *TestSessionRunner) resolve(input string) (string, error) {
	mainRoot := r.Locator.MainRoot()
	if input == "" {
		chosen, err := r.chooseWorktree(mainRoot)
		if err != nil {
			return "", err
		}
		input = chosen
	}

	path := domain.ResolveWorktree(input, mainRoot, r.Locator.IsDir)
	if path == "" {
		return "", fmt.Errorf("worktree が見つかりません: %s", input)
	}
	abs, err := r.Locator.Abs(path)
	if err != nil {
		return "", fmt.Errorf("worktree の場所を決められません: %w", err)
	}
	// mdev-go の worktree かどうかを確かめる。別のリポジトリを指したまま
	// ビルドに進むと、失敗の理由が go の出力にしか出てこない。
	if !r.Locator.IsDir(filepath.Join(abs, "cmd", "mdev")) {
		return "", fmt.Errorf("mdev-go の worktree ではありません(cmd/mdev がない): %s", abs)
	}
	return abs, nil
}

// chooseWorktree は .worktree/ の一覧から選ばせる。
func (r *TestSessionRunner) chooseWorktree(mainRoot string) (string, error) {
	if mainRoot == "" {
		return "", errors.New("リポジトリの中で実行してください")
	}
	names := r.Locator.List(filepath.Join(mainRoot, domain.WorktreeDirName))
	if len(names) == 0 {
		return "", fmt.Errorf("%s に worktree がありません", domain.WorktreeDirName)
	}
	chosen, err := r.Chooser.Choose("worktree を選ぶ", names)
	if err != nil {
		return "", err
	}
	if chosen == "" {
		return "", errors.New("選ばれませんでした")
	}
	return chosen, nil
}

// prepareHome は隔離したデータディレクトリを整える。
//
// 既定の設定を置き、レイアウトを **組んだバイナリの絶対パス**へ向けて
// 書き出す。CONDUCTOR_HOME 経由のままだと、置き忘れたときに設置済みの
// バイナリが動いて「切り替えたのに直っていない」になる。
//
// **毎回書き直す。** 試すたびに worktree の今の姿で始まってほしいので、
// ここに「利用者のカスタマイズ」という概念は無い。
func (r *TestSessionRunner) prepareHome(spec domain.TestSessionSpec) error {
	layout, ok := r.Assets.Asset("layouts/multi.kdl")
	if !ok {
		return errors.New("同梱されていない資産です: layouts/multi.kdl")
	}
	if err := r.Files.Write(spec.Layout,
		[]byte(domain.RenderTestLayout(string(layout), spec.Binary))); err != nil {
		return err
	}

	defaults, ok := r.Assets.Asset("config.default.json")
	if !ok {
		return errors.New("同梱されていない資産です: config.default.json")
	}
	home := filepath.Dir(filepath.Dir(spec.Layout))
	if err := r.Files.Write(filepath.Join(home, "config.default.json"), defaults); err != nil {
		return err
	}
	// 設定そのものは残す。試している間に変えた内容が毎回戻ると確かめにくい。
	config := filepath.Join(home, "config.json")
	if r.Files.Exists(config) {
		return nil
	}
	return r.Files.Write(config, defaults)
}

// report は dry-run の内容を出す。
func (r *TestSessionRunner) report(out io.Writer, spec domain.TestSessionSpec) error {
	_, _ = fmt.Fprintf(out, "WORKTREE=%s\n", spec.Worktree)
	_, _ = fmt.Fprintf(out, "CONDUCTOR_HOME=%s\n", spec.ConductorHome)
	_, _ = fmt.Fprintf(out, "BINARY=%s\n", spec.Binary)
	_, _ = fmt.Fprintf(out, "SESSION=%s\n", spec.Session)
	_, _ = fmt.Fprintf(out, "CMD=%s\n", spec.LaunchCommand())
	return nil
}
