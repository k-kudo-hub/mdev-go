package git

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// TestLatestTagAgainstRealRepo は実際のリポジトリからタグを引けることを
// 確かめる。test.sh「52. check-update.sh」が用意するのと同じ形の
// ベアリポジトリを使う。
func TestLatestTagAgainstRealRepo(t *testing.T) {
	t.Setenv("GIT_CONFIG_GLOBAL", os.DevNull)
	t.Setenv("GIT_CONFIG_SYSTEM", os.DevNull)
	// `git init` の既定ブランチ名は git のバージョンや配布物で変わる。
	// 手順を環境に依存させないよう明示する(logrepo_test.go の gitEnv と同じ理由)。
	t.Setenv("GIT_CONFIG_COUNT", "1")
	t.Setenv("GIT_CONFIG_KEY_0", "init.defaultBranch")
	t.Setenv("GIT_CONFIG_VALUE_0", "main")

	root := t.TempDir()
	remote := filepath.Join(root, "upd.git")
	run := func(dir string, args ...string) {
		t.Helper()
		full := args
		if dir != "" {
			full = append([]string{"-C", dir}, args...)
		}
		if out, err := execGit(full); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	run("", "init", "--bare", "--quiet", remote)
	run(remote, "symbolic-ref", "HEAD", "refs/heads/main")
	// 空の remote を clone してから育てるのではなく、独立したリポジトリを
	// 作って push する(手順が remote の HEAD に依存しないようにするため)。
	seed := filepath.Join(root, "seed")
	run("", "init", "--quiet", seed)
	run(seed, "symbolic-ref", "HEAD", "refs/heads/main")
	if err := os.WriteFile(filepath.Join(seed, "f"), []byte("x\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	run(seed, "add", ".")
	run(seed, "-c", "user.email=a@b", "-c", "user.name=a", "commit", "--quiet", "-m", "init")
	run(seed, "tag", "v0.1.0")
	run(seed, "tag", "v0.2.0")
	run(seed, "tag", "not-semver")
	run(seed, "remote", "add", "origin", remote)
	run(seed, "push", "--quiet", "origin", "main", "--tags")

	got, ok := NewRemoteTags().LatestTag(remote)
	if !ok {
		t.Fatal("タグを引けませんでした")
	}
	if got != "v0.2.0" {
		t.Errorf("LatestTag = %q, want v0.2.0", got)
	}
}

// execGit はテストの準備で git を実行する。
func execGit(args []string) (string, error) {
	return runGitWithEnv(append(os.Environ(), "GIT_TERMINAL_PROMPT=0"), args...)
}

// TestLatestTagFailures は引けない場合に ok=false を返すことを確かめる。
// 更新確認はセッションの起動を止めてはならないので、どの失敗も同じ扱いになる。
func TestLatestTagFailures(t *testing.T) {
	tests := []struct {
		name string
		url  string
		run  func(env []string, args ...string) (string, error)
	}{
		{
			name: "URL が空",
			url:  "",
			run:  func([]string, ...string) (string, error) { return "", nil },
		},
		{
			name: "リモートへ到達できない",
			url:  "https://example.invalid/o/r.git",
			run:  func([]string, ...string) (string, error) { return "", errors.New("接続できない") },
		},
		{
			name: "semver タグが無い",
			url:  "https://example.invalid/o/r.git",
			run:  func([]string, ...string) (string, error) { return "a\trefs/heads/main\n", nil },
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tags := &RemoteTags{run: tt.run}
			if got, ok := tags.LatestTag(tt.url); ok {
				t.Errorf("LatestTag = %q, ok = true, want ok = false", got)
			}
		})
	}
}

// TestLatestTagBoundsTheWait は待ち時間を抑える設定が渡ることを確かめる。
//
// これはセッションを開く前に走る処理で、到達できないリモートで起動が
// 固まってはならない。HTTPS と SSH で抑え方が違うため、両方を確認する。
func TestLatestTagBoundsTheWait(t *testing.T) {
	var gotArgs []string
	var gotEnv []string
	tags := &RemoteTags{run: func(env []string, args ...string) (string, error) {
		gotArgs, gotEnv = args, env
		return "a\trefs/tags/v1.0.0\n", nil
	}}

	if _, ok := tags.LatestTag("https://example.com/o/r.git"); !ok {
		t.Fatal("タグを引けませんでした")
	}

	wantArgs := []string{
		"-c", "http.lowSpeedLimit=1000",
		"-c", "http.lowSpeedTime=5",
		"ls-remote", "--tags", "https://example.com/o/r.git",
	}
	if !slices.Equal(gotArgs, wantArgs) {
		t.Errorf("引数 = %v, want %v", gotArgs, wantArgs)
	}
	for _, want := range []string{
		"GIT_TERMINAL_PROMPT=0",
		"GIT_SSH_COMMAND=ssh -o ConnectTimeout=5 -o BatchMode=yes",
	} {
		if !slices.Contains(gotEnv, want) {
			t.Errorf("環境変数に %q がありません", want)
		}
	}
}

// TestRunGitWithEnvFails は git が失敗したときに error になることを確かめる。
func TestRunGitWithEnvFails(t *testing.T) {
	out, err := runGitWithEnv(os.Environ(), "ls-remote", "--tags", filepath.Join(t.TempDir(), "none.git"))
	if err == nil {
		t.Errorf("error になりませんでした: %q", out)
	}
	if strings.TrimSpace(out) != "" {
		t.Errorf("失敗時の出力 = %q, want 空", out)
	}
}
