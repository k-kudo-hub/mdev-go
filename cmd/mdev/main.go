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
	"github.com/k-kudo-hub/mdev-go/internal/infra/git"
	"github.com/k-kudo-hub/mdev-go/internal/infra/news"
	"github.com/k-kudo-hub/mdev-go/internal/infra/procscan"
	"github.com/k-kudo-hub/mdev-go/internal/infra/release"
	"github.com/k-kudo-hub/mdev-go/internal/infra/shell"
	"github.com/k-kudo-hub/mdev-go/internal/infra/store"
	"github.com/k-kudo-hub/mdev-go/internal/infra/zellij"
	"github.com/k-kudo-hub/mdev-go/internal/tui"
)

// version はビルド時に焼き込む mdev の版である。
//
// リリースのビルドは `-ldflags "-X main.version=<タグ>"` で埋める
// (.github/workflows/tag.yml と Makefile)。手元の `go build` では既定の
// まま残り、自己更新はそれを見て何もしない。
var version = "dev"

func main() {
	// pending は CONDUCTOR_HOME に依存せずホーム直下に固定する。hook は
	// conductor の外にある Claude Code セッションでも発火するためである。
	home, err := os.UserHomeDir()
	if err != nil {
		fmt.Fprintln(os.Stderr, "mdev: ホームディレクトリを特定できません:", err)
		os.Exit(1)
	}

	deps := buildDeps(home, os.Getenv, infra.SystemClock{}, infra.SystemClock{})
	deps.Version = version
	os.Exit(cli.Execute(deps))
}

