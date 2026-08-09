package cli

import (
	"fmt"
	"io"

	"github.com/spf13/cobra"

	"github.com/k-kudo-hub/mdev-go/internal/app"
)

// HookSettingsService は settings.json の hooks を切り替える / 元へ戻す
// ユースケースである。実体は app.HookSwitcher。
type HookSettingsService interface {
	Switch(dryRun bool) (app.SwitchHooksResult, error)
	Restore(dryRun bool) (app.RestoreHooksResult, error)
}

// dryRunFlag は書き込みを行わないことを指定するフラグ名。
const dryRunFlag = "dry-run"

// newHooksCommand は `mdev hooks` とその子コマンドを組み立てる。
//
// Claude Code から呼ばれる `mdev hook`(単数)とは別で、こちらは利用者が
// 手で実行して Claude Code の設定そのものを書き換えるためのコマンドである。
func newHooksCommand(deps Deps) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "hooks",
		Short: "Claude Code の settings.json の hooks を切り替える",
		Long: "~/.claude/settings.json の hooks に登録されている conductor の\n" +
			"シェルスクリプト呼び出しを `mdev hook` サブコマンドへ差し替える。\n" +
			"書き換えは同じディレクトリへバックアップを作ってから原子的に行い、\n" +
			"hooks 以外のキー(permissions など)には触れない。",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return cmd.Help()
		},
	}
	cmd.AddCommand(newHooksSwitchCommand(deps), newHooksRestoreCommand(deps))
	return cmd
}

// newHooksSwitchCommand は `mdev hooks switch` を組み立てる。
func newHooksSwitchCommand(deps Deps) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "switch",
		Short: "hooks を mdev のサブコマンドへ切り替える",
		Long: "hooks のスクリプト呼び出しを `mdev hook` へ差し替える。\n" +
			"既に切り替え済みなら何もしない(何度実行しても結果は同じ)。",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			dryRun, err := cmd.Flags().GetBool(dryRunFlag)
			if err != nil {
				return err
			}
			result, err := deps.HookSettings.Switch(dryRun)
			if err != nil {
				return err
			}
			printSwitchResult(cmd.OutOrStdout(), result)
			return nil
		},
	}
	addDryRunFlag(cmd)
	return cmd
}

// newHooksRestoreCommand は `mdev hooks restore` を組み立てる。
func newHooksRestoreCommand(deps Deps) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "restore",
		Short: "最新のバックアップから settings.json を復元する",
		Long: "`mdev hooks switch` が作った最新のバックアップで settings.json を\n" +
			"置き換える。バックアップが無い場合と既に一致している場合は\n" +
			"何もせず、その状態を報告する。",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			dryRun, err := cmd.Flags().GetBool(dryRunFlag)
			if err != nil {
				return err
			}
			result, err := deps.HookSettings.Restore(dryRun)
			if err != nil {
				return err
			}
			printRestoreResult(cmd.OutOrStdout(), result)
			return nil
		},
	}
	addDryRunFlag(cmd)
	return cmd
}

// addDryRunFlag は --dry-run を追加する。
func addDryRunFlag(cmd *cobra.Command) {
	cmd.Flags().Bool(dryRunFlag, false, "変更内容を表示するだけで書き込まない")
}

// printSwitchResult は switch の結果を人が読める形で書き出す。
func printSwitchResult(w io.Writer, result app.SwitchHooksResult) {
	// 出力先へ書けない状況で追加の報告先は無いため、書き込み失敗は無視する。
	_, _ = fmt.Fprintf(w, "settings.json: %s\n", result.SettingsPath)

	if len(result.Changes) == 0 {
		_, _ = fmt.Fprintln(w, "hooks は既に mdev を指しています。変更はありません。")
		return
	}

	_, _ = fmt.Fprintf(w, "\n置き換える hook コマンド(%d 件):\n", len(result.Changes))
	for _, c := range result.Changes {
		_, _ = fmt.Fprintf(w, "  [%s]\n    - %s\n    + %s\n", c.Event, c.Before, c.After)
	}
	_, _ = fmt.Fprintln(w)

	if result.DryRun {
		_, _ = fmt.Fprintln(w, "--dry-run のため書き込んでいません。")
		return
	}
	_, _ = fmt.Fprintf(w, "バックアップ: %s\n", result.BackupPath)
	_, _ = fmt.Fprintln(w, "hooks を mdev へ切り替えました。")
}

// printRestoreResult は restore の結果を人が読める形で書き出す。
func printRestoreResult(w io.Writer, result app.RestoreHooksResult) {
	_, _ = fmt.Fprintf(w, "settings.json: %s\n", result.SettingsPath)

	if !result.Found {
		_, _ = fmt.Fprintln(w, "mdev が作ったバックアップがありません。"+
			"`mdev hooks switch` を実行していないか、既に手作業で片付けられています。")
		return
	}
	_, _ = fmt.Fprintf(w, "バックアップ: %s\n", result.BackupPath)

	if !result.Changed {
		_, _ = fmt.Fprintln(w, "settings.json はバックアップと同じ内容です。変更はありません。")
		return
	}
	if result.DryRun {
		_, _ = fmt.Fprintln(w, "--dry-run のため書き込んでいません。")
		return
	}
	_, _ = fmt.Fprintln(w, "settings.json をバックアップの内容へ復元しました。")
}
