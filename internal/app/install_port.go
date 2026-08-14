package app

// FileStore は install / uninstall が触るファイルの読み書きである。
//
// 相手が CONDUCTOR_HOME・~/.claude/settings.json・~/.codex/config.toml と
// 複数の根に散らばるため、根ごとに port を分けず絶対パスを受ける 1 つの口に
// している。どのパスを触るかの判断はユースケース側にあり、テストはすべての
// 書き込みを 1 か所で観測できる。
type FileStore interface {
	// Read はファイルを読む。無ければ ok=false を返す(それはエラーではない)。
	Read(path string) (data []byte, ok bool, err error)
	// Write はファイルを書く。親ディレクトリは作る。
	Write(path string, data []byte) error
	// WriteExecutable は実行権限付きでファイルを書く。
	WriteExecutable(path string, data []byte) error
	// Remove はファイルまたはディレクトリを消す。無ければ成功として扱う。
	Remove(path string) error
	// ListDir は path 直下の名前を昇順で返す。無ければ空を返す。
	ListDir(path string) ([]string, error)
	// Exists はファイルまたはディレクトリが在るかを返す。
	Exists(path string) bool
}

// AssetReader は同梱資産を読む。
type AssetReader interface {
	// Names は同梱されている資産の名前を返す。
	Names() []string
	// Asset は埋め込まれた資産そのものを返す。
	//
	// **CONDUCTOR_HOME の実ファイルは見ない。** install は「同梱物を配る」側
	// なので、配る中身が既に置いてあるファイルに引きずられてはいけない。
	Asset(name string) ([]byte, bool)
}

// CommandChecker はコマンドが使えるかを調べる。
type CommandChecker interface {
	// Available は name が PATH にあるかを返す。
	Available(name string) bool
}
