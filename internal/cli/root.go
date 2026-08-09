package cli

import (
	"fmt"
	"io"
	"os"

	"github.com/spf13/cobra"
)

// 終了コード。hook は Claude Code から呼ばれるため、失敗しても会話を
// 止めない非ブロッキングの扱い(0 と 2 以外)にする。
const (
	exitOK    = 0
	exitError = 1
)

// Deps はサブコマンドが必要とする依存である。組み立ては cmd/mdev が行う。
type Deps struct {
	// Hooks は hook イベントを処理するユースケース。
	Hooks HookService
	// Record は作業サマリを daily log へ記録するユースケース。
	Record RecordService
	// HookSettings は settings.json の hooks を切り替えるユースケース。
	HookSettings HookSettingsService
	// Getenv は環境変数を読む。テストで差し替える。
	Getenv func(string) string
}

// NewRootCommand は mdev のルートコマンドを組み立てる。
func NewRootCommand(deps Deps) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "mdev",
		Short: "Zellij 上のコーディングエージェントセッションを統括する",
		// hook から呼ばれる経路でエラー時に使い方が出力されると、
		// Claude Code の画面が読みづらくなるため抑止する。
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	cmd.AddCommand(newHookCommand(deps))
	cmd.AddCommand(newRecordCommand(deps))
	cmd.AddCommand(newHooksCommand(deps))
	return cmd
}

// Execute はルートコマンドを実行し、プロセスの終了コードを返す。
// エラーは標準エラー出力に出す。
func Execute(deps Deps) int {
	return execute(NewRootCommand(deps), os.Stderr)
}

// execute はコマンドを実行し、エラーを stderr へ書いて終了コードを返す。
func execute(cmd *cobra.Command, stderr io.Writer) int {
	if err := cmd.Execute(); err != nil {
		// 出力先へ書けない状況で追加の報告先は無いため、書き込み失敗は無視する。
		_, _ = fmt.Fprintln(stderr, "mdev:", err)
		return exitError
	}
	return exitOK
}
