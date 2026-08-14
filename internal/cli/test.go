package cli

import (
	"io"

	"github.com/spf13/cobra"
)

// TestService は `mdev test` のユースケースである。実体は app.TestSessionRunner。
type TestService interface {
	RunTest(out io.Writer, worktree string, dryRun bool) error
}

// newTestCommand は `mdev test` を組み立てる。
func newTestCommand(deps Deps) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "test [worktree]",
		Short: "worktree のソースを隔離環境で試す",
		Long: "worktree のソースから mdev を組み、その worktree の中の隔離した\n" +
			"データディレクトリでセッションを新しい端末の窓に開く。\n" +
			"設置済みの環境には一切触れないため、2 つの worktree を同時に試せる。\n" +
			"引数はパスでもブランチ名でもよく、省くと .worktree/ から選ぶ。",
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			dryRun, err := cmd.Flags().GetBool(dryRunFlag)
			if err != nil {
				return err
			}
			worktree := ""
			if len(args) > 0 {
				worktree = args[0]
			}
			return deps.Test.RunTest(cmd.OutOrStdout(), worktree, dryRun)
		},
	}
	cmd.Flags().Bool(dryRunFlag, false, "起動せずに解決した内容だけを出す")
	return cmd
}
