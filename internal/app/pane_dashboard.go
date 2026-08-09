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
	Shell       ShellRunner
}

// Startup は最初の描画の前に一度だけ行う処理である。
//
// このセッションに登録済みのタスクのタブを作り直す(issue #36)。レジストリが
// 空のときやタブが既にある場合は何も起きない。単発描画(--once)でも走らせる。
// 現行版も ONCE の判定より前で restore-session.sh を呼んでいる。
func (p *DashboardPane) Startup() {
	p.Shell.RestoreSession()
}

// Refresh は 1 回ぶんの描画内容を組み立てる。
//
// 先頭でスクリーン検出を走らせてから pending を読む。順序が逆だと、その回に
// 観測した状態が一覧に反映されず、screen 方式のエージェント(codex)のタスクが
// 出てこなくなる。
func (p *DashboardPane) Refresh(env PaneEnv) (domain.DashboardInput, error) {
	session := env.Session()

	p.Shell.ScreenDetectTick(session)

	views, err := p.Pending.List(session)
	if err != nil {
		return domain.DashboardInput{}, fmt.Errorf("pending の読み取りに失敗しました: %w", err)
	}

	tabOrder := domain.ParseTabNames(p.Tabs.ListTabs())
	return domain.DashboardInput{
		Session: session,
		Items:   domain.DashboardItems(tabOrder, views),
	}, nil
}

// Jump は item のタブへ移動する。
//
// 移動したあと pending を消すのは「hooks も screen 検出も持たないエージェント」
// のときだけである。claude は hooks が、screen 方式のエージェントはスクリーン
// 検出が、それぞれ pending のライフサイクルを持っている。screen 方式の
// Notification はターンが目に見えて再開するまで残るのが正しい状態なので、
// ここで消しても次のポーリングで作り直されるだけである。
func (p *DashboardPane) Jump(env PaneEnv, item domain.PendingView) error {
	if err := p.Focuser.FocusTab(item.Tab); err != nil {
		return fmt.Errorf("タブへの移動に失敗しました: %w", err)
	}

	agent := item.AgentOrDefault
	if agent == domain.DefaultAgent {
		return nil
	}
	if p.Config.Load().AgentDetection(agent) == domain.DetectionScreen {
		return nil
	}

	if err := p.Remover.DeleteByName(env.Session(), item.Name); err != nil {
		return fmt.Errorf("pending の削除に失敗しました: %w", err)
	}
	return nil
}

// PrepareDelete は削除フローの前半を行う。
//
// 作業サマリを daily log へ記録してから、作業ログを同期でアップロードする。
// アップロードが失敗した場合は Cancelled を立てて戻り、**何も消さない**。
// タブを消してしまうと作業ログを永久に失うためで、これがこのフローで最も
// 重要な契約である。
//
// 後半は CommitDelete が行う。呼び出し側は Message が空でなければ、その内容を
// 表示してから CommitDelete を呼ぶ(タブが閉じる前に URL を確認できるように
// するため、現行版もこの順で待ちを入れている)。
func (p *DashboardPane) PrepareDelete(env PaneEnv, tab string) (DeletePreparation, error) {
	// PaneEnv と RecordEnv は同じ形(ZELLIJ_SESSION_NAME だけ)なので変換で渡す。
	if err := p.Recorder.Execute(tab, RecordEnv(env)); err != nil {
		return DeletePreparation{}, fmt.Errorf("作業サマリの記録に失敗しました: %w", err)
	}

	output, err := p.Shell.UploadLog(tab)
	if err != nil {
		return DeletePreparation{Cancelled: true}, nil
	}
	return DeletePreparation{Message: output}, nil
}

// CommitDelete は削除フローの後半を行う。
//
// pending → レジストリ → スクリーン検出の状態 → タブ、の順に片付ける。
// レジストリを消すのは、削除したタスクが次回のセッション復元で蘇らないように
// するためである(issue #36)。スクリーン検出の状態を消すのは、同じ名前の
// タブが後で作られたときに前のタスクの状態を引き継がせないためである。
//
// タブの id が引けなかった場合はタブを閉じない。現行 Dashboard は close-tab への
// フォールバックを持たないため、その非対称もそのまま再現している。
func (p *DashboardPane) CommitDelete(env PaneEnv, tab string) error {
	session := env.Session()

	if err := p.Remover.DeleteByTab(session, tab); err != nil {
		return fmt.Errorf("pending の削除に失敗しました: %w", err)
	}
	if err := p.Registry.RemoveByTab(session, tab); err != nil {
		return fmt.Errorf("レジストリからの削除に失敗しました: %w", err)
	}
	if err := p.ScreenState.Remove(session, domain.ScreenTabSlug(tab)); err != nil {
		return fmt.Errorf("スクリーン検出の状態の削除に失敗しました: %w", err)
	}

	if id := domain.ResolveTabID(p.Tabs.ListTabs(), tab); id != "" {
		p.Closer.CloseTabByID(id)
	}
	return nil
}
