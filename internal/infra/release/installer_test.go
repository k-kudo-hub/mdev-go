package release

import (
	"archive/tar"
	"compress/gzip"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// tarEntry は tarball へ入れる 1 エントリである。
type tarEntry struct {
	name string
	body string
	dir  bool
}

// writeTarGz は entries を持つ tar.gz を作ってそのパスを返す。
func writeTarGz(t *testing.T, dir string, entries []tarEntry) string {
	t.Helper()
	path := filepath.Join(dir, "release.tar.gz")
	file, err := os.Create(path) //nolint:gosec // テスト用の一時パス
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = file.Close() }()

	gz := gzip.NewWriter(file)
	writer := tar.NewWriter(gz)
	for _, entry := range entries {
		header := &tar.Header{Name: entry.name, Mode: 0o644, Size: int64(len(entry.body))}
		if entry.dir {
			header.Typeflag = tar.TypeDir
			header.Mode = 0o755
			header.Size = 0
		}
		if err := writer.WriteHeader(header); err != nil {
			t.Fatal(err)
		}
		if !entry.dir {
			if _, err := writer.Write([]byte(entry.body)); err != nil {
				t.Fatal(err)
			}
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	return path
}

// releaseEntries は GitHub のソース tarball と同じ形(<repo>-<version>/ 配下)
// のエントリを返す。
func releaseEntries() []tarEntry {
	return []tarEntry{
		{name: "conductor-0.2.0/", dir: true},
		{name: "conductor-0.2.0/install.sh", body: "#!/bin/bash\necho installed\n"},
		{name: "conductor-0.2.0/scripts/", dir: true},
		{name: "conductor-0.2.0/scripts/task-lib.sh", body: "x\n"},
	}
}

// newTestInstaller は install.sh の実行だけを記録に差し替えた Installer を返す。
func newTestInstaller() (*Installer, *[]string, *[]string) {
	installer := NewInstaller()
	scripts := &[]string{}
	envs := &[]string{}
	installer.run = func(env []string, script string) error {
		*scripts = append(*scripts, script)
		*envs = env
		return nil
	}
	return installer, scripts, envs
}

// TestInstallFromFileURL は file:// の tarball から install.sh を見つけて
// 実行することを確かめる。test.sh「53. update.sh」と同じ形の tarball を使う。
func TestInstallFromFileURL(t *testing.T) {
	work := t.TempDir()
	archive := writeTarGz(t, work, releaseEntries())
	installer, scripts, envs := newTestInstaller()

	err := installer.Install("file://"+archive, "v0.2.0", "https://github.com/o/r.git")
	if err != nil {
		t.Fatalf("Install が失敗しました: %v", err)
	}
	if len(*scripts) != 1 {
		t.Fatalf("install.sh の実行 = %d 回, want 1", len(*scripts))
	}
	// GitHub の tarball は <repo>-<version>/ 配下に展開されるので 2 段目にある。
	if want := filepath.Join("conductor-0.2.0", "install.sh"); !strings.HasSuffix((*scripts)[0], want) {
		t.Errorf("実行したスクリプト = %q, want %q で終わる", (*scripts)[0], want)
	}
	// tarball には .git が無いため、版と更新元は環境変数で渡すしかない。
	for _, want := range []string{"CONDUCTOR_VERSION=v0.2.0", "CONDUCTOR_REPO_URL=https://github.com/o/r.git"} {
		if !slices.Contains(*envs, want) {
			t.Errorf("環境変数に %q がありません", want)
		}
	}
}

// TestInstallFailures は取得・展開・探索の失敗が error になることを確かめる。
func TestInstallFailures(t *testing.T) {
	work := t.TempDir()

	broken := filepath.Join(work, "broken.tar.gz")
	if err := os.WriteFile(broken, []byte("これは tar.gz ではない"), 0o600); err != nil {
		t.Fatal(err)
	}
	// install.sh を含まない tarball(現行版の空ガードに対応する)。
	noScript := writeTarGz(t, t.TempDir(), []tarEntry{
		{name: "conductor-0.2.0/", dir: true},
		{name: "conductor-0.2.0/README.md", body: "x\n"},
	})
	// 深すぎる位置の install.sh は拾わない(奥の別スクリプトを踏まないため)。
	tooDeep := writeTarGz(t, t.TempDir(), []tarEntry{
		{name: "a/b/c/install.sh", body: "x\n"},
	})

	tests := []struct {
		name    string
		url     string
		wantMsg string
	}{
		{name: "取得に失敗", url: "file://" + filepath.Join(work, "none.tar.gz"), wantMsg: "ダウンロードに失敗しました"},
		{name: "展開に失敗", url: "file://" + broken, wantMsg: "展開に失敗しました"},
		{name: "install.sh が無い", url: "file://" + noScript, wantMsg: "install.sh が見つかりません"},
		{name: "install.sh が深すぎる", url: "file://" + tooDeep, wantMsg: "install.sh が見つかりません"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			installer, scripts, _ := newTestInstaller()
			err := installer.Install(tt.url, "v0.2.0", "https://github.com/o/r.git")
			if err == nil {
				t.Fatal("error になりませんでした")
			}
			if !strings.Contains(err.Error(), tt.wantMsg) {
				t.Errorf("error = %v, want %q を含む", err, tt.wantMsg)
			}
			if len(*scripts) != 0 {
				t.Errorf("失敗しているのに install.sh を実行しました: %v", *scripts)
			}
		})
	}
}

// TestInstallPropagatesScriptFailure は install.sh の失敗が伝わることを
// 確かめる。ここを握り潰すと、更新できていないのに完了と出てしまう。
func TestInstallPropagatesScriptFailure(t *testing.T) {
	archive := writeTarGz(t, t.TempDir(), releaseEntries())
	installer := NewInstaller()
	installer.run = func([]string, string) error { return errors.New("install に失敗") }

	if err := installer.Install("file://"+archive, "v0.2.0", "https://github.com/o/r.git"); err == nil {
		t.Error("install.sh の失敗が伝わっていません")
	}
}

// TestInstallIgnoresPathTraversal は展開先の外を指すエントリを無視すること
// を確かめる。取得経路が壊れたときにホームディレクトリを書き換えられる形に
// してはならない。
func TestInstallIgnoresPathTraversal(t *testing.T) {
	work := t.TempDir()
	outside := filepath.Join(work, "outside.txt")
	archive := writeTarGz(t, work, []tarEntry{
		{name: "../../../../../../../../" + strings.TrimPrefix(outside, "/"), body: "PWNED"},
		{name: "conductor-0.2.0/", dir: true},
		{name: "conductor-0.2.0/install.sh", body: "x\n"},
	})

	installer, scripts, _ := newTestInstaller()
	if err := installer.Install("file://"+archive, "v0.2.0", "u"); err != nil {
		t.Fatalf("Install が失敗しました: %v", err)
	}
	if len(*scripts) != 1 {
		t.Errorf("install.sh の実行 = %d 回, want 1", len(*scripts))
	}
	if _, err := os.Stat(outside); !os.IsNotExist(err) {
		t.Errorf("展開先の外にファイルが作られました: %v", err)
	}
}

// TestRunInstallScript は実際に bash で実行できることを確かめる。
func TestRunInstallScript(t *testing.T) {
	dir := t.TempDir()
	marker := filepath.Join(dir, "marker")
	script := filepath.Join(dir, "install.sh")
	body := "#!/bin/bash\nprintf '%s' \"$CONDUCTOR_VERSION\" > " + marker + "\n"
	if err := os.WriteFile(script, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}

	if err := runInstallScript(append(os.Environ(), "CONDUCTOR_VERSION=v9.9.9"), script); err != nil {
		t.Fatalf("runInstallScript が失敗しました: %v", err)
	}
	got, err := os.ReadFile(marker)
	if err != nil || string(got) != "v9.9.9" {
		t.Errorf("環境変数が渡っていません: %q (%v)", got, err)
	}
}

// TestInstallRejectsOversizedDownload は取得量が上限を超えたときに、
// 切り詰めた内容で先へ進まず「上限を超えた」と分かる error になることを
// 確かめる。
//
// 黙って切り詰めると、途中までの tarball を正常なものとして展開しにかかり、
// 「展開に失敗しました: unexpected EOF」という原因の分からない失敗になる。
func TestInstallRejectsOversizedDownload(t *testing.T) {
	archive := writeTarGz(t, t.TempDir(), releaseEntries())
	installer, scripts, _ := newTestInstaller()
	// tarball の実サイズより小さい上限にする。
	installer.maxBytes = 8

	err := installer.Install("file://"+archive, "v0.2.0", "u")
	if err == nil {
		t.Fatal("上限を超えたのに error になりませんでした")
	}
	if !strings.Contains(err.Error(), "ダウンロードに失敗しました") ||
		!strings.Contains(err.Error(), "上限") {
		t.Errorf("error = %v, want ダウンロード段階の上限超過と分かる文言", err)
	}
	if len(*scripts) != 0 {
		t.Errorf("失敗しているのに install.sh を実行しました: %v", *scripts)
	}
}

// TestInstallRejectsOversizedEntry は展開する 1 ファイルが上限を超えたときに
// error になることを確かめる。
//
// 切り詰めた中身のまま install.sh を置いて実行すると、途中で切れた
// スクリプトが走ってしまう。
func TestInstallRejectsOversizedEntry(t *testing.T) {
	// 圧縮すると小さくなる大きなファイルを入れる。tarball 自体は上限に
	// 収まるので、download は通り抜けて展開段階で引っかかる。
	archive := writeTarGz(t, t.TempDir(), []tarEntry{
		{name: "conductor-0.2.0/", dir: true},
		{name: "conductor-0.2.0/install.sh", body: strings.Repeat("a", 5000)},
	})
	installer, scripts, _ := newTestInstaller()
	installer.maxBytes = 2000

	err := installer.Install("file://"+archive, "v0.2.0", "u")
	if err == nil {
		t.Fatal("上限を超えたのに error になりませんでした")
	}
	if !strings.Contains(err.Error(), "上限") {
		t.Errorf("error = %v, want 上限超過と分かる文言", err)
	}
	if len(*scripts) != 0 {
		t.Errorf("失敗しているのに install.sh を実行しました: %v", *scripts)
	}
}

// TestInstallAcceptsSizeAtLimit はちょうど上限のときは通ることを確かめる
// (上限 +1 で読む実装が、境界で誤検知しないこと)。
func TestInstallAcceptsSizeAtLimit(t *testing.T) {
	dir := t.TempDir()
	archive := writeTarGz(t, dir, releaseEntries())
	info, err := os.Stat(archive)
	if err != nil {
		t.Fatal(err)
	}
	installer, scripts, _ := newTestInstaller()
	installer.maxBytes = info.Size()

	if err := installer.Install("file://"+archive, "v0.2.0", "u"); err != nil {
		t.Fatalf("ちょうど上限なのに失敗しました: %v", err)
	}
	if len(*scripts) != 1 {
		t.Errorf("install.sh の実行 = %d 回, want 1", len(*scripts))
	}
}
