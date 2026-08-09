package shell

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// recordedCall は run に渡された引数の記録。
type recordedCall struct {
	name string
	args []string
	env  []string
}

// newTestRunner は外部コマンドを実行しない Runner と呼び出しの記録を返す。
func newTestRunner(out string, err error) (*Runner, *[]recordedCall) {
	calls := &[]recordedCall{}
	r := &Runner{
		conductorHome: "/ch",
		env:           []string{"CONDUCTOR_HOME=/ch", "ZELLIJ_SESSION_NAME=s1"},
		run: func(env []string, name string, args ...string) (string, error) {
			*calls = append(*calls, recordedCall{name: name, args: args, env: env})
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
