package cli

import (
	"fmt"
	"io"

	"github.com/spf13/cobra"
)

// AssetService は同梱資産の解決である。実体は infra の資産読み取り。
//
// 他のコマンドと違って app のユースケースを挟まない。ここでやることは
// 「名前を受けて中身を返す」だけで、判断が 1 つも無いためである。
type AssetService interface {
	// Names は同梱されている資産の名前を返す。
	Names() []string
	// Read は資産の中身を返す。CONDUCTOR_HOME の実ファイルがあればそれを、
	// 無ければ実行ファイルへ埋め込まれたものを返す。
	Read(name string) ([]byte, error)
}

// newAssetsCommand は `mdev assets` を組み立てる。
//
// 設置の道具である。6-3 のインストーラがここから資産を取り出して置く。
// 引数が無ければ名前の一覧を出す。
func newAssetsCommand(deps Deps) *cobra.Command {
	return &cobra.Command{
		Use:   "assets [名前]",
		Short: "同梱の資産を取り出す",
		Long: "レイアウトや既定設定を標準出力へ書き出す。CONDUCTOR_HOME に同じ名前の\n" +
			"実ファイルがあればそちらを、無ければ実行ファイルに埋め込まれたものを出す。\n" +
			"名前を省くと、取り出せる資産の一覧を出す。",
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 {
				return listAssets(deps.Assets, cmd.OutOrStdout())
			}
			body, err := deps.Assets.Read(args[0])
			if err != nil {
				return err
			}
			if _, err := cmd.OutOrStdout().Write(body); err != nil {
				return fmt.Errorf("資産の書き出しに失敗しました: %w", err)
			}
			return nil
		},
	}
}

// listAssets は資産の名前を 1 行ずつ書き出す。
func listAssets(service AssetService, out io.Writer) error {
	for _, name := range service.Names() {
		if _, err := fmt.Fprintln(out, name); err != nil {
			return fmt.Errorf("一覧の書き出しに失敗しました: %w", err)
		}
	}
	return nil
}
