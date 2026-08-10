package shell

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

// recordedCall は run に渡された引数の記録。
type recordedCall struct {
	name    string
	args    []string
	env     []string
	timeout time.Duration
}

// newTestRunner は外部コマンドを実行しない Runner と呼び出しの記録を返す。
func newTestRunner(out string, err error) (*Runner, *[]recordedCall) {
	calls := &[]recordedCall{}
	r := &Runner{
		conductorHome: "/ch",
		env:           []string{"CONDUCTOR_HOME=/ch", "ZELLIJ_SESSION_NAME=s1"},
		run: func(timeout time.Duration, env []string, name string, args ...string) (string, error) {
			*calls = append(*calls, recordedCall{name: name, args: args, env: env, timeout: timeout})
			return out, err
		},
	}
	return r, calls
}

func TestRunnerUploadLogStripsPrefix(t *testing.T) {
	t.Parallel()

	// 現行版の `${upload_out#upload-log: }` と同じく印を外して返す。
	r, calls := newTestRunner("upload-log: https://example.com/log/1\n", nil)

	out, err := r.UploadLog("alpha")
	if err != nil {
		t.Fatalf("UploadLog() = %v", err)
	}
	if out != "https://example.com/log/1" {
		t.Errorf("UploadLog() = %q, want URL のみ", out)
	}

	want := []string{"/ch/scripts/upload-log.sh", "alpha"}
	if !reflect.DeepEqual((*calls)[0].args, want) {
		t.Errorf("引数 = %v, want %v", (*calls)[0].args, want)
	}
	if (*calls)[0].name != "bash" {
		t.Errorf("コマンド = %q, want bash", (*calls)[0].name)
	}
}

func TestRunnerUploadLogWithoutPrefix(t *testing.T) {
	t.Parallel()

	// 印が付いていない出力はそのまま返す(TrimPrefix は一致しなければ無変換)。
	r, _ := newTestRunner("plain output\n", nil)
	out, err := r.UploadLog("alpha")
	if err != nil {
		t.Fatalf("UploadLog() = %v", err)
	}
	if out != "plain output" {
		t.Errorf("UploadLog() = %q", out)
	}
}

func TestRunnerUploadLogReturnsErrorOnFailure(t *testing.T) {
	t.Parallel()

	// 非 0 終了は削除を中止させるためのエラーとして返す。
	r, _ := newTestRunner("", errors.New("exit status 1"))
	out, err := r.UploadLog("alpha")
	if err == nil {
		t.Fatal("失敗したのにエラーが返っていない")
	}
	if out != "" {
		t.Errorf("失敗時の出力 = %q, want 空", out)
	}
}

func TestRunnerPassesEnvToChildren(t *testing.T) {
	t.Parallel()

	// スクリプトは CONDUCTOR_HOME と ZELLIJ_SESSION_NAME を見るため、
	// 子プロセスへの引き継ぎが欠かせない。
	r, calls := newTestRunner("", nil)
	r.RestoreSession()

	env := (*calls)[0].env
	for _, want := range []string{"CONDUCTOR_HOME=/ch", "ZELLIJ_SESSION_NAME=s1"} {
		if !slicesContains(env, want) {
			t.Errorf("環境変数に %q が無い: %v", want, env)
		}
	}
}

func TestRunnerScriptCalls(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		call func(*Runner)
		want []string
	}{
		{
			name: "restore-task は 3 つの引数を渡す",
			call: func(r *Runner) { r.RestoreTask("alpha", "s1", "2026-08-09T10:00:00+0900") },
			want: []string{"/ch/scripts/restore-task.sh", "alpha", "s1", "2026-08-09T10:00:00+0900"},
		},
		{
			name: "fetch-news は --force を付ける",
			call: func(r *Runner) { r.FetchNews() },
			want: []string{"/ch/scripts/fetch-news.sh", "--force"},
		},
		{
			name: "restore-session は引数なし",
			call: func(r *Runner) { r.RestoreSession() },
			want: []string{"/ch/scripts/restore-session.sh"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			r, calls := newTestRunner("", nil)
			tt.call(r)
			if !reflect.DeepEqual((*calls)[0].args, tt.want) {
				t.Errorf("引数 = %v, want %v", (*calls)[0].args, tt.want)
			}
		})
	}
}

