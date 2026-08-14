package cli

import "github.com/spf13/cobra"

// NewsService は `mdev news fetch` のユースケースである。実体は
// app.NewsRefresher。
//
// **error を返さない**のは、ニュースの取得に失敗してもセッションの起動を
// 止めないためである(現行 fetch-news.sh もすべての失敗経路で黙って
// exit 0 する)。その判断はユースケース側にある。
type NewsService interface {
	Refresh(force bool)
}

// newNewsCommand は `mdev news` とその配下を組み立てる。
func newNewsCommand(deps Deps) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "news",
		Short: "AI 関連ニュースを扱う",
	}
	cmd.AddCommand(newNewsFetchCommand(deps))
	return cmd
}

// newNewsFetchCommand は `mdev news fetch` を組み立てる。
func newNewsFetchCommand(deps Deps) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "fetch",
		Short: "当日の AI 関連ニュースを取得する",
		Long: "フィードを引いて $CONDUCTOR_HOME/news/<日付>.json へ保存し、保持期間を過ぎた\n" +
			"古いファイルを消す。セッションの起動ごとに走るため、当日分が既にあれば\n" +
			"何もしない(--force で取り直す)。取得に失敗しても何も出さずに終わる。",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			force, err := cmd.Flags().GetBool(forceFlag)
			if err != nil {
				return err
			}
			deps.News.Refresh(force)
			return nil
		},
	}
	cmd.Flags().Bool(forceFlag, false, "当日分があっても取り直す")
	return cmd
}
