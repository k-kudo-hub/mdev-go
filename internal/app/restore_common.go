package app

import (
	"errors"
	"fmt"
	"strings"

	"github.com/k-kudo-hub/mdev-go/internal/domain"
)

// 復元の 2 経路(起動時のセッション復元と Done からの復元)が共有する判断。
//
// 2 つは入力(レジストリのエントリと daily のレコード)も失敗の扱いも違うが、
// 「再開してよいセッション ID か」と「タブを作り直せたか」の判定は同じで
// なければならない。別々に書くと、片方だけに直しが入って挙動がずれる。
// 実際 screen- 前置の合成 ID を弾く修正は Done 側にだけ入れれば足りるが、
// 同じ関数を通しておけばセッション復元側へ回り込む経路が塞がる。

// resumeSessionID は再開に使うエージェントのセッション ID を返す。
//
// 次のすべてを満たしたときだけ再開する。1 つでも欠ければ空を返し、
// 新規セッションで起動する(壊れた --resume をしない)。
//
//   - セッション ID が記録されている
//   - transcript のパスが記録されている
//   - **セッション ID がスクリーン検出の合成 ID ではない**
//   - transcript のファイルが実在する
//
// 3 つ目は現行 Shell 版に無い条件で、Go 版で足した修正である。hook を持たない
// エージェント(codex)の完了はタブの画面から検出するため、その pending の
// claude_session_id は `screen-<slug>` というタブ名から作った合成 ID になる
// (domain.ScreenPendingSessionID)。これは daily ログにもそのまま書かれ、
// transcript はレジストリから借りて実在するので、現行版の 3 条件をそのまま
// 通ってしまう。結果として Done から戻したときに
// `codex resume screen-cx_task-1234567890` という存在しない ID で起動する
// (evidence §5-1)。
//
// レジストリには合成 ID が入らないため、セッション復元の側では現状この条件に
// 引っかかるエントリが無い。それでもここで弾くのは、pending から
// レジストリへ書き戻す経路が将来増えたときに同じバグが戻らないようにする
// ためである。
func resumeSessionID(paths PathChecker, sessionID, transcriptPath string) string {
	if sessionID == "" || transcriptPath == "" {
		return ""
	}
	if strings.HasPrefix(sessionID, domain.ScreenSessionIDPrefix) {
		return ""
	}
	if !paths.IsFile(transcriptPath) {
		return ""
	}
	return sessionID
}

// recreateTask はタスクタブを作り直し、画面へ出す説明と失敗を返す。
//
// タブは出来たがフォーカスを確認できずペインを組めなかった場合
// (ErrTabNotRegistered / ErrFocusNotConfirmed = 現行 create_task の rc=3)は
// **成功として扱い**、説明だけを返す。タブとエージェントは動いているので、
// 失敗にすると同名のタブが増えるだけで永久に直らない。
//
// error を返すのはタブそのものが作られなかった場合だけである。呼び出し側は
// これを見て「エントリを残して再試行させる」か「終了コード 4 相当にする」かを
// 決める。
func recreateTask(creator TaskMaker, env PaneEnv, spec TaskSpec) (string, error) {
	_, err := creator.Execute(env, spec)
	switch {
	case err == nil:
		return "", nil
	case errors.Is(err, ErrTabNotRegistered), errors.Is(err, ErrFocusNotConfirmed):
		return fmt.Sprintf("タスク %s はタブだけ復元しました(操作バーは作れていません): %v",
			spec.Name, err), nil
	default:
		return "", err
	}
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
