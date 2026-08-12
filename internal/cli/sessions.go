package cli

import (
	"fmt"
	"io"

	"github.com/spf13/cobra"

	"github.com/k-kudo-hub/mdev-go/internal/app"
)

// autoFlag は起動前の自動掃除を指定するフラグ名。
const autoFlag = "auto"

// SessionCleanService は溜まったセッションとプロセスを片付ける
// ユースケースである。実体は app.SessionCleaner。
type SessionCleanService interface {
	Clean(dryRun bool) (app.CleanupResult, error)
}

// newSessionsCommand は `mdev sessions` とその子コマンドを組み立てる。
func newSessionsCommand(deps Deps) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "sessions",
		Short: "zellij セッションを管理する",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return cmd.Help()
		},
	}
	cmd.AddCommand(newSessionsCleanCommand(deps))
	return cmd
}

// newSessionsCleanCommand は `mdev sessions clean` を組み立てる。
func newSessionsCleanCommand(deps Deps) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "clean",
		Short: "使われていない zellij セッションと残骸を片付ける",
		Long: "終了済みセッション・誰も開いていない mdev セッション・一覧に出ない\n" +
			"zellij サーバ・親を失った zellij action を片付ける。\n" +
			"**アタッチしているクライアントがあるセッションには触れない。**\n" +
			"タスクはレジストリから復元されるため、閉じたセッションを片付けても\n" +
			"作業は失われない(レジストリと pending には触れない)。",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			dryRun, err := cmd.Flags().GetBool(dryRunFlag)
			if err != nil {
				return err
			}
			auto, err := cmd.Flags().GetBool(autoFlag)
			if err != nil {
				return err
			}

			result, cleanErr := deps.SessionClean.Clean(dryRun)
			if auto {
				// 起動前に走るので、何があってもセッションの起動を止めない。
				printAutoSummary(cmd.OutOrStdout(), result, cleanErr)
				return nil
			}
			if cleanErr != nil {
				return cleanErr
			}
			printCleanResult(cmd.OutOrStdout(), result)
			return nil
		},
	}
	addDryRunFlag(cmd)
	cmd.Flags().Bool(autoFlag, false,
		"起動前の自動掃除として実行する(要約だけを出し、失敗しても正常終了する)")
	return cmd
}

// printAutoSummary は --auto のときの出力を書き出す。
//
// 数えるのは **実際に片付けた件数** であって、片付けようとした件数ではない。
// 消す直前の再確認で飛ばしたもの、予算切れで見送ったもの、失敗したものが
// あるため、計画の件数を出すと「掃除した」と嘘をつくことになる。
//
// **1 件も片付けなければ 1 文字も出さない。** これはセッションを開くたびに
// 走るので、毎回何か出ると起動時の画面が埋まってしまう。失敗も黙って
// 飲み込む。掃除は最善努力で、失敗を理由に起動を止める価値は無い。
func printAutoSummary(w io.Writer, result app.CleanupResult, err error) {
	if err != nil || result.Done.IsEmpty() {
		return
	}
	done := result.Done
	// 出力先へ書けない状況で追加の報告先は無いため、書き込み失敗は無視する。
	_, _ = fmt.Fprintf(w, "掃除: 終了済み %d 件, 未使用 %d 件, ゾンビ %d 件, 残骸 %d 件\n",
		done.ExitedSessions, done.DetachedSessions,
		done.ZombieServers, done.OrphanClients)
}

// printCleanResult は手で実行したときの結果を書き出す。
//
// 何を対象にしたかを名前まで出す。dry-run はこれを見て「消していいか」を
// 判断するためのものなので、件数だけでは足りない。
func printCleanResult(w io.Writer, result app.CleanupResult) {
	plan := result.Plan
	if plan.IsEmpty() {
		_, _ = fmt.Fprintln(w, "片付けるものはありません。")
		return
	}

	printCleanTargets(w, "終了済みセッション(削除)", plan.ExitedSessions)
	printCleanTargets(w, "誰も開いていない mdev セッション(終了して削除)", plan.DetachedSessions)

	if len(plan.ZombieServers) > 0 {
		_, _ = fmt.Fprintf(w, "\n一覧に出ない zellij サーバ(%d 件):\n", len(plan.ZombieServers))
		for _, server := range plan.ZombieServers {
			_, _ = fmt.Fprintf(w, "  pid=%d session=%s\n", server.PID, server.Session)
		}
	}
	if len(plan.OrphanClients) > 0 {
		_, _ = fmt.Fprintf(w, "\n親を失った zellij action(%d 件):\n", len(plan.OrphanClients))
		for _, orphan := range plan.OrphanClients {
			_, _ = fmt.Fprintf(w, "  pid=%d\n", orphan.PID)
		}
	}

	if result.DryRun {
		_, _ = fmt.Fprintln(w, "\n--dry-run のため何も実行していません。")
		return
	}
	_, _ = fmt.Fprintln(w, "\n片付けました。")
}

// printCleanTargets は名前の一覧を 1 節として書き出す。
func printCleanTargets(w io.Writer, title string, names []string) {
	if len(names) == 0 {
		return
	}
	_, _ = fmt.Fprintf(w, "\n%s(%d 件):\n", title, len(names))
	for _, name := range names {
		_, _ = fmt.Fprintf(w, "  %s\n", name)
	}
}
