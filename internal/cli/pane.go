package cli

import (
	"fmt"
	"io"

	"github.com/spf13/cobra"
)

// paneNames は `mdev pane` が受け付けるペイン名である。
// 実体のモデルは internal/tui にあるが、cli は tui を参照できない
// (ADR-0002)ため、引数の検証に使う名前だけをここに持つ。
var paneNames = []string{"dashboard", "waiting", "done", "news"}

// PaneService はダッシュボード系ペインを動かすユースケースである。
// 実体は internal/tui の Panes で、組み立ては cmd/mdev が行う。
type PaneService interface {
	// Run はペインを対話モードで動かす。ペインが終了するまで戻らない。
	Run(name string) error
	// Once はペインを 1 回だけ描画した文字列を返す。
	Once(name string) (string, error)
}

// newPaneCommand は `mdev pane <name> [--once]` を組み立てる。
func newPaneCommand(deps Deps) *cobra.Command {
	var once bool

	cmd := &cobra.Command{
		Use:   "pane <" + paneNames[0] + "|" + paneNames[1] + "|" + paneNames[2] + "|" + paneNames[3] + ">",
		Short: "ダッシュボード系のペインを表示する",
		Long: "Zellij の Main タブに並ぶ 4 つのペインを表示する。\n" +
			"レイアウト(layouts/multi.kdl)のペイン起動コマンドから呼ばれる。\n\n" +
			"  dashboard  実行中タスクの一覧([番号] で移動 / d+[番号] で削除)\n" +
			"  waiting    外部の返答待ちタスクの一覧(キー操作なし)\n" +
			"  done       当日の完了タスクの一覧(r+[番号] でダッシュボードへ戻す)\n" +
			"  news       AI 関連ニュース([番号] で開く / r で取得し直す)",
		Args:      cobra.MatchAll(cobra.ExactArgs(1), cobra.OnlyValidArgs),
		ValidArgs: paneNames,
		RunE: func(cmd *cobra.Command, args []string) error {
			name := args[0]
			if !once {
				return deps.Panes.Run(name)
			}
			out, err := deps.Panes.Once(name)
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
