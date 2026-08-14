package cli

import (
	"fmt"
	"io"
	"os"

	"github.com/spf13/cobra"
)

// 終了コード。
//
// Claude Code の hook 仕様(https://code.claude.com/docs/en/hooks)では
// 終了コードの意味が次の 3 通りに分かれる(2026-08-09 時点)。
//
//   - 0: 成功。stdout が JSON 出力として解釈される
//   - 2: ブロッキングエラー。stderr が Claude へ渡され、Stop では会話が
//     止まらなくなり、UserPromptSubmit ではユーザーの入力が消える
//   - それ以外: 非ブロッキングエラー。処理はそのまま進み、transcript に
//     `<hook name> hook error` と stderr の 1 行目が出る
//
// mdev の hook は pending ファイルとレジストリの更新という補助的な副作用で
// あり、失敗しても会話を止めるべきではない。そのため失敗時は 2 ではなく 1 を
// 返す(非ブロッキング。失敗したことは stderr 経由で transcript に現れる)。
const (
	exitOK    = 0
	exitError = 1
	// exitBlocking は Claude Code がブロッキングエラーとして扱う終了コードで、
	// mdev は返さない。使っていないことをテストで固定するために置いている。
	exitBlocking = 2
)

// Deps はサブコマンドが必要とする依存である。組み立ては cmd/mdev が行う。
type Deps struct {
	// Hooks は hook イベントを処理するユースケース。
	Hooks HookService
	// Record は作業サマリを daily log へ記録するユースケース。
	Record RecordService
	// HookSettings は settings.json の hooks を切り替えるユースケース。
	HookSettings HookSettingsService
	// Panes はダッシュボード系ペインを動かすユースケース。
	Panes PaneService
	// Update は conductor を最新のリリースへ入れ直すユースケース。
	Update UpdateService
	// UpdateCheck は起動時の更新確認のユースケース。
	UpdateCheck UpdateCheckService
	// SessionClean は溜まったセッションと残骸を片付けるユースケース。
	SessionClean SessionCleanService
	// News は当日の AI 関連ニュースを取得するユースケース。
	News NewsService
	// Codex は codex の notify を処理するユースケース。
	Codex CodexService
	// Agent は設定されたエージェント CLI を起動するユースケース。
	Agent AgentService
	// Version はビルド時に焼き込まれた mdev の版である。
	//
	// 焼き込まれていない場合は空になる(参照は VersionOrDev を通す)。
	Version string
	// Getenv は環境変数を読む。テストで差し替える。
	Getenv func(string) string
}

// VersionOrDev は焼き込まれた版を返す。空なら DevVersion を返す。
//
// 空を「版が無い」ではなく「開発中のビルド」として扱うのは、ldflags を
// 付け忘れたビルドと `go build` そのままのビルドを区別する意味が無いためで
// ある。どちらも配布物ではないので、自己更新は行わない。
func (d Deps) VersionOrDev() string {
	if d.Version == "" {
		return DevVersion
	}
	return d.Version
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
	cmd.AddCommand(newPaneCommand(deps))
	cmd.AddCommand(newVersionCommand(deps))
	cmd.AddCommand(newSessionsCommand(deps))
	cmd.AddCommand(newUpdateCommand(deps))
	cmd.AddCommand(newCheckUpdateCommand(deps))
	cmd.AddCommand(newNewsCommand(deps))
	cmd.AddCommand(newCodexCommand(deps))
	cmd.AddCommand(newAgentCommand(deps))
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
