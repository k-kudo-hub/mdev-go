package app

import (
	"fmt"

	"github.com/k-kudo-hub/mdev-go/internal/domain"
)

// PaneEnv はペイン実行時の環境変数のうち mdev が使うものである。
type PaneEnv struct {
	ZellijSession string // ZELLIJ_SESSION_NAME
}

// Session はペインが対象とするセッション名を返す。
func (e PaneEnv) Session() string {
	return domain.SessionName(e.ZellijSession)
}

// TaskRecorder は作業サマリを daily log へ記録する。実体は RecordOutput である。
//
// 削除フローは「記録してからアップロードし、成功したときだけ消す」という
// 順序を持つため、その入口をユースケースから差し替えられるようにしている。
type TaskRecorder interface {
	Execute(tab string, env RecordEnv) error
}

// DashboardSnapshot は 1 回ぶんの Dashboard の状態である。
//
// 画面に出す文字列は domain のレンダリング関数が組み立てたものをそのまま持つ。
// tui は domain を参照できない(ADR-0002 の依存方向)ため、描画結果と、番号で
// タブを引くための操作だけをこの型が渡す。一覧の中身は外へ出さない。
type DashboardSnapshot struct {
	// Text は画面に出す文字列。
	Text string
	// Tabs は表示順のタブ名。番号キーの解決に使う。
	Tabs []string

	// items は表示順の pending そのもの。ジャンプ時にどのファイルを消すか、
	// エージェントの検出方式は何かを判断するために要る。tui へ渡さないよう
	// 非公開にしている。
	items []domain.PendingView
}

// DeletePreparation は削除フローの前半(記録とアップロード)の結果である。
type DeletePreparation struct {
	// Cancelled は upload-log.sh が失敗し、削除を中止すべきことを表す。
	// このとき pending もレジストリもタブも一切触っていない。
	Cancelled bool
	// Message はアップロード結果(ログの URL)。空なら表示するものが無い。
	Message string
}

// DashboardPane は Dashboard ペインのユースケースである
// (現行 dashboard-loop.sh 相当)。
type DashboardPane struct {
	Pending     PendingLister
	Remover     PendingRemover
	Registry    RegistryRemover
	ScreenState ScreenStateRemover
	Tabs        TabLister
	Closer      TabCloser
	Focuser     Focuser
	Config      ConfigLoader
	Recorder    TaskRecorder
	Detector    ScreenTicker
	Restorer    SessionStarter
	Shell       ShellRunner
}

// Startup は最初の描画の前に一度だけ行う処理である。
//
// このセッションに登録済みのタスクのタブを作り直す(issue #36)。レジストリが
// 空のときやタブが既にある場合は何も起きない。単発描画(--once)でも走らせる。
// 現行版も ONCE の判定より前で restore-session.sh を呼んでいる。
//
// 失敗は返らない。復元は最善努力で、作り直せなかったタスクはレジストリに
// 残って次回の起動で再試行される。ここで止まってダッシュボードが出ないほうが
// 害が大きい。
func (p *DashboardPane) Startup(env PaneEnv) {
	p.Restorer.Restore(env)
}

