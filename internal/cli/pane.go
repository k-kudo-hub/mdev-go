package cli

import (
	"fmt"
	"io"
	"strings"

	"github.com/spf13/cobra"
)

// paneNames は `mdev pane` が受け付けるペイン名である。
// 実体のモデルは internal/tui にあるが、cli は tui を参照できない
// (ADR-0002)ため、引数の検証に使う名前だけをここに持つ。
var paneNames = []string{"dashboard", "waiting", "done", "news", "task-create", "task-control"}

// paneTaskControl はタブ名を第 2 引数に取る唯一のペインである。
const paneTaskControl = "task-control"

// PaneService はダッシュボード系ペインを動かすユースケースである。
// 実体は internal/tui の Panes で、組み立ては cmd/mdev が行う。
type PaneService interface {
	// Run はペインを対話モードで動かす。ペインが終了するまで戻らない。
	// arg は task-control のタブ名で、他のペインでは空文字である。
	Run(name, arg string) error
	// Once はペインを 1 回だけ描画した文字列を返す。
	Once(name, arg string) (string, error)
}

// validPaneName は第 1 引数がペイン名として正しいかを検証する。
func validPaneName(_ *cobra.Command, args []string) error {
	if len(args) == 0 {
		return nil
	}
	for _, name := range paneNames {
		if args[0] == name {
			return nil
		}
	}
	return fmt.Errorf("不正な引数 %q です。使えるのは %s のいずれかです",
		args[0], strings.Join(paneNames, ", "))
}

// newPaneCommand は `mdev pane <name> [タブ名] [--once]` を組み立てる。
func newPaneCommand(deps Deps) *cobra.Command {
	var once bool

	cmd := &cobra.Command{
		Use:   "pane <" + strings.Join(paneNames, "|") + "> [タブ名]",
		Short: "conductor のペインを表示する",
		Long: "Zellij のペイン起動コマンド(layouts/multi.kdl と create_task)から呼ばれる。\n\n" +
			"  dashboard     実行中タスクの一覧([番号] で移動 / d+[番号] で削除)\n" +
			"  waiting       外部の返答待ちタスクの一覧(キー操作なし)\n" +
			"  done          当日の完了タスクの一覧(r+[番号] でダッシュボードへ戻す)\n" +
			"  news          AI 関連ニュース([番号] で開く / r で取得し直す)\n" +
			"  task-create   タスク作成(n で開始)\n" +
			"  task-control  タスクタブの操作バー(m: Main / w: Waiting / dd: 削除)\n" +
			"                第 2 引数にタブ名を取る",
		// 第 2 引数は任意のタブ名なので、名前の検証は第 1 引数だけに掛ける
		// (cobra.OnlyValidArgs は全引数を検証してしまう)。
		Args:      cobra.MatchAll(cobra.RangeArgs(1, 2), validPaneName),
		ValidArgs: paneNames,
		RunE: func(cmd *cobra.Command, args []string) error {
			name := args[0]
			arg := ""
			if len(args) > 1 {
				arg = args[1]
			}
			if name == paneTaskControl && arg == "" {
				return fmt.Errorf("%s はタブ名を引数に取ります", paneTaskControl)
			}
			if !once {
				return deps.Panes.Run(name, arg)
			}
			out, err := deps.Panes.Once(name, arg)
			if err != nil {
				return err
			}
			if _, err := io.WriteString(cmd.OutOrStdout(), out); err != nil {
				return fmt.Errorf("ペインの出力に失敗しました: %w", err)
			}
			return nil
		},
	}

	cmd.Flags().BoolVar(&once, "once", false,
		"1 回だけ描画して終了する(表示の突き合わせ用)")
	return cmd
}
