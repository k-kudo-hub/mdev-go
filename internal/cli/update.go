package cli

import (
	"io"
	"os"
	"time"

	"github.com/spf13/cobra"
)

// forceFlag はキャッシュを無視して引き直すことを指定するフラグ名。
const forceFlag = "force"

// noticeReadDelay は更新の案内を出した後に置く間である。
//
// この確認はセッションを開く直前に走り、その後すぐ zellij が画面を取る。
// 間を置かないと案内が一瞬で流れて読めない(現行 check-update.sh の
// `[ -t 1 ] && sleep 2`)。端末でないときは待たない。
const noticeReadDelay = 2 * time.Second

// UpdateService は `mdev update` のユースケースである。実体は app.Updater。
type UpdateService interface {
	Update(out io.Writer) error
}

// UpdateCheckService は起動時の更新確認である。実体は app.UpdateChecker。
//
// 戻り値は画面へ出す案内で、空なら出すものが無い。**error を返さない**のは
// セッションの起動を止めないためで、その判断はユースケース側にある。
type UpdateCheckService interface {
	Check(force bool) string
}

// newUpdateCommand は `mdev update` を組み立てる。
func newUpdateCommand(deps Deps) *cobra.Command {
	return &cobra.Command{
		Use:   "update",
		Short: "conductor を最新のリリースへ更新する",
		Long: "$CONDUCTOR_HOME/REPO_URL に記録された更新元から最新のリリースを取得し、\n" +
			"同梱の install.sh を実行して入れ直す。既に最新なら何もしない。",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return deps.Update.Update(cmd.OutOrStdout())
		},
	}
}

// newCheckUpdateCommand は `mdev check-update` を組み立てる。
func newCheckUpdateCommand(deps Deps) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "check-update",
		Short: "新しいリリースがあれば 1 行で知らせる",
		Long: "インストール済みの版とリモートの最新タグを比べ、新しい版があれば案内を出す。\n" +
			"セッションの起動前に走るため、設定の不備や通信の失敗では何も出さずに終わる。\n" +
			"結果は 1 日 1 回だけ引き直す(--force で引き直しを強制する)。",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			force, err := cmd.Flags().GetBool(forceFlag)
			if err != nil {
				return err
			}
			notice := deps.UpdateCheck.Check(force)
			if notice == "" {
				return nil
			}
			// 出力先へ書けない状況で追加の報告先は無いため失敗は無視する。
			_, _ = io.WriteString(cmd.OutOrStdout(), notice)
			if isTerminal(cmd.OutOrStdout()) {
				time.Sleep(noticeReadDelay)
			}
			return nil
		},
	}
	cmd.Flags().Bool(forceFlag, false, "1 日 1 回のキャッシュを無視して引き直す")
	return cmd
}

// isTerminal は出力先が端末かどうかを返す。
//
// テストは bytes.Buffer を渡すため常に偽になり、待ちが入らない。
func isTerminal(out io.Writer) bool {
	file, ok := out.(*os.File)
	if !ok {
		return false
	}
	info, err := file.Stat()
	if err != nil {
		return false
	}
	return info.Mode()&os.ModeCharDevice != 0
}
