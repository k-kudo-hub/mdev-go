package cli

import (
	"fmt"
	"io"

	"github.com/spf13/cobra"

	"github.com/k-kudo-hub/mdev-go/internal/app"
)

// HookService は hook イベントを処理するユースケースである。
// 実体は app.HookHandler で、テストでは記録用の実装に差し替える。
type HookService interface {
	HandleNotify(raw []byte, env app.HookEnv) error
	HandlePostTool(raw []byte, env app.HookEnv) error
	HandleResolve(raw []byte, env app.HookEnv) error
}

// hook 実行時に読む環境変数。zellij のタブ生成時に conductor が設定する。
const (
	envZellijSession = "ZELLIJ_SESSION_NAME"
	envTaskTabName   = "TASK_TAB_NAME"
	envTaskType      = "TASK_TYPE"
	envTaskAgent     = "TASK_AGENT"
)

// newHookCommand は `mdev hook` とその子コマンドを組み立てる。
func newHookCommand(deps Deps) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "hook",
		Short: "Claude Code の hook イベントを処理する",
		Long: "Claude Code の hook から呼ばれ、標準入力の JSON をもとに\n" +
			"タスクの待ち状態(pending)とタスクレジストリを更新する。",
		// 子コマンドの指定を必須にする。
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return cmd.Help()
		},
	}

	cmd.AddCommand(
		newHookSubCommand(deps, "notify",
			"Notification / Stop hook を処理する",
			func(s HookService, raw []byte, env app.HookEnv) error { return s.HandleNotify(raw, env) }),
		newHookSubCommand(deps, "post-tool",
			"PostToolUse hook を処理する",
			func(s HookService, raw []byte, env app.HookEnv) error { return s.HandlePostTool(raw, env) }),
		newHookSubCommand(deps, "resolve",
			"UserPromptSubmit hook を処理する",
			func(s HookService, raw []byte, env app.HookEnv) error { return s.HandleResolve(raw, env) }),
	)
	return cmd
}

// newHookSubCommand は標準入力を読んで handle に渡すだけの子コマンドを作る。
func newHookSubCommand(deps Deps, name, short string, handle func(HookService, []byte, app.HookEnv) error) *cobra.Command {
	return &cobra.Command{
		Use:   name,
		Short: short,
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			raw, err := io.ReadAll(cmd.InOrStdin())
			if err != nil {
				return fmt.Errorf("標準入力の読み取りに失敗しました: %w", err)
			}
			return handle(deps.Hooks, raw, hookEnv(deps))
		},
	}
}

// hookEnv は環境変数から HookEnv を組み立てる。
func hookEnv(deps Deps) app.HookEnv {
	return app.HookEnv{
		ZellijSession: deps.Getenv(envZellijSession),
		TaskTabName:   deps.Getenv(envTaskTabName),
		TaskType:      deps.Getenv(envTaskType),
		TaskAgent:     deps.Getenv(envTaskAgent),
	}
}