// buildDeps は実行時の依存一式を組み立てる(ADR-0002 の DI はここだけ)。
//
// home は pending と settings.json の基準になるホームディレクトリ、getenv は
// 環境変数の読み取り、clock は「今」を供給する時計、sleeper は待ちを作るもの
// である。いずれも引数で受けるのは、ゴールデンテストが隔離したディレクトリと
// 固定の日付、待たない sleeper で同じ依存グラフを組み立て直せるようにする
// ためである。
func buildDeps(home string, getenv func(string) string, clock app.Clock, sleeper app.Sleeper) cli.Deps {
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
		Flavor: store.NewFlavorStore(conductorHome),
	}

	// ダッシュボード系 4 ペイン。pending はホーム直下、daily とニュースは
	// CONDUCTOR_HOME 配下という置き場所の違いを PaneStore がそのまま持つ。
	paneStore := store.NewPaneStore(store.PendingRoot(home), conductorHome)
	tabs := zellij.NewTabController()
	binary := store.NewMdevBinaryStore(conductorHome)
	registry := store.NewRegistryStore(store.RegistryRoot(conductorHome))

	// スクリーン検出(hook を持たない codex の状態判定)。ダッシュボードの
	// 読み直しの先頭で毎回走る。書き込み先は pending と状態ファイルの 2 つで、
	// screen 由来の pending が借りる 3 キーはレジストリから引く。
	detector := &app.ScreenDetector{
		Panes:    tabs,
		Dumper:   tabs,
		Config:   paneStore,
		State:    paneStore,
		Pending:  paneStore,
		Remover:  paneStore,
		Writer:   pending,
		Registry: registry,
		Focuser:  zellij.NewFocuser(),
		Clock:    clock,
	}

	// タスク作成(n フロー)と task-control ペイン。zellij を直接駆動するため、
	// タブ登録待ち・フォーカス検証・全体予算の防御を持つ TaskCreator を通す。
	// sleeper に clock をそのまま渡せないのは、clock が app.Clock として
	// 受け取られており Sleep を持つとは限らないためである。
	creator := &app.TaskCreator{
		Tabs:        tabs,
		ScreenState: paneStore,
		Config:      paneStore,
		Clock:       clock,
		Sleeper:     sleeper,
		Launcher:    binary,
	}

	// セッション復元(起動時にレジストリからタスクタブを作り直す)。
	// 最善努力なので、作り直せなかったタスクの説明は戻り値で受け取り、
	// ダッシュボードの画面へ出す。ペインは動作中の Bubble Tea プログラム
	// なので、標準エラーへ直接書くと描画が崩れる。
	restorer := &app.SessionRestorer{
		Registry: registry,
		Tabs:     tabs,
		Creator:  creator,
		Paths:    paneStore,
		Focuser:  zellij.NewFocuser(),
		Clock:    clock,
	}

	// Done からの復元。daily ログの読み書きは DailyStore が持ち、タブの
	// 作り直しはタスク作成をそのまま再利用する。警告の出力先を渡さない
	// (nil)のは、この経路がロックを取れなければ書き込まずにエラーを返し、
	// 標準エラーへ書くことが無いためである。
	taskRestorer := &app.TaskRestorer{
		Daily:   store.NewDailyStore(store.DailyRoot(conductorHome), nil),
		Creator: creator,
		Paths:   paneStore,
	}

	// 作業ログのアップロード(dd / d+番号 の前半)。要約は claude CLI、push は
	// git バイナリで、どちらも上限を設けない。失敗すると呼び出し側がタブの
	// 削除を中止する(作業ログを失わないための契約)。
	uploader := &app.LogUploader{
		Config:     paneStore,
		Pending:    pending,
		Transcript: store.NewTranscriptStore(),
		Daily:      store.NewDailyStore(store.DailyRoot(conductorHome), nil),
		Summarizer: shell.NewSummaryGenerator(),
		Pusher:     git.NewLogRepository(conductorHome),
		Clock:      clock,
	}

	// zellij のセッション操作。掃除(sessions clean)と、ペインの
	// 「誰か開いているか」の確認の両方が使う。
	zellijSessions := zellij.NewSessionController()
	processes := procscan.NewScanner()

	panes := tui.Panes{
		Dashboard: &app.DashboardPane{
			Pending:     paneStore,
			Remover:     paneStore,
			Registry:    registry,
			ScreenState: paneStore,
			Tabs:        tabs,
			Closer:      tabs,
			Focuser:     zellij.NewFocuser(),
			Config:      paneStore,
			Recorder:    record,
			Detector:    detector,
			Restorer:    restorer,
			Uploader:    uploader,
		},
		Waiting: &app.WaitingPane{Pending: paneStore},
		Done:    &app.DonePane{Daily: paneStore, Restorer: taskRestorer, Clock: clock},
		News: &app.NewsPane{
			News:    paneStore,
			Fetcher: news.NewFetcher(conductorHome),
			Opener:  shell.NewOpener(),
			Clock:   clock,
		},
		TaskCreate: &app.TaskCreatePane{
			Config:  paneStore,
			Dirs:    paneStore,
			Creator: creator,
			Home:    home,
		},
		TaskControl: &app.TaskControlPane{
			Raw:     pending,
			Focuser: zellij.NewFocuser(),
			Clock:   clock,
			Deleter: &app.TaskDeleter{
				Remover:     paneStore,
				Registry:    registry,
				ScreenState: paneStore,
				Tabs:        tabs,
				Closer:      tabs,
				Recorder:    record,
				Uploader:    uploader,
				// 自分のタブの中で動いているので、id が引けなければ
				// 「今のタブ」を閉じてよい(Dashboard との非対称)。
				CloseActiveOnMissingID: true,
			},
		},
		Env: app.PaneEnv{ZellijSession: getenv("ZELLIJ_SESSION_NAME")},
		// 誰も開いていないセッションではポーリングを落とす。閉じたまま
		// 残ったセッションが zellij サーバを劣化させ続けないようにする。
		Attach: tui.AttachWatch{
			Checker: zellijSessions,
			Session: getenv("ZELLIJ_SESSION_NAME"),
		},
	}

	// 更新まわり。状態(REPO_URL / VERSION / .update-check)は CONDUCTOR_HOME
	// 直下にあり、リモートのタグ引きは git バイナリで行う。
	updateState := store.NewUpdateStateStore(conductorHome)
	remoteTags := git.NewRemoteTags()

	return cli.Deps{
		Hooks:        hooks,
		Record:       record,
		HookSettings: hookSettings,
		Panes:        panes,
		Update: &app.Updater{
			State:     updateState,
			Remote:    remoteTags,
			Installer: release.NewInstaller(),
			Getenv:    getenv,
		},
		SessionClean: &app.SessionCleaner{
			Sessions:  zellijSessions,
			Clients:   zellijSessions,
			Remover:   zellijSessions,
			Processes: processes,
			Signaler:  processes,
			Sockets:   zellijSessions,
			Traces: store.NewSessionTraceStore(
				store.RegistryRoot(conductorHome), store.PendingRoot(home)),
			Sleeper: sleeper,
			Clock:   clock,
		},
		UpdateCheck: &app.UpdateChecker{
			Config: paneStore,
			State:  updateState,
			Remote: remoteTags,
			Clock:  clock,
		},
		Getenv: getenv,
	}
}
