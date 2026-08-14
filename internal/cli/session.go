package cli

import (
	"errors"
	"io"

	"github.com/spf13/cobra"

	"github.com/k-kudo-hub/mdev-go/internal/app"
)

// SessionService はセッションの起動である。実体は app.SessionLauncher。
type SessionService interface {
	// Start は attach するか新しく作る。成功すれば戻らない。
	Start(out io.Writer, req app.SessionRequest) error
	// StartDev は単一の開発セッションを開く。成功すれば戻らない。
	StartDev(name string) error
	// Attach は名前を選んで attach する。成功すれば戻らない。
	Attach(name string) error
	// ClearPending は pending をすべて消す。
	ClearPending(out io.Writer) error
}

// newFlag は時刻付きの新しいセッションを強制するフラグ名。
const newFlag = "new"

// runSession は attach-or-create を実行する。
//
// 引数を取るのは `mdev <名前>` のためである。cobra は先に子コマンドを探すので、
// `mdev install` のような既知の名前はそちらへ渡る。**その結果、子コマンドと
// 同じ名前のセッションは名前で開けない**(`mdev news` は News の取得になる)。
// 起動の入口を 1 語で保つほうの利益が上回ると判断した。
//
// ただし、既知のコマンド名と 1 文字しか違わない引数は差し戻す。`mdev instal`
// が黙って「instal というセッションを開く」に化けるのを防ぐためで、本当に
// その名前で開きたい場合の逃げ道(`mdev attach <名前>`)も一緒に案内する。
//
// 開く直前には名前を 1 行出す。差し戻しをすり抜けた打ち間違いでも、画面に
// 出ていれば気づける。
func runSession(deps Deps, cmd *cobra.Command, args []string) error {
	force, err := cmd.Flags().GetBool(newFlag)
	if err != nil {
		return err
	}
	req := app.SessionRequest{Dir: deps.Getwd()}
	if len(args) > 0 {
		req.Name = args[0]
		if nearest, ok := app.NearestCommand(req.Name, commandNames(cmd)); ok {
			return errors.New(app.RenderCommandTypo(req.Name, nearest))
		}
		// 出力先へ書けない状況で追加の報告先は無いため失敗は無視する。
		_, _ = io.WriteString(cmd.OutOrStdout(), app.RenderOpeningSession(req.Name))
	}
	if force {
		req.Stamp = deps.Now().Format(app.NewSessionTimeLayout)
	}
	return deps.Session.Start(cmd.OutOrStdout(), req)
}

// commandNames はルートが持つ子コマンドの名前を返す。
//
// 一覧を手で持たないのは、コマンドを足したときに差し戻しの対象が自動で
// 増えるようにするためである。手書きの表は必ず古くなる。
func commandNames(cmd *cobra.Command) []string {
	children := cmd.Commands()
	names := make([]string, 0, len(children))
	for _, child := range children {
		names = append(names, child.Name())
	}
	return names
}

// newDevCommand は `mdev dev` を組み立てる(エイリアス dev)。
func newDevCommand(deps Deps) *cobra.Command {
	return &cobra.Command{
		Use:   "dev [名前]",
		Short: "単一の開発セッションを開く",
		Long: "レイアウト dev.kdl(エージェント + エディタ + git)でセッションを開く。\n" +
			"名前を省くと <ディレクトリ名>-<時刻> になる(毎回新しいセッション)。",
		Args: cobra.MaximumNArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			name := ""
			if len(args) > 0 {
				name = args[0]
			}
			return deps.Session.StartDev(name)
		},
	}
}

// newAttachCommand は `mdev attach` を組み立てる(エイリアス zs)。
func newAttachCommand(deps Deps) *cobra.Command {
	return &cobra.Command{
		Use:   "attach [名前]",
		Short: "既存のセッションへ入る",
		Long: "名前を省くと一覧から選ぶ。指定した名前のセッションが無ければ、\n" +
			"その名前で新しく作る(現行の zs と同じ)。",
		Args: cobra.MaximumNArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			name := ""
			if len(args) > 0 {
				name = args[0]
			}
			return deps.Session.Attach(name)
		},
	}
}

// newPendingCommand は `mdev pending` を組み立てる。
func newPendingCommand(deps Deps) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "pending",
		Short: "待ち状態(pending)を扱う",
	}
	cmd.AddCommand(&cobra.Command{
		Use:   "clear",
		Short: "待ち状態をすべて消す",
		Long: "ダッシュボードに出ている待ち状態をすべて消す。タスクそのものや\n" +
			"作業ログには触らない。",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return deps.Session.ClearPending(cmd.OutOrStdout())
		},
	})
	return cmd
}
