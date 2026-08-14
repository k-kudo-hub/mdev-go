package store

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

// Read は資産の中身を返す(ReadAsset を参照)。
func (s AssetStore) Read(name string) ([]byte, error) {
	return ReadAsset(s.conductorHome, name)
}
