package store

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/k-kudo-hub/mdev-go/internal/domain"
)

// registryDirName は CONDUCTOR_HOME 直下のレジストリディレクトリ名。
const registryDirName = "tasks"

// entrySuffix はレジストリエントリの拡張子。
const entrySuffix = ".json"

// RegistryRoot はタスクレジストリの置き場所を返す。
// 現行 registry-lib.sh の `$CONDUCTOR_HOME/tasks` に対応する。
func RegistryRoot(conductorHome string) string {
	return filepath.Join(conductorHome, registryDirName)
}

// RegistryStore はタスクレジストリを読み書きする app.RegistryStore の実装である。
// レイアウトは <root>/<zellij セッション名>/<エージェントのセッション ID>.json。
//
// pending がユーザーの応答待ちの間だけ存在するのに対し、レジストリのエントリは
// タスクの生存期間中ずっと残る。zellij セッションが落ちた後にタスクタブを
// --resume 付きで再構築するために使う。
type RegistryStore struct {
	root string
}

// NewRegistryStore は root 配下を使う RegistryStore を返す。
// root には RegistryRoot の戻り値を渡す。
func NewRegistryStore(root string) *RegistryStore {
	return &RegistryStore{root: root}
}

// sessionDir はセッションのエントリを置くディレクトリを返す。
func (s *RegistryStore) sessionDir(session string) string {
	return filepath.Join(s.root, session)
}

// Upsert は (Session, ClaudeSessionID) のエントリを作成または完全上書きする。
// Session か ClaudeSessionID が空の場合は何もしない(現行版と同じ)。
func (s *RegistryStore) Upsert(entry domain.RegistryEntry) error {
	if entry.Session == "" || entry.ClaudeSessionID == "" {
		return nil
	}
	b, err := json.Marshal(entry)
	if err != nil {
		return fmt.Errorf("レジストリエントリの JSON 化に失敗しました: %w", err)
	}
	path := filepath.Join(s.sessionDir(entry.Session), entry.ClaudeSessionID+entrySuffix)
	return writeFileAtomic(path, append(b, '\n'))
}

// List はセッションのエントリを、ファイル名の昇順で返す。
//
// 1 ファイルずつ検証し、壊れているものは黙って読み飛ばす。全件をまとめて
// 読む方式(`jq -s`)では 1 件の破損が復元処理全体を止めてしまうためで、
// 現行 restore-session.sh も同じ理由で 1 ファイルずつ検証している。
func (s *RegistryStore) List(session string) ([]domain.RegistryEntry, error) {
	paths, err := s.entryPaths(session)
	if err != nil {
		return nil, err
	}

	entries := make([]domain.RegistryEntry, 0, len(paths))
	for _, path := range paths {
		entry, ok := readEntry(path)
		if !ok {
			continue
		}
		entries = append(entries, entry)
	}
	return entries, nil
}

// Remove は 1 件のエントリを削除する。存在しない場合も成功として扱う。
// session か sid が空の場合は何もしない(現行版と同じ)。
func (s *RegistryStore) Remove(session, sessionID string) error {
	if session == "" || sessionID == "" {
		return nil
	}
	path := filepath.Join(s.sessionDir(session), sessionID+entrySuffix)
	if err := os.Remove(path); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("レジストリエントリの削除に失敗しました: %w", err)
	}
	return nil
}

// LatestByTabMtime はタブ名が一致するエントリのうち、ファイルの更新時刻が
// 最も新しい 1 件を返す。
//
// 現行 screen-detect-lib.sh の `_screen_registry_lookup` に対応する。
//
//	t=$(stat -f %m "$f" 2>/dev/null || echo 0)
//	if [[ "$t" -ge "$best_t" ]]; then best="$f"; best_t="$t"; fi
//
// 比較が `-ge`(以上)なので、同じ更新時刻なら**後に見たもの**が勝つ。
// 現行版の走査順は glob(ファイル名の昇順)なので、ここでも同じ並びで
// 走査して同着の扱いを揃える。
//
// **選択キーが復元処理(updated_at)と違う**点に注意。この非対称は現行仕様を
// そのまま維持したものである(evidence §2-6)。更新時刻が読めないファイルは
// 現行版と同じく 0 として扱う。
func (s *RegistryStore) LatestByTabMtime(session, tab string) (domain.RegistryEntry, bool) {
	paths, err := s.entryPaths(session)
	if err != nil {
		return domain.RegistryEntry{}, false
	}

	var (
		best  domain.RegistryEntry
		found bool
		bestT int64
	)
	for _, path := range paths {
		entry, ok := readEntry(path)
		if !ok || entry.Tab != tab {
			continue
		}
		modified := int64(0)
		if info, statErr := os.Stat(path); statErr == nil {
			modified = info.ModTime().Unix()
		}
		if found && modified < bestT {
			continue
		}
		best, bestT, found = entry, modified, true
	}
	return best, found
}

// RemoveByTab はタブ名が一致するエントリをすべて削除する。
// pending が無くセッション ID が分からない削除経路で使う。
// session か tab が空の場合は何もしない(現行版と同じ)。
func (s *RegistryStore) RemoveByTab(session, tab string) error {
	if session == "" || tab == "" {
		return nil
	}
	paths, err := s.entryPaths(session)
	if err != nil {
		return err
	}
	for _, path := range paths {
		// 読めないエントリはタブ名を判定できないため残す(現行版と同じ)。
		if entry, ok := readEntry(path); !ok || entry.Tab != tab {
			continue
		}
		if err := os.Remove(path); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return fmt.Errorf("レジストリエントリの削除に失敗しました: %w", err)
		}
	}
	return nil
}

// entryPaths はセッションディレクトリ直下の .json ファイルをファイル名の昇順で返す。
// ディレクトリが無い場合は空を返す(まだ 1 件も登録されていない状態)。
func (s *RegistryStore) entryPaths(session string) ([]string, error) {
	dir := s.sessionDir(session)
	dirEntries, err := os.ReadDir(dir)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("レジストリ %s の読み取りに失敗しました: %w", dir, err)
	}

	paths := make([]string, 0, len(dirEntries))
	for _, e := range dirEntries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), entrySuffix) {
			continue
		}
		paths = append(paths, filepath.Join(dir, e.Name()))
	}
	return paths, nil
}

// readEntry は 1 件のエントリを読む。読めない・壊れている場合は ok=false を返す。
func readEntry(path string) (domain.RegistryEntry, bool) {
	b, err := os.ReadFile(path) //nolint:gosec // レジストリ配下の列挙結果のみを渡す
	if err != nil {
		return domain.RegistryEntry{}, false
	}
	var entry domain.RegistryEntry
	if err := json.Unmarshal(b, &entry); err != nil {
		return domain.RegistryEntry{}, false
	}
	return entry, true
}
