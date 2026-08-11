package store

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/k-kudo-hub/mdev-go/internal/app"
)

var _ app.PendingRawStore = (*PendingStore)(nil)

// FindRawByTab は session の pending からタブ名が一致する 1 件を生のまま返す。
//
// FindByTab と同じ選び方(ファイル名の昇順で最初の一致、読めない・壊れている
// ものは読み飛ばす)をするが、中身を構造体へ写さずバイト列のまま返す。
// Waiting の切り替えは pending の一部のキーだけを書き換えるもので、mdev が
// 知らないキーを落としてはならない(現行版の jq も知らないキーを保つ)。
func (s *PendingStore) FindRawByTab(session, tab string) (string, []byte, bool, error) {
	dir := filepath.Join(s.root, session)
	entries, err := os.ReadDir(dir)
	if errors.Is(err, fs.ErrNotExist) {
		return "", nil, false, nil
	}
	if err != nil {
		return "", nil, false, fmt.Errorf("pending %s の読み取りに失敗しました: %w", dir, err)
	}

	// os.ReadDir はファイル名の昇順で返すため、並べ替えは要らない。
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), pendingSuffix) {
			continue
		}
		b, err := os.ReadFile(filepath.Join(dir, entry.Name())) //nolint:gosec // 列挙結果のパス
		if err != nil {
			continue
		}
		var probe struct {
			Tab string `json:"tab"`
		}
		if err := json.Unmarshal(b, &probe); err != nil || probe.Tab != tab {
			continue
		}
		return entry.Name(), b, true, nil
	}
	return "", nil, false, nil
}

// WriteRaw は name の pending を data で置き換える。
//
// 同一ディレクトリに一時ファイルを作ってから rename するため、並行して読む側
// (ダッシュボードのポーリング)が書きかけの内容を見ることはない。
// 現行版は jq の出力をリダイレクトしており末尾に改行が付くので、ここでも足す。
func (s *PendingStore) WriteRaw(session, name string, data []byte) error {
	return writeFileAtomic(filepath.Join(s.root, session, name), append(data, '\n'))
}
