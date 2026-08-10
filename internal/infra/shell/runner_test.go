package shell

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
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

// ---- 起動方法の使い分け ---------------------------------------------------

func TestCommandUsesProcessGroupOnlyWithTimeout(t *testing.T) {
	t.Parallel()

	// 上限つきの呼び出しは proc.Command を通す(プロセスグループごと切るため)。
	// 孫まで止まることの実証は internal/infra/proc のテストが持つ。
	// 上限なしの呼び出しは素の exec.Command のままにする。プロセスグループを
	// 分けると端末が閉じたときの SIGHUP が子へ連鎖しなくなり、上限で自分から
	// 片付く保証も無いまま残り続けるためである。
	tests := []struct {
		name       string
		timeout    time.Duration
		wantPgroup bool
	}{
		{name: "上限つきはプロセスグループを分ける", timeout: time.Second, wantPgroup: true},
		{name: "上限なしは分けない", timeout: noTimeout, wantPgroup: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			cmd, cancel := command(tt.timeout, "true")
			defer cancel()

			gotPgroup := cmd.SysProcAttr != nil && cmd.SysProcAttr.Setpgid
			if gotPgroup != tt.wantPgroup {
				t.Errorf("Setpgid = %v, want %v", gotPgroup, tt.wantPgroup)
			}
			if got := cmd.Cancel != nil; got != tt.wantPgroup {
				t.Errorf("Cancel の差し替え = %v, want %v", got, tt.wantPgroup)
			}
		})
	}
}
