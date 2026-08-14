package cli

import (
	"io"

	"github.com/spf13/cobra"
)

// InstallService は `mdev install` のユースケースである。実体は app.Installer。
type InstallService interface {
	Install(out io.Writer) error
}

// UninstallService は `mdev uninstall` のユースケースである。実体は app.Uninstaller。
type UninstallService interface {
	Uninstall(out io.Writer, keepData bool) error
}

// keepDataFlag はデータを残して設定だけ外すことを指定するフラグ名。
const keepDataFlag = "keep-data"

// newInstallCommand は `mdev install` を組み立てる。
func newInstallCommand(deps Deps) *cobra.Command {
	return &cobra.Command{
		Use:   "install",
		Short: "mdev を設置して既存の環境を移行する",
		Long: "同梱の資産を CONDUCTOR_HOME へ配り、Claude Code の hooks と codex の notify を\n" +
			"mdev へ向け、残っている Shell スクリプトを撤去する。\n" +
			"何度実行しても同じ状態になり、2 回目はファイルを 1 つも書き換えない。\n" +
			"利用者のデータ(config.json の中身・daily・tasks・news)には触らない。",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return deps.Install.Install(cmd.OutOrStdout())
		},
	}
}

// newUninstallCommand は `mdev uninstall` を組み立てる。
func newUninstallCommand(deps Deps) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "uninstall",
		Short: "mdev を取り除く",
		Long: "Claude Code の hooks と codex の notify から mdev を外し、CONDUCTOR_HOME と\n" +
			"pending を削除する。--keep-data を付けると設定の解除だけを行い、\n" +
			"作業ログ(daily)やタスクの記録は残す。",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			keep, err := cmd.Flags().GetBool(keepDataFlag)
			if err != nil {
				return err
			}
			return deps.Uninstall.Uninstall(cmd.OutOrStdout(), keep)
		},
	}
	cmd.Flags().Bool(keepDataFlag, false, "CONDUCTOR_HOME と pending を残す")
	return cmd
}
