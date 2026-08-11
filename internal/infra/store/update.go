package store

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/k-kudo-hub/mdev-go/internal/app"
)

// CONDUCTOR_HOME 直下の状態ファイル名。いずれも install.sh と更新確認が置く。
const (
	// repoURLFileName は更新元リポジトリを記録するファイル。
	repoURLFileName = "REPO_URL"
	// versionFileName はインストール済みの版を記録するファイル。
	versionFileName = "VERSION"
	// updateCacheFileName は 1 日 1 回の更新確認の結果を残すファイル。
	updateCacheFileName = ".update-check"
)

// UpdateStateStore は CONDUCTOR_HOME 直下の状態ファイルを読み書きする
// app.UpdateStateStore の実装である。
type UpdateStateStore struct {
	conductorHome string
}

var _ app.UpdateStateStore = (*UpdateStateStore)(nil)

// NewUpdateStateStore は conductorHome 直下を見る UpdateStateStore を返す。
func NewUpdateStateStore(conductorHome string) *UpdateStateStore {
	return &UpdateStateStore{conductorHome: conductorHome}
}

// RepoURL は更新元リポジトリを返す。無い・読めない場合は空文字を返す。
func (s *UpdateStateStore) RepoURL() string {
	return s.readTrimmed(repoURLFileName)
}

// InstalledVersion はインストール済みの版を返す。
//
// 無い・読めない場合は空文字を返す。v0.0.0 への正規化は domain 側で行う
// (中身が壊れている場合と欠落を同じ扱いにするため)。
func (s *UpdateStateStore) InstalledVersion() string {
	return s.readTrimmed(versionFileName)
}

// ReadUpdateCache は .update-check の 1 行目を (日付, タグ) として返す。
//
// 現行版の `read -r CACHE_DATE CACHE_TAG < "$CACHE_FILE"` と同じく、
// 1 行目を空白で 2 つに割る。読めない・形が違う場合は空を返し、
// 呼び出し側が引き直す。
func (s *UpdateStateStore) ReadUpdateCache() (string, string) {
	line := s.readTrimmed(updateCacheFileName)
	if i := strings.IndexByte(line, '\n'); i >= 0 {
		line = line[:i]
	}
	fields := strings.Fields(line)
	if len(fields) < 2 {
		return "", ""
	}
	return fields[0], fields[1]
}

// WriteUpdateCache は .update-check を書き換える。
func (s *UpdateStateStore) WriteUpdateCache(date, tag string) error {
	path := filepath.Join(s.conductorHome, updateCacheFileName)
	if err := os.MkdirAll(s.conductorHome, dirPerm); err != nil {
		return fmt.Errorf("ディレクトリ %s の作成に失敗しました: %w", s.conductorHome, err)
	}
	return writeFileAtomic(path, []byte(date+" "+tag+"\n"))
}

// readTrimmed は conductorHome 直下のファイルを読んで前後の空白を落とす。
func (s *UpdateStateStore) readTrimmed(name string) string {
	b, err := os.ReadFile(filepath.Join(s.conductorHome, name)) //nolint:gosec // CONDUCTOR_HOME 直下の固定ファイル名
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(b))
}
