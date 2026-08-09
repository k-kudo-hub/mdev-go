package app

import (
	"time"

	"github.com/k-kudo-hub/mdev-go/internal/domain"
)

// PendingStore は pending ファイルの読み書きを抽象化する。
// 実装は internal/infra/store にある。
type PendingStore interface {
	// Event は (session, sessionID) の pending の event を返す。
	// pending が無い場合と JSON が壊れている場合はいずれも空文字を返す。
	// 現行 Shell 版が `jq -r '.event' ... 2>/dev/null` で両者を空文字に潰し、
	// 同じ経路(Stop に上書きされる / PostToolUse では削除しない)に落として
	// いた挙動をそのまま port の契約にしている。
	Event(session, sessionID string) string

	// Save は pending を書き込む。既存の内容は完全に置き換える。
	Save(session, sessionID string, pending domain.Pending) error

	// Delete は pending を削除する。存在しない場合も成功として扱う。
	Delete(session, sessionID string) error
}

// RegistryStore はタスクレジストリへの書き込みを抽象化する。
type RegistryStore interface {
	// Upsert は (Session, ClaudeSessionID) のエントリを作成または完全上書きする。
	Upsert(entry domain.RegistryEntry) error
}

// Focuser は zellij のタブフォーカス移動を抽象化する。
type Focuser interface {
	// FocusTab は名前でタブにフォーカスを移す。
	FocusTab(name string) error
}

// Clock は現在時刻を供給する。domain は time.Now() を呼ばないため
// (ADR-0002)、時刻はこの port からユースケースが取得して domain に渡す。
type Clock interface {
	Now() time.Time
}

// HookEnv は hook 実行時の環境変数のうち mdev が使うものである。
//
// zellij のタブ生成時に conductor が設定するため、TaskTabName が非空である
// ことが「conductor が作ったタスクタブである」ことの判定に使われる。
// hook は同一マシン上のすべての Claude Code セッションで発火するため、
// この判定なしにレジストリへ登録すると conductor 外のセッションまで拾ってしまう。
type HookEnv struct {
	ZellijSession string // ZELLIJ_SESSION_NAME
	TaskTabName   string // TASK_TAB_NAME
	TaskType      string // TASK_TYPE
	TaskAgent     string // TASK_AGENT
}

// IsTaskTab は conductor が作ったタスクタブかどうかを返す。
// 現行版の `[ -n "$ZELLIJ_SESSION_NAME" ] && [ -n "$TASK_TAB_NAME" ]` に対応する。
func (e HookEnv) IsTaskTab() bool {
	return e.ZellijSession != "" && e.TaskTabName != ""
}

// InZellij は zellij セッション内で実行されているかを返す。
// フォーカス移動を行うかどうかの判定に使う。
func (e HookEnv) InZellij() bool {
	return e.ZellijSession != ""
}
