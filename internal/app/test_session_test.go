package app_test

import (
	"bytes"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/k-kudo-hub/mdev-go/internal/app"
)

// fakeWorktreeLocator は worktree の探索の代役である。
type fakeWorktreeLocator struct {
	mainRoot string
	dirs     map[string]bool
	names    []string
	absErr   error
}

func (f fakeWorktreeLocator) MainRoot() string     { return f.mainRoot }
func (f fakeWorktreeLocator) IsDir(p string) bool  { return f.dirs[p] }
func (f fakeWorktreeLocator) List(string) []string { return f.names }
func (f fakeWorktreeLocator) Abs(p string) (string, error) {
	if f.absErr != nil {
		return "", f.absErr
	}
	return p, nil
}

// fakeBuilder はビルドの代役である。
type fakeBuilder struct {
	built [][2]string
	err   error
}

func (b *fakeBuilder) Build(worktree, out string) error {
	b.built = append(b.built, [2]string{worktree, out})
	return b.err
}

// fakeTerminal は端末の起動の代役である。
type fakeTerminal struct {
	title   string
	command string
	calls   int
	err     error
}

func (t *fakeTerminal) Launch(title, command string) error {
	t.title, t.command = title, command
	t.calls++
	return t.err
}

// testRunnerFixture は TestSessionRunner と観測用の fake をまとめたものである。
type testRunnerFixture struct {
	runner   *app.TestSessionRunner
	builder  *fakeBuilder
	terminal *fakeTerminal
	files    *fakeFileStore
	chooser  *fakeChooser
}

const testWorktree = "/main/.worktree/fix-bug"

// newTestRunnerFixture は fix-bug の worktree が在る状態の一式を返す。
func newTestRunnerFixture() *testRunnerFixture {
	f := &testRunnerFixture{
		builder:  &fakeBuilder{},
		terminal: &fakeTerminal{},
		files:    newFakeFileStore(),
		chooser:  &fakeChooser{},
	}
	f.runner = &app.TestSessionRunner{
		Locator: fakeWorktreeLocator{
			mainRoot: "/main",
			dirs: map[string]bool{
				testWorktree: true,
				filepath.Join(testWorktree, "cmd", "mdev"): true,
			},
			names: []string{"fix-bug", "other"},
		},
		Builder:  f.builder,
		Terminal: f.terminal,
		Chooser:  f.chooser,
		Assets:   testAssets,
		Files:    f.files,
	}
	return f
}

// TestRunTestBuildsAndLaunches は組んでから開くことを確かめる。
func TestRunTestBuildsAndLaunches(t *testing.T) {
	t.Parallel()

	f := newTestRunnerFixture()
	var out bytes.Buffer
	if err := f.runner.RunTest(&out, "fix-bug", false); err != nil {
		t.Fatalf("RunTest = %v", err)
	}

	want := [2]string{testWorktree, testWorktree + "/.mdev-test/bin/mdev"}
	if len(f.builder.built) != 1 || f.builder.built[0] != want {
		t.Errorf("ビルド = %v, want %v", f.builder.built, want)
	}
	if f.terminal.calls != 1 {
		t.Fatalf("端末の起動 = %d 回, want 1", f.terminal.calls)
	}
	if !strings.Contains(f.terminal.command, "CONDUCTOR_HOME="+"'"+testWorktree+"/.mdev-test'") {
		t.Errorf("隔離されていない: %s", f.terminal.command)
	}

	// レイアウトは組んだバイナリの絶対パスを指す。
	layout := f.files.files[testWorktree+"/.mdev-test/layouts/multi.kdl"]
	if !strings.Contains(layout, testWorktree+"/.mdev-test/bin/mdev") {
		t.Errorf("レイアウトが組んだバイナリを指していない:\n%s", layout)
	}
	if strings.Contains(layout, "CONDUCTOR_HOME") {
		t.Errorf("環境変数経由のままの呼び出しが残っている:\n%s", layout)
	}
	// 既定の設定も置く(置かないと単価表もエージェントも無いまま動く)。
	for _, name := range []string{"config.default.json", "config.json"} {
		if _, ok := f.files.files[testWorktree+"/.mdev-test/"+name]; !ok {
			t.Errorf("%s が置かれていない", name)
		}
	}
}

