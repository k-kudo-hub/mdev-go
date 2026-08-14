package app

import (
	"errors"
	"fmt"
	"time"

	"github.com/k-kudo-hub/mdev-go/internal/domain"
)

// turnRecord は「エージェントのターンが区切れた」ことを pending とレジストリへ
// 反映するための入力である。
//
// Claude Code の hook(HandleNotify)と codex の notify(Notify)は、入力の
// 渡され方が違うだけで、書き込む中身も順序も同じである。違うのは 3 点だけ
// なので、それをこの型のフィールドとして受ける。
type turnRecord struct {
	// Session / SessionID は pending とレジストリの鍵。
	Session   string
	SessionID string
	// Tab / Dir / TaskType はタスクの素性。
	Tab      string
	Dir      string
	TaskType string
	// Agent は記録するエージェント名。**既定値が経路ごとに違う**
	// (hook は claude、codex の notify は codex)。
	Agent string
	// Message は pending に残す文言。
	Message string
	// Event は pending の event。**hook は入力の hook_event_name をそのまま
	// 使い、codex は常に Stop** である(codex にはターン完了しか来ない)。
	Event string
	// TranscriptPath は会話ログの場所。引けなければ空。
	TranscriptPath string
	// RegisterTask はレジストリへ入れるかどうか(conductor のタスクタブか)。
	RegisterTask bool
	// Overwrite は既存の pending(event が existing)を上書きしてよいかを
	// 返す。**判定が経路ごとに違う**(hook は Notification と Waiting を守り、
	// codex は Waiting だけを守る)。
	Overwrite func(existing string) bool
}

// applyTurnRecord は turnRecord をレジストリと pending へ書く。
//
// 処理順は現行 Shell 版に合わせている。レジストリの更新を pending の上書き
// 判定より先に行うため、pending を書かない場合でもレジストリは最新化される
// (再起動後の復元がタスクを取りこぼさない)。
//
// 途中の失敗で処理を打ち切らない。現行版は set -e を使っておらず、レジストリの
// 更新に失敗しても pending の書き込みへ進むため、失敗経路でもファイル状態が
// 現行版と一致するよう副作用をすべて実行してからエラーをまとめて返す。
func applyTurnRecord(pending PendingStore, registry RegistryStore, now time.Time, r turnRecord) error {
	var errs []error
	if r.RegisterTask {
		entry := domain.RegistryEntry{
			Tab:             r.Tab,
			Session:         r.Session,
			ClaudeSessionID: r.SessionID,
			UpdatedAt:       now.Format(domain.RegistryUpdatedAtLayout),
			Dir:             r.Dir,
			TaskType:        r.TaskType,
			Agent:           r.Agent,
			TranscriptPath:  r.TranscriptPath,
		}
		if err := registry.Upsert(entry); err != nil {
			errs = append(errs, fmt.Errorf("レジストリの更新に失敗しました: %w", err))
		}
	}

	if !r.Overwrite(pending.Event(r.Session, r.SessionID)) {
		return errors.Join(errs...)
	}

	record := domain.Pending{
		Tab:             r.Tab,
		Session:         r.Session,
		ClaudeSessionID: r.SessionID,
		Message:         r.Message,
		Event:           r.Event,
		Time:            now.Format(domain.PendingTimeLayout),
		Agent:           r.Agent,
		TranscriptPath:  r.TranscriptPath,
		Dir:             r.Dir,
		TaskType:        r.TaskType,
	}
	if err := pending.Save(r.Session, r.SessionID, record); err != nil {
		errs = append(errs, fmt.Errorf("pending の書き込みに失敗しました: %w", err))
	}
	return errors.Join(errs...)
}
