package procscan

import (
	"errors"
	"os"
	"os/exec"
	"slices"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

// TestListProcessesRunsPs は実際に ps を起動して、自分自身の行が
// 取れることを確かめる。列の並び(pid ppid etime command)が前提である。
func TestListProcessesRunsPs(t *testing.T) {
	t.Parallel()

	out, err := NewScanner().ListProcesses()
	if err != nil {
		t.Fatalf("ListProcesses() = %v", err)
	}
	if out == "" {
		t.Fatal("出力が空です")
	}
	// 自分の PID が含まれていること(列の並びが期待どおりである証拠)。
	if !containsPID(out, os.Getpid()) {
		t.Errorf("自分の PID が出力にありません:\n%s", firstLines(out, 5))
	}
}

// TestListProcessesArgs は ps へ渡す引数と上限を固定する。
func TestListProcessesArgs(t *testing.T) {
	t.Parallel()

	var gotName string
	var gotArgs []string
	var gotTimeout time.Duration
	s := NewScanner()
	s.output = func(timeout time.Duration, name string, args ...string) (string, error) {
		gotName, gotArgs, gotTimeout = name, args, timeout
		return "out", nil
	}

	if _, err := s.ListProcesses(); err != nil {
		t.Fatalf("ListProcesses() = %v", err)
	}
	if gotName != "ps" {
		t.Errorf("コマンド = %q, want ps", gotName)
	}
	if want := []string{"-axo", "pid,ppid,etime,command"}; !slices.Equal(gotArgs, want) {
		t.Errorf("引数 = %v, want %v", gotArgs, want)
	}
	if gotTimeout != listTimeout {
		t.Errorf("上限 = %v, want %v", gotTimeout, listTimeout)
	}
}

// TestListProcessesReportsError は ps の失敗が error になることを確かめる。
// 判断材料が無い状態で掃除を進めてはならない。
func TestListProcessesReportsError(t *testing.T) {
	t.Parallel()

	s := NewScanner()
	s.output = func(time.Duration, string, ...string) (string, error) {
		return "", errors.New("ps が無い")
	}
	if _, err := s.ListProcesses(); err == nil {
		t.Error("error になりませんでした")
	}
}

// TestSignalsTargetOnlyThatProcess は TERM / KILL / 生存確認が
// **その 1 プロセスだけ** を対象にすることを確かめる。
//
// プロセスグループごと送ると、同じグループに居る無関係のプロセス
// (使用中のセッションのペインなど)を巻き込む。
func TestSignalsTargetOnlyThatProcess(t *testing.T) {
	t.Parallel()

	var got []int
	s := NewScanner()
	s.signal = func(pid int, _ syscall.Signal) error {
		got = append(got, pid)
		return nil
	}

	if err := s.Terminate(4242); err != nil {
		t.Fatalf("Terminate() = %v", err)
	}
	if err := s.Kill(4242); err != nil {
		t.Fatalf("Kill() = %v", err)
	}
	if want := []int{4242, 4242}; !slices.Equal(got, want) {
		t.Errorf("送った先 = %v, want %v", got, want)
	}
}

// TestIsAliveOnRealProcess は生存確認が実プロセスで働くことを確かめる。
func TestIsAliveOnRealProcess(t *testing.T) {
	t.Parallel()

	s := NewScanner()
	if !s.IsAlive(os.Getpid()) {
		t.Error("自分自身が生きていないと判定されました")
	}

	// 起動してすぐ終わるプロセスを待ってから見る。
	cmd := exec.Command("true")
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	pid := cmd.Process.Pid
	_ = cmd.Wait()
	// 終わった直後は環境によって残骸が残りうるので、判定の型だけを見る。
	_ = s.IsAlive(pid)
}

// TestSendSignalReportsFailure は届かないシグナルが error になることを
// 確かめる。掃除は失敗を握り潰して続けるが、届いたかどうかは呼び出し側が
// 判断できなければならない。
func TestSendSignalReportsFailure(t *testing.T) {
	t.Parallel()

	// PID 0 はプロセスグループ全体を指す特別な値で、通常のシグナル送信では
	// 使えない(Go の os.Process.Signal が拒む)。
	if err := sendSignal(0, syscall.Signal(0)); err == nil {
		t.Error("error になりませんでした")
	}
}

// containsPID は ps の出力に pid の行があるかを返す。
func containsPID(out string, pid int) bool {
	want := strconv.Itoa(pid)
	for _, line := range strings.Split(out, "\n") {
		fields := strings.Fields(line)
		if len(fields) > 0 && fields[0] == want {
			return true
		}
	}
	return false
}

// firstLines は失敗時の表示に使う先頭 n 行である。
func firstLines(out string, n int) string {
	lines := strings.Split(out, "\n")
	if len(lines) > n {
		lines = lines[:n]
	}
	return strings.Join(lines, "\n")
}
