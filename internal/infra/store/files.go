package store

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"

	"github.com/k-kudo-hub/mdev-go/internal/app"
	"github.com/k-kudo-hub/mdev-go/internal/infra/fsutil"
)

// FileStore は install / uninstall が触るファイルを実際に読み書きする
// app.FileStore の実装である。
type FileStore struct{}

var _ app.FileStore = FileStore{}

// NewFileStore は FileStore を返す。
func NewFileStore() FileStore { return FileStore{} }

// Read はファイルを読む。無ければ ok=false を返す(それはエラーではない)。
func (FileStore) Read(path string) ([]byte, bool, error) {
	b, err := os.ReadFile(path) //nolint:gosec // 設置先はユースケースが組み立てる
	if errors.Is(err, fs.ErrNotExist) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, fmt.Errorf("%s の読み取りに失敗しました: %w", path, err)
	}
	return b, true, nil
}

// Write はファイルを書く。親ディレクトリは作る。
//
// 一時ファイルを作ってから rename する(fsutil)。設置の途中で落ちても、
// 書きかけの設定が残らない。
func (FileStore) Write(path string, data []byte) error {
	return fsutil.WriteFile(path, data)
}

// WriteExecutable は実行権限付きでファイルを書く。
func (FileStore) WriteExecutable(path string, data []byte) error {
	return fsutil.WriteFileMode(path, data, 0o755)
}

// Remove はファイルまたはディレクトリを消す。無ければ成功として扱う。
func (FileStore) Remove(path string) error {
	if err := os.RemoveAll(path); err != nil {
		return fmt.Errorf("%s の削除に失敗しました: %w", path, err)
	}
	return nil
}

// ListDir は path 直下の名前を昇順で返す。無ければ空を返す。
func (FileStore) ListDir(path string) ([]string, error) {
	entries, err := os.ReadDir(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("%s の一覧に失敗しました: %w", path, err)
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		names = append(names, e.Name())
	}
	sort.Strings(names)
	return names, nil
}

// Exists はファイルまたはディレクトリが在るかを返す。
//
// リンク切れの symlink も「在る」と答える。そこに在るのは未設置ではなく
// 壊れた設置なので、黙って上書きせずに読み取りの失敗として表に出す。
func (FileStore) Exists(path string) bool {
	_, err := os.Lstat(path)
	return err == nil
}

// AssetPath は同梱資産の設置先を返す(テストと案内の組み立て用)。
func AssetPath(conductorHome, name string) string {
	return filepath.Join(conductorHome, filepath.FromSlash(name))
}
