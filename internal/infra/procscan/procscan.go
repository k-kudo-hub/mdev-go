// Package procscan は動いているプロセスの一覧取得とシグナル送信を担当する。
// internal/app が定義する port の実装(adapter)である(ADR-0002)。
//
// プロセスグループごと打ち切る起動方法(internal/infra/proc)とは役割が違う。
// あちらは「自分が起こしたコマンドを確実に終わらせる」ためのもので、
// こちらは「自分が起こしたのではない残骸を見つけて片付ける」ためのものである。
package procscan

import (
	"context"
	"fmt"
	"os"
	"syscall"
	"time"

	"github.com/k-kudo-hub/mdev-go/internal/app"
	"github.com/k-kudo-hub/mdev-go/internal/infra/proc"
)

// listTimeout は ps 1 回の実行時間の上限である。
// 掃除はセッションの起動前に走るので、返らないコマンドで起動を止めない。
const listTimeout = 10 * time.Second

// psArgs は プロセス一覧の取り方である。macOS と Linux の両方で同じ列が出る。
var psArgs = []string{"-axo", "pid,ppid,etime,command"}

// Scanner はプロセスの一覧取得とシグナル送信を行う。
type Scanner struct {
	// output は ps を実行して標準出力を返す。テストで差し替える。
	output func(timeout time.Duration, name string, args ...string) (string, error)
	// signal はプロセスへシグナルを送る。テストで差し替える。
	signal func(pid int, sig syscall.Signal) error
}

var (
	_ app.ProcessLister   = (*Scanner)(nil)
	_ app.ProcessSignaler = (*Scanner)(nil)
)

// NewScanner は ps とシグナルを使う Scanner を返す。
func NewScanner() *Scanner {
	return &Scanner{output: commandOutput, signal: sendSignal}
}

// ListProcesses は `ps -axo pid,ppid,etime,command` の標準出力を返す。
func (s *Scanner) ListProcesses() (string, error) {
	out, err := s.output(listTimeout, "ps", psArgs...)
	if err != nil {
		return "", fmt.Errorf("プロセス一覧の取得に失敗しました: %w", err)
	}
	return out, nil
}

// Terminate は SIGTERM を送る。zellij サーバに後始末の機会を与える。
func (s *Scanner) Terminate(pid int) error {
	return s.signal(pid, syscall.SIGTERM)
}

// Kill は SIGKILL を送る。TERM で終わらなかったものへの最後の手段である。
func (s *Scanner) Kill(pid int) error {
	return s.signal(pid, syscall.SIGKILL)
}

// IsAlive は pid のプロセスがまだ居るかを返す。
//
// シグナル 0 は「送れるかどうかだけを確かめる」ための約束事で、プロセスに
// 何の影響も与えない。TERM の後に KILL が要るかを決めるために使う。
func (s *Scanner) IsAlive(pid int) bool {
	return s.signal(pid, syscall.Signal(0)) == nil
}

// sendSignal は pid へシグナルを送る。
//
// プロセスグループではなく **その 1 プロセスだけ** へ送る。掃除の対象は
// 個別に見分けた残骸であり、グループごと送ると同じグループに居る無関係の
// プロセス(使用中のセッションのペインなど)を巻き込みうる。
func sendSignal(pid int, sig syscall.Signal) error {
	// 見つけるだけなら失敗しない(Unix では FindProcess は常に成功する)。
	process, err := os.FindProcess(pid)
	if err != nil {
		return fmt.Errorf("プロセス %d が見つかりません: %w", pid, err)
	}
	if err := process.Signal(sig); err != nil {
		return fmt.Errorf("プロセス %d へのシグナル送信に失敗しました: %w", pid, err)
	}
	return nil
}

// commandOutput は外部コマンドを実行して標準出力を返す。
// 上限でプロセスグループごと切られた場合もエラーとして返る。
func commandOutput(timeout time.Duration, name string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	out, err := proc.Command(ctx, name, args...).Output()
	if err != nil {
		return "", err //nolint:wrapcheck // 呼び出し側が用途に応じて包む
	}
	return string(out), nil
}
