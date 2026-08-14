package cli

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/k-kudo-hub/mdev-go/internal/app"
)

// newInitCommand は `mdev init` とその配下を組み立てる。
func newInitCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "init",
		Short: "シェルへ読み込ませる定義を出力する",
	}
	cmd.AddCommand(&cobra.Command{
		Use:   "zsh",
		Short: "zsh 用のエイリアス定義を出力する",
		Long: "$CONDUCTOR_HOME/init.zsh から eval される。エイリアスを足しても\n" +
			".zshrc も init.zsh も書き換えずに済むよう、定義はここが持つ。",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			// 出力先へ書けない状況で追加の報告先は無いため失敗は無視する。
			_, _ = fmt.Fprint(cmd.OutOrStdout(), app.InitZshScript)
			return nil
		},
	})
	return cmd
}
