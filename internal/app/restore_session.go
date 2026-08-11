package app

import (
	"fmt"

	"github.com/k-kudo-hub/mdev-go/internal/domain"
)

// SessionStarter は起動時にタスクタブを作り直す。実体は SessionRestorer。
//
// 戻り値は作り直せなかったタスクの説明である。**標準エラーへ書いてはならない。**
// この処理は動作中の Bubble Tea プログラムの中から呼ばれ、同じ端末へ直接
// 書くとインラインレンダラの描画が崩れる。呼び出し側が画面へ出す。
type SessionStarter interface {
	Restore(env PaneEnv) []string
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
//
// 戻り値は作り直せなかったタスクの説明である。**ここから標準エラーへ書かない。**
// 呼び出し元は動作中の Bubble Tea プログラムなので、同じ端末へ直接書くと
// 描画が崩れる(SessionStarter のコメントを参照)。
func (r *SessionRestorer) Restore(env PaneEnv) []string {
	session := env.Session()
	entries, err := r.Registry.List(session)
	if err != nil || len(entries) == 0 {
		// 読めない場合も「1 件も無い」と同じ扱いにする(現行版も
		// `ls "$REG_DIR"/*.json >/dev/null 2>&1 || exit 0` で黙って抜ける)。
		return nil
	}

	var warnings []string
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
				warnings = append(warnings, fmt.Sprintf(
					"タスク %s のレジストリエントリを削除できませんでした: %v", entry.Tab, err))
			}
			continue
		}
		ok, warning := r.create(env, entry)
		if warning != "" {
			warnings = append(warnings, warning)
		}
		if ok {
			restored++
		}
	}

	// create_task はフォーカスを最後に作ったタブへ残す。ダッシュボードで終わる。
	if restored > 0 {
		_ = r.Focuser.FocusTab(domain.MainTabName)
	}
	return warnings
}

// create は 1 件のエントリからタブを作り、「復元した」と数えてよいかと、
// 画面へ出す説明(空なら何も無い)を返す。
//
// タブは出来たがフォーカスを確認できずペインを組めなかった場合
// (ErrTabNotRegistered / ErrFocusNotConfirmed = 現行版の rc=3)も
// **復元したものとして数える**。タブとエージェントは動いているので、失敗として
// 扱うと次回の起動ではこのタブが既存としてスキップされ、永久に直らない。
// 数えないと最後のダッシュボード帰還も起きず、フォーカスが半端なタブに残る。
func (r *SessionRestorer) create(env PaneEnv, entry domain.RegistryEntry) (bool, string) {
	warning, err := recreateTask(r.Creator, env, TaskSpec{
		Dir:    entry.Dir,
		Type:   entry.TaskType,
		Name:   entry.Tab,
		Resume: resumeSessionID(r.Paths, entry.ClaudeSessionID, entry.TranscriptPath),
		Agent:  entry.Agent,
	})
	if err != nil {
		// タブそのものが作れなかった。エントリは残して次回に賭ける。
		return false, fmt.Sprintf("タスク %s を復元できませんでした: %v", entry.Tab, err)
	}
	return true, warning
}
