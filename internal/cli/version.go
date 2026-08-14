package cli

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/k-kudo-hub/mdev-go/internal/app"
)

// DevVersion はビルド時に版を焼き込まなかった場合の値である。
//
// 値の出どころは domain で、app が境界へ出している。ここで別に定義すると
// 3 か所で同じ文字列を持つことになり、片方だけ変えても気づけない。
const DevVersion = app.DevVersion

// newVersionCommand は `mdev version` を組み立てる。
//
// 出力は版の文字列 1 行だけにする。install がバイナリの自己申告と
// VERSION ファイルの一致を確かめる(ADR-0004 D3)ので、機械が読む前提の
// 出力にしておく。
func newVersionCommand(deps Deps) *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "mdev の版を表示する",
		Long: "ビルド時に焼き込まれた版を表示する。\n" +
			"焼き込まれていない(手元でビルドした)場合は \"dev\" になる。",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			// 出力先へ書けない状況で追加の報告先は無いため、失敗は無視する。
			_, _ = fmt.Fprintln(cmd.OutOrStdout(), deps.VersionOrDev())
			return nil
		},
	}
}
