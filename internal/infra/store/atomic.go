package store

import (
	"os"

	"github.com/k-kudo-hub/mdev-go/internal/infra/fsutil"
)

// dirPerm / filePerm は現行 Shell 版が既定の umask(022)で作るものに合わせる。
const (
	dirPerm  os.FileMode = fsutil.DirPerm
	filePerm os.FileMode = fsutil.FilePerm
)

// writeFileAtomic は path を原子的に置き換える(fsutil.WriteFile の別名)。
//
// 実体を infra/fsutil へ置いているのは、同じ書き方を必要とする adapter が
// store だけではないためである(ニュースの保存も読み手と競合する)。
func writeFileAtomic(path string, data []byte) error {
	return fsutil.WriteFile(path, data)
}

// writeFileAtomicMode は writeFileAtomic のパーミッション指定版である。
// 既存ファイルの権限を引き継ぎたい settings.json の書き換えで使う。
func writeFileAtomicMode(path string, data []byte, perm os.FileMode) error {
	return fsutil.WriteFileMode(path, data, perm)
}
