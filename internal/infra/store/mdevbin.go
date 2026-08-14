package store

import (
	"os"
	"path/filepath"
)

// mdev バイナリの置き場所。Makefile の install ターゲットも、hooks に書く
// コマンド文字列も `${CONDUCTOR_HOME:-$HOME/.claude-conductor}/bin/mdev` という
// 同じ規約に従う。
const (
	binDirName     = "bin"
	mdevBinaryName = "mdev"
)

// MdevBinaryStore は CONDUCTOR_HOME 配下の mdev バイナリの所在を調べる
// app.MdevBinaryLocator の実装である。
//
// hooks のコマンド文字列は環境変数展開の形のまま残すため、実際にどのファイルが
// 呼ばれるかは hook 実行時の CONDUCTOR_HOME で決まる。ここでは mdev 自身の
// 実行時に解決した CONDUCTOR_HOME を使う。mdev-test のように CONDUCTOR_HOME を
// worktree へ向けている場合、その worktree の bin/mdev を見ることになり、
// 「切り替えたのにダッシュボードが無反応」という一番起きやすい取り違えを拾える。
type MdevBinaryStore struct {
	conductorHome string
	// executable は今動いているバイナリの場所を返す。テストで差し替える。
	executable func() (string, error)
}

// NewMdevBinaryStore は conductorHome 配下を見る MdevBinaryStore を返す。
// conductorHome には ConductorHome の戻り値を渡す。
func NewMdevBinaryStore(conductorHome string) *MdevBinaryStore {
	return &MdevBinaryStore{conductorHome: conductorHome, executable: osExecutable}
}

// MdevBinary は hooks が呼ぶ mdev のパスと、それが実在するかを返す。
// ディレクトリは hook から実行できないため、存在しないものとして扱う。
func (s *MdevBinaryStore) MdevBinary() (string, bool) {
	path := filepath.Join(s.conductorHome, binDirName, mdevBinaryName)
	info, err := os.Stat(path)
	return path, err == nil && !info.IsDir()
}
