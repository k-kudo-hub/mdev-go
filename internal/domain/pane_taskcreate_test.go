package domain_test

import (
	"testing"

	"github.com/k-kudo-hub/mdev-go/internal/domain"
)

func TestRenderTaskCreateMenu(t *testing.T) {
	t.Parallel()

	// task-create-loop.sh の main_loop 冒頭をバイト列で固定する。
	// 区切り線は 26 本(Dashboard / Waiting / Done と同じ)。
	want := "\033[1m  New Task\033[0m  \033[2m[my-session]\033[0m\n" +
		"\033[2m  ──────────────────────────\033[0m\n" +
		"\n" +
		"  \033[2m[n]\033[0m Create task\n" +
		"\n"
	if got := domain.RenderTaskCreateMenu("my-session"); got != want {
		t.Errorf("RenderTaskCreateMenu() = %q\nwant %q", got, want)
	}
}

func TestRenderTaskCreateError(t *testing.T) {
	t.Parallel()

	// 現行版の `echo -e "  ${RED}検索対象ディレクトリが見つかりません${NC}"`。
	want := "  \033[0;31m" + domain.TaskCreateSearchDirsMissing + "\033[0m\n"
	if got := domain.RenderTaskCreateError(domain.TaskCreateSearchDirsMissing); got != want {
		t.Errorf("RenderTaskCreateError() = %q\nwant %q", got, want)
	}
}
