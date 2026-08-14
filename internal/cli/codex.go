package cli

import (
	"github.com/spf13/cobra"

	"github.com/k-kudo-hub/mdev-go/internal/app"
)

// CodexService は codex の notify を処理するユースケースである。
// 実体は app.CodexNotifier。
type CodexService interface {
	Notify(raw []byte, env app.HookEnv) error
}

// newCodexCommand は `mdev codex` とその配下を組み立てる。
func newCodexCommand(deps Deps) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "codex",
		Short: "codex CLI との連携を扱う",
	}
	cmd.AddCommand(newCodexNotifyCommand(deps))
	return cmd
}

// newCodexNotifyCommand は `mdev codex notify` を組み立てる。
//
// codex は ~/.codex/config.toml の `notify` に登録されたコマンドを、JSON を
// **最後の引数** に足して呼ぶ。標準入力から読む Claude Code の hook とは
// 渡され方が違うため、引数から受ける。
//
// 引数の数を縛らないのは、codex が将来 payload の前に何かを足しても
// 壊れないようにするためである(現行版の `"${@: -1}"` と同じ)。引数が 1 つも
// 無いときは空の payload として扱い、何もせずに終わる。
func newCodexNotifyCommand(deps Deps) *cobra.Command {
	return &cobra.Command{
		Use:   "notify [payload]",
		Short: "codex のターン完了通知を処理する",
		Long: "codex の notify から呼ばれ、最後の引数の JSON をもとにタスクの待ち状態\n" +
			"(pending)とタスクレジストリを更新する。~/.codex/config.toml の notify に\n" +
			"登録して使う。ターン完了以外の通知は何もせずに終わる。",
		RunE: func(_ *cobra.Command, args []string) error {
			return deps.Codex.Notify([]byte(lastArg(args)), hookEnv(deps))
		},
	}
}

// lastArg は最後の引数を返す。引数が無ければ空文字を返す
// (現行版の `PAYLOAD="${@: -1}"`)。
func lastArg(args []string) string {
	if len(args) == 0 {
		return ""
	}
	return args[len(args)-1]
}
