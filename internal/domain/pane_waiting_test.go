package domain_test

import (
	"strings"
	"testing"

	"github.com/k-kudo-hub/mdev-go/internal/domain"
)

// 現行 waiting-loop.sh の ONCE 出力を隔離環境で実測して写したもの。
const wantWaitingOneItem = "\x1b[1m  Waiting\x1b[0m \x1b[2m[external]\x1b[0m\n" +
	"\x1b[2m  ──────────────────────────\x1b[0m\n" +
	"\n" +
	"  \x1b[0;33m■\x1b[0m \x1b[1mreview\x1b[0m \x1b[2m[11:01:00]\x1b[0m\n" +
	"      waiting for pr review\n" +
	"\n" +
	"\x1b[2m  ──────────────────────────\x1b[0m\n" +
	"  \x1b[1mWaiting: 1\x1b[0m\n"

const wantWaitingEmpty = "\x1b[1m  Waiting\x1b[0m \x1b[2m[external]\x1b[0m\n" +
	"\x1b[2m  ──────────────────────────\x1b[0m\n" +
	"\n" +
	"  \x1b[2mNo waiting tasks\x1b[0m\n"

func TestRenderWaiting(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		items []domain.PendingView
		want  string
	}{
		{
			// Dashboard と違い番号は振られず、■ は常に黄色である。
			name: "1 件",
			items: []domain.PendingView{
				{Tab: "review", Event: "Waiting", Message: "waiting for pr review", Time: "11:01:00"},
			},
			want: wantWaitingOneItem,
		},
		{
			name:  "0 件なら No waiting tasks で区切り線もフッタも出ない",
			items: nil,
			want:  wantWaitingEmpty,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := domain.RenderWaiting(tt.items); got != tt.want {
				t.Errorf("RenderWaiting() の出力が違う\n got: %q\nwant: %q", got, tt.want)
			}
		})
	}
}

func TestRenderWaitingTruncatesMessageToSixtyBytes(t *testing.T) {
	t.Parallel()

	got := domain.RenderWaiting([]domain.PendingView{
		{Tab: "t", Event: "Waiting", Message: strings.Repeat("y", 70), Time: "11:00:00"},
	})
	if !strings.Contains(got, "      "+strings.Repeat("y", 60)+"\n") {
		t.Errorf("message が 60 バイトに切られていない: %q", got)
	}
	if strings.Contains(got, strings.Repeat("y", 61)) {
		t.Errorf("61 バイト以上出力されている: %q", got)
	}
}

func TestRenderWaitingCountsAllItems(t *testing.T) {
	t.Parallel()

	items := make([]domain.PendingView, 0, 3)
	for range 3 {
		items = append(items, domain.PendingView{Tab: "t", Event: "Waiting"})
	}
	if got := domain.RenderWaiting(items); !strings.Contains(got, "\x1b[1mWaiting: 3\x1b[0m") {
		t.Errorf("件数が 3 になっていない: %q", got)
	}
}
