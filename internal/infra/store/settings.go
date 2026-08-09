package store

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/k-kudo-hub/mdev-go/internal/app"
)

// Claude Code のユーザー設定ファイルの置き場所。
// 公式ドキュメント(https://code.claude.com/docs/en/settings)では
// `~/.claude/settings.json` に固定されており、置き場所を変える環境変数は無い。
const (
	claudeDirName    = ".claude"
	settingsFileName = "settings.json"
)

// settingsBackupPrefix は mdev が作るバックアップのファイル名の前置きである。
// 前置きで自分の作ったバックアップだけを見分け、他ツールや利用者が置いた
// settings.json.bak などには触れない。
const settingsBackupPrefix = settingsFileName + ".mdev-backup-"

// settingsBackupTimeLayout はバックアップ名に使う UTC タイムスタンプの形式。
// 辞書順と時系列が一致するため、最新のバックアップは名前の最大値で選べる。
const settingsBackupTimeLayout = "20060102T150405Z"

// ClaudeSettingsPath は Claude Code のユーザー設定ファイルのパスを返す。
func ClaudeSettingsPath(home string) string {
	return filepath.Join(home, claudeDirName, settingsFileName)
}

// SettingsStore は settings.json を読み書きする app.SettingsStore の実装である。
//
// 書き込みは同一ディレクトリでの原子的な置き換えで行い、既存ファイルの
// パーミッションを引き継ぐ。利用者が権限を絞っている設定ファイルを
// mdev の都合で緩めないためである。
type SettingsStore struct {
	path  string
	clock app.Clock
}

// NewSettingsStore は path の settings.json を扱う SettingsStore を返す。
// path には ClaudeSettingsPath の戻り値を渡す。
func NewSettingsStore(path string, clock app.Clock) *SettingsStore {
	return &SettingsStore{path: path, clock: clock}
}

// Path は settings.json のパスを返す。
func (s *SettingsStore) Path() string { return s.path }

// Read は settings.json の内容をそのまま返す。
func (s *SettingsStore) Read() ([]byte, error) {
	b, err := os.ReadFile(s.path) //nolint:gosec // 呼び出し側が決めた設定ファイルのパス
	if err != nil {
		return nil, fmt.Errorf("設定ファイル %s の読み取りに失敗しました: %w", s.path, err)
	}
	return b, nil
}

// Backup は data を settings.json と同じディレクトリへ退避し、そのパスを返す。
//
// ファイル名は settings.json.mdev-backup-<UTC タイムスタンプ(秒)> である。
// 同じ秒に 2 回作られた場合は上書きになるが、Switch は変更がある場合にしか
// 退避しないため、同じ秒の 2 回目は「1 回目より新しい切り替え前の内容」であり、
// 最新として上書きされるのが正しい。
func (s *SettingsStore) Backup(data []byte) (string, error) {
	name := settingsBackupPrefix + s.clock.Now().UTC().Format(settingsBackupTimeLayout)
	path := filepath.Join(filepath.Dir(s.path), name)
	if err := writeFileAtomicMode(path, data, s.mode()); err != nil {
		return "", fmt.Errorf("設定ファイルの退避に失敗しました: %w", err)
	}
	return path, nil
}

// Write は settings.json を data で原子的に置き換える。
func (s *SettingsStore) Write(data []byte) error {
	if err := writeFileAtomicMode(s.path, data, s.mode()); err != nil {
		return fmt.Errorf("設定ファイル %s の書き込みに失敗しました: %w", s.path, err)
	}
	return nil
}

// LatestBackup は最も新しいバックアップのパスと内容を返す。
// バックアップが 1 つも無い場合(ディレクトリごと無い場合を含む)は
// found=false を返す。switch を実行していない状態は異常ではないためである。
func (s *SettingsStore) LatestBackup() (string, []byte, bool, error) {
	dir := filepath.Dir(s.path)
	entries, err := os.ReadDir(dir)
	if errors.Is(err, fs.ErrNotExist) {
		return "", nil, false, nil
	}
	if err != nil {
		return "", nil, false, fmt.Errorf("ディレクトリ %s の一覧に失敗しました: %w", dir, err)
	}

	var names []string
	for _, e := range entries {
		if e.IsDir() || !strings.HasPrefix(e.Name(), settingsBackupPrefix) {
			continue
		}
		names = append(names, e.Name())
	}
	if len(names) == 0 {
		return "", nil, false, nil
	}
	// タイムスタンプの形式が辞書順 = 時系列であるため、名前の最大値が最新。
	sort.Strings(names)

	path := filepath.Join(dir, names[len(names)-1])
	data, err := os.ReadFile(path) //nolint:gosec // 自分が作ったバックアップ
	if err != nil {
		return "", nil, false, fmt.Errorf("バックアップ %s の読み取りに失敗しました: %w", path, err)
	}
	return path, data, true, nil
}

// mode は settings.json の現在のパーミッションを返す。
// ファイルがまだ無い場合は他のファイルと同じ既定値を使う。
func (s *SettingsStore) mode() os.FileMode {
	info, err := os.Stat(s.path)
	if err != nil {
		return filePerm
	}
	return info.Mode().Perm()
}
