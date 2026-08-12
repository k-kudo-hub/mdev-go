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

// TestParseClientList は attach の有無の判定を固定する。
//
// ここを取り違えると、使っているセッションを kill してしまう。
func TestParseClientList(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		out  string
		want int
		ok   bool
	}{
		{
			// 実機の出力(attach 1 つ)。
			name: "クライアント 1 つ",
			out: "CLIENT_ID ZELLIJ_PANE_ID RUNNING_COMMAND\n" +
				"1         terminal_3     /Users/kazuto/.claude-conductor/bin/mdev pane news\n",
			want: 1, ok: true,
		},
		{
			name: "クライアント 2 つ",
			out: "CLIENT_ID ZELLIJ_PANE_ID RUNNING_COMMAND\n" +
				"1         terminal_3     a\n2         terminal_4     b\n",
			want: 2, ok: true,
		},
		{
			// 見出しだけ = 誰も開いていない。
			name: "見出しだけなら detached",
			out:  "CLIENT_ID ZELLIJ_PANE_ID RUNNING_COMMAND\n",
			want: 0, ok: true,
		},
		{
			// 見出しが無い = 応答の形が想定と違う。誰も居ないとは
			// 断定できないので判断不能にする(呼び出し側が attach 扱いへ倒す)。
			name: "空の出力は判断不能",
			out:  "",
			ok:   false,
		},
		{name: "見出しの無いゴミ出力は判断不能", out: "なにかおかしい\n", ok: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, ok := domain.ParseClientList(tt.out)
			if ok != tt.ok {
				t.Fatalf("ParseClientList ok = %v, want %v", ok, tt.ok)
			}
			if ok && got != tt.want {
				t.Errorf("ParseClientList = %d, want %d", got, tt.want)
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
// 期待値は **zellij 0.44.1(macOS)の実出力** である。セッションのソケットの
// mtime を遡らせて `list-sessions` を実行し、以下の表記を採取した。
//
//	1day 30m 32s / 3days 24s
//	1month 9days 13h 26m 57s
//	1year 1month 4days 7h 26m 57s
//	2years 2months 8days 14h 53m 21s
//
// 年・月・日は値が 1 より大きいと複数形になり、h / m / s は変化しない
// (Rust の humantime crate の item_plural / item の違い)。
//
// **日以上の単位を読めないと、1 日以上放置されたセッションがすべて
// 「作られたばかり」に見えて永久に掃除されない。** この機能が本来いちばん
// 片付けたい相手なので、ここは実測に基づいて固定する。
//
// 読めない値は 0 になる。0 は作成直後と同じ扱いで掃除の対象から外れる
// ため、安全側である。
func TestParseSessionListAge(t *testing.T) {
	t.Parallel()

	const (
		day   = 24 * time.Hour
		month = 2630016 * time.Second
		year  = 31557600 * time.Second
	)
	tests := []struct {
		note string
		want time.Duration
	}{
		// --- 実機で採取した表記 ---
		{note: "2s", want: 2 * time.Second},
		{note: "12m 50s", want: 12*time.Minute + 50*time.Second},
		{note: "16h 20m 11s", want: 16*time.Hour + 20*time.Minute + 11*time.Second},
		{note: "1day 30m 32s", want: day + 30*time.Minute + 32*time.Second},
		{note: "3days 24s", want: 3*day + 24*time.Second},
		{
			note: "1month 9days 13h 26m 57s",
			want: month + 9*day + 13*time.Hour + 26*time.Minute + 57*time.Second,
		},
		{
			note: "1year 1month 4days 7h 26m 57s",
			want: year + month + 4*day + 7*time.Hour + 26*time.Minute + 57*time.Second,
		},
		{
			note: "2years 2months 8days 14h 53m 21s",
			want: 2*year + 2*month + 8*day + 14*time.Hour + 53*time.Minute + 21*time.Second,
		},
		// --- 秒未満(作った直後に見たとき)---
		{note: "500ms", want: 500 * time.Millisecond},
		{note: "1s 200ms", want: time.Second + 200*time.Millisecond},
		// --- 読めない書式は 0(掃除の対象から外れる)---
		{note: "しばらく前", want: 0},
		{note: "12x", want: 0},
		{note: "", want: 0},
		{note: "10m ほげ", want: 0},
		{note: "days", want: 0},
		{note: "-5m", want: 0},
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

// TestParseSessionListAgeClearsCleanupMinAge は「1 日以上前」が確実に
// 掃除の猶予を超えることを確かめる。
//
// ここが 0 に落ちると、放置されたセッションが永久に片付かない。
func TestParseSessionListAgeClearsCleanupMinAge(t *testing.T) {
	t.Parallel()

	for _, note := range []string{
		"1day 30m 32s", "3days 24s", "1month 9days 13h 26m 57s",
		"1year 1month 4days 7h 26m 57s", "2years 2months 8days 14h 53m 21s",
	} {
		entries := domain.ParseSessionList("s [Created " + note + " ago] \n")
		if got := entries[0].Age; got < domain.CleanupMinAge {
			t.Errorf("Age(%q) = %v, 猶予 %v を超えていない(掃除されない)",
				note, got, domain.CleanupMinAge)
		}
	}
}

// TestParseSessionListWithPathologicalName は名前そのものが注記に似た
// 文字列を含む場合でも取り違えないことを確かめる。
func TestParseSessionListWithPathologicalName(t *testing.T) {
	t.Parallel()

	got := domain.ParseSessionList(
		"weird [Created yesterday] name [Created 5m 0s ago] \n" +
			"has (EXITED marker [Created 5m 0s ago] \n")
	want := []domain.SessionEntry{
		// 注記は行末側に付くので、最後の目印で切る。
		{Name: "weird [Created yesterday] name", Age: 5 * time.Minute},
		// "(EXITED" が名前側にあるだけでは終了済みにしない。
		{Name: "has (EXITED marker", Age: 5 * time.Minute},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("ParseSessionList = %#v, want %#v", got, want)
	}
}

// TestIsNoSessionsOutput は「セッション 0 件」の見分け方を固定する。
//
// 実機(zellij 0.44.1)では rc=1・標準出力は空・標準エラーに
// 「No active zellij sessions found.」が出る。ここを取り違えて
// 「壊れている」を「0 件」と読むと、生きているセッションのサーバを
// すべてゾンビとみなして kill することになる。
func TestIsNoSessionsOutput(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name           string
		stdout, stderr string
		want           bool
	}{
		{
			name:   "実機の 0 件",
			stderr: "No active zellij sessions found.\n",
			want:   true,
		},
		{
			// 別の失敗を 0 件と読んではならない。
			name:   "別の失敗",
			stderr: "error: could not connect to server\n",
			want:   false,
		},
		{name: "何も出ない失敗", want: false},
		{
			// 一覧が取れているなら 0 件ではない。
			name:   "出力がある",
			stdout: "s [Created 1s ago] \n",
			stderr: "No active zellij sessions found.\n",
			want:   false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := domain.IsNoSessionsOutput(tt.stdout, tt.stderr); got != tt.want {
				t.Errorf("IsNoSessionsOutput = %v, want %v", got, tt.want)
			}
		})
	}
}