func TestRunnerScreenDetectTickSourcesLibrary(t *testing.T) {
	t.Parallel()

	// screen-detect-lib.sh は関数を定義するだけなので、source してから呼ぶ。
	r, calls := newTestRunner("", nil)
	r.ScreenDetectTick("s1")

	args := (*calls)[0].args
	if args[0] != "-c" {
		t.Fatalf("bash -c で呼んでいない: %v", args)
	}
	if !strings.Contains(args[1], "screen_detect_tick") {
		t.Errorf("関数を呼んでいない: %q", args[1])
	}
	// パスもセッション名も位置パラメータで渡す。本文へ埋め込むと、値に含まれる
	// `$` やバッククォートを bash が展開してしまう。
	if strings.Contains(args[1], "/ch/scripts/screen-detect-lib.sh") {
		t.Errorf("パスを本文へ埋め込んでいる: %q", args[1])
	}
	want := []string{"_", "/ch/scripts/screen-detect-lib.sh", "s1"}
	if !reflect.DeepEqual(args[2:], want) {
		t.Errorf("位置パラメータ = %v, want %v", args[2:], want)
	}
}

func TestRunnerScreenDetectTickDoesNotExpandConductorHome(t *testing.T) {
	// 実際の bash で確かめる。os.Chdir を伴うため並列化しない。
	root := t.TempDir()
	t.Chdir(root)

	// `$(...)` とバッククォートを含む CONDUCTOR_HOME。fmt の %q は shell の
	// エスケープではないので、スクリプト本文へ埋め込むと bash がこれを
	// コマンド置換として実行し、source 先のパスも別物に化ける。
	home := filepath.Join(root, "h $(touch pwned) `touch pwned`")
	scripts := filepath.Join(home, "scripts")
	if err := os.MkdirAll(scripts, 0o755); err != nil {
		t.Fatalf("ディレクトリの作成に失敗: %v", err)
	}

	// 呼ばれたことと引数を記録するだけのライブラリを置く。
	called := filepath.Join(root, "called")
	lib := "screen_detect_tick() { printf '%s' \"$1\" > \"" + called + "\"; }\n"
	if err := os.WriteFile(filepath.Join(scripts, "screen-detect-lib.sh"), []byte(lib), 0o600); err != nil {
		t.Fatalf("ライブラリの配置に失敗: %v", err)
	}

	NewRunner(home).ScreenDetectTick("s1")

	if _, err := os.Stat(filepath.Join(root, "pwned")); err == nil {
		t.Error("CONDUCTOR_HOME に含まれる文字列がコマンドとして実行された")
	}
	got, err := os.ReadFile(called)
	if err != nil {
		t.Fatalf("screen_detect_tick が呼ばれていない: %v", err)
	}
	if string(got) != "s1" {
		t.Errorf("渡されたセッション名 = %q, want s1", got)
	}
}

// slicesContains は s に v が含まれるかを返す。
func slicesContains(s []string, v string) bool {
	for _, item := range s {
		if item == v {
			return true
		}
	}
	return false
}

func TestNewRunnerSetsConductorHome(t *testing.T) {
	t.Parallel()

	r := NewRunner("/custom/home")
	if !slicesContains(r.env, "CONDUCTOR_HOME=/custom/home") {
		t.Errorf("CONDUCTOR_HOME が渡されていない")
	}
	if r.script("x.sh") != "/custom/home/scripts/x.sh" {
		t.Errorf("script() = %q", r.script("x.sh"))
	}
}

// ---- 実行時間の上限 -------------------------------------------------------

