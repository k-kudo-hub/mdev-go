package store

import "github.com/k-kudo-hub/mdev-go/assets"

// AssetStore は同梱資産の解決を cli の port として束ねたものである。
type AssetStore struct {
	conductorHome string
}

// NewAssetStore は conductorHome の実ファイルを優先する AssetStore を返す。
func NewAssetStore(conductorHome string) AssetStore {
	return AssetStore{conductorHome: conductorHome}
}

// Names は同梱されている資産の名前を返す。
func (s AssetStore) Names() []string { return AssetNames() }

// Asset は **埋め込まれた資産そのもの**を返す。
//
// Read と違って CONDUCTOR_HOME の実ファイルは見ない。install は「同梱物を
// 配る」側なので、配る中身が既に置いてあるファイルに引きずられてはいけない
// (置いてあるものを尊重するかどうかの判断はユースケース側にある)。
func (s AssetStore) Asset(name string) ([]byte, bool) {
	return assets.Read(name)
}

// Read は資産の中身を返す(ReadAsset を参照)。
func (s AssetStore) Read(name string) ([]byte, error) {
	return ReadAsset(s.conductorHome, name)
}
