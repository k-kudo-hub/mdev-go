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
			"hooks 以外のキー(permissions の類)には触れない。",
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
				printSwitchFailure(cmd.ErrOrStderr(), result)
				return err
			}
			printSwitchResult(cmd.OutOrStdout(), result)
			printSwitchWarnings(cmd.ErrOrStderr(), result)
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
		Short: "hooks を conductor のスクリプトへ戻す",
		Long: "`mdev hook` の呼び出しを conductor のスクリプト呼び出しへ戻す。\n" +
			"switch と逆向きの差し替えなので、切り替え後に settings.json へ\n" +
			"加わった hooks 以外の変更(permissions の類)はそのまま残る。\n" +
			"settings.json ごと失われている場合に限り、`mdev hooks switch` が\n" +
			"作った最新のバックアップで書き戻す。",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			dryRun, err := cmd.Flags().GetBool(dryRunFlag)
			if err != nil {
				return err
			}
			result, err := deps.HookSettings.Restore(dryRun)
			if err != nil {
				printRestoreFailure(cmd.ErrOrStderr(), result)
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
	} else {
		printChanges(w, "置き換える hook コマンド", result.Changes)
		if !result.DryRun {
			_, _ = fmt.Fprintf(w, "バックアップ: %s\n", result.BackupPath)
			_, _ = fmt.Fprintln(w, "hooks を mdev へ切り替えました。")
		}
	}
	if printDryRunNotice(w, result.DryRun) {
		return
	}
	// 印は hooks の変更の有無に依らず書き直すので、どちらの経路でも出す。
	_, _ = fmt.Fprintf(w, "Go 版を使う印: %s\n", result.FlavorPath)
}

// printDryRunNotice は dry-run のときだけ断りを 1 行出し、出したかを返す。
//
// 呼び出し側はこれが true なら「実際に書いた結果」を出さずに終える。
// 経路ごとに同じ早期 return を書くと、経路が増えたときに 2 度出たり
// 出なかったりする(実際に settings.json 欠損 + バックアップありの
// dry-run で 2 度出ていた)。断りを出す場所はここ 1 つに保つ。
func printDryRunNotice(w io.Writer, dryRun bool) bool {
	if !dryRun {
		return false
	}
	// 出力先へ書けない状況で追加の報告先は無いため、書き込み失敗は無視する。
	_, _ = fmt.Fprintln(w, "--dry-run のため書き込んでいません。")
	return true
}

// printSwitchWarnings は切り替えは成功したが利用者が対処すべき事柄を書き出す。
//
// dry-run でも出す。実行してからでは遅い類の警告だからである。
func printSwitchWarnings(w io.Writer, result app.SwitchHooksResult) {
	if result.MissingBinaryPath != "" {
		// 出力先へ書けない状況で追加の報告先は無いため、書き込み失敗は無視する。
		_, _ = fmt.Fprintf(w, "\n警告: %s が見つかりません。\n", result.MissingBinaryPath)
		_, _ = fmt.Fprintln(w, "  切り替え後の hooks はこのパスを呼びます。hook の失敗は会話を止めませんが、")
		_, _ = fmt.Fprintln(w, "  pending が書かれずダッシュボードが反応しなくなります。")
		_, _ = fmt.Fprintln(w, "  `make install` で配置してください。")
	}

	if len(result.RemainingScripts) > 0 {
		_, _ = fmt.Fprintf(w, "\n警告: 切り替えられなかった conductor スクリプトの呼び出しが残っています(%d 件):\n",
			len(result.RemainingScripts))
		for _, c := range result.RemainingScripts {
			_, _ = fmt.Fprintf(w, "  [%s] %s\n", c.Event, c.Command)
		}
		_, _ = fmt.Fprintln(w, "  既知の 3 種類と一致しないため手で確認してください。")
		_, _ = fmt.Fprintln(w, "  このイベントだけ Shell 版のまま動きます。")
	}
}

