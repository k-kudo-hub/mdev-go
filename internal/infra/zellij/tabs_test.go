package zellij

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestTabControllerListTabs(t *testing.T) {
	t.Parallel()

	var got []string
	c := &TabController{
		output: func(name string, args ...string) string {
			got = append([]string{name}, args...)
			return "ID POS NAME\n1 x alpha\n"
		},
	}

	if out := c.ListTabs(); out != "ID POS NAME\n1 x alpha\n" {
		t.Errorf("ListTabs() = %q", out)
	}
	want := []string{"zellij", "action", "list-tabs"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("実行コマンド = %v, want %v", got, want)
	}
}

func TestTabControllerListTabsReturnsEmptyOnFailure(t *testing.T) {
	t.Parallel()

	// zellij の外で動いた場合など。タブが 1 つも無い扱いになる。
	c := &TabController{output: func(string, ...string) string { return "" }}
	if out := c.ListTabs(); out != "" {
		t.Errorf("ListTabs() = %q, want 空", out)
	}
}

func TestTabControllerCloseTabByID(t *testing.T) {
	t.Parallel()

	var got []string
	c := &TabController{
		run: func(name string, args ...string) error {
			got = append([]string{name}, args...)
			return nil
		},
	}
	c.CloseTabByID("7")

	want := []string{"zellij", "action", "close-tab-by-id", "7"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("実行コマンド = %v, want %v", got, want)
	}
}

func TestTabControllerCloseTabByIDIgnoresFailure(t *testing.T) {
	t.Parallel()

	// 既に閉じられている場合など。削除フローとしては進んでよい。
	c := &TabController{run: func(string, ...string) error { return errors.New("失敗") }}
	c.CloseTabByID("7")
}

func TestNewTabControllerIsWired(t *testing.T) {
	t.Parallel()

	c := NewTabController()
	if c.output == nil || c.run == nil {
		t.Error("実コマンドの実行関数が設定されていない")
	}
}

func TestCommandOutputCutsOffAtTimeout(t *testing.T) {
	t.Parallel()

	// 返らない zellij は上限で切られ、空文字(= タブが 1 つも無い扱い)になる。
	// 切れないとダッシュボードのポーリングがそこで止まる。
	start := time.Now()
	out := commandOutput(50*time.Millisecond, "sleep", "30")
	elapsed := time.Since(start)

	if out != "" {
		t.Errorf("出力 = %q, want 空", out)
	}
	if elapsed > 10*time.Second {
		t.Errorf("上限で切れていない: %v かかった", elapsed)
	}
}

func TestRunCommandCutsOffAtTimeout(t *testing.T) {
	t.Parallel()

	start := time.Now()
	err := runCommand(50*time.Millisecond, "sleep", "30")
	elapsed := time.Since(start)

	if err == nil {
		t.Fatal("上限を超えたのにエラーが返っていない")
	}
	if elapsed > 10*time.Second {
		t.Errorf("上限で切れていない: %v かかった", elapsed)
	}
}

func TestCommandTimeoutIsTenSeconds(t *testing.T) {
	t.Parallel()

	// ポーリングが毎周期呼ぶ list-tabs の上限。値そのものを固定しておく。
	if commandTimeout != 10*time.Second {
		t.Errorf("commandTimeout = %v, want 10s", commandTimeout)
	}
}

// ---- 孫プロセスまで止まること ---------------------------------------------

// grandchildScript は孫プロセスを 1 つ spawn して自分も待ち続ける bash の本文。
//
// $1 に孫の PID を書き出すファイルのパスを取る。孫は SIGTERM を無視し、待ち方も
// 外部コマンド任せにしない(while ループ)ため、止めるにはプロセスグループ全体への
// SIGKILL しか手が無い。zellij CLI 自体は孫を作らないが、上限で切る経路は
// すべて同じ止め方でなければならないため、ここでも実プロセスで確かめる。
const grandchildScript = `
trap "" TERM
bash -c 'trap "" TERM; echo $$ > "$1"; while :; do sleep 1; done' _ "$1" </dev/null >/dev/null 2>&1 &
while :; do sleep 1; done
`

func TestRunCommandKillsGrandchildren(t *testing.T) {
	t.Parallel()

	pidFile := filepath.Join(t.TempDir(), "grandchild.pid")
	if err := runCommand(time.Second, "bash", "-c", grandchildScript, "_", pidFile); err == nil {
		t.Fatal("上限を超えたのにエラーが返っていない")
	}

	pid := readPID(t, pidFile)
	if err := waitProcessGone(pid); err != nil {
		t.Errorf("孫プロセス(pid %d)が生き残っている: %v", pid, err)
	}
}

func TestCommandOutputKillsGrandchildren(t *testing.T) {
	t.Parallel()

	pidFile := filepath.Join(t.TempDir(), "grandchild.pid")
	if out := commandOutput(time.Second, "bash", "-c", grandchildScript, "_", pidFile); out != "" {
		t.Errorf("出力 = %q, want 空", out)
	}

	pid := readPID(t, pidFile)
	if err := waitProcessGone(pid); err != nil {
		t.Errorf("孫プロセス(pid %d)が生き残っている: %v", pid, err)
	}
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
