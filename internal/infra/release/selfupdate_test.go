package release

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/k-kudo-hub/mdev-go/internal/domain"
)

// newSelfUpdateFixture は file:// で取れる配布物一式を作り、置き換え対象の
// 実行ファイルと SelfReplacer を返す。
func newSelfUpdateFixture(t *testing.T, newBinary string) (*SelfReplacer, string, domain.SelfUpdatePlan) {
	t.Helper()

	dist := t.TempDir()
	assetPath := filepath.Join(dist, "mdev_darwin_arm64")
	if err := os.WriteFile(assetPath, []byte(newBinary), 0o644); err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256([]byte(newBinary))
	checksums := hex.EncodeToString(sum[:]) + "  mdev_darwin_arm64\n"
	checksumsPath := filepath.Join(dist, "checksums.txt")
	if err := os.WriteFile(checksumsPath, []byte(checksums), 0o644); err != nil {
		t.Fatal(err)
	}

	// 置き換えられる側(今動いているバイナリのつもり)。
	binDir := t.TempDir()
	target := filepath.Join(binDir, "mdev")
	if err := os.WriteFile(target, []byte("OLD BINARY"), 0o755); err != nil { //nolint:gosec // テストの実行ファイル
		t.Fatal(err)
	}

	r := NewSelfReplacer()
	r.executable = func() (string, error) { return target, nil }
	plan := domain.SelfUpdatePlan{
		Current:      "v0.10.0",
		Latest:       "v0.11.0",
		AssetName:    "mdev_darwin_arm64",
		AssetURL:     "file://" + assetPath,
		ChecksumsURL: "file://" + checksumsPath,
	}
	return r, target, plan
}

// TestSelfReplacerReplaces は照合が通ったバイナリで置き換わることを確かめる。
func TestSelfReplacerReplaces(t *testing.T) {
	t.Parallel()

	r, target, plan := newSelfUpdateFixture(t, "NEW BINARY")

	got, err := r.Replace(plan)
	if err != nil {
		t.Fatalf("Replace() = %v", err)
	}
	if got != target {
		t.Errorf("置き換えたパス = %q, want %q", got, target)
	}
	content, err := os.ReadFile(target) //nolint:gosec // テストの一時ディレクトリ
	if err != nil {
		t.Fatal(err)
	}
	if string(content) != "NEW BINARY" {
		t.Errorf("中身 = %q, want %q", content, "NEW BINARY")
	}
	info, err := os.Stat(target)
	if err != nil {
		t.Fatal(err)
	}
	// 実行できなければ置き換えた意味が無い。
	if info.Mode().Perm() != binaryPerm {
		t.Errorf("権限 = %v, want %v", info.Mode().Perm(), binaryPerm)
	}
}

// TestSelfReplacerRejectsChecksumMismatch は **この機能で最も重要な条件**
// を固定する。SHA-256 が合わないバイナリを実行ファイルとして置かない。
func TestSelfReplacerRejectsChecksumMismatch(t *testing.T) {
	t.Parallel()

	r, target, plan := newSelfUpdateFixture(t, "NEW BINARY")
	// checksums.txt はそのままに、配布物だけをすり替える。
	assetPath := strings.TrimPrefix(plan.AssetURL, "file://")
	if err := os.WriteFile(assetPath, []byte("TAMPERED"), 0o644); err != nil {
		t.Fatal(err)
	}

	if _, err := r.Replace(plan); err == nil {
		t.Fatal("照合が合わないのに置き換えました")
	}
	content, _ := os.ReadFile(target) //nolint:gosec // テストの一時ディレクトリ
	if string(content) != "OLD BINARY" {
		t.Errorf("元のバイナリが壊されました: %q", content)
	}
	// 一時ファイルも残さない。
	assertNoLeftovers(t, filepath.Dir(target))
}