// printSwitchFailure は switch が失敗したときの後始末の手掛かりを書き出す。
//
// エラーメッセージだけでは settings.json が半端な状態かどうか、どこに控えが
// あるのかが分からない。失敗した場所で伝えるべきことが変わる。
//
//   - settings.json の書き込みまでに失敗した: 書き込みは原子的な置き換えなので
//     settings.json は元のままである
//   - 書き込みの後(印の書き込み)で失敗した: settings.json は既に切り替わって
//     いる。ここで「変更されていません」と伝えると嘘になり、利用者は
//     切り替わった settings.json を元のままだと思い込む
func printSwitchFailure(w io.Writer, result app.SwitchHooksResult) {
	// 出力先へ書けない状況で追加の報告先は無いため、書き込み失敗は無視する。
	if result.SettingsWritten {
		_, _ = fmt.Fprintf(w, "settings.json は mdev へ切り替え済みです。バックアップ: %s\n", result.BackupPath)
		_, _ = fmt.Fprintln(w, "  元へ戻すには `mdev hooks restore` を実行してください。")
		return
	}
	if result.BackupPath == "" {
		return
	}
	_, _ = fmt.Fprintf(w, "settings.json は変更されていません。バックアップ: %s\n", result.BackupPath)
}

// printRestoreFailure は restore が失敗したときの後始末の手掛かりを書き出す。
//
// printSwitchFailure と対称で、印の削除だけが失敗した場合に
// 「settings.json はもう戻っている」ことを伝える。これが無いと、利用者は
// 復元が丸ごと失敗したと思って restore を繰り返すことになる。
func printRestoreFailure(w io.Writer, result app.RestoreHooksResult) {
	if !result.SettingsWritten {
		return
	}
	// 出力先へ書けない状況で追加の報告先は無いため、書き込み失敗は無視する。
	if result.RestoredFromBackup {
		_, _ = fmt.Fprintf(w, "settings.json はバックアップの内容で復元済みです。バックアップ: %s\n",
			result.BackupPath)
		return
	}
	_, _ = fmt.Fprintln(w, "settings.json は conductor のスクリプトへ復元済みです。")
}

// printRestoreResult は restore の結果を人が読める形で書き出す。
// 通常の復元は switch と対称に、置き換える内容の一覧を出す。
func printRestoreResult(w io.Writer, result app.RestoreHooksResult) {
	_, _ = fmt.Fprintf(w, "settings.json: %s\n", result.SettingsPath)

	switch {
	case result.SettingsMissing:
		// 復元できない(バックアップが無い)ときは印にも触れていないので、
		// dry-run の断りも印の行も出さずに終える。
		if !printBackupFallback(w, result) {
			return
		}
	case len(result.Changes) == 0:
		_, _ = fmt.Fprintln(w, "hooks は既に conductor のスクリプトを指しています。変更はありません。")
	default:
		printChanges(w, "元へ戻す hook コマンド", result.Changes)
		if !result.DryRun {
			_, _ = fmt.Fprintln(w, "hooks を conductor のスクリプトへ戻しました。")
		}
	}
	if printDryRunNotice(w, result.DryRun) {
		return
	}
	// 印は hooks の変更の有無に依らず消すので、どの経路でも出す。
	_, _ = fmt.Fprintf(w, "Go 版を使う印を消しました: %s\n", result.FlavorPath)
}

// printBackupFallback は settings.json が無いときの復元結果を書き出す。
// 戻り値は復元できたか(= この先の行を出してよいか)である。
func printBackupFallback(w io.Writer, result app.RestoreHooksResult) bool {
	if !result.RestoredFromBackup {
		_, _ = fmt.Fprintln(w, "settings.json がありません。"+
			"mdev が作ったバックアップも見つからないため復元できません。")
		return false
	}
	_, _ = fmt.Fprintf(w, "settings.json がありません。バックアップ: %s\n", result.BackupPath)
	if !result.DryRun {
		_, _ = fmt.Fprintln(w, "バックアップの内容で settings.json を復元しました。")
	}
	return true
}

// printChanges は置換の一覧を before / after の形で書き出す。
func printChanges(w io.Writer, title string, changes []app.HookCommandChange) {
	_, _ = fmt.Fprintf(w, "\n%s(%d 件):\n", title, len(changes))
	for _, c := range changes {
		_, _ = fmt.Fprintf(w, "  [%s]\n    - %s\n    + %s\n", c.Event, c.Before, c.After)
	}
	_, _ = fmt.Fprintln(w)
}
