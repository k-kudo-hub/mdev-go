package store

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/k-kudo-hub/mdev-go/internal/app"
	"github.com/k-kudo-hub/mdev-go/internal/domain"
)

// screenStateDirName は pending ディレクトリ配下のスクリーン検出状態の置き場。
// 現行版の `$PENDING_DIR/.screen-state/<slug>` に対応する。
const screenStateDirName = ".screen-state"

// newsDirName は CONDUCTOR_HOME 直下のニュース置き場。
const newsDirName = "news"

// newsSuffix はニュースファイルの拡張子。
const newsSuffix = ".json"

// PaneStore はペインが使うファイル入出力をまとめて担当する。
//
// pending はホーム直下(CONDUCTOR_HOME と無関係)、daily とニュースは
// CONDUCTOR_HOME 配下と置き場所の規約が違うため、両方の根を持つ。
type PaneStore struct {
	pendingRoot   string
	conductorHome string
}

var (
	_ app.PendingLister      = (*PaneStore)(nil)
	_ app.PendingRemover     = (*PaneStore)(nil)
	_ app.ScreenStateRemover = (*PaneStore)(nil)
	_ app.DailyReader        = (*PaneStore)(nil)
	_ app.NewsReader         = (*PaneStore)(nil)
	_ app.ConfigLoader       = (*PaneStore)(nil)
)

// NewPaneStore はペイン用のストアを返す。
// pendingRoot には PendingRoot の戻り値を渡す。
func NewPaneStore(pendingRoot, conductorHome string) *PaneStore {
	return &PaneStore{pendingRoot: pendingRoot, conductorHome: conductorHome}
}

// sessionDir は session の pending ディレクトリを返す。
func (s *PaneStore) sessionDir(session string) string {
	return filepath.Join(s.pendingRoot, session)
}

// List は session の pending をファイル名の昇順で読む。
//
// 現行版の glob `"$PENDING_DIR"/*.json` に対応する。読めないファイルや壊れた
// JSON も要素としては残す(domain.ParsePendingView が全フィールドを空文字に
// 潰し、タブ名の一致判定から外れる)。ディレクトリが無い場合は空を返す。
func (s *PaneStore) List(session string) ([]domain.PendingView, error) {
	dir := s.sessionDir(session)
	entries, err := os.ReadDir(dir)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("pending %s の読み取りに失敗しました: %w", dir, err)
	}

	// os.ReadDir はファイル名の昇順で返す。現行の glob はロケールの照合順序に
	// 従うが、pending のファイル名はエージェントのセッション ID(ASCII)なので
	// どちらでも同じ並びになる。
	views := make([]domain.PendingView, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), pendingSuffix) {
			continue
		}
		b, err := os.ReadFile(filepath.Join(dir, entry.Name())) //nolint:gosec // 列挙結果のパス
		if err != nil {
			// 読めないファイルは中身が空だったのと同じ扱いにする。
			b = nil
		}
		views = append(views, domain.ParsePendingView(entry.Name(), b))
	}
	return views, nil
}

// DeleteByTab は tab に一致する pending をすべて削除する。
//
// 同じタブに複数の pending が残ることがある(--resume での再開でエージェントの
// セッション ID が変わるため)。現行版もタブ名の一致するものを全部消している。
func (s *PaneStore) DeleteByTab(session, tab string) error {
	views, err := s.List(session)
	if err != nil {
		return err
	}
	for _, view := range views {
		if view.Tab != tab {
			continue
		}
		if err := s.DeleteByName(session, view.Name); err != nil {
			return err
		}
	}
	return nil
}

// DeleteByName は pending をファイル名で 1 件削除する。
// 存在しない場合も成功として扱う(現行版の `rm -f` 相当)。
func (s *PaneStore) DeleteByName(session, name string) error {
	path := filepath.Join(s.sessionDir(session), name)
	if err := os.Remove(path); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("pending %s の削除に失敗しました: %w", path, err)
	}
	return nil
}

// Remove はスクリーン検出の状態ファイルを削除する。
// 存在しない場合も成功として扱う。
func (s *PaneStore) Remove(session, slug string) error {
	path := filepath.Join(s.sessionDir(session), screenStateDirName, slug)
	if err := os.Remove(path); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("スクリーン検出の状態 %s の削除に失敗しました: %w", path, err)
	}
	return nil
}

// ReadToday は date の daily ファイルを全セッションぶん読み、行の並びを返す。
//
// 現行版の `find "$DAILY_BASE" -name "<date>.jsonl" -type f` に対応する。
// 現行の find は探索順(ディレクトリの並び)のままファイルを連結するが、
// ここではセッション名の昇順に固定する。完了時刻が同じエントリ同士の並びが
// 環境によって変わらないようにするためで、差異は evidence に記録している。
func (s *PaneStore) ReadToday(date string) [][]byte {
	base := DailyRoot(s.conductorHome)
	entries, err := os.ReadDir(base)
	if err != nil {
		return nil
	}

	var lines [][]byte
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		path := filepath.Join(base, entry.Name(), date+dailySuffix)
		b, err := os.ReadFile(path) //nolint:gosec // daily の規約どおりのパス
		if err != nil {
			continue
		}
		for _, line := range strings.Split(string(b), "\n") {
			if strings.TrimSpace(line) == "" {
				continue
			}
			lines = append(lines, []byte(line))
		}
	}
	return lines
}

// Read は date のニュースファイルの中身を返す。読めなければ nil を返す。
func (s *PaneStore) Read(date string) []byte {
	path := filepath.Join(s.conductorHome, newsDirName, date+newsSuffix)
	b, err := os.ReadFile(path) //nolint:gosec // news の規約どおりのパス
	if err != nil {
		return nil
	}
	return b
}

// Load は設定を読む。読めなければゼロ値を返す。
//
// 現行 task-lib.sh の agent_detection は `jq ... 2>/dev/null` で失敗を握り潰し、
// 検出方式を既定の "hooks" に落とす。設定が壊れていても画面が出なくなるより
// マシなので、その扱いに合わせている。
func (s *PaneStore) Load() domain.Config {
	config, err := LoadConfig(s.conductorHome)
	if err != nil {
		return domain.Config{}
	}
	return config
}
