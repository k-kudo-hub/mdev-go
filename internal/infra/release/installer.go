// Package release は配布物(リリース tarball)の取得と再インストールを担当する。
// internal/app が定義する port の実装(adapter)である(ADR-0002)。
package release

import (
	"archive/tar"
	"compress/gzip"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/k-kudo-hub/mdev-go/internal/app"
)

// downloadTimeout は tarball の取得を諦めるまでの時間
// (現行 update.sh の `curl --max-time 60`)。
const downloadTimeout = 60 * time.Second

// maxDownloadBytes は受け取る tarball と展開する 1 ファイルの上限である。
//
// 現行版には無い防御だが、展開先はディスクなので、応答が壊れて延々と
// 流れてきたときにディスクを埋めないための歯止めを置く。conductor の
// ソース tarball は数百 KB で、100 MB は十分に余裕がある。
//
// 上限に達したら **必ず error にする**。黙って切り詰めると、壊れた tarball や
// 途中までのファイルで install.sh を走らせてしまい、更新に失敗したことに
// 気づけないまま conductor が半端な状態になる。
const maxDownloadBytes = 100 << 20

// installScriptName は tarball の中から探すインストーラ。
const installScriptName = "install.sh"

// installScriptDepth は install.sh を探す深さである。
//
// GitHub のソース tarball は <repo>-<version>/ の下に展開されるため 2 で足りる。
// 深く掘らないのは、リポジトリの奥にある別の install.sh を拾わないためである
// (現行 update.sh の `find -maxdepth 2`)。
const installScriptDepth = 2

// install.sh へ渡す環境変数。tarball には .git が入っていないため、
// install.sh が版と更新元を知る手段はこれしかない。
const (
	versionEnv = "CONDUCTOR_VERSION"
	repoURLEnv = "CONDUCTOR_REPO_URL"
)

// dirPerm は展開先ディレクトリのパーミッション。
const dirPerm = 0o755

// Installer はリリース tarball を取得して install.sh を実行する。
type Installer struct {
	client *http.Client
	// maxBytes は受け取る / 展開する 1 ファイルの上限。テストで小さくする。
	maxBytes int64
	// run は install.sh を実行する。テストで差し替える。
	run func(env []string, script string) error
}

var _ app.ReleaseInstaller = (*Installer)(nil)

// NewInstaller は Installer を返す。
//
// file:// を扱えるようにしてあるのは、更新の流れを実際のネットワーク無しで
// 試せるようにするためである(現行版も CONDUCTOR_TARBALL_URL に file:// を
// 渡す形でテストしている)。
func NewInstaller() *Installer {
	transport := &http.Transport{}
	transport.RegisterProtocol("file", http.NewFileTransport(http.Dir("/")))
	return &Installer{
		client:   &http.Client{Timeout: downloadTimeout, Transport: transport},
		maxBytes: maxDownloadBytes,
		run:      runInstallScript,
	}
}

// Install は tarballURL からソースを取り、その中の install.sh を実行する。
func (i *Installer) Install(tarballURL, version, repoURL string) error {
	work, err := os.MkdirTemp("", "mdev-update-")
	if err != nil {
		return fmt.Errorf("作業ディレクトリの作成に失敗しました: %w", err)
	}
	defer func() { _ = os.RemoveAll(work) }()

	archive := filepath.Join(work, "src.tar.gz")
	if err := i.download(tarballURL, archive); err != nil {
		return fmt.Errorf("ダウンロードに失敗しました: %s: %w", tarballURL, err)
	}
	if err := extractTarGz(archive, work, i.maxBytes); err != nil {
		return fmt.Errorf("展開に失敗しました: %w", err)
	}

	script, ok := findInstallScript(work)
	if !ok {
		return errors.New("展開したソースに install.sh が見つかりません。")
	}
	// 現行版と同じく install.sh のあるディレクトリを起点に実行する。
	env := append(os.Environ(), versionEnv+"="+version, repoURLEnv+"="+repoURL)
	if err := i.run(env, script); err != nil {
		return fmt.Errorf("install.sh の実行に失敗しました: %w", err)
	}
	return nil
}

