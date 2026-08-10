package app

import (
	"fmt"

	"github.com/k-kudo-hub/mdev-go/internal/domain"
)

// TaskDeleter はタスクタブの削除フローである。
//
// Dashboard の d+番号 と task-control の dd が同じ手順を踏む。共有するのは、
// この手順が持つ契約(アップロードに失敗したら何も消さない・消す順番)を
// 2 か所で書き分けると片方だけがずれるためである。
//
// 現行版の 2 経路には 1 つだけ意図的でない差があった。task-control 側は
// スクリーン検出の状態ファイルを消しておらず、同じ名前のタブを作り直したときに
// 前のタスクの状態を引き継いでしまう。統合にあたって消す側へ揃えた
// (Shell 側の欠落バグの修正。evidence に記録)。
type TaskDeleter struct {
	Remover     PendingRemover
	Registry    RegistryRemover
	ScreenState ScreenStateRemover
	Tabs        TabLister
	Closer      TabCloser
	Recorder    TaskRecorder
	Shell       ShellRunner

	// CloseActiveOnMissingID はタブの id を引けなかったときに
	// `close-tab`(今のタブを閉じる)へ落ちるかどうかである。
	//
	// task-control は自分のタブの中で動いているので、id が引けなくても
	// 「今のタブ」を閉じれば概ね正しい。Dashboard は Main タブの中で
	// 動いているため、これを立てると Main を閉じてしまう。
	CloseActiveOnMissingID bool
}

// Prepare は削除フローの前半を行う。
//
// 作業サマリを daily log へ記録してから、作業ログを同期でアップロードする。
// アップロードが失敗した場合は Cancelled を立てて戻り、**何も消さない**。
// タブを消してしまうと作業ログを永久に失うためで、これがこのフローで最も
// 重要な契約である。
//
// 後半は Commit が行う。呼び出し側は Message が空でなければ、その内容を
// 表示してから Commit を呼ぶ(タブが閉じる前に URL を確認できるようにするため、
// 現行版もこの順で待ちを入れている)。
func (d *TaskDeleter) Prepare(env PaneEnv, tab string) (DeletePreparation, error) {
	// PaneEnv と RecordEnv は同じ形(ZELLIJ_SESSION_NAME だけ)なので変換で渡す。
	if err := d.Recorder.Execute(tab, RecordEnv(env)); err != nil {
		return DeletePreparation{}, fmt.Errorf("作業サマリの記録に失敗しました: %w", err)
	}

	output, err := d.Shell.UploadLog(tab)
	if err != nil {
		return DeletePreparation{Cancelled: true}, nil
	}
	return DeletePreparation{Message: output}, nil
}

// Commit は削除フローの後半を行う。
//
// pending → レジストリ → スクリーン検出の状態 → タブ、の順に片付ける。
// レジストリを消すのは、削除したタスクが次回のセッション復元で蘇らないように
// するためである(issue #36)。スクリーン検出の状態を消すのは、同じ名前の
// タブが後で作られたときに前のタスクの状態を引き継がせないためである。
//
// タブは id を指定して閉じる。同期のアップロードは数秒かかることがあり、その間に
// 表示中のタブが変わっている可能性がある(Main への自動移動など)ため、
// `close-tab` では別のタブを閉じてしまう。id を引けなかったときに
// `close-tab` へ落ちるかどうかは CloseActiveOnMissingID が決める。
func (d *TaskDeleter) Commit(env PaneEnv, tab string) error {
	session := env.Session()

	if err := d.Remover.DeleteByTab(session, tab); err != nil {
		return fmt.Errorf("pending の削除に失敗しました: %w", err)
	}
	if err := d.Registry.RemoveByTab(session, tab); err != nil {
		return fmt.Errorf("レジストリからの削除に失敗しました: %w", err)
	}
	if err := d.ScreenState.Remove(session, domain.ScreenTabSlug(tab)); err != nil {
		return fmt.Errorf("スクリーン検出の状態の削除に失敗しました: %w", err)
	}

	if id := domain.ResolveTabID(d.Tabs.ListTabs(), tab); id != "" {
		d.Closer.CloseTabByID(id)
		return nil
	}
	if d.CloseActiveOnMissingID {
		d.Closer.CloseActiveTab()
	}
	return nil
}
