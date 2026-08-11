package app

import (
	"errors"
	"fmt"
	"io"
	"strings"

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
	// Warn はタブだけ復元できた場合の説明を書く先。
	Warn io.Writer
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
func (r *TaskRestorer) Restore(env PaneEnv, tab, session, completedAt string) error {
	if tab == "" || session == "" || completedAt == "" {
		return fmt.Errorf("%w: 引数が足りません", ErrRestoreEntryNotFound)
	}
	date := completedAt
	if len(date) > dailyDateLength {
		date = date[:dailyDateLength]
	}

	target, found := r.Daily.FindRestorable(session, date, tab, completedAt)
	if !found {
		return fmt.Errorf("%w: %s(%s)", ErrRestoreEntryNotFound, tab, completedAt)
	}
	if target.Dir == "" {
		return fmt.Errorf("%w: %s", ErrRestoreDirUnknown, tab)
	}
	if !r.Paths.IsDir(target.Dir) {
		return fmt.Errorf("%w: %s(%s)", ErrRestoreDirMissing, tab, target.Dir)
	}

	if err := r.create(env, tab, target); err != nil {
		return err
	}

	// タブが実際に出来たときだけ印を付ける。付けてしまうと Done から消え、
	// 作業ログだけが残って手掛かりが無くなる。
	if err := r.Daily.MarkRestored(session, date, tab, completedAt); err != nil {
		return fmt.Errorf("%w: %s: %w", ErrRestoreDailyUpdate, tab, err)
	}
	return nil
}

// create はタブを作り直す。
//
// タブは出来たがフォーカスを確認できずペインを組めなかった場合
// (ErrTabNotRegistered / ErrFocusNotConfirmed = 現行版の rc=3)は成功として
// 扱う。タブとエージェントは動いており、Done に残して再試行させると同名タブが
// 増えるだけになる。
func (r *TaskRestorer) create(env PaneEnv, tab string, target domain.DailyRestoreTarget) error {
	_, err := r.Creator.Execute(env, TaskSpec{
		Dir:    target.Dir,
		Type:   target.TaskType,
		Name:   tab,
		Resume: r.resumeID(target),
		Agent:  target.Agent,
	})
	switch {
	case err == nil:
		return nil
	case errors.Is(err, ErrTabNotRegistered), errors.Is(err, ErrFocusNotConfirmed):
		r.warnf("タスク %s はタブだけ復元しました(操作バーは作れていません): %v", tab, err)
		return nil
	default:
		return fmt.Errorf("%w: %s: %w", ErrRestoreTabFailed, tab, err)
	}
}

// resumeID は再開に使うエージェントのセッション ID を返す。
//
// 現行版の 3 条件(セッション ID がある / transcript のパスが記録されている /
// そのファイルが実在する)に加えて、**スクリーン検出が合成した ID を除く**。
//
// hook を持たないエージェント(codex)の完了はタブの画面から検出するため、
// その pending の claude_session_id は `screen-<slug>` というタブ名から作った
// 合成 ID である(domain.ScreenPendingSessionID)。これは daily ログにもその
// まま書かれ、transcript はレジストリから借りて実在するので、現行版の 3 条件を
// そのまま通ってしまう。結果として Done から戻したときに
// `codex resume screen-cx_task-1234567890` という存在しない ID で起動する。
// Go 版で足した修正で、Shell 版との意図的な差異である(evidence §5-1)。
func (r *TaskRestorer) resumeID(target domain.DailyRestoreTarget) string {
	if target.ClaudeSessionID == "" || target.TranscriptPath == "" {
		return ""
	}
	if strings.HasPrefix(target.ClaudeSessionID, domain.ScreenSessionIDPrefix) {
		return ""
	}
	if !r.Paths.IsFile(target.TranscriptPath) {
		return ""
	}
	return target.ClaudeSessionID
}

// warnf は警告を 1 行書く。書き込みの失敗を報告する先は無いため無視する。
func (r *TaskRestorer) warnf(format string, args ...any) {
	if r.Warn == nil {
		return
	}
	_, _ = fmt.Fprintf(r.Warn, "mdev: "+format+"\n", args...)
}
