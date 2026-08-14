package cli

import "github.com/spf13/cobra"

// AgentService は `mdev agent launch` のユースケースである。
// 実体は app.AgentLauncher。
type AgentService interface {
	// Launch は設定されたエージェントへプロセスを置き換える。
	// 成功すれば **戻らない**。
	Launch() error
}

// newAgentCommand は `mdev agent` とその配下を組み立てる。
func newAgentCommand(deps Deps) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "agent",
		Short: "エージェント CLI を扱う",
	}
	cmd.AddCommand(newAgentLaunchCommand(deps))
	return cmd
}

// newAgentLaunchCommand は `mdev agent launch` を組み立てる。
func newAgentLaunchCommand(deps Deps) *cobra.Command {
	return &cobra.Command{
		Use:   "launch",
		Short: "設定されたエージェント CLI を起動する",
		Long: "config.json の .agent.command を語に分けて、そのコマンドへプロセスを\n" +
			"置き換える(既定は claude)。dev.kdl の Agent ペインから呼ばれる。\n" +
			"静的な KDL は設定を読めないため、どの CLI を起こすかはここで決める。",
		Args: cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			return deps.Agent.Launch()
		},
	}
}