// download は URL の内容を dest へ保存する。
func (i *Installer) download(url, dest string) error {
	resp, err := i.client.Get(url) //nolint:noctx // Client.Timeout で上限を持つ
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("状態コード %d", resp.StatusCode)
	}

	file, err := os.Create(dest) //nolint:gosec // 自分で作った一時ディレクトリ配下
	if err != nil {
		return err
	}
	defer func() { _ = file.Close() }()
	// 上限より 1 バイト多く読み、超えたかどうかを書き込み量で判定する。
	// ちょうど上限で切ると、切り詰めた内容を正常な tarball として
	// 展開しにかかってしまう。
	written, err := io.Copy(file, io.LimitReader(resp.Body, i.maxBytes+1))
	if err != nil {
		return err
	}
	if written > i.maxBytes {
		return fmt.Errorf("受け取った内容が上限 %d バイトを超えました", i.maxBytes)
	}
	return file.Close()
}

// extractTarGz は tar.gz を dest 配下へ展開する。
//
// 展開先の外を指す名前(絶対パス・.. を含むもの)は無視する。リリース
// tarball は自分のリポジトリのものだが、取得経路が壊れたときに
// ホームディレクトリを書き換えられる形にはしない。
func extractTarGz(archive, dest string, maxBytes int64) error {
	file, err := os.Open(archive) //nolint:gosec // 自分で作った一時ディレクトリ配下
	if err != nil {
		return err
	}
	defer func() { _ = file.Close() }()

	gz, err := gzip.NewReader(file)
	if err != nil {
		return err
	}
	defer func() { _ = gz.Close() }()

	reader := tar.NewReader(gz)
	for {
		header, err := reader.Next()
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return err
		}
		target, ok := safeJoin(dest, header.Name)
		if !ok {
			continue
		}
		if err := extractEntry(reader, header, target, maxBytes); err != nil {
			return err
		}
	}
}

// extractEntry は 1 エントリを書き出す。ディレクトリと通常ファイルだけを扱い、
// シンボリックリンクなどは無視する(リリース tarball には含まれない)。
func extractEntry(reader io.Reader, header *tar.Header, target string, maxBytes int64) error {
	switch header.Typeflag {
	case tar.TypeDir:
		return os.MkdirAll(target, dirPerm)
	case tar.TypeReg:
		if err := os.MkdirAll(filepath.Dir(target), dirPerm); err != nil {
			return err
		}
		file, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, header.FileInfo().Mode().Perm()) //nolint:gosec // 展開先は自分で作った一時ディレクトリ
		if err != nil {
			return err
		}
		defer func() { _ = file.Close() }()
		// download と同じく上限 +1 で読み、超えたら error にする。
		// 切り詰めた中身のまま install.sh を置くと、壊れたスクリプトを実行する。
		written, err := io.Copy(file, io.LimitReader(reader, maxBytes+1))
		if err != nil {
			return err
		}
		if written > maxBytes {
			return fmt.Errorf("%s が上限 %d バイトを超えました", header.Name, maxBytes)
		}
		return file.Close()
	default:
		return nil
	}
}

// safeJoin は dest 配下に収まるパスだけを返す。
func safeJoin(dest, name string) (string, bool) {
	if name == "" || filepath.IsAbs(name) {
		return "", false
	}
	target := filepath.Join(dest, filepath.Clean(name))
	if target != dest && !strings.HasPrefix(target, dest+string(os.PathSeparator)) {
		return "", false
	}
	return target, true
}

// findInstallScript は root 配下 installScriptDepth 段までから install.sh を探す。
//
// 複数あるときは名前順で最初のものを使う。現行版は find の並び(不定)に
// 任せているが、選ぶものが実行のたびに変わらないほうがよい。
func findInstallScript(root string) (string, bool) {
	dirs := []string{root}
	for depth := 1; depth < installScriptDepth; depth++ {
		var next []string
		for _, dir := range dirs {
			entries, err := os.ReadDir(dir)
			if err != nil {
				continue
			}
			for _, entry := range entries {
				if entry.IsDir() {
					next = append(next, filepath.Join(dir, entry.Name()))
				}
			}
		}
		dirs = append(dirs, next...)
	}
	sort.Strings(dirs)

	for _, dir := range dirs {
		path := filepath.Join(dir, installScriptName)
		if info, err := os.Stat(path); err == nil && info.Mode().IsRegular() {
			return path, true
		}
	}
	return "", false
}

// runInstallScript は install.sh を bash で実行する。
//
// 標準入出力は引き継ぐ。install.sh は進み具合を出し、設定によっては
// .zshrc へ追記してよいかを尋ねるため、利用者と直接やり取りできなければ
// ならない。上限は設けない(現行版も付けていない)。
func runInstallScript(env []string, script string) error {
	cmd := exec.Command("bash", script) //nolint:gosec // 展開した tarball 内の固定名
	cmd.Dir = filepath.Dir(script)
	cmd.Env = env
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}