// TestSelfReplacerFailures は取得できない場合に元のバイナリを保つことを
// 確かめる。
func TestSelfReplacerFailures(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		prepare func(t *testing.T, plan *domain.SelfUpdatePlan)
		wantMsg string
	}{
		{
			name: "checksums が取れない",
			prepare: func(t *testing.T, plan *domain.SelfUpdatePlan) {
				t.Helper()
				plan.ChecksumsURL = "file:///nonexistent/checksums.txt"
			},
			wantMsg: "checksums.txt の取得に失敗",
		},
		{
			name: "自分の環境の値が checksums に無い",
			prepare: func(t *testing.T, plan *domain.SelfUpdatePlan) {
				t.Helper()
				plan.AssetName = "mdev_darwin_amd64"
			},
			wantMsg: "SHA-256 がありません",
		},
		{
			name: "バイナリが取れない",
			prepare: func(t *testing.T, plan *domain.SelfUpdatePlan) {
				t.Helper()
				plan.AssetURL = "file:///nonexistent/mdev_darwin_arm64"
			},
			wantMsg: "バイナリの取得に失敗",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			r, target, plan := newSelfUpdateFixture(t, "NEW BINARY")
			tt.prepare(t, &plan)

			_, err := r.Replace(plan)
			if err == nil {
				t.Fatal("error になりませんでした")
			}
			if !strings.Contains(err.Error(), tt.wantMsg) {
				t.Errorf("error = %v, want %q を含む", err, tt.wantMsg)
			}
			content, _ := os.ReadFile(target) //nolint:gosec // テストの一時ディレクトリ
			if string(content) != "OLD BINARY" {
				t.Errorf("元のバイナリが壊されました: %q", content)
			}
			assertNoLeftovers(t, filepath.Dir(target))
		})
	}
}

// TestSelfReplacerRejectsOversized は上限を超えたバイナリを置かないことを
// 確かめる。
func TestSelfReplacerRejectsOversized(t *testing.T) {
	t.Parallel()

	r, target, plan := newSelfUpdateFixture(t, strings.Repeat("a", 5000))
	r.maxBytes = 100

	if _, err := r.Replace(plan); err == nil {
		t.Fatal("上限を超えたのに置き換えました")
	}
	content, _ := os.ReadFile(target) //nolint:gosec // テストの一時ディレクトリ
	if string(content) != "OLD BINARY" {
		t.Errorf("元のバイナリが壊されました: %q", content)
	}
}

// TestSelfReplacerResolvesSymlink は **リンクではなく実体を置き換える**
// ことを確かめる。
//
// リンクを置き換えるとリンクそのものがバイナリになり、リンク元は古いまま
// 残る。次に起動したときにどちらが動くかが分からなくなる。
func TestSelfReplacerResolvesSymlink(t *testing.T) {
	t.Parallel()

	r, target, plan := newSelfUpdateFixture(t, "NEW BINARY")
	link := filepath.Join(t.TempDir(), "mdev-link")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	r.executable = func() (string, error) { return resolveForTest(t, link), nil }

	got, err := r.Replace(plan)
	if err != nil {
		t.Fatalf("Replace() = %v", err)
	}
	// macOS の /var は /private/var へのリンクなので、比較も解決後で行う。
	if want := resolveForTest(t, target); got != want {
		t.Errorf("置き換えたパス = %q, want 実体の %q", got, want)
	}
	content, _ := os.ReadFile(target) //nolint:gosec // テストの一時ディレクトリ
	if string(content) != "NEW BINARY" {
		t.Errorf("実体が置き換わっていません: %q", content)
	}
}

// resolveForTest は currentExecutable と同じ解決を行う。
func resolveForTest(t *testing.T, path string) string {
	t.Helper()
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil {
		t.Fatal(err)
	}
	return resolved
}

// assertNoLeftovers は置き換え先のディレクトリに一時ファイルが残って
// いないことを確かめる。
func assertNoLeftovers(t *testing.T, dir string) {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if strings.Contains(entry.Name(), ".new-") {
			t.Errorf("一時ファイルが残っています: %s", entry.Name())
		}
	}
}

// TestCurrentExecutableResolves は自分自身のパスが解決できることを確かめる
// (テスト実行中のバイナリで確かめる)。
func TestCurrentExecutableResolves(t *testing.T) {
	t.Parallel()

	got, err := currentExecutable()
	if err != nil {
		t.Fatalf("currentExecutable() = %v", err)
	}
	if !filepath.IsAbs(got) {
		t.Errorf("パス = %q, want 絶対パス", got)
	}
}