func TestRunnerAppliesTimeoutPerScript(t *testing.T) {
	t.Parallel()

	// ポーリングと起動の経路にだけ上限を付ける。ここが返らないとポーリングが
	// 止まる(完了起点なので、着弾しないと次の合図が張られない)。
	// 利用者が起こす長時間処理は待つほうが安全なので上限を付けない。
	tests := []struct {
		name string
		call func(*Runner)
		want time.Duration
	}{
		{
			name: "スクリーン検出は 15 秒",
			call: func(r *Runner) { r.ScreenDetectTick("s1") },
			want: 15 * time.Second,
		},
		{
			name: "セッション復元は 60 秒",
			call: func(r *Runner) { r.RestoreSession() },
			want: 60 * time.Second,
		},
		{
			name: "アップロードは上限なし",
			call: func(r *Runner) { _, _ = r.UploadLog("alpha") },
			want: 0,
		},
		{
			name: "restore-task は上限なし",
			call: func(r *Runner) { r.RestoreTask("alpha", "s1", "2026-08-10T10:00:00+0900") },
			want: 0,
		},
		{
			name: "ニュース取得は上限なし",
			call: func(r *Runner) { r.FetchNews() },
			want: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			r, calls := newTestRunner("", nil)
			tt.call(r)
			if got := (*calls)[0].timeout; got != tt.want {
				t.Errorf("上限 = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestRunCommandCutsOffAtTimeout(t *testing.T) {
	t.Parallel()

	// 返らない子プロセスは上限で切られ、エラーとして戻る。切れないと
	// ポーリングのチェーンがそこで止まる。
	start := time.Now()
	out, err := runCommand(50*time.Millisecond, nil, "sleep", "30")
	elapsed := time.Since(start)

	if err == nil {
		t.Fatal("上限を超えたのにエラーが返っていない")
	}
	if out != "" {
		t.Errorf("出力 = %q, want 空", out)
	}
	if elapsed > 10*time.Second {
		t.Errorf("上限で切れていない: %v かかった", elapsed)
	}
}

func TestRunCommandWithoutTimeoutRuns(t *testing.T) {
	t.Parallel()

	// 上限なし(0)でも通常どおり実行できる。
	out, err := runCommand(noTimeout, nil, "echo", "ok")
	if err != nil {
		t.Fatalf("runCommand() = %v", err)
	}
	if strings.TrimSpace(out) != "ok" {
		t.Errorf("出力 = %q, want ok", out)
	}
}

// ---- 孫プロセスまで止まること ---------------------------------------------

// grandchildScript は孫プロセスを 1 つ spawn して自分も待ち続ける bash の本文。
//
// $1 に孫の PID を書き出すファイルのパスを取る。孫は SIGTERM を無視し、待ち方も
// 外部コマンド任せにしない(while ループ)ため、止めるにはプロセスグループ全体への
// SIGKILL しか手が無い。実環境で起きたのは「screen-detect-lib.sh が spawn した
// `zellij action` が上限で切られても生き残る」ことで、この形はその最小再現である。
//
// 孫の標準出力は /dev/null へ向ける。親の標準出力のパイプを孫が握る件は
// internal/infra/proc の TestCommandDoesNotWaitForGrandchildHoldingStdout が扱う。
const grandchildScript = `
trap "" TERM
bash -c 'trap "" TERM; echo $$ > "$1"; while :; do sleep 1; done' _ "$1" </dev/null >/dev/null 2>&1 &
while :; do sleep 1; done
`

func TestRunCommandKillsGrandchildren(t *testing.T) {
	t.Parallel()

	// 上限で切るとき、直接の子(bash)だけでなく、その子が spawn した孫まで
	// 止まらなければならない。生き残った孫は CPU を空転させ zellij サーバを
	// 劣化させる(タブ遷移レースの増幅ループ)。
	pidFile := filepath.Join(t.TempDir(), "grandchild.pid")
	if _, err := runCommand(time.Second, nil, "bash", "-c", grandchildScript, "_", pidFile); err == nil {
		t.Fatal("上限を超えたのにエラーが返っていない")
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
