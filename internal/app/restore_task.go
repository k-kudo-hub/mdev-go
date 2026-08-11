package app

import (
	"errors"
	"fmt"

	"github.com/k-kudo-hub/mdev-go/internal/domain"
)

// 現行 restore-task.sh の終了コード 0-5 に対応する sentinel エラー。
//
// 呼び出し側(Done ペイン)は今のところどれも区別せず、失敗すればエントリが
// Done に残ることで利用者が気づく作りになっている。それでも種類を分けて
// 返すのは、どこで止まったかが分からないと利用者に説明できないためである。
var (
	// ErrRestoreEntryNotFound は引数が足りない、または一致する未復元の
	// エントリが無い(現行版の exit 1)。
	ErrRestoreEntryNotFound = errors.New("復元するエントリが見つかりません")
	// ErrRestoreDirUnknown はエントリに作業ディレクトリの記録が無い
	// (dir を記録する前の古いエントリ。現行版の exit 2)。
	ErrRestoreDirUnknown = errors.New("エントリに作業ディレクトリが記録されていません")
	// ErrRestoreDirMissing は記録された作業ディレクトリが消えている
	// (閉じた worktree など。現行版の exit 3)。
	ErrRestoreDirMissing = errors.New("記録された作業ディレクトリがありません")
	// ErrRestoreTabFailed はタブそのものを作れなかった(現行版の exit 4)。
	ErrRestoreTabFailed = errors.New("タブを作り直せませんでした")
	// ErrRestoreDailyUpdate はタブは出来たが daily ログを更新できなかった
	// (現行版の exit 5)。タスクは Done に残ったままになる。
	ErrRestoreDailyUpdate = errors.New("daily ログを更新できませんでした")
)

// dailyDateLength は completed_at の先頭から日付として切り出す長さ。
// 現行版の `${COMPLETED_AT:0:10}` に対応する(completed_at は ASCII)。
const dailyDateLength = 10

// DailyRestoreStore は Done から戻すタスクを daily ログで探し、印を付ける。
type DailyRestoreStore interface {
	// FindRestorable は (tab, completedAt) が一致し、まだ復元されていない
	// 最初の 1 件を返す。ファイルが無い場合は found=false を返す。
	FindRestorable(session, date, tab, completedAt string) (domain.DailyRestoreTarget, bool)
	// MarkRestored は同じ条件の最初の 1 件へ restored: true を付ける。
	MarkRestored(session, date, tab, completedAt string) error
}

// TaskRestorer は Done のタスクをダッシュボードへ戻すユースケースである
// (現行 restore-task.sh 相当)。
//
// タブを作り直してから daily ログへ `restored: true` を付ける。順序が逆だと、
// タブが出来ていないのに Done から消えて作業ログだけが残る。
type TaskRestorer struct {
	Daily   DailyRestoreStore
	Creator TaskMaker
	Paths   PathChecker
}

// Restore は (tab, session, completedAt) のタスクをダッシュボードへ戻す。
//
// session と completedAt は daily ログの置き場所を決める。Done は当日ぶんを
// セッションをまたいで並べるため、今いる zellij セッションとは限らない。
// 日付は完了時刻の先頭 10 文字である(日付をまたいだ直後に前日のエントリを
// 引けるようにするため)。
//
// エラーの種類は現行版の終了コード 0-5 に対応する。タブが出来たあとに
// daily の更新で失敗した場合(ErrRestoreDailyUpdate)は、タスクが Done に
// 残ったまま**タブも存在する**状態になる。現行版と同じ挙動である。
//
// 第 1 戻り値はタブだけ復元できた場合の説明である(空なら何も無い)。
// **標準エラーへは書かない。** この処理は動作中の Bubble Tea プログラムの
// 中から呼ばれ、同じ端末へ直接書くとインラインレンダラの描画が崩れる。
func (r *TaskRestorer) Restore(env PaneEnv, tab, session, completedAt string) (string, error) {
	if tab == "" || session == "" || completedAt == "" {
		return "", fmt.Errorf("%w: 引数が足りません", ErrRestoreEntryNotFound)
	}
	date := completedAt
	if len(date) > dailyDateLength {
		date = date[:dailyDateLength]
	}

	target, found := r.Daily.FindRestorable(session, date, tab, completedAt)
	if !found {
		return "", fmt.Errorf("%w: %s(%s)", ErrRestoreEntryNotFound, tab, completedAt)
	}
	if target.Dir == "" {
		return "", fmt.Errorf("%w: %s", ErrRestoreDirUnknown, tab)
	}
	if !r.Paths.IsDir(target.Dir) {
		return "", fmt.Errorf("%w: %s(%s)", ErrRestoreDirMissing, tab, target.Dir)
	}

	warning, err := recreateTask(r.Creator, env, TaskSpec{
		Dir:    target.Dir,
		Type:   target.TaskType,
		Name:   tab,
		Resume: resumeSessionID(r.Paths, target.ClaudeSessionID, target.TranscriptPath),
		Agent:  target.Agent,
	})
	if err != nil {
		return "", fmt.Errorf("%w: %s: %w", ErrRestoreTabFailed, tab, err)
	}

	// タブが実際に出来たときだけ印を付ける。付けてしまうと Done から消え、
	// 作業ログだけが残って手掛かりが無くなる。
	if err := r.Daily.MarkRestored(session, date, tab, completedAt); err != nil {
		return warning, fmt.Errorf("%w: %s: %w", ErrRestoreDailyUpdate, tab, err)
	}
	return warning, nil
}
