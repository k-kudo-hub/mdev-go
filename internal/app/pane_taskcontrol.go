package app

import (
	"fmt"

	"github.com/k-kudo-hub/mdev-go/internal/domain"
)

// PendingRawStore は pending ファイルを解釈せずに読み書きする。
//
// Waiting の切り替えは pending の一部のキー(event / prev_event / time)だけを
// 書き換えるもので、mdev が知らないキーを落としてはならない。構造体を経由すると
// 将来 pending にキーが増えたとき黙って消えてしまうため、バイト列のまま扱う
// port を分けている(現行 waiting-toggle.sh の jq も知らないキーを保つ)。
type PendingRawStore interface {
	// FindRawByTab は session の pending からタブ名が一致する 1 件を、
	// ファイル名と中身の組で返す。該当が無ければ found=false を返す。
	// 複数該当する場合はファイル名の昇順で最初の 1 件(現行の glob と同じ)。
	FindRawByTab(session, tab string) (name string, data []byte, found bool, err error)
	// WriteRaw は name の pending を data で原子的に置き換える。
	WriteRaw(session, name string, data []byte) error
}

// TaskControlPane はタスクタブ下部の操作バーのユースケースである
// (現行 task-control.sh 相当)。
type TaskControlPane struct {
	// Pending は表示のために今の event を引く。
	Pending PendingFinder
	// Raw は Waiting の切り替えで pending を書き換える。
	Raw PendingRawStore
	// Focuser は m キーで Main タブへ移る。
	Focuser Focuser
	// Deleter は dd の削除フローを持つ。
	Deleter *TaskDeleter
	Clock   Clock
}

// Refresh は操作バーの表示を組み立てる。
//
// このタブの pending が Waiting なら WAITING 表示になる。pending が無い場合は
// 通常表示である(現行 current_event が空文字を返す場合に対応)。
func (p *TaskControlPane) Refresh(env PaneEnv, tab string) (string, error) {
	pending, found, err := p.Pending.FindByTab(env.Session(), tab)
	if err != nil {
		return "", fmt.Errorf("pending の読み取りに失敗しました: %w", err)
	}
	event := ""
	if found {
		event = pending.Event
	}
	return domain.RenderTaskControlBar(domain.TaskControlWaiting(event)), nil
}

// GoToMain は Main タブへフォーカスを移す(m キー)。
func (p *TaskControlPane) GoToMain() error {
	if err := p.Focuser.FocusTab(domain.MainTabName); err != nil {
		return fmt.Errorf("タブ %s への移動に失敗しました: %w", domain.MainTabName, err)
	}
	return nil
}

// ToggleWaiting はこのタブの Waiting 状態を切り替える(w キー)。
//
// 対象は「このタブの pending のうちファイル名の昇順で最初の 1 件」だけである。
// pending がまだ無いタスクでは**何もしない**(新しく作らない)。Waiting は
// エージェントが Notification か Stop を出したタスクにだけ意味があり、
// 勝手に pending を作るとセッション ID を鍵にする解決 hook が後片付けを
// できなくなる。
func (p *TaskControlPane) ToggleWaiting(env PaneEnv, tab string) error {
	session := env.Session()
	name, data, found, err := p.Raw.FindRawByTab(session, tab)
	if err != nil {
		return fmt.Errorf("pending の読み取りに失敗しました: %w", err)
	}
	if !found {
		return nil
	}

	now := p.Clock.Now().Format(domain.PendingTimeLayout)
	toggled, ok := domain.ToggleWaiting(data, now)
	if !ok {
		// 変換できない中身だった。現行版も jq が失敗したら書き戻さず
		// 元のファイルを残す。
		return nil
	}
	if err := p.Raw.WriteRaw(session, name, toggled); err != nil {
		return fmt.Errorf("pending の書き込みに失敗しました: %w", err)
	}
	return nil
}

// PrepareDelete は削除フローの前半(記録とアップロード)を行う。
func (p *TaskControlPane) PrepareDelete(env PaneEnv, tab string) (DeletePreparation, error) {
	return p.Deleter.Prepare(env, tab)
}

// CommitDelete は削除フローの後半(片付けとタブを閉じる)を行う。
func (p *TaskControlPane) CommitDelete(env PaneEnv, tab string) error {
	return p.Deleter.Commit(env, tab)
}
