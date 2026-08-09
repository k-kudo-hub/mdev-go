// Package store は pending / registry / config のファイル入出力を担当する。
// internal/app が定義する port の実装(adapter)である(ADR-0002)。
package store

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/k-kudo-hub/mdev-go/internal/domain"
)

// pendingDirName は pending を置くホームディレクトリ直下のディレクトリ名。
const pendingDirName = ".claude-pending"

// PendingRoot は pending の置き場所を返す。
//
// 現行 Shell 版と同じく CONDUCTOR_HOME に依存せずホームディレクトリ直下に固定する。
// hook は conductor の外にある Claude Code セッションでも発火するため、
// conductor の設置場所と無関係に一意に決まる必要があるためである。
func PendingRoot(home string) string {
	return filepath.Join(home, pendingDirName)
}

// PendingStore は pending ファイルを読み書きする app.PendingStore の実装である。
// レイアウトは <root>/<zellij セッション名>/<エージェントのセッション ID>.json。
type PendingStore struct {
	root string
}

// NewPendingStore は root 配下を使う PendingStore を返す。
// root には PendingRoot の戻り値を渡す。
func NewPendingStore(root string) *PendingStore {
	return &PendingStore{root: root}
}

// path は (session, sessionID) の pending ファイルのパスを返す。
func (s *PendingStore) path(session, sessionID string) string {
	return filepath.Join(s.root, session, sessionID+".json")
}

// Event は pending の event を返す。
// ファイルが無い場合、読めない場合、JSON が壊れている場合はいずれも空文字を返す
// (現行版が `jq -r '.event' ... 2>/dev/null` で空文字に潰していた挙動と同じ)。
func (s *PendingStore) Event(session, sessionID string) string {
	b, err := os.ReadFile(s.path(session, sessionID))
	if err != nil {
		return ""
	}
	var pending domain.Pending
	if err := json.Unmarshal(b, &pending); err != nil {
		return ""
	}
	return pending.Event
}

// Save は pending を書き込む。同一ディレクトリに一時ファイルを作ってから
// rename するため、並行して読む側が書きかけの内容を見ることはない。
func (s *PendingStore) Save(session, sessionID string, pending domain.Pending) error {
	b, err := json.Marshal(pending)
	if err != nil {
		return fmt.Errorf("pending の JSON 化に失敗しました: %w", err)
	}
	// 現行版は jq の出力をリダイレクトしており末尾に改行が付く。差分を出さない
	// ようにここでも改行を付ける。
	return writeFileAtomic(s.path(session, sessionID), append(b, '\n'))
}

// Delete は pending を削除する。存在しない場合も成功として扱う
// (現行版の `rm -f` 相当)。
func (s *PendingStore) Delete(session, sessionID string) error {
	if err := os.Remove(s.path(session, sessionID)); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("pending の削除に失敗しました: %w", err)
	}
	return nil
}
