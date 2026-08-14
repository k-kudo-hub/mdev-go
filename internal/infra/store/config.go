package store

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/k-kudo-hub/mdev-go/assets"
	"github.com/k-kudo-hub/mdev-go/internal/domain"
)

// 設定ファイル名。ユーザーが置いた config.json を優先し、無ければ同梱の
// config.default.json を使う。
const (
	configFileName        = "config.json"
	defaultConfigFileName = "config.default.json"
)

// conductorHomeDirName は CONDUCTOR_HOME 未設定時のホーム直下のディレクトリ名。
const conductorHomeDirName = ".claude-conductor"

// ConductorHome は conductor の設置場所を返す。
// 現行版の `${CONDUCTOR_HOME:-$HOME/.claude-conductor}` に対応する。
func ConductorHome(home, envValue string) string {
	if envValue != "" {
		return envValue
	}
	return filepath.Join(home, conductorHomeDirName)
}

// ConfigPath は読み込む設定ファイルのパスを返す。
//
// 現行 task-lib.sh の load_config と同じく、config.json があればそれを、
// 無ければ config.default.json を返す。キー単位のマージは行わない
// ファイル単位のフォールバックである。config.json を置いたユーザーは
// その 1 ファイルだけが有効になる。
func ConfigPath(conductorHome string) string {
	path := filepath.Join(conductorHome, configFileName)
	if _, err := os.Stat(path); err == nil {
		return path
	}
	return filepath.Join(conductorHome, defaultConfigFileName)
}

// LoadConfig は設定を読み込む。
//
// どちらのファイルも無い場合は、実行ファイルに埋め込まれた
// config.default.json を使う。設置の前や、設置物が欠けた状態でも既定の
// 設定で動けるようにするためである(埋め込みは assets を参照)。
//
// ファイルはあるが JSON が壊れている場合はエラーを返す。設定の破損は
// 利用者が直すべき状態であり、黙って既定値で動くと料金計算などが
// 静かに誤るためである。
func LoadConfig(conductorHome string) (domain.Config, error) {
	path := ConfigPath(conductorHome)
	b, err := os.ReadFile(path) //nolint:gosec // CONDUCTOR_HOME 配下の固定ファイル名
	if errors.Is(err, fs.ErrNotExist) {
		embedded, ok := assets.Read(defaultConfigFileName)
		if !ok {
			return domain.Config{}, nil
		}
		b, path = embedded, "(同梱の "+defaultConfigFileName+")"
	} else if err != nil {
		return domain.Config{}, fmt.Errorf("設定ファイル %s の読み取りに失敗しました: %w", path, err)
	}

	var cfg domain.Config
	if err := json.Unmarshal(b, &cfg); err != nil {
		return domain.Config{}, fmt.Errorf("設定ファイル %s の解釈に失敗しました: %w", path, err)
	}
	return cfg, nil
}
