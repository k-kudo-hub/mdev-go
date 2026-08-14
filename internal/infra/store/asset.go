package store

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/k-kudo-hub/mdev-go/assets"
)

// ErrUnknownAsset は名前に覚えが無いことを表す。
var ErrUnknownAsset = errors.New("そのような資産はありません")

// AssetNames は同梱されている資産の名前を返す(CONDUCTOR_HOME からの相対パス)。
func AssetNames() []string { return assets.Names() }

// ReadAsset は資産の中身を返す。
//
// CONDUCTOR_HOME に同じ名前の実ファイルがあればそれを、無ければ実行ファイルに
// 埋め込まれたものを返す。**実ファイルを優先するのがこの関数の要点**である。
// 利用者がレイアウトや既定設定に手を入れた場合、その変更が更新のたびに
// 消えるのでは手を入れる意味が無い。埋め込みは「まだ何も置かれていない」
// ときの土台であって、上書きの対象ではない。
//
// 実ファイルがあるが読めない場合はエラーを返す。権限や壊れた設置を黙って
// 埋め込みで埋めると、利用者が置いたはずの内容と食い違ったまま動く。
func ReadAsset(conductorHome, name string) ([]byte, error) {
	embedded, known := assets.Read(name)
	if !known {
		return nil, fmt.Errorf("%w: %s", ErrUnknownAsset, name)
	}

	path := filepath.Join(conductorHome, filepath.FromSlash(name))
	b, err := os.ReadFile(path) //nolint:gosec // 埋め込み済みの名前に限る
	switch {
	case err == nil:
		return b, nil
	case errors.Is(err, fs.ErrNotExist):
		return embedded, nil
	default:
		return nil, fmt.Errorf("資産 %s の読み取りに失敗しました: %w", path, err)
	}
}
