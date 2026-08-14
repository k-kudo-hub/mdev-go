package store

import (
	"os"
	"path/filepath"

	"github.com/k-kudo-hub/mdev-go/internal/app"
)

var _ app.DirLister = (*PaneStore)(nil)

// ListDirs は roots 配下のディレクトリを深さ depth まで列挙する。
//
// 現行 task-create-loop.sh の `fd --type d --max-depth <depth> . <roots...>` を
// 置き換えるものである。fd への外部依存を無くすため自前で掘る。
//
// fd の既定と揃えている点:
//
//   - 掘るのはディレクトリだけ(--type d)
//   - ドットで始まる名前は候補にも探索先にもしない(隠しファイルの除外)
//   - シンボリックリンクは辿らない(DirEntry.IsDir はリンクでは偽になる)
//   - 深さは root の直下を 1 として depth まで(--max-depth)
//
// 違う点は 2 つある。fd は .gitignore を解釈するが、ここでは解釈しない
// (既定の深さ 1 では結果が変わらない)。並びは fd が並列走査のため不定だが、
// ここでは root の順 → 名前の昇順に固定する(選択 UI の並びが実行のたびに
// 変わらないようにするため)。どちらも evidence に記録している。
//
// 読めない root は黙って飛ばす(現行版も `2>/dev/null` で握り潰す)。
func (s *PaneStore) ListDirs(roots []string, depth int) []string {
	dirs := []string{}
	for _, root := range roots {
		dirs = appendChildDirs(dirs, root, 1, depth)
	}
	return dirs
}

// appendChildDirs は dir の直下のディャレクトリを level から depth まで集める。
func appendChildDirs(dirs []string, dir string, level, depth int) []string {
	if level > depth {
		return dirs
	}
	// os.ReadDir は名前の昇順で返す。
	entries, err := os.ReadDir(dir)
	if err != nil {
		return dirs
	}
	for _, entry := range entries {
		if !entry.IsDir() || isHiddenName(entry.Name()) {
			continue
		}
		path := filepath.Join(dir, entry.Name())
		dirs = append(dirs, path)
		dirs = appendChildDirs(dirs, path, level+1, depth)
	}
	return dirs
}

// isHiddenName はドットで始まる名前かどうかを返す。
func isHiddenName(name string) bool {
	return len(name) > 0 && name[0] == '.'
}

// IsDir は path が実在するディレクトリかどうかを返す。
// 現行 task-create-loop.sh が起点を選ぶときの `[[ -d "$expanded" ]]` に対応する。
func (s *PaneStore) IsDir(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}

// codexHomeDirName は CODEX_HOME 未設定時のホーム直下のディレクトリ名。
const codexHomeDirName = ".codex"

// CodexConfigPath は codex の設定ファイルの場所を返す。
// 現行版の `${CODEX_HOME:-$HOME/.codex}/config.toml` に対応する。
func CodexConfigPath(codexHome, home string) string {
	if codexHome == "" {
		codexHome = filepath.Join(home, codexHomeDirName)
	}
	return filepath.Join(codexHome, "config.toml")
}