// Refresh は 1 回ぶんの描画内容を組み立てる。
//
// 先頭でスクリーン検出を走らせてから pending を読む。順序が逆だと、その回に
// 観測した状態が一覧に反映されず、screen 方式のエージェント(codex)のタスクが
// 出てこなくなる。
//
// ただし検出を走らせるのは、設定に screen 方式のエージェントが 1 つでも
// ある場合だけである。1 つも無ければ検出しても見つかるものが無く、中で走る
// `zellij action list-panes`(実測 1.1〜1.5 秒)の負荷だけが残る。2 秒ごとの
// ポーリングでは zellij サーバをほぼ占有し続けることになるため、設定を見て
// 静的に省く。
//
// 省くのは「設定を読めて、そこに screen 方式が 1 つも無かった」場合だけである。
// 読めなかった場合は従来どおり走らせる。読めない原因(CONDUCTOR_HOME の指し先が
// 違う・ファイルが壊れている)は codex を設定している利用者にも起こりうるので、
// 黙って検出を止めると codex のタスクが一覧から消え、原因の分からない不具合に
// なる。負荷を減らすために表示を落とすほうが害が大きい。
func (p *DashboardPane) Refresh(env PaneEnv) (DashboardSnapshot, error) {
	session := env.Session()

	if config, ok := p.Config.Load(); !ok || config.HasScreenDetectionAgent() {
		if err := p.Detector.Tick(env); err != nil {
			return DashboardSnapshot{}, fmt.Errorf("スクリーン検出に失敗しました: %w", err)
		}
	}

	views, err := p.Pending.List(session)
	if err != nil {
		return DashboardSnapshot{}, fmt.Errorf("pending の読み取りに失敗しました: %w", err)
	}

	tabOrder := domain.ParseTabNames(p.Tabs.ListTabs())
	items := domain.DashboardItems(tabOrder, views)
	tabs := make([]string, 0, len(items))
	for _, item := range items {
		tabs = append(tabs, item.Tab)
	}
	return DashboardSnapshot{
		Text:  domain.RenderDashboard(domain.DashboardInput{Session: session, Items: items}),
		Tabs:  tabs,
		items: items,
	}, nil
}

// Jump は snapshot の number 番目(1 始まり)のタブへ移動する。
// 範囲外の番号は何もしない(現行版も `$key -le $count` で弾いている)。
func (p *DashboardPane) Jump(env PaneEnv, snapshot DashboardSnapshot, number int) error {
	if number < 1 || number > len(snapshot.items) {
		return nil
	}
	return p.jump(env, snapshot.items[number-1])
}

// jump は item のタブへ移動する。
//
// 移動したあと pending を消すのは「hooks も screen 検出も持たないエージェント」
// のときだけである。claude は hooks が、screen 方式のエージェントはスクリーン
// 検出が、それぞれ pending のライフサイクルを持っている。screen 方式の
// Notification はターンが目に見えて再開するまで残るのが正しい状態なので、
// ここで消しても次のポーリングで作り直されるだけである。
func (p *DashboardPane) jump(env PaneEnv, item domain.PendingView) error {
	if err := p.Focuser.FocusTab(item.Tab); err != nil {
		return fmt.Errorf("タブへの移動に失敗しました: %w", err)
	}

	agent := item.AgentOrDefault
	if agent == domain.DefaultAgent {
		return nil
	}
	// 設定が読めなかった場合はゼロ値から既定の hooks が返る。現行 task-lib.sh の
	// agent_detection も `jq ... 2>/dev/null` で同じ既定へ落ちるため、ここでは
	// 読めたかどうかを区別しない。
	config, _ := p.Config.Load()
	if config.AgentDetection(agent) == domain.DetectionScreen {
		return nil
	}

	if err := p.Remover.DeleteByName(env.Session(), item.Name); err != nil {
		return fmt.Errorf("pending の削除に失敗しました: %w", err)
	}
	return nil
}

// PrepareDelete は削除フローの前半(記録とアップロード)を行う。
// 実処理は TaskDeleter が持ち、Dashboard と task-control で共有する。
func (p *DashboardPane) PrepareDelete(env PaneEnv, tab string) (DeletePreparation, error) {
	return p.deleter().Prepare(env, tab)
}

// CommitDelete は削除フローの後半(片付けとタブを閉じる)を行う。
func (p *DashboardPane) CommitDelete(env PaneEnv, tab string) error {
	return p.deleter().Commit(env, tab)
}

// deleter は Dashboard 用の削除フローを組み立てる。
//
// CloseActiveOnMissingID を立てないのは、現行 Dashboard が id を引けなければ
// タブを閉じずに終わるためである(close-tab へ落ちるのは task-control だけ)。
// この非対称は現行版のものをそのまま再現している。
func (p *DashboardPane) deleter() *TaskDeleter {
	return &TaskDeleter{
		Remover:     p.Remover,
		Registry:    p.Registry,
		ScreenState: p.ScreenState,
		Tabs:        p.Tabs,
		Closer:      p.Closer,
		Recorder:    p.Recorder,
		Shell:       p.Shell,
	}
}
