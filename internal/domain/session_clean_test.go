package domain_test

import (
	"reflect"
	"testing"
	"time"

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
				{Name: "mdev-go-224042", Age: 40*time.Minute + 42*time.Second},
				{Name: "claude-conductor-224231", Age: 12*time.Minute + 50*time.Second, Exited: true},
				{Name: "claude-conductor-215255", Age: 12*time.Minute + 50*time.Second, Exited: true},
				{Name: "kazuto", Age: 12*time.Minute + 50*time.Second, Exited: true},
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

// TestParseSessionListWithSpacedNames は空白を含むセッション名を
// 取り違えないことを確かめる。
//
// 掃除は名前を指定して kill / delete するので、最初の語だけを見ると
// 無関係のセッションを消しにいく。
func TestParseSessionListWithSpacedNames(t *testing.T) {
	t.Parallel()

	got := domain.ParseSessionList(
		"my dev session [Created 40m 42s ago] \n" +
			"another one [Created 12m 50s ago] (EXITED - attach to resurrect)\n")
	want := []domain.SessionEntry{
		{Name: "my dev session", Age: 40*time.Minute + 42*time.Second},
		{Name: "another one", Age: 12*time.Minute + 50*time.Second, Exited: true},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("ParseSessionList = %#v, want %#v", got, want)
	}
}

// TestParseSessionListAge は作成からの経過時間の読み取りを固定する。
//
// 読めない値は 0 になる。0 は作成直後と同じ扱いで掃除の対象から外れる
// ため、安全側である。
func TestParseSessionListAge(t *testing.T) {
	t.Parallel()

	tests := []struct {
		note string
		want time.Duration
	}{
		{note: "2s", want: 2 * time.Second},
		{note: "12m 50s", want: 12*time.Minute + 50*time.Second},
		{note: "16h 20m 11s", want: 16*time.Hour + 20*time.Minute + 11*time.Second},
		{note: "3d 1h", want: 3*24*time.Hour + time.Hour},
		// 読めない書式は 0(掃除の対象から外れる)。
		{note: "しばらく前", want: 0},
		{note: "12x", want: 0},
		{note: "", want: 0},
		{note: "10m ほげ", want: 0},
	}
	for _, tt := range tests {
		t.Run(tt.note, func(t *testing.T) {
			t.Parallel()
			entries := domain.ParseSessionList("s [Created " + tt.note + " ago] \n")
			if len(entries) != 1 {
				t.Fatalf("読めた行 = %d, want 1", len(entries))
			}
			if got := entries[0].Age; got != tt.want {
				t.Errorf("Age(%q) = %v, want %v", tt.note, got, tt.want)
			}
		})
	}
}
