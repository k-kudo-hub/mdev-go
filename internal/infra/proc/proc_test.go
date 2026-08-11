package proc

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

// grandchildScript は孫プロセスを 1 つ spawn して自分も待ち続ける bash の本文。
//
// $1 に孫の PID を書き出すファイルのパスを取る。孫は SIGTERM を無視し、待ち方も
// 外部コマンド任せにしない(while ループ)ため、止めるにはプロセスグループ全体への
// SIGKILL しか手が無い。実環境で起きたのは「bash スクリプトが spawn した
// `zellij action` が上限で切られても生き残る」ことで、この形はその最小再現である。
//
// 孫の標準出力は /dev/null へ向ける。親のパイプを孫が握る件は
// TestCommandDoesNotWaitForGrandchildHoldingStdout が別に扱う。
const grandchildScript = `
trap "" TERM
bash -c 'trap "" TERM; echo $$ > "$1"; while :; do sleep 1; done' _ "$1" </dev/null >/dev/null 2>&1 &
while :; do sleep 1; done
`

// stdoutHolderScript は親の標準出力のパイプを握ったまま残る孫を spawn する。
//
// 直接の子だけを切ると、Go はこのパイプが閉じるまで Wait から返れない
// (Output は標準出力の写し取りが終わるのを待つ)。孫ごと止まればパイプは
// 直ちに閉じる。sleep が有限なのは、万一止まらなくてもテストが永久に
// ぶら下がらないようにするためである。
const stdoutHolderScript = `
bash -c 'sleep 30' &
sleep 30
`

func TestCommandKillsGrandchildren(t *testing.T) {
	t.Parallel()

	pidFile := filepath.Join(t.TempDir(), "grandchild.pid")
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	if _, err := Command(ctx, "bash", "-c", grandchildScript, "_", pidFile).Output(); err == nil {
		t.Fatal("上限を超えたのにエラーが返っていない")
	}

	pid := readPID(t, pidFile)
	if err := waitProcessGone(pid); err != nil {
		t.Errorf("孫プロセス(pid %d)が生き残っている: %v", pid, err)
	}
}

func TestCommandDoesNotWaitForGrandchildHoldingStdout(t *testing.T) {
	t.Parallel()

	// 孫が標準出力のパイプを握ったままだと、直接の子を切っても Wait は返らない。
	// 返らない呼び出しはポーリングのチェーンをそこで止めてしまう。
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()

	start := time.Now()
	_, err := Command(ctx, "bash", "-c", stdoutHolderScript).Output()
	elapsed := time.Since(start)

	if err == nil {
		t.Fatal("上限を超えたのにエラーが返っていない")
	}
	if elapsed > 10*time.Second {
		t.Errorf("孫がパイプを握ったまま待たされた: %v かかった", elapsed)
	}
}

func TestCommandRunsNormally(t *testing.T) {
	t.Parallel()

	// 打ち切られない context でも通常どおり実行できる(Go の os/exec は
	// ctx.Done() が nil のとき Cancel も WaitDelay も参照しない)。
	// なお上限を持たない呼び出しはこのパッケージを使わない決まりである。
	out, err := Command(context.Background(), "echo", "ok").Output()
	if err != nil {
		t.Fatalf("Command() = %v", err)
	}
	if strings.TrimSpace(string(out)) != "ok" {
		t.Errorf("出力 = %q, want ok", out)
	}
}

func TestCommandReturnsExitError(t *testing.T) {
	t.Parallel()

	// 非 0 終了は従来どおりエラーとして返る(呼び出し側はこれで処理を止める)。
	if err := Command(context.Background(), "false").Run(); err == nil {
		t.Error("非 0 終了なのにエラーが返っていない")
	}
}

func TestCommandSetsProcessGroupAndWaitDelay(t *testing.T) {
	t.Parallel()

	cmd := Command(context.Background(), "true")
	if cmd.SysProcAttr == nil || !cmd.SysProcAttr.Setpgid {
		t.Error("Setpgid が立っていない: 孫が同じプロセスグループに入らない")
	}
	if cmd.Cancel == nil {
		t.Error("Cancel が差し替えられていない: 既定の打ち切りは直接の子だけを切る")
	}
	if cmd.WaitDelay != waitDelay {
		t.Errorf("WaitDelay = %v, want %v", cmd.WaitDelay, waitDelay)
	}
}

func TestKillGroupWithoutProcess(t *testing.T) {
	t.Parallel()

	// 起動前に呼ばれた場合。打ち切りではなく「既に終わっている」として扱わせる。
	if err := killGroup(nil); !errors.Is(err, os.ErrProcessDone) {
		t.Errorf("killGroup(nil) = %v, want os.ErrProcessDone", err)
	}
}

func TestKillGroupOnReapedProcess(t *testing.T) {
	t.Parallel()

	// reap 済みの相手には撃たない。os/exec は Wait4 の前に「済み」の印を立てる
	// ので、印が立った後の PID はいつ使い回されてもおかしくない。生の
	// syscall.Kill はこの印を見ないため、os.Process.Signal を通して確かめる。
	cmd := exec.Command("true")
	if err := cmd.Run(); err != nil {
		t.Fatalf("下準備のコマンドが失敗: %v", err)
	}
	if err := killGroup(cmd.Process); !errors.Is(err, os.ErrProcessDone) {
		t.Errorf("killGroup(reap 済み) = %v, want os.ErrProcessDone", err)
	}
}

func TestKillGroupOnMissingGroup(t *testing.T) {
	t.Parallel()

	// 起動を経ていない os.Process が指す先が居ない場合。ESRCH は
	// os.ErrProcessDone に変換される(しないと Go は「打ち切りに失敗した」と
	// みなして別のエラーを被せる)。
	pid := freePGID(t)
	if err := killGroup(&os.Process{Pid: pid}); !errors.Is(err, os.ErrProcessDone) {
		t.Errorf("killGroup(存在しないグループ) = %v, want os.ErrProcessDone", err)
	}
}

// freePGID は存在しないプロセスグループの ID を返す。
func freePGID(t *testing.T) int {
	t.Helper()

	for pid := 90000; pid < 99000; pid++ {
		if err := syscall.Kill(-pid, 0); errors.Is(err, syscall.ESRCH) {
			return pid
		}
	}
	t.Skip("空いているプロセスグループ ID が見つからない")
	return 0
}

// readPID は pidFile に書かれた PID を読む。テストが失敗して孫が生き残った
// 場合に備え、後始末で必ず SIGKILL を送る。
func readPID(t *testing.T, pidFile string) int {
	t.Helper()

	b, err := os.ReadFile(pidFile)
	if err != nil {
		t.Fatalf("孫の PID が書かれていない: %v", err)
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(b)))
	if err != nil {
		t.Fatalf("PID の解釈に失敗: %v", err)
	}
	t.Cleanup(func() { _ = syscall.Kill(pid, syscall.SIGKILL) })
	return pid
}

// waitProcessGone は pid のプロセスが消えるまで待つ。消えなければエラーを返す。
// シグナル 0 の kill は生存確認で、シグナルは送らない(`kill -0` と同じ)。
func waitProcessGone(pid int) error {
	deadline := time.Now().Add(5 * time.Second)
	for {
		if err := syscall.Kill(pid, 0); err != nil {
			return nil
		}
		if time.Now().After(deadline) {
			return errors.New("5 秒待っても消えなかった")
		}
		time.Sleep(20 * time.Millisecond)
	}
}
