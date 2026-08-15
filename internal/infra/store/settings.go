package store

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/k-kudo-hub/mdev-go/internal/app"
)

// Claude Code のユーザー設定ファイルの置き場所。
// 公式ドキュメント(https://code.claude.com/docs/en/settings)では
// `~/.claude/settings.json` に固定されており、置き場所を変える環境変数は無い。
const (
	claudeDirName    = ".claude"
	settingsFileName = "settings.json"
)

// settingsBackupInfix は mdev が作るバックアップのファイル名で、
// 対象ファイル名とタイムスタンプの間に入れる目印である。
// この目印で自分の作ったバックアップだけを見分け、他ツールや利用者が置いた
// settings.json.bak の類には触れない。
const settingsBackupInfix = ".mdev-backup-"

// settingsBackupTimeLayout はバックアップ名に使う UTC タイムスタンプの形式。
// 辞書順と時系列が一致するため、最新のバックアップは名前の最大値で選べる。
const settingsBackupTimeLayout = "20060102T150405Z"

// ClaudeSettingsPath は Claude Code のユーザー設定ファイルのパスを返す。
func ClaudeSettingsPath(home string) string {
	return filepath.Join(home, claudeDirName, settingsFileName)
}

// SettingsPath は書き換え対象の settings.json のパスを返す。
//
// envValue(MDEV_SETTINGS_FILE)が空でなければそれを使う。実環境の
// ~/.claude/settings.json へ適用する前に、コピーに対して install を
// 試せるようにするための逃げ道である。
func SettingsPath(home, envValue string) string {
	if envValue != "" {
		return envValue
	}
	return ClaudeSettingsPath(home)
}

// SettingsStore は settings.json を退避する app.SettingsBackup の実装である。
//
// **退避専用である。** 以前は読み書きと復元(最新のバックアップを引く)も
// 持っていたが、読み書きは install が FileStore を通るようになり、復元は
// `mdev hooks restore` ごと廃止した(v0.14)。戻す操作は利用者が cp で行う
// (手順は README に書いてある)。
//
// 退避は同一ディレクトリでの原子的な置き換えで行い、既存ファイルの
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

// Backup は data を settings.json と同じディレクトリへ退避し、そのパスを返す。
//
// ファイル名は <対象ファイル名>.mdev-backup-<UTC タイムスタンプ(秒)> である。
// 同じ秒に 2 回作られた場合は上書きになるが、Switch は変更がある場合にしか
// 退避しないため、同じ秒の 2 回目は「1 回目より新しい書き換え前の内容」であり、
// 最新として上書きされるのが正しい。
func (s *SettingsStore) Backup(data []byte) (string, error) {
	name := s.backupPrefix() + s.clock.Now().UTC().Format(settingsBackupTimeLayout)
	path := filepath.Join(filepath.Dir(s.path), name)
	if err := writeFileAtomicMode(path, data, s.mode()); err != nil {
		return "", fmt.Errorf("設定ファイルの退避に失敗しました: %w", err)
	}
	return path, nil
}

// backupPrefix はバックアップのファイル名の前置きを対象ファイル名から導く。
//
// 固定名にすると、MDEV_SETTINGS_FILE で同じディレクトリ内のコピーを対象に
// 予行演習した場合に、実ファイルの復元がコピーのバックアップを拾ってしまう。
func (s *SettingsStore) backupPrefix() string {
	return filepath.Base(s.path) + settingsBackupInfix
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
