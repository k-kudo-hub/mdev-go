package app

import "github.com/k-kudo-hub/mdev-go/internal/domain"

// セッションの掃除(現行の蓄積対策)が必要とする port。
//
// zellij の CLI とプロセス操作は、どれも「返らなくなる」ことがある。
// 掃除はセッションの起動前に走るため、実装は必ず実行時間の上限を持つこと。

// SessionLister は zellij のセッション一覧を返す。
type SessionLister interface {
	// ListSessions は `zellij list-sessions --no-formatting` の標準出力を返す。
	// セッションが 1 つも無い場合、zellij は非 0 で終わることがあるため、
	// 実装は出力が取れていれば error にしない。
	ListSessions() (string, error)
}

// SessionClientLister はセッションにアタッチしているクライアントを返す。
type SessionClientLister interface {
	// ListClients は `zellij --session <name> action list-clients` の
	// 標準出力を返す。
	//
	// **失敗は「アタッチあり」として扱うこと**(安全側)。誰も居ないと
	// 誤って判断すると、使用中のセッションを kill する。
	ListClients(session string) (string, error)
}

// SessionRemover はセッションを終了・削除する。
type SessionRemover interface {
	// KillSession は動いているセッションを終了させる。
	KillSession(name string) error
	// DeleteSession はセッションのメタデータを削除する。
	DeleteSession(name string) error
}

// ProcessLister は動いているプロセスの一覧を返す。
type ProcessLister interface {
	// ListProcesses は `ps -axo pid,ppid,command` の標準出力を返す。
	ListProcesses() (string, error)
}

// ProcessSignaler はプロセスへシグナルを送る。
type ProcessSignaler interface {
	// Terminate は SIGTERM を送る(後始末の機会を与える)。
	Terminate(pid int) error
	// Kill は SIGKILL を送る(TERM で終わらなかったものへの最後の手段)。
	Kill(pid int) error
	// IsAlive は pid のプロセスがまだ居るかを返す。
	IsAlive(pid int) bool
}

// SessionAttachChecker はセッションを誰か開いているかを返す。
//
// ペインのポーリングが「誰も見ていないなら遅く回す」ために使う。
// **判断できない場合は true(開いている)を返すこと。** 誰も居ないと
// 誤って判断すると、実際には見ている画面が 60 秒間隔になって固まって
// 見える。
type SessionAttachChecker interface {
	IsAttached(session string) bool
}

// SessionSocketLocator は自分が見ている zellij のソケット置き場を返す。
//
// 掃除の対象を「自分から見える範囲」へ絞るために使う。zellij は
// 一時ディレクトリ配下にソケットを置き、`list-sessions` はその範囲しか
// 見ない。別の一時ディレクトリで起動されたサーバは一覧に出ないが、
// それは「ゾンビ」ではなく「こちらから見えていないだけ」である。
//
// 空文字を返した場合は範囲を決められないということなので、呼び出し側は
// ゾンビの掃除を行わない(触れない側へ倒す)。
type SessionSocketLocator interface {
	SocketDir() string
}

// SessionTraceChecker は mdev がそのセッション名で残した痕跡を探す。
//
// 終了済みセッションのメタデータを消してよいかの判断に使う。mdev が
// resurrection(終了済みセッションへ attach して復活させる仕組み)を
// 使わないのは **mdev 自身の設計判断** であって、利用者が手で作った
// セッションにまで押し付けてよいものではない。痕跡が無い終了済み
// セッションには触れない(メタデータが残っていても無害である)。
type SessionTraceChecker interface {
	// HasTrace は mdev がそのセッションを扱った跡があるかを返す。
	HasTrace(session string) bool
}

// SelfReplacer は mdev 自身のバイナリを新しいものへ置き換える。
//
// 取得したバイナリの SHA-256 を配布元の checksums.txt と照合し、
// **合わなければ必ず失敗させること。** 素性の分からないものを実行ファイル
// として置くわけにはいかない。
//
// 戻り値は置き換えたバイナリのパスである。
type SelfReplacer interface {
	Replace(plan domain.SelfUpdatePlan) (path string, err error)
}