// TestRunTestKeepsExistingConfig は試している間に変えた設定を残すことを
// 確かめる。毎回戻ると確かめにくい。
func TestRunTestKeepsExistingConfig(t *testing.T) {
	t.Parallel()

	f := newTestRunnerFixture()
	const mine = `{"search_dirs":["~/try"]}` + "\n"
	f.files.files[testWorktree+"/.mdev-test/config.json"] = mine

	var out bytes.Buffer
	if err := f.runner.RunTest(&out, "fix-bug", false); err != nil {
		t.Fatalf("RunTest = %v", err)
	}
	if got := f.files.files[testWorktree+"/.mdev-test/config.json"]; got != mine {
		t.Errorf("設定を書き換えた: %q", got)
	}
}

// TestRunTestDryRun は起動せずに内容だけを出すことを確かめる。
func TestRunTestDryRun(t *testing.T) {
	t.Parallel()

	f := newTestRunnerFixture()
	var out bytes.Buffer
	if err := f.runner.RunTest(&out, "fix-bug", true); err != nil {
		t.Fatalf("RunTest = %v", err)
	}

	for _, want := range []string{"WORKTREE=", "CONDUCTOR_HOME=", "BINARY=", "SESSION=test-fix-bug", "CMD="} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("%q が無い:\n%s", want, out.String())
		}
	}
	if len(f.builder.built) != 0 || f.terminal.calls != 0 || len(f.files.writes) != 0 {
		t.Error("dry-run なのに何かした")
	}
}

// TestRunTestResolvesInput は指定の解決を確かめる。
func TestRunTestResolvesInput(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		input   string
		pick    string
		wantErr string
	}{
		{name: "ブランチ名", input: "fix-bug"},
		{name: "パスを直に", input: testWorktree},
		{name: "省くと選ばせる", input: "", pick: "fix-bug"},
		{name: "見つからない", input: "missing", wantErr: "worktree が見つかりません"},
		{name: "選ばなければ止まる", input: "", wantErr: "選ばれませんでした"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			f := newTestRunnerFixture()
			f.chooser.pick = tt.pick

			var out bytes.Buffer
			err := f.runner.RunTest(&out, tt.input, true)
			if tt.wantErr == "" {
				if err != nil {
					t.Fatalf("RunTest = %v", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("エラー = %v, want %q を含む", err, tt.wantErr)
			}
		})
	}
}

// TestRunTestRejectsForeignWorktree は mdev-go 以外の worktree を弾くことを
// 確かめる。
//
// そのままビルドへ進むと、失敗の理由が go の出力にしか出てこない。
func TestRunTestRejectsForeignWorktree(t *testing.T) {
	t.Parallel()

	f := newTestRunnerFixture()
	f.runner.Locator = fakeWorktreeLocator{
		mainRoot: "/main",
		dirs:     map[string]bool{"/other/repo": true},
	}
	var out bytes.Buffer
	err := f.runner.RunTest(&out, "/other/repo", true)
	if err == nil || !strings.Contains(err.Error(), "cmd/mdev") {
		t.Errorf("エラー = %v", err)
	}
}

// TestRunTestReportsBuildFailure はビルドの失敗を伝えることを確かめる。
// 端末は開かない(開いても中身が古い)。
func TestRunTestReportsBuildFailure(t *testing.T) {
	t.Parallel()

	f := newTestRunnerFixture()
	f.builder.err = errors.New("コンパイルエラー")

	var out bytes.Buffer
	err := f.runner.RunTest(&out, "fix-bug", false)
	if err == nil || !strings.Contains(err.Error(), "コンパイルエラー") {
		t.Errorf("エラー = %v", err)
	}
	if f.terminal.calls != 0 {
		t.Error("失敗したのに端末を開いた")
	}
}

// TestRunTestWithoutRepository はリポジトリの外での案内を確かめる。
func TestRunTestWithoutRepository(t *testing.T) {
	t.Parallel()

	f := newTestRunnerFixture()
	f.runner.Locator = fakeWorktreeLocator{}

	var out bytes.Buffer
	err := f.runner.RunTest(&out, "", true)
	if err == nil || !strings.Contains(err.Error(), "リポジトリ") {
		t.Errorf("エラー = %v", err)
	}
}
