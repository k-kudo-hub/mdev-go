package app

import (
	"errors"
	"fmt"
	"io"

	"github.com/k-kudo-hub/mdev-go/internal/domain"
)

// SessionStarter は起動時にタスクタブを作り直す。実体は SessionRestorer。
type SessionStarter interface {
	Restore(env PaneEnv)
}

// SessionRestorer は zellij セッションの再起動後にタスクタブを作り直す
// ユースケースである(現行 restore-session.sh 相当、issue #36)。
//
// レジストリのエントリからタブを作り、transcript が残っていればエージェントの
// 前回の会話を再開する(claude --resume / codex resume)。
//
// **最善努力で、決して失敗を外へ返さない。** 作り直せなかったタスクはエントリを
// 残したまま飛ばし(次回の起動で再試行される)、ディレクトリが消えたエントリは
// 捨てる。復元の失敗でダッシュボードが立ち上がらないほうが害が大きい。
type SessionRestorer struct {
	Registry RegistryLister
	Tabs     TabNameQuerier
	Creator  TaskMaker
	Paths    PathChecker
	Focuser  Focuser
	// Warn は作り直せなかったタスクの説明を書く先(通常は os.Stderr)。
	Warn io.Writer
}

var _ SessionStarter = (*SessionRestorer)(nil)

// Restore は登録済みタスクのタブを作り直す。
//
// 手順は現行版と同じである。
//
//  1. レジストリを読む。空(または読めない)ならここで終わり、zellij も呼ばない
//  2. タブごとに updated_at が最新の 1 件へ畳む(domain.LatestPerTab)。
//     --resume での再開はセッション ID を変えるので、古いエントリは使えない ID を持つ
//  3. 既にタブがあるものは飛ばす(エントリは残す)
//  4. dir が無い・消えているエントリは捨てる。作り直す先が無い
//  5. transcript が残っていれば再開、無ければ新規で起動する
//  6. 1 件でも作り直したらダッシュボードのタブへ戻る
//
// 既存タブの一覧はループの前で 1 度だけ引く。エントリはタブごとに 1 件へ
// 畳まれているため、ループ中に作ったタブを数え直す必要は無い。
func (r *SessionRestorer) Restore(env PaneEnv) {
	session := env.Session()
	entries, err := r.Registry.List(session)
	if err != nil || len(entries) == 0 {
		// 読めない場合も「1 件も無い」と同じ扱いにする(現行版も
		// `ls "$REG_DIR"/*.json >/dev/null 2>&1 || exit 0` で黙って抜ける)。
		return
	}

	existing := r.Tabs.QueryTabNames(ZellijCallTimeout)
	restored := 0
	for _, entry := range domain.LatestPerTab(entries) {
		if entry.Tab == "" || containsName(existing, entry.Tab) {
			continue
		}
		if entry.Dir == "" || !r.Paths.IsDir(entry.Dir) {
			// 作り直す先が無い(記録の無い古いエントリ、閉じた worktree)。
			// 残しても永久に復元できないので捨てる。
			if err := r.Registry.RemoveByTab(session, entry.Tab); err != nil {
				r.warnf("タスク %s のレジストリエントリを削除できませんでした: %v", entry.Tab, err)
			}
			continue
		}
		if r.create(env, entry) {
			restored++
		}
	}

	// create_task はフォーカスを最後に作ったタブへ残す。ダッシュボードで終わる。
	if restored > 0 {
		_ = r.Focuser.FocusTab(domain.MainTabName)
	}
}

// create は 1 件のエントリからタブを作り、「復元した」と数えてよいかを返す。
//
// タブは出来たがフォーカスを確認できずペインを組めなかった場合
// (ErrTabNotRegistered / ErrFocusNotConfirmed = 現行版の rc=3)も
// **復元したものとして数える**。タブとエージェントは動いているので、失敗として
// 扱うと次回の起動ではこのタブが既存としてスキップされ、永久に直らない。
// 数えないと最後のダッシュボード帰還も起きず、フォーカスが半端なタブに残る。
func (r *SessionRestorer) create(env PaneEnv, entry domain.RegistryEntry) bool {
	_, err := r.Creator.Execute(env, TaskSpec{
		Dir:    entry.Dir,
		Type:   entry.TaskType,
		Name:   entry.Tab,
		Resume: r.resumeID(entry),
		Agent:  entry.Agent,
	})
	switch {
	case err == nil:
		return true
	case errors.Is(err, ErrTabNotRegistered), errors.Is(err, ErrFocusNotConfirmed):
		r.warnf("タスク %s はタブだけ復元しました(操作バーは作れていません): %v", entry.Tab, err)
		return true
	default:
		// タブそのものが作れなかった。エントリは残して次回に賭ける。
		r.warnf("タスク %s を復元できませんでした: %v", entry.Tab, err)
		return false
	}
}

// resumeID は再開に使うエージェントのセッション ID を返す。
//
// 3 条件(セッション ID がある / transcript のパスが記録されている /
// そのファイルが実在する)がすべて揃ったときだけ再開する。1 つでも欠ければ
// 空を返し、新規セッションで起動する(壊れた --resume をしない)。
func (r *SessionRestorer) resumeID(entry domain.RegistryEntry) string {
	if entry.ClaudeSessionID == "" || entry.TranscriptPath == "" {
		return ""
	}
	if !r.Paths.IsFile(entry.TranscriptPath) {
		return ""
	}
	return entry.ClaudeSessionID
}

// warnf は警告を 1 行書く。書き込みの失敗を報告する先は無いため無視する。
func (r *SessionRestorer) warnf(format string, args ...any) {
	if r.Warn == nil {
		return
	}
	_, _ = fmt.Fprintf(r.Warn, "mdev: "+format+"\n", args...)
}

// containsName は names に name が含まれるかを返す。
func containsName(names []string, name string) bool {
	for _, existing := range names {
		if existing == name {
			return true
		}
	}
	return false
}
