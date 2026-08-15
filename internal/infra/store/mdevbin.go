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

// MdevBinaryStore は CONDUCTOR_HOME 配下の mdev バイナリの所在を調べる。
//
// 使い道は 1 つで、タスクタブの操作バーを起動するコマンド行を組み立てるとき
// (app.TaskControlLauncher の実装。taskcontrol.go)の**行き先の推測**である。
// 通常は os.Executable が返す「今動いているバイナリ」を使い、それを引けない
// ときだけここへ落ちる。
//
// CONDUCTOR_HOME から組み立てるのは、hooks のコマンド文字列と同じ規約に
// 揃えるためである。mdev-test のように CONDUCTOR_HOME を worktree へ向けて
// いる場合はその worktree の bin/mdev を指すことになり、「別の版を動かして
// いるつもりが設置済みのほうを見ていた」という取り違えを拾える。
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
