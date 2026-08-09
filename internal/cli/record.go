package cli

import (
	"github.com/spf13/cobra"

	"github.com/k-kudo-hub/mdev-go/internal/app"
)

// RecordService はタスクの作業サマリを daily log へ記録するユースケースである。
// 実体は app.RecordOutput で、テストでは記録用の実装に差し替える。
type RecordService interface {
	Execute(tab string, env app.RecordEnv) error
}

// newRecordCommand は `mdev record [tab]` を組み立てる。
func newRecordCommand(deps Deps) *cobra.Command {
	return &cobra.Command{
		Use:   "record [tab]",
		Short: "タスクの作業サマリを daily log へ記録する",
		Long: "タスクタブを閉じるときに呼ばれ、pending と transcript から作業サマリを\n" +
			"$CONDUCTOR_HOME/daily/<セッション名>/<日付>.jsonl へ 1 行追記する。\n" +
			"pending は削除しない。",
		// タブ名を省略した場合は何もせず正常終了する(現行版と同じ)。
		// 判断はユースケース側にあるため、ここでは空文字を渡す。
		Args: cobra.MaximumNArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			tab := ""
			if len(args) > 0 {
				tab = args[0]
			}
			return deps.Record.Execute(tab, app.RecordEnv{
				ZellijSession: deps.Getenv(envZellijSession),
			})
		},
	}
}
