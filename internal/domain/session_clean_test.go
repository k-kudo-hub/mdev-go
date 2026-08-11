package domain_test

import (
	"reflect"
	"testing"

	"github.com/k-kudo-hub/mdev-go/internal/domain"
)

// realSessionList は実機(zellij 0.44.1 / macOS)の出力そのものである。
// 生きているセッションには `]` の後ろに空白が 1 つ残る。
const realSessionList = "mdev-go-224042 [Created 40m 42s ago] \n" +
	"claude-conductor-224231 [Created 12m 50s ago] (EXITED - attach to resurrect)\n" +
	"claude-conductor-215255 [Created 12m 50s ago] (EXITED - attach to resurrect)\n" +
	"kazuto [Created 12m 50s ago] (EXITED - attach to resurrect)\n"

func TestParseSessionList(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		out  string
		want []domain.SessionEntry
	}{
		{
			name: "実機の出力",
			out:  realSessionList,
			want: []domain.SessionEntry{
				{Name: "mdev-go-224042"},
				{Name: "claude-conductor-224231", Exited: true},
				{Name: "claude-conductor-215255", Exited: true},
				{Name: "kazuto", Exited: true},
			},
		},
		{
			// セッションが 1 つも無いときの案内はセッション行ではない。
			name: "セッションが無い",
			out:  "No active zellij sessions found.\n",
			want: nil,
		},
		{name: "空の出力", out: "", want: nil},
		{name: "空行だけ", out: "\n\n", want: nil},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := domain.ParseSessionList(tt.out); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("ParseSessionList = %#v, want %#v", got, tt.want)
			}
		})
	}
}

func TestSessionNamesByState(t *testing.T) {
	t.Parallel()

	entries := domain.ParseSessionList(realSessionList)
	if got, want := domain.AliveSessionNames(entries), []string{"mdev-go-224042"}; !reflect.DeepEqual(got, want) {
		t.Errorf("AliveSessionNames = %v, want %v", got, want)
	}
	want := []string{"claude-conductor-224231", "claude-conductor-215255", "kazuto"}
	if got := domain.ExitedSessionNames(entries); !reflect.DeepEqual(got, want) {
		t.Errorf("ExitedSessionNames = %v, want %v", got, want)
	}
}

// TestAttachedClientCount は attach の有無の判定を固定する。
//
// ここを取り違えると、使っているセッションを kill してしまう。
func TestAttachedClientCount(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		out  string
		want int
	}{
		{
			// 実機の出力(attach 1 つ)。
			name: "クライアント 1 つ",
			out: "CLIENT_ID ZELLIJ_PANE_ID RUNNING_COMMAND\n" +
				"1         terminal_3     /Users/kazuto/.claude-conductor/bin/mdev pane news\n",
			want: 1,
		},
		{
			name: "クライアント 2 つ",
			out: "CLIENT_ID ZELLIJ_PANE_ID RUNNING_COMMAND\n" +
				"1         terminal_3     a\n2         terminal_4     b\n",
			want: 2,
		},
		{
			// 見出しだけ = 誰も開いていない。
			name: "detached",
			out:  "CLIENT_ID ZELLIJ_PANE_ID RUNNING_COMMAND\n",
			want: 0,
		},
		{name: "空の出力", out: "", want: 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := domain.AttachedClientCount(tt.out); got != tt.want {
				t.Errorf("AttachedClientCount = %d, want %d", got, tt.want)
			}
		})
	}
}
