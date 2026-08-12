package store

import (
	"os"
	"path/filepath"

	"github.com/k-kudo-hub/mdev-go/internal/app"
)

// SessionTraceStore は mdev がセッション名で残した痕跡を探す
// app.SessionTraceChecker の実装である。
//
// 探すのは 2 か所で、どちらもセッション名のディレクトリである。
//
//   - レジストリ(CONDUCTOR_HOME/tasks/<セッション名>/)
//   - pending(~/.claude-pending/<セッション名>/)
//
// mdev が一度でもそのセッションでタスクを扱っていれば、どちらかが残る。
type SessionTraceStore struct {
	registryRoot string
	pendingRoot  string
}

var _ app.SessionTraceChecker = (*SessionTraceStore)(nil)

// NewSessionTraceStore は痕跡を探す SessionTraceStore を返す。
// 引数には RegistryRoot / PendingRoot の戻り値を渡す。
func NewSessionTraceStore(registryRoot, pendingRoot string) *SessionTraceStore {
	return &SessionTraceStore{registryRoot: registryRoot, pendingRoot: pendingRoot}
}

// HasTrace は mdev がそのセッションを扱った跡があるかを返す。
//
// 判断できない(読めない)場合は false を返す。痕跡が無いものとして扱えば
// 掃除の対象から外れるので、安全側へ倒れる。
func (s *SessionTraceStore) HasTrace(session string) bool {
	if session == "" {
		return false
	}
	for _, root := range []string{s.registryRoot, s.pendingRoot} {
		if root == "" {
			continue
		}
		info, err := os.Stat(filepath.Join(root, session))
		if err == nil && info.IsDir() {
			return true
		}
	}
	return false
}
