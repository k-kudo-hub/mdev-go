package git_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	infragit "github.com/k-kudo-hub/mdev-go/internal/infra/git"
)

// gitEnv は実環境の git 設定に左右されないようにする。
//
// 利用者の ~/.gitconfig に commit.gpgsign や core.hooksPath が入っていると
// テストの commit が失敗しうるため、グローバルとシステムの設定を無効化する。
func gitEnv(t *testing.T) {
	t.Helper()
	t.Setenv("GIT_CONFIG_GLOBAL", os.DevNull)
	t.Setenv("GIT_CONFIG_SYSTEM", os.DevNull)
	t.Setenv("GIT_TERMINAL_PROMPT", "0")
}

// runGit はテストの準備で git を実行する。
func runGit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	full := args
	if dir != "" {
		full = append([]string{"-C", dir}, args...)
	}
	cmd := exec.Command("git", full...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v が失敗しました: %v\n%s", args, err, out)
	}
	return string(out)
}

// newBareRepo は空のベアリポジトリを作ってそのパスを返す。
func newBareRepo(t *testing.T, dir string) string {
	t.Helper()
	runGit(t, "", "init", "--bare", "--quiet", dir)
	return dir
}

// seedRepo は main ブランチに 1 コミットあるベアリポジトリを作る。
func seedRepo(t *testing.T, root, name string) string {
	t.Helper()
	remote := newBareRepo(t, filepath.Join(root, name+".git"))
	seed := filepath.Join(root, name+"-seed")
	runGit(t, "", "clone", "--quiet", remote, seed)
	runGit(t, seed, "checkout", "--quiet", "-b", "main")
	if err := os.WriteFile(filepath.Join(seed, "README.md"), []byte("# logs\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	runGit(t, seed, "add", ".")
	runGit(t, seed, "-c", "user.email=s@s", "-c", "user.name=s", "commit", "--quiet", "-m", "init")
	runGit(t, seed, "push", "--quiet", "origin", "main")
	return remote
}

// TestLogRepositoryPushBootstrapsEmptyRepo は test.sh「46. A」に対応する。
// ブランチが 1 本も無い作りたてのリポジトリでも push できなければならない。
func TestLogRepositoryPushBootstrapsEmptyRepo(t *testing.T) {
	gitEnv(t)
	root := t.TempDir()
	remote := newBareRepo(t, filepath.Join(root, "empty.git"))
	home := filepath.Join(root, "conductor")
	repo := infragit.NewLogRepository(home)

	ref, err := repo.Push(remote, "main", "work-log/2026/07/04/120000_boot.md", "hello world")
	if err != nil {
		t.Fatalf("Push が失敗しました: %v", err)
	}
	if !strings.HasPrefix(ref, "work-log/2026/07/04/120000_boot.md @ ") {
		t.Errorf("参照文字列 = %q, want '<相対パス> @ <sha>' 形式", ref)
	}

	verify := filepath.Join(root, "verify")
	runGit(t, "", "clone", "--quiet", remote, verify)
	got, err := os.ReadFile(filepath.Join(verify, "work-log/2026/07/04/120000_boot.md"))
	if err != nil {
		t.Fatalf("push したログが remote にありません: %v", err)
	}
	// 現行版の `printf '%s\n'` と同じく末尾に改行が 1 つ付く。
	if string(got) != "hello world\n" {
		t.Errorf("ログの内容 = %q, want %q", got, "hello world\n")
	}
}

// TestLogRepositoryPushIsIdempotent は test.sh「46. B」に対応する。
// 同じ内容の再アップロードを「commit するものが無い」失敗にすると、
// アップロードのやり直しのたびに dd が止まってしまう。
func TestLogRepositoryPushIsIdempotent(t *testing.T) {
	gitEnv(t)
	root := t.TempDir()
	remote := newBareRepo(t, filepath.Join(root, "empty.git"))
	repo := infragit.NewLogRepository(filepath.Join(root, "conductor"))

	const relPath = "work-log/2026/07/04/120000_boot.md"
	if _, err := repo.Push(remote, "main", relPath, "hello world"); err != nil {
		t.Fatalf("1 回目の Push が失敗しました: %v", err)
	}
	if _, err := repo.Push(remote, "main", relPath, "hello world"); err != nil {
		t.Fatalf("同じ内容の再 Push が失敗しました: %v", err)
	}
}

// TestLogRepositoryPushCreatesBranch は test.sh「46. C/D」に対応する。
// 同じキャッシュを使い回して別の新しいブランチへ push できることが、
// 古い FETCH_HEAD を土台にしていないことの証拠になる。
func TestLogRepositoryPushCreatesBranch(t *testing.T) {
	gitEnv(t)
	root := t.TempDir()
	remote := seedRepo(t, root, "pop")
	repo := infragit.NewLogRepository(filepath.Join(root, "conductor"))

	if _, err := repo.Push(remote, "logs-2026", "work-log/x.md", "content x"); err != nil {
		t.Fatalf("新しいブランチへの Push が失敗しました: %v", err)
	}
	if _, err := repo.Push(remote, "logs-2027", "work-log/y.md", "content y"); err != nil {
		t.Fatalf("キャッシュ再利用でのブランチ切り替えが失敗しました: %v", err)
	}

	verify := filepath.Join(root, "verify")
	runGit(t, "", "clone", "--quiet", "--branch", "logs-2027", remote, verify)
	if _, err := os.Stat(filepath.Join(verify, "work-log/y.md")); err != nil {
		t.Errorf("切り替え後のブランチにログがありません: %v", err)
	}
	// 既存の main を土台にしているので README も残っている。
	if _, err := os.Stat(filepath.Join(verify, "README.md")); err != nil {
		t.Errorf("既存ブランチの内容が失われています: %v", err)
	}
}

// TestLogRepositoryPushUsesExistingBranch は既存ブランチへの追記を確かめる。
func TestLogRepositoryPushUsesExistingBranch(t *testing.T) {
	gitEnv(t)
	root := t.TempDir()
	remote := seedRepo(t, root, "pop")
	repo := infragit.NewLogRepository(filepath.Join(root, "conductor"))

	if _, err := repo.Push(remote, "main", "work-log/a.md", "a"); err != nil {
		t.Fatalf("Push が失敗しました: %v", err)
	}
	verify := filepath.Join(root, "verify")
	runGit(t, "", "clone", "--quiet", remote, verify)
	for _, name := range []string{"README.md", "work-log/a.md"} {
		if _, err := os.Stat(filepath.Join(verify, name)); err != nil {
			t.Errorf("%s がありません: %v", name, err)
		}
	}
	// コミットの作者は -c で渡した固定値になる(利用者の設定を汚さない)。
	got := strings.TrimSpace(runGit(t, verify, "log", "-1", "--format=%an <%ae>%n%s"))
	want := "claude-conductor <conductor@local>\nchore: add work log work-log/a.md"
	if got != want {
		t.Errorf("コミット情報 =\n%s\nwant\n%s", got, want)
	}
}

// TestLogRepositoryPushRebuildsBrokenCache は .git を失ったキャッシュが
// 作り直されることを確かめる(中断された clone の残骸からの回復)。
func TestLogRepositoryPushRebuildsBrokenCache(t *testing.T) {
	gitEnv(t)
	root := t.TempDir()
	remote := seedRepo(t, root, "pop")
	home := filepath.Join(root, "conductor")
	cache := filepath.Join(home, "upload-cache", infragit.RepoSlug(remote))
	if err := os.MkdirAll(cache, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(cache, "leftover"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}

	if _, err := infragit.NewLogRepository(home).Push(remote, "main", "work-log/a.md", "a"); err != nil {
		t.Fatalf("壊れたキャッシュからの Push が失敗しました: %v", err)
	}
	if _, err := os.Stat(filepath.Join(cache, "leftover")); !os.IsNotExist(err) {
		t.Errorf("キャッシュが作り直されていません(残骸が残っています)")
	}
}

// TestLogRepositoryPushFailsOnUnreachableRepo は到達できないリポジトリで
// error になることを確かめる。ここで成功を返すと作業ログを失ったまま
// タブが消えてしまう。
func TestLogRepositoryPushFailsOnUnreachableRepo(t *testing.T) {
	gitEnv(t)
	root := t.TempDir()
	repo := infragit.NewLogRepository(filepath.Join(root, "conductor"))
	if _, err := repo.Push(filepath.Join(root, "does-not-exist.git"), "main", "a.md", "a"); err == nil {
		t.Error("到達できないリポジトリで error になりませんでした")
	}
}

// TestResolveRepoURL は設定値の解釈を固定する。
func TestResolveRepoURL(t *testing.T) {
	tests := []struct{ repo, want string }{
		{"owner/name", "git@github.com:owner/name.git"},
		{"https://github.com/owner/name.git", "https://github.com/owner/name.git"},
		{"ssh://git@github.com/owner/name.git", "ssh://git@github.com/owner/name.git"},
		{"git@github.com:owner/name.git", "git@github.com:owner/name.git"},
		{"/var/log-repo.git", "/var/log-repo.git"},
		{"./log-repo.git", "./log-repo.git"},
		{"../log-repo.git", "../log-repo.git"},
	}
	for _, tt := range tests {
		if got := infragit.ResolveRepoURL(tt.repo); got != tt.want {
			t.Errorf("ResolveRepoURL(%q) = %q, want %q", tt.repo, got, tt.want)
		}
	}
}

// TestRepoSlug はキャッシュのディレクトリ名の畳み方を固定する。
func TestRepoSlug(t *testing.T) {
	tests := []struct{ repo, want string }{
		{"owner/name", "owner_name"},
		{"git@github.com:owner/name.git", "git_github.com_owner_name.git"},
		{"https://github.com/owner/name.git", "https_github.com_owner_name.git"},
		{"/tmp/remote-log.git", "_tmp_remote-log.git"},
	}
	for _, tt := range tests {
		if got := infragit.RepoSlug(tt.repo); got != tt.want {
			t.Errorf("RepoSlug(%q) = %q, want %q", tt.repo, got, tt.want)
		}
	}
}

// TestLogReference は参照文字列の組み立てを固定する。
//
// "owner/name" で設定されているときだけ GitHub の blob URL を組み立てる。
// それ以外は URL の作り方が分からないので、相対パスと sha を出す。
func TestLogReference(t *testing.T) {
	tests := []struct{ name, repo, want string }{
		{
			name: "owner/name は blob URL になる",
			repo: "owner/name",
			want: "https://github.com/owner/name/blob/main/work-log/a.md",
		},
		{
			name: "HTTPS URL は相対パスと sha になる",
			repo: "https://example.com/owner/name.git",
			want: "work-log/a.md @ abc123",
		},
		{
			name: "SSH URL は相対パスと sha になる",
			repo: "git@github.com:owner/name.git",
			want: "work-log/a.md @ abc123",
		},
		{
			name: "ローカルパスは相対パスと sha になる",
			repo: "/tmp/remote-log.git",
			want: "work-log/a.md @ abc123",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := infragit.LogReference(tt.repo, "main", "work-log/a.md", "abc123")
			if got != tt.want {
				t.Errorf("LogReference(%q) = %q, want %q", tt.repo, got, tt.want)
			}
		})
	}
}
