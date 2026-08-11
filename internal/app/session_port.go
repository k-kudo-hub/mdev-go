package app

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
