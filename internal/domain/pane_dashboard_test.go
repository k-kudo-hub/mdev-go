package domain_test

import (
	"strings"
	"testing"

	"github.com/k-kudo-hub/mdev-go/internal/domain"
)

// 現行 dashboard-loop.sh の ONCE 出力を隔離環境で実測して写したもの。
// 生成コマンド(evidence の「Dashboard 実測」を参照):
//
//	MOCK_TABS="beta alpha" ZELLIJ_SESSION_NAME=s1 CONDUCTOR_DASHBOARD_ONCE=1 \
//	  bash dashboard-loop.sh
const wantDashboardTwoItems = "\x1b[1m  Current Tasks\x1b[0m \x1b[2m[s1]\x1b[0m\n" +
	"\x1b[2m  ──────────────────────────\x1b[0m\n" +
	"\n" +
	"  \x1b[0;33m[1]\x1b[0m \x1b[0;32m■\x1b[0m \x1b[1mbeta\x1b[0m \x1b[2m[10:01:00]\x1b[0m done\n" +
	"      turn finished\n" +
	"\n" +
	"  \x1b[0;33m[2]\x1b[0m \x1b[0;31m■\x1b[0m \x1b[1malpha\x1b[0m \x1b[2m[10:00:00]\x1b[0m\n" +
	"      needs permission\n" +
	"\n" +
	"\x1b[2m  ──────────────────────────\x1b[0m\n" +
	"  \x1b[1mPending: 2\x1b[0m  \x1b[2m[num]: jump / d+[num]: delete\x1b[0m\n" +
	"\x1b[2m  ──────────────────────────\x1b[0m\n"

const wantDashboardEmpty = "\x1b[1m  Current Tasks\x1b[0m \x1b[2m[s1]\x1b[0m\n" +
	"\x1b[2m  ──────────────────────────\x1b[0m\n" +
	"\n" +
	"  \x1b[0;32mAll tasks running\x1b[0m\n" +
	"\n" +
	"\x1b[2m  ──────────────────────────\x1b[0m\n"

func TestRenderDashboard(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		input domain.DashboardInput
		want  string
	}{
		{
			name: "Stop は緑の done 付き / それ以外は赤",
			input: domain.DashboardInput{
				Session: "s1",
				Items: []domain.PendingView{
					{Tab: "beta", Event: "Stop", Message: "turn finished", Time: "10:01:00"},
					{Tab: "alpha", Event: "Notification", Message: "needs permission", Time: "10:00:00"},
				},
			},
			want: wantDashboardTwoItems,
		},
		{
			name:  "空なら All tasks running と区切り線 1 本",
			input: domain.DashboardInput{Session: "s1"},
			want:  wantDashboardEmpty,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := domain.RenderDashboard(tt.input); got != tt.want {
				t.Errorf("RenderDashboard() の出力が違う\n got: %q\nwant: %q", got, tt.want)
			}
		})
	}
}

func TestRenderDashboardTruncatesMessageToSixtyBytes(t *testing.T) {
	t.Parallel()

	long := strings.Repeat("x", 70)
	got := domain.RenderDashboard(domain.DashboardInput{
		Session: "s1",
		Items: []domain.PendingView{
			{Tab: "alpha", Event: "Stop", Message: long, Time: "10:00:00"},
		},
	})

	wantLine := "      " + strings.Repeat("x", 60)
	if !strings.Contains(got, wantLine+"\n") {
		t.Errorf("message が 60 バイトに切られていない: %q", got)
	}
	if strings.Contains(got, strings.Repeat("x", 61)) {
		t.Errorf("61 バイト以上出力されている: %q", got)
	}
}

func TestRenderDashboardNumbersItemsFromOne(t *testing.T) {
	t.Parallel()

	items := make([]domain.PendingView, 0, 12)
	for range 12 {
		items = append(items, domain.PendingView{Tab: "t", Event: "Stop"})
	}
	got := domain.RenderDashboard(domain.DashboardInput{Session: "s1", Items: items})

	// 10 件を超えても番号は振られ続ける(押せるのは 1-9 だけという非対称も現行通り)。
	for _, want := range []string{"[1]", "[9]", "[10]", "[12]"} {
		if !strings.Contains(got, "\x1b[0;33m"+want+"\x1b[0m") {
			t.Errorf("番号 %s が出ていない", want)
		}
	}
	if !strings.Contains(got, "\x1b[1mPending: 12\x1b[0m") {
		t.Errorf("件数が 12 になっていない: %q", got)
	}
}
