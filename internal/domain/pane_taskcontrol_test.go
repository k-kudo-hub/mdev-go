package domain_test

import (
	"testing"

	"github.com/k-kudo-hub/mdev-go/internal/domain"
)

// task-control.sh の render_bar をバイト列で固定する。
//
//	Waiting: echo -e "${YELLOW}${BOLD}  ● WAITING${NC}${DIM}  |  m: Main  |  w: Resume  |  dd: Delete tab${NC}"
//	通常   : echo -e "${DIM}  m: Main  |  w: Waiting  |  dd: Delete tab${NC}"
//
// 色は task-control.sh の定義(YELLOW='\033[0;33m' BOLD='\033[1m'
// DIM='\033[2m' NC='\033[0m')。echo -e なので末尾に改行が 1 つ付く。

func TestRenderTaskControlBar(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		waiting bool
		want    string
	}{
		{
			name:    "通常",
			waiting: false,
			want:    "\033[2m  m: Main  |  w: Waiting  |  dd: Delete tab\033[0m\n",
		},
		{
			name:    "WAITING",
			waiting: true,
			want: "\033[0;33m\033[1m  ● WAITING\033[0m\033[2m" +
				"  |  m: Main  |  w: Resume  |  dd: Delete tab\033[0m\n",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := domain.RenderTaskControlBar(tc.waiting); got != tc.want {
				t.Errorf("RenderTaskControlBar(%v) = %q, want %q", tc.waiting, got, tc.want)
			}
		})
	}
}

func TestTaskControlWaiting(t *testing.T) {
	t.Parallel()

	// current_event は pending の event をそのまま返し、render_bar は
	// それが "Waiting" かどうかだけを見る。
	tests := []struct {
		event string
		want  bool
	}{
		{domain.EventWaiting, true},
		{domain.EventNotification, false},
		{domain.EventStop, false},
		{"", false},
		{"waiting", false},
	}
	for _, tc := range tests {
		t.Run(tc.event, func(t *testing.T) {
			t.Parallel()
			if got := domain.TaskControlWaiting(tc.event); got != tc.want {
				t.Errorf("TaskControlWaiting(%q) = %v, want %v", tc.event, got, tc.want)
			}
		})
	}
}
