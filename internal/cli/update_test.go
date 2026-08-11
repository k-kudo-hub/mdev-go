package cli

import (
	"bytes"
	"errors"
	"io"
	"os"
	"strings"
	"testing"
)

// fakeUpdateService は `mdev update` のユースケースの代役である。
type fakeUpdateService struct {
	calls  int
	output string
	err    error
}

func (s *fakeUpdateService) Update(out io.Writer) error {
	s.calls++
	if s.output != "" {
		_, _ = io.WriteString(out, s.output)
	}
	return s.err
}

// fakeUpdateCheckService は起動時の更新確認の代役である。
type fakeUpdateCheckService struct {
	forces []bool
	notice string
}

func (s *fakeUpdateCheckService) Check(force bool) string {
	s.forces = append(s.forces, force)
	return s.notice
}

// runCLIWithOut は runCLI と同じだが、標準出力の内容も返す。
func runCLIWithOut(t *testing.T, deps Deps, args ...string) (int, string, string) {
	t.Helper()

	cmd := NewRootCommand(deps)
	var stdout, stderr bytes.Buffer
	cmd.SetIn(strings.NewReader(""))
	cmd.SetOut(&stdout)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs(args)

	code := execute(cmd, &stderr)
	return code, stdout.String(), stderr.String()
}

// TestUpdateCommand は `mdev update` がユースケースへ委ね、出力を素通しする
// ことを確かめる。
func TestUpdateCommand(t *testing.T) {
	t.Parallel()

	update := &fakeUpdateService{output: "✅ v0.2.0 に更新しました。\n"}
	code, stdout, stderr := runCLIWithOut(t, Deps{Update: update}, "update")

	if code != exitOK {
		t.Errorf("終了コード = %d, want %d (stderr=%q)", code, exitOK, stderr)
	}
	if update.calls != 1 {
		t.Errorf("呼び出し = %d 回, want 1", update.calls)
	}
	if !strings.Contains(stdout, "更新しました") {
		t.Errorf("標準出力 = %q", stdout)
	}
}

// TestUpdateCommandFailure は更新の失敗が非 0 終了になることを確かめる。
// 利用者が明示的に叩くコマンドなので、失敗は必ず伝える。
func TestUpdateCommandFailure(t *testing.T) {
	t.Parallel()

	update := &fakeUpdateService{err: errors.New("ダウンロードに失敗しました")}
	code, _, stderr := runCLIWithOut(t, Deps{Update: update}, "update")

	if code != exitError {
		t.Errorf("終了コード = %d, want %d", code, exitError)
	}
	if !strings.Contains(stderr, "ダウンロードに失敗しました") {
		t.Errorf("標準エラー = %q", stderr)
	}
}

// TestCheckUpdateCommand は案内の有無と --force の受け渡しを確かめる。
func TestCheckUpdateCommand(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		args      []string
		notice    string
		wantOut   string
		wantForce bool
	}{
		{
			name:      "案内がある",
			args:      []string{"check-update"},
			notice:    "\n  📦 新しいバージョン v0.2.0 があります。\n",
			wantOut:   "\n  📦 新しいバージョン v0.2.0 があります。\n",
			wantForce: false,
		},
		{
			// 出すものが無ければ 1 文字も出さない。セッションの起動前に
			// 走るので、余計な出力は毎回の邪魔になる。
			name:      "案内が無ければ何も出さない",
			args:      []string{"check-update"},
			notice:    "",
			wantOut:   "",
			wantForce: false,
		},
		{
			name:      "--force を渡す",
			args:      []string{"check-update", "--force"},
			notice:    "",
			wantOut:   "",
			wantForce: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			check := &fakeUpdateCheckService{notice: tt.notice}
			code, stdout, stderr := runCLIWithOut(t, Deps{UpdateCheck: check}, tt.args...)

			if code != exitOK {
				t.Errorf("終了コード = %d, want %d (stderr=%q)", code, exitOK, stderr)
			}
			if stdout != tt.wantOut {
				t.Errorf("標準出力 = %q, want %q", stdout, tt.wantOut)
			}
			if want := []bool{tt.wantForce}; len(check.forces) != 1 || check.forces[0] != want[0] {
				t.Errorf("force = %v, want %v", check.forces, want)
			}
		})
	}
}

// TestIsTerminal は端末でない出力先で待ちが入らないことを固定する。
func TestIsTerminal(t *testing.T) {
	t.Parallel()

	if isTerminal(&bytes.Buffer{}) {
		t.Error("bytes.Buffer が端末と判定されました")
	}
	// 通常ファイル(テスト実行時のリダイレクト先)も端末ではない。
	file, err := os.CreateTemp(t.TempDir(), "out")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = file.Close() }()
	if isTerminal(file) {
		t.Error("通常ファイルが端末と判定されました")
	}
}
