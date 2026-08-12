package cli

import (
	"fmt"

	"github.com/spf13/cobra"
)

// DevVersion はビルド時に版を焼き込まなかった場合の値である。
//
// `go build` をそのまま実行した開発中のバイナリがこれになる。自己更新は
// この値のとき何もしない(比較する土台が無く、手元のビルドを配布物で
// 上書きしてしまうため)。
const DevVersion = "dev"

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
