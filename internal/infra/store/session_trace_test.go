package store_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/k-kudo-hub/mdev-go/internal/infra/store"
)

// TestSessionTraceStore は mdev が扱った跡の探し方を固定する。
//
// 跡があるかどうかで、終了済みセッションのメタデータを消してよいかが
// 決まる。緩めると利用者が手で作ったセッションの復活先を消してしまう。
func TestSessionTraceStore(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	registryRoot := filepath.Join(root, "conductor", "tasks")
	pendingRoot := filepath.Join(root, "home", ".claude-pending")
	for _, dir := range []string{
		filepath.Join(registryRoot, "from-registry"),
		filepath.Join(pendingRoot, "from-pending"),
	} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	// ディレクトリではなくファイルは跡とみなさない。
	if err := os.WriteFile(filepath.Join(registryRoot, "is-a-file"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}

	s := store.NewSessionTraceStore(registryRoot, pendingRoot)
	tests := []struct {
		session string
		want    bool
	}{
		{session: "from-registry", want: true},
		{session: "from-pending", want: true},
		{session: "never-seen", want: false},
		{session: "is-a-file", want: false},
		{session: "", want: false},
	}
	for _, tt := range tests {
		t.Run(tt.session, func(t *testing.T) {
			t.Parallel()
			if got := s.HasTrace(tt.session); got != tt.want {
				t.Errorf("HasTrace(%q) = %v, want %v", tt.session, got, tt.want)
			}
		})
	}
}

// TestSessionTraceStoreWithMissingRoots は置き場が無い環境で跡なしと
// 判断することを確かめる(掃除の対象から外れる = 安全側)。
func TestSessionTraceStoreWithMissingRoots(t *testing.T) {
	t.Parallel()

	s := store.NewSessionTraceStore(filepath.Join(t.TempDir(), "none"), "")
	if s.HasTrace("any") {
		t.Error("置き場が無いのに跡ありと判断しました")
	}
}
