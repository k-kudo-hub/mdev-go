// Command mdev は Zellij 上のコーディングエージェントセッションを統括する CLI である。
//
// このパッケージは依存の組み立て(DI)とエントリポイントのみを持ち、
// 業務ロジックは internal 以下の各パッケージに置く(ADR-0002)。
package main

import (
	"fmt"
	"os"

	"github.com/k-kudo-hub/mdev-go/internal/app"
	"github.com/k-kudo-hub/mdev-go/internal/cli"
	"github.com/k-kudo-hub/mdev-go/internal/infra"
	"github.com/k-kudo-hub/mdev-go/internal/infra/shell"
	"github.com/k-kudo-hub/mdev-go/internal/infra/store"
	"github.com/k-kudo-hub/mdev-go/internal/infra/zellij"
	"github.com/k-kudo-hub/mdev-go/internal/tui"
)

func main() {
	// pending は CONDUCTOR_HOME に依存せずホーム直下に固定する。hook は
	// conductor の外にある Claude Code セッションでも発火するためである。
	home, err := os.UserHomeDir()
	if err != nil {
		fmt.Fprintln(os.Stderr, "mdev: ホームディレクトリを特定できません:", err)
		os.Exit(1)
	}

	os.Exit(cli.Execute(buildDeps(home, os.Getenv, infra.SystemClock{})))
}

// buildDeps は実行時の依存一式を組み立てる(ADR-0002 の DI はここだけ)。
//
// home は pending と settings.json の基準になるホームディレクトリ、getenv は
// 環境変数の読み取り、clock は「今」を供給する時計である。3 つとも引数で
// 受けるのは、ゴールデンテストが隔離したディレクトリと固定の日付で同じ
// 依存グラフを組み立て直せるようにするためである。
func buildDeps(home string, getenv func(string) string, clock app.Clock) cli.Deps {
	conductorHome := store.ConductorHome(home, getenv("CONDUCTOR_HOME"))

	pending := store.NewPendingStore(store.PendingRoot(home))

	hooks := &app.HookHandler{
		Pending:  pending,
		Registry: store.NewRegistryStore(store.RegistryRoot(conductorHome)),
		Focuser:  zellij.NewFocuser(),
		Clock:    clock,
	}

	// ロックを取れなかったことは stderr に警告するだけで処理は続ける
	// (fail-open)。record は hook 経路と同じく会話を止めてはならない。
	record := &app.RecordOutput{
		Pending:    pending,
		Transcript: store.NewTranscriptStore(),
		Daily:      store.NewDailyStore(store.DailyRoot(conductorHome), os.Stderr),
		Pricing:    store.NewPricingStore(conductorHome),
		Clock:      clock,
	}

	// settings.json は CONDUCTOR_HOME ではなく Claude Code の設定であるため
	// ホーム直下の ~/.claude/settings.json を見る。MDEV_SETTINGS_FILE を
	// 指定すると別のファイルを対象にできる(実環境へ適用する前の試行用)。
	// 切り替え先のバイナリは hooks のコマンド文字列と同じ規約で
	// CONDUCTOR_HOME 配下を見る。
	hookSettings := &app.HookSwitcher{
		Settings: store.NewSettingsStore(
			store.SettingsPath(home, getenv("MDEV_SETTINGS_FILE")),
			clock,
		),
		Binary: store.NewMdevBinaryStore(conductorHome),
	}

	// ダッシュボード系 4 ペイン。pending はホーム直下、daily とニュースは
	// CONDUCTOR_HOME 配下という置き場所の違いを PaneStore がそのまま持つ。
	// upload-log / restore-task / fetch-news / restore-session / スクリーン検出は
	// まだ Shell のままで、shell.Runner が env を引き継いで同期で呼ぶ。
	paneStore := store.NewPaneStore(store.PendingRoot(home), conductorHome)
	tabs := zellij.NewTabController()
	runner := shell.NewRunner(conductorHome)

	panes := tui.Panes{
		Dashboard: &app.DashboardPane{
			Pending:     paneStore,
			Remover:     paneStore,
			Registry:    store.NewRegistryStore(store.RegistryRoot(conductorHome)),
			ScreenState: paneStore,
			Tabs:        tabs,
			Closer:      tabs,
			Focuser:     zellij.NewFocuser(),
			Config:      paneStore,
			Recorder:    record,
			Shell:       runner,
		},
		Waiting: &app.WaitingPane{Pending: paneStore},
		Done:    &app.DonePane{Daily: paneStore, Shell: runner, Clock: clock},
		News: &app.NewsPane{
			News:   paneStore,
			Shell:  runner,
			Opener: shell.NewOpener(),
			Clock:  clock,
		},
		Env: app.PaneEnv{ZellijSession: getenv("ZELLIJ_SESSION_NAME")},
	}

	return cli.Deps{
		Hooks:        hooks,
		Record:       record,
		HookSettings: hookSettings,
		Panes:        panes,
		Getenv:       getenv,
	}
}
