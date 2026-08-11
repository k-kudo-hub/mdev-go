package store

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/k-kudo-hub/mdev-go/internal/app"
	"github.com/k-kudo-hub/mdev-go/internal/domain"
	"github.com/k-kudo-hub/mdev-go/internal/infra/fsutil"
)

// FlavorStore は CONDUCTOR_HOME 直下の FLAVOR ファイルを読み書きする
// app.FlavorStore の実装である。
//
// 読むのは conductor の install.sh(bash)なので、こちら側は書くことと
// 消すことだけを担う。
type FlavorStore struct {
	conductorHome string
}

var _ app.FlavorStore = (*FlavorStore)(nil)

// NewFlavorStore は conductorHome 直下を見る FlavorStore を返す。
func NewFlavorStore(conductorHome string) *FlavorStore {
	return &FlavorStore{conductorHome: conductorHome}
}

// Path は FLAVOR ファイルのパスを返す。
func (s *FlavorStore) Path() string {
	return filepath.Join(s.conductorHome, domain.FlavorFileName)
}

// WriteFlavor は印を書く。
//
// 原子的に置き換えるのは、install.sh が同時に読みうるためである。
// 書きかけの内容を読まれると、印が "g" などに見えて Go 版の設定が
// 巻き戻る(このファイルが防ごうとしている事故そのものになる)。
func (s *FlavorStore) WriteFlavor(flavor string) error {
	path := s.Path()
	if err := fsutil.WriteFile(path, []byte(domain.FlavorFileContent(flavor))); err != nil {
		return fmt.Errorf("%s の書き込みに失敗しました: %w", path, err)
	}
	return nil
}

// RemoveFlavor は印を消す。無い場合も成功として扱う。
//
// 目的は「印が無い状態にする」ことなので、既にそうなっているのは失敗ではない。
func (s *FlavorStore) RemoveFlavor() error {
	path := s.Path()
	if err := os.Remove(path); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("%s の削除に失敗しました: %w", path, err)
	}
	return nil
}
