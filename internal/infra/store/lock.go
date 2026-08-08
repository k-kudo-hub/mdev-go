package store

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// DefaultLockTimeout はロック取得を諦めるまでの既定の待ち時間
// (現行 lock-lib.sh の acquire_lock 第 2 引数の既定値と同じ)。
const DefaultLockTimeout = 5 * time.Second

// lockPollInterval はロックが空くのを待つポーリング間隔
// (現行版の `sleep 0.1` と同じ)。
const lockPollInterval = 100 * time.Millisecond

// pidFileName はロックの所有者 PID を書くファイル名。
const pidFileName = "pid"

// Lock はディレクトリの作成でロックを表す排他制御である。
//
// daily log の追記(record-output)と読み書き直し(restore-task)を直列化して、
// 完了記録が同時書き込みで失われるのを防ぐために使う。macOS には flock(1) が
// 無いため、現行 lock-lib.sh はアトミックな mkdir と PID ファイルでこれを
// 実現している。その方式をそのまま移植したものである。
//
// 所有者のプロセスが消えたロック(stale)は自動的に回収する。回収は
// 「別名へ rename してから削除」の順で行う。rename は競合しても 1 つの
// プロセスしか成功しないため、別のプロセスが作り直したばかりのロックを
// 誤って消すことがない。
type Lock struct {
	dir string
}

// NewLock は dir をロックディレクトリとして使う Lock を返す。
func NewLock(dir string) *Lock {
	return &Lock{dir: dir}
}

// Acquire はロックを取得する。timeout までに取得できなければ false を返す。
//
// 呼び出し側は false を受けても処理を続けてよい(fail-open)。ロック待ちで
// hook やループが止まることのほうが、まれな競合よりも実害が大きいためである。
func (l *Lock) Acquire(timeout time.Duration) (bool, error) {
	deadline := int(timeout / lockPollInterval)
	waited := 0

	for {
		err := os.Mkdir(l.dir, dirPerm)
		if err == nil {
			break
		}
		if !errors.Is(err, fs.ErrExist) {
			return false, fmt.Errorf("ロック %s の作成に失敗しました: %w", l.dir, err)
		}

		if l.reclaimIfStale() {
			continue
		}

		waited++
		if waited >= deadline {
			return false, nil
		}
		time.Sleep(lockPollInterval)
	}

	// PID の書き込み失敗はロックの取得自体を無効にしない。所有者不明の
	// ロックは stale 判定されないため、タイムアウトで解放されるだけである。
	_ = os.WriteFile(l.pidPath(), []byte(strconv.Itoa(os.Getpid())), filePerm)
	return true, nil
}

// Release はロックを解放する。
func (l *Lock) Release() error {
	if err := os.RemoveAll(l.dir); err != nil {
		return fmt.Errorf("ロック %s の解放に失敗しました: %w", l.dir, err)
	}
	return nil
}

// pidPath は所有者 PID を書くファイルのパスを返す。
func (l *Lock) pidPath() string {
	return filepath.Join(l.dir, pidFileName)
}

// reclaimIfStale は所有者プロセスが消えているロックを回収する。
// 回収を試みた(= mkdir をやり直す価値がある)場合に true を返す。
func (l *Lock) reclaimIfStale() bool {
	owner, ok := l.owner()
	if !ok || processExists(owner) {
		return false
	}

	// 別名へ退避してから消す。rename は競合しても 1 つしか成功しないため、
	// 他のプロセスが作り直したロックを消してしまうことがない。
	stale := fmt.Sprintf("%s.stale.%d", l.dir, os.Getpid())
	if err := os.Rename(l.dir, stale); err != nil {
		// 退避に失敗した(他のプロセスが先に回収した)場合も
		// mkdir からやり直す。
		return true
	}
	_ = os.RemoveAll(stale)
	return true
}

// owner はロックの所有者 PID を返す。PID ファイルが無い、または数値として
// 読めない場合は ok=false を返す(所有者不明として stale 判定しない)。
func (l *Lock) owner() (int, bool) {
	b, err := os.ReadFile(l.pidPath()) //nolint:gosec // ロックディレクトリ配下の固定ファイル名
	if err != nil {
		return 0, false
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(b)))
	if err != nil || pid <= 0 {
		return 0, false
	}
	return pid, true
}

// processExists は PID のプロセスが存在するかを返す。
//
// 現行版の `kill -0 "$owner"` と同じ判定である。シグナルを送る権限が無い
// 場合(他ユーザーのプロセス)も現行版と同じく「存在しない」と扱う。
func processExists(pid int) bool {
	process, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	return process.Signal(syscall.Signal(0)) == nil
}
