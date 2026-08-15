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

// PendingFinder は pending をタブ名で探す。
//
// record は「どのタスクタブが閉じられたか」しか知らないため、セッション ID を
// 鍵にする PendingStore とは別の引き方が要る。ユースケースが必要とする操作
// 単位で port を分けている(ADR-0002)。
type PendingFinder interface {
	// FindByTab は session の pending からタブ名が一致する 1 件を返す。
	// 該当が無ければ found=false を返す。複数該当する場合にどれを返すかは
	// 実装が決める(現行版はファイル名の辞書順で最初の 1 件)。
	FindByTab(session, tab string) (pending domain.Pending, found bool, err error)
}

// TranscriptReader はエージェントの transcript ファイルを読む。
type TranscriptReader interface {
	// Read は path の内容を返す。ファイルが無い・読めない場合は found=false を
	// 返す。現行版の `[ -f "$TRANSCRIPT_PATH" ]` と同じ扱いである。
	Read(path string) (data []byte, found bool)
}

// DailyAppender は daily log へレコードを 1 行書く。
type DailyAppender interface {
	// Append は session の date のファイルへ record を書く。
	// date は domain.DailyFileDateLayout で整形した文字列である。
	//
	// 同じ記録の書き直しになる行が既にあれば、それを取り除いてから末尾へ書く
	// (追記ではなく置換)。取り除く条件は次のすべてを満たす行である。
	//
	//   - tab が record.Tab と一致する
	//   - claude_session_id が record.ClaudeSessionID と一致する
	//   - restored が true ではない(復元済みの履歴は残す)
	//
	// 置換キーを持たないレコード(domain.DailyRecord.HasDedupeKey が false)は、
	// 何も取り除かずに追記する。同じ引数で 2 回呼んでも daily log の行数が
	// 増えないことが、キーを持つ場合にこのポートの利用者(RecordOutput)が
	// 頼っている性質である。
	Append(session, date string, record domain.DailyRecord) error
}

// PricingLoader は料金表を読む。
//
// 設定が読めない場合もエラーを返さない。現行版は pricing の取得に失敗しても
// 空の単価表で処理を続け、料金の記録より作業ログの記録を優先するためである。
type PricingLoader interface {
	Load() domain.Pricing
}

// RecordEnv は record 実行時の環境変数のうち mdev が使うものである。
type RecordEnv struct {
	ZellijSession string // ZELLIJ_SESSION_NAME
}
