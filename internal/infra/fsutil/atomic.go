// Package fsutil は infra の各 adapter が共有するファイル操作を持つ。
//
// 置き場所(store / news / release ...)ではなく「壊れた状態を読ませない
// 書き方」という関心で切り出している。同じ原子的書き込みを adapter ごとに
// 書き直すと、片方だけ一時ファイル名が固定のまま競合する、といったずれが出る。
package fsutil

import (
	"fmt"
	"os"
	"path/filepath"
)

// DirPerm / FilePerm は現行 Shell 版が既定の umask(022)で作るものに合わせる。
const (
	DirPerm  os.FileMode = 0o755
	FilePerm os.FileMode = 0o644
)

// WriteFile は path と同じディレクトリに一時ファイルを作って書き込み、
// rename で置き換える。読み手が書きかけの内容を見ることを防ぐ。
//
// 現行 Shell 版は `mktemp` を使っており、TMPDIR が別ファイルシステムにある
// 場合に rename が原子的にならなかった。同一ディレクトリに固定することで
// この穴を塞いでいる(挙動互換の範囲内の堅牢化)。
func WriteFile(path string, data []byte) error {
	return WriteFileMode(path, data, FilePerm)
}

// WriteFileMode は WriteFile のパーミッション指定版である。
// 既存ファイルの権限を引き継ぎたい settings.json の書き換えで使う。
func WriteFileMode(path string, data []byte, perm os.FileMode) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, DirPerm); err != nil {
		return fmt.Errorf("ディレクトリ %s の作成に失敗しました: %w", dir, err)
	}

	tmp, err := os.CreateTemp(dir, filepath.Base(path)+".tmp-*")
	if err != nil {
		return fmt.Errorf("一時ファイルの作成に失敗しました: %w", err)
	}
	tmpName := tmp.Name()

	if err := writeAndClose(tmp, data); err != nil {
		_ = os.Remove(tmpName)
		return err
	}
	// CreateTemp は 0600 で作るため、意図した権限へ揃える。
	if err := os.Chmod(tmpName, perm); err != nil {
		_ = os.Remove(tmpName)
		return fmt.Errorf("一時ファイルの権限設定に失敗しました: %w", err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		_ = os.Remove(tmpName)
		return fmt.Errorf("%s への置き換えに失敗しました: %w", path, err)
	}
	return nil
}

// writeAndClose は f に data を書き、必ず閉じる。
func writeAndClose(f *os.File, data []byte) (err error) {
	defer func() {
		if closeErr := f.Close(); closeErr != nil && err == nil {
			err = fmt.Errorf("一時ファイルのクローズに失敗しました: %w", closeErr)
		}
	}()
	if _, err := f.Write(data); err != nil {
		return fmt.Errorf("一時ファイルへの書き込みに失敗しました: %w", err)
	}
	return nil
}
