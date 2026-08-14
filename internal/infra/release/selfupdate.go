package release

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"

	"github.com/k-kudo-hub/mdev-go/internal/app"
	"github.com/k-kudo-hub/mdev-go/internal/domain"
)

// binaryPerm は置き換える実行ファイルのパーミッションである。
const binaryPerm = 0o755

// SelfReplacer は自分自身のバイナリを新しいものへ置き換える。
//
// 取得は Installer と同じ流儀(上限つき・file:// を扱える)にする。
// テストが実際のネットワークを使わずに済むようにするためで、
// 現行の tarball 取得と同じ理由である。
type SelfReplacer struct {
	client *http.Client
	// maxBytes は受け取るバイナリの上限。テストで小さくする。
	maxBytes int64
	// executable は今動いているバイナリの実体のパスを返す。テストで差し替える。
	executable func() (string, error)
}

var _ app.SelfReplacer = (*SelfReplacer)(nil)

// NewSelfReplacer は SelfReplacer を返す。
func NewSelfReplacer() *SelfReplacer {
	return &SelfReplacer{
		client:     newHTTPClient(),
		maxBytes:   maxDownloadBytes,
		executable: currentExecutable,
	}
}

// Replace は plan のバイナリを取得して自分自身を置き換える。
//
// 置き換えたバイナリのパスを返す。手順は次のとおりで、**照合が済むまで
// 実行ファイルには一切触れない**。
//
// **rename に至る前の失敗は app.ErrSelfUpdateNotStarted で包む。** そこまでの
// 失敗では実行ファイルが無傷なので、呼び出し側は警告を出して先へ進んでよい。
//
//  1. checksums.txt を取り、自分の環境向けの SHA-256 を引く
//  2. バイナリを同じディレクトリの一時ファイルへ取得する
//  3. 取得したものの SHA-256 を計算して照合する。**合わなければ中止**
//  4. 実行権限を与えて rename で置き換える
//
// 一時ファイルを **置き換え先と同じディレクトリ** に作るのは、rename が
// ファイルシステムをまたげないためである。
func (r *SelfReplacer) Replace(plan domain.SelfUpdatePlan) (string, error) {
	target, err := r.executable()
	if err != nil {
		return "", notStarted(err)
	}

	want, err := r.wantedChecksum(plan)
	if err != nil {
		return "", notStarted(err)
	}

	dir := filepath.Dir(target)
	tmp, err := os.CreateTemp(dir, filepath.Base(target)+".new-*")
	if err != nil {
		return "", notStarted(fmt.Errorf("一時ファイルの作成に失敗しました: %w", err))
	}
	tmpName := tmp.Name()
	defer func() { _ = os.Remove(tmpName) }()

	sum, err := r.downloadTo(tmp, plan.AssetURL)
	if err != nil {
		_ = tmp.Close()
		return "", notStarted(err)
	}
	if err := tmp.Close(); err != nil {
		return "", notStarted(fmt.Errorf("一時ファイルのクローズに失敗しました: %w", err))
	}
	if sum != want {
		return "", notStarted(fmt.Errorf(
			"取得したバイナリの SHA-256 が一致しません(期待 %s, 実際 %s)", want, sum))
	}

	if err := os.Chmod(tmpName, binaryPerm); err != nil {
		return "", notStarted(fmt.Errorf("実行権限の付与に失敗しました: %w", err))
	}
	// **rename で置き換える。** 実行中のバイナリを直接書き換えることは
	// できないが、rename なら安全である。走っているプロセスは元の中身を
	// 参照し続け(macOS で実測して確認)、次に起動したときから新しいものに
	// なる。置き換えは原子的なので、途中で失敗して壊れた実行ファイルが
	// 残ることもない。
	if err := os.Rename(tmpName, target); err != nil {
		return "", fmt.Errorf("バイナリの置き換えに失敗しました: %w", err)
	}
	return target, nil
}

// notStarted は「まだ実行ファイルに触れていない」失敗として包む。
//
// ここまでの失敗ではバイナリが無傷なので、呼び出し側は警告を出して
// conductor 資産の更新へ進んでよい(app.ErrSelfUpdateNotStarted を参照)。
func notStarted(err error) error {
	return fmt.Errorf("%w: %w", app.ErrSelfUpdateNotStarted, err)
}

// wantedChecksum は checksums.txt から自分の環境向けの値を取り出す。
func (r *SelfReplacer) wantedChecksum(plan domain.SelfUpdatePlan) (string, error) {
	body, err := r.fetch(plan.ChecksumsURL)
	if err != nil {
		return "", fmt.Errorf("%s の取得に失敗しました: %w", domain.ChecksumsAssetName, err)
	}
	want, ok := domain.FindChecksum(string(body), plan.AssetName)
	if !ok {
		return "", fmt.Errorf("%s に %s の SHA-256 がありません",
			domain.ChecksumsAssetName, plan.AssetName)
	}
	return want, nil
}

// fetch は URL の内容を読み込んで返す(小さなファイル用)。
func (r *SelfReplacer) fetch(url string) ([]byte, error) {
	resp, err := getOK(r.client, url)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()

	var buf bytes.Buffer
	if _, err := copyLimited(&buf, resp.Body, r.maxBytes); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// downloadTo は URL の内容を w へ書き、その SHA-256 を 16 進で返す。
//
// 書きながら計算するので、確かめるために読み直す必要が無い。
func (r *SelfReplacer) downloadTo(w io.Writer, url string) (string, error) {
	resp, err := getOK(r.client, url)
	if err != nil {
		return "", fmt.Errorf("バイナリの取得に失敗しました: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	hash := sha256.New()
	written, err := copyLimited(io.MultiWriter(w, hash), resp.Body, r.maxBytes)
	if err != nil {
		return "", fmt.Errorf("バイナリの取得に失敗しました: %w", err)
	}
	if written == 0 {
		return "", errors.New("取得したバイナリが空でした")
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}

// currentExecutable は今動いているバイナリの実体のパスを返す。
//
// **シンボリックリンクを解いた実体** を返す。リンクを置き換えると
// リンクそのものがバイナリになってしまい、リンク元は古いままになる。
func currentExecutable() (string, error) {
	path, err := os.Executable()
	if err != nil {
		return "", fmt.Errorf("自分自身のパスを特定できません: %w", err)
	}
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil {
		return "", fmt.Errorf("自分自身のパスを解決できません: %w", err)
	}
	return resolved, nil
}
