package shell

import (
	"errors"
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
	r.FetchNews()

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
			name: "upload-log はタブ名を渡す",
			call: func(r *Runner) { _, _ = r.UploadLog("alpha") },
			want: []string{"/ch/scripts/upload-log.sh", "alpha"},
		},
		{
			name: "fetch-news は --force を付ける",
			call: func(r *Runner) { r.FetchNews() },
			want: []string{"/ch/scripts/fetch-news.sh", "--force"},
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

	// ここに残っているのは利用者が起こす長時間処理だけで、いずれも途中で
	// 切ると作業ログを失う・取得が中途半端に終わる。ポーリングのチェーンも
	// 止めない経路なので上限は付けない。
	tests := []struct {
		name string
		call func(*Runner)
		want time.Duration
	}{
		{
			name: "アップロードは上限なし",
			call: func(r *Runner) { _, _ = r.UploadLog("alpha") },
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
