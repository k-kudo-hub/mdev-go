package shell

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/k-kudo-hub/mdev-go/internal/app"
)

// 端末の窓を開く手順は端末ごとに違う。現行 mdev-test の分岐をそのまま写す。
//
//   - Warp: 一時的な Launch Configuration を書いて `warp://launch/<名前>` を
//     開く。別アプリの自動化許可が要らず、worktree の中で始まる
//   - iTerm: `.command` を開いても確実に走らないため、スクリプティング API を
//     叩く。許可が下りていなければ Terminal.app へ落ちる
//   - それ以外: `.command` を書いて Terminal.app に開かせる。LaunchServices
//     経由なので自動化の許可が要らず、ログインシェルの PATH で走る

// terminalProgramEnv は今の端末を見分ける環境変数である。
const terminalProgramEnv = "TERM_PROGRAM"

// warpProgram / itermProgram は分岐に使う端末の名前である。
const (
	warpProgram  = "WarpTerminal"
	itermProgram = "iTerm.app"
)

// Terminal は新しい端末の窓でコマンドを走らせる app.TerminalLauncher の
// 実装である。
type Terminal struct {
	// program は今の端末の名前。テストで差し替える。
	program string
	// home は Warp の設定を置く場所。テストで差し替える。
	home string
	// tempDir は一時スクリプトの置き場所。テストで差し替える。
	tempDir string
	// run はコマンドを実行する。テストで差し替える。
	run func(name string, args ...string) error
}

var _ app.TerminalLauncher = (*Terminal)(nil)

// NewTerminal は今の端末を見て動く Terminal を返す。
func NewTerminal(home string) *Terminal {
	return &Terminal{
		program: os.Getenv(terminalProgramEnv),
		home:    home,
		tempDir: os.TempDir(),
		run:     runCommand,
	}
}

// Launch は title の窓を開いて command を走らせる。
func (t *Terminal) Launch(title, command string) error {
	if t.program == warpProgram {
		return t.launchWarp(title, command)
	}

	script, err := t.writeLaunchScript(command)
	if err != nil {
		return err
	}
	if t.program == itermProgram {
		// 窓の生成と実行を 1 文にまとめる。分けると、途中で失敗したときに
		// 空の窓と Terminal の窓が両方残る。
		osa := fmt.Sprintf(
			`tell application "iTerm" to create window with default profile command "bash '%s'"`, script)
		if err := t.run("osascript", "-e", osa); err == nil {
			return nil
		}
	}
	return t.run("open", "-a", "Terminal", script)
}

// launchWarp は Warp の Launch Configuration を書いて開く。
func (t *Terminal) launchWarp(title, command string) error {
	dir := filepath.Join(t.home, ".warp", "launch_configurations")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("warp の設定を置けません: %w", err)
	}
	name := "mdev-test-" + title
	body := strings.Join([]string{
		"---",
		"name: " + name,
		"windows:",
		"  - tabs:",
		"      - title: " + title,
		"        layout:",
		"          commands:",
		"            - exec: " + yamlQuote(command),
		"",
	}, "\n")
	path := filepath.Join(dir, name+".yaml")
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil { //nolint:gosec // 設定は他プロセスが読む
		return fmt.Errorf("warp の設定を書けません: %w", err)
	}
	return t.run("open", "warp://launch/"+name)
}

// writeLaunchScript は Terminal.app が開ける `.command` を書く。
//
// 走り終えたら自分を消す。試すたびに一時ファイルが積み上がらないようにする
// (現行版と同じ)。
func (t *Terminal) writeLaunchScript(command string) (string, error) {
	f, err := os.CreateTemp(t.tempDir, "mdev-test-*.command")
	if err != nil {
		return "", fmt.Errorf("起動スクリプトを作れません: %w", err)
	}
	path := f.Name()
	body := "#!/bin/bash\n" + command + "\nrm -f '" + path + "'\n"
	if _, err := f.WriteString(body); err != nil {
		_ = f.Close()
		return "", fmt.Errorf("起動スクリプトを書けません: %w", err)
	}
	if err := f.Close(); err != nil {
		return "", fmt.Errorf("起動スクリプトを閉じられません: %w", err)
	}
	if err := os.Chmod(path, 0o755); err != nil {
		return "", fmt.Errorf("起動スクリプトに実行権限を付けられません: %w", err)
	}
	return path, nil
}

// yamlQuote は YAML の二重引用符付き文字列にする。
func yamlQuote(s string) string {
	escaped := strings.ReplaceAll(s, `\`, `\\`)
	escaped = strings.ReplaceAll(escaped, `"`, `\"`)
	return `"` + escaped + `"`
}

// runCommand は外部コマンドを実行する。
func runCommand(name string, args ...string) error {
	return exec.Command(name, args...).Run() //nolint:gosec // 呼び出し側が組み立てた固定の並び
}
