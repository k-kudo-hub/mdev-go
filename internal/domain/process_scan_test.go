package domain_test

import (
	"reflect"
	"testing"
	"time"

	"github.com/k-kudo-hub/mdev-go/internal/domain"
)

// realProcessList は実機(macOS)の `ps -axo pid,ppid,command` から
// zellij 関連の行を抜き出したものである。
//
// サーバが PPID=1 で動き、ペインのプロセスがその直接の子になる。
const realProcessList = "  PID  PPID     ELAPSED COMMAND\n" +
	"18001     1    01:00:59 /opt/homebrew/bin/zellij --server /var/folders/5w/T/zellij-501/contract_version_1/mdev-go-224042\n" +
	"17997 17339    01:00:59 zellij --new-session-with-layout /Users/kazuto/.claude-conductor/layouts/multi.kdl --session mdev-go-224042\n" +
	"18002 18001    01:00:58 /Users/kazuto/.claude-conductor/bin/mdev pane dashboard\n" +
	"18005 18001    01:00:58 /Users/kazuto/.claude-conductor/bin/mdev pane news\n" +
	"27682 18001       12:03 /Users/kazuto/.claude-conductor/bin/mdev pane task-control -- blogs-dev\n" +
	"28468 18001       05:00 nvim\n"

func TestParseProcessList(t *testing.T) {
	t.Parallel()

	got := domain.ParseProcessList("  PID  PPID     ELAPSED COMMAND\n" +
		"18001     1 54-10:00:41 /opt/homebrew/bin/zellij --server /tmp/z/mdev-go-1\n" +
		"18002 18001    01:02:03 /home/u/.claude-conductor/bin/mdev pane dashboard\n" +
		"18003 18001       07:30 nvim\n" +
		"ゴミ行\n")
	want := []domain.ProcessEntry{
		{
			PID: 18001, PPID: 1,
			// 54-10:00:41 = 54 日 10 時間 41 秒。
			Elapsed: 54*24*time.Hour + 10*time.Hour + 41*time.Second,
			Command: "/opt/homebrew/bin/zellij --server /tmp/z/mdev-go-1",
		},
		{
			PID: 18002, PPID: 18001,
			Elapsed: time.Hour + 2*time.Minute + 3*time.Second,
			Command: "/home/u/.claude-conductor/bin/mdev pane dashboard",
		},
		{
			// MM:SS の形(時間の桁が無い)。
			PID: 18003, PPID: 18001,
			Elapsed: 7*time.Minute + 30*time.Second,
			Command: "nvim",
		},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("ParseProcessList = %#v, want %#v", got, want)
	}
}

func TestZellijServers(t *testing.T) {
	t.Parallel()

	got := domain.ZellijServers(domain.ParseProcessList(realProcessList))
	want := []domain.ZellijServer{{PID: 18001, Elapsed: time.Hour + 59*time.Second, Session: "mdev-go-224042"}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("ZellijServers = %#v, want %#v", got, want)
	}
}

// TestZellijServersIgnoresClients はサーバではないプロセスを拾わないことを
// 確かめる。クライアント(--new-session-with-layout)も `--session <name>` を
// 持つため、素朴に名前を拾うと二重に数える。
func TestZellijServersIgnoresClients(t *testing.T) {
	t.Parallel()

	entries := domain.ParseProcessList(
		"17997 17339 01:00 zellij --new-session-with-layout /l/multi.kdl --session mdev-go-1\n" +
			"28468 18001 01:00 nvim\n" +
			"18099     1 01:00 /opt/homebrew/bin/zellij --server\n")
	if got := domain.ZellijServers(entries); len(got) != 0 {
		t.Errorf("ZellijServers = %#v, want 空", got)
	}
}

// TestMdevManagedSessions は「mdev が管理しているか」の判定を固定する。
//
// ここを緩めると、手で作った dev のようなセッションまで kill してしまう。
func TestMdevManagedSessions(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		out  string
		want map[string]bool
	}{
		{
			name: "実機の出力",
			out:  realProcessList,
			want: map[string]bool{"mdev-go-224042": true},
		},
		{
			// ペインが mdev でないセッションは対象外(手動セッション)。
			name: "mdev pane が居ない",
			out: "18001     1 10:00 /opt/homebrew/bin/zellij --server /tmp/z/dev\n" +
				"18002 18001 10:00 -zsh\n",
			want: map[string]bool{},
		},
		{
			// 別のサーバの子である mdev pane を取り違えない。
			name: "サーバごとに分けて数える",
			out: "1001     1 10:00 /opt/homebrew/bin/zellij --server /tmp/z/mdev-a\n" +
				"1002     1 10:00 /opt/homebrew/bin/zellij --server /tmp/z/manual-b\n" +
				"1003  1001 10:00 /home/u/.claude-conductor/bin/mdev pane dashboard\n" +
				"1004  1002 10:00 -zsh\n",
			want: map[string]bool{"mdev-a": true},
		},
		{
			// 親が見つからない mdev pane はどのセッションにも紐づけない。
			name: "親のサーバが居ない",
			out:  "1003  9999 10:00 /home/u/.claude-conductor/bin/mdev pane dashboard\n",
			want: map[string]bool{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := domain.MdevManagedSessions(domain.ParseProcessList(tt.out))
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("MdevManagedSessions = %v, want %v", got, tt.want)
			}
		})
	}
}

// TestZombieServers は「一覧に出ないのに動いているサーバ」の判定を固定する。
//
// **生きていると一覧に出ているセッションのサーバを含めてはならない。**
// 使用中のセッションを落とす唯一の経路になりうる。
func TestZombieServers(t *testing.T) {
	t.Parallel()

	const old = 10 * time.Minute
	servers := []domain.ZellijServer{
		{PID: 1, Elapsed: old, Session: "in-use"},
		{PID: 2, Elapsed: old, Session: "exited-but-running"},
		{PID: 3, Elapsed: old, Session: "unknown-to-zellij"},
		// 起動しかけ。まだ list-sessions に載っていないだけかもしれない。
		{PID: 4, Elapsed: 3 * time.Second, Session: "starting-up"},
	}
	sessions := []domain.SessionEntry{
		{Name: "in-use"},
		{Name: "exited-but-running", Exited: true},
	}

	got := domain.ZombieServers(servers, sessions)
	want := []domain.ZellijServer{
		{PID: 2, Elapsed: old, Session: "exited-but-running"},
		{PID: 3, Elapsed: old, Session: "unknown-to-zellij"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("ZombieServers = %#v, want %#v", got, want)
	}
}

// TestOrphanZellijClients は親を失った zellij action の検出を固定する。
func TestOrphanZellijClients(t *testing.T) {
	t.Parallel()

	entries := domain.ParseProcessList(
		"2001     1 03:00 zellij action list-tabs\n" +
			"2002  1500 03:00 zellij action list-tabs\n" + // 親が生きている = 実行中
			"2003     1 03:00 /opt/homebrew/bin/zellij --server /tmp/z/a\n" + // サーバは別扱い
			"2004     1 03:00 nvim\n")
	got := domain.OrphanZellijClients(entries)
	want := []domain.ProcessEntry{
		{PID: 2001, PPID: 1, Elapsed: 3 * time.Minute, Command: "zellij action list-tabs"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("OrphanZellijClients = %#v, want %#v", got, want)
	}
}

// TestParseElapsedFormats は ELAPSED 列の書式ごとの読み取りを固定する。
//
// 読めない値は 0 になる。0 は「起動直後」と同じ扱いで掃除の対象から
// 外れるため、安全側である。
func TestParseElapsedFormats(t *testing.T) {
	t.Parallel()

	tests := []struct {
		field string
		want  time.Duration
	}{
		{field: "00:05", want: 5 * time.Second},
		{field: "12:03", want: 12*time.Minute + 3*time.Second},
		{field: "01:00:59", want: time.Hour + 59*time.Second},
		{field: "54-10:00:41", want: 54*24*time.Hour + 10*time.Hour + 41*time.Second},
		// 読めない書式は 0(掃除の対象から外れる)。
		{field: "ELAPSED", want: 0},
		{field: "5", want: 0},
		{field: "a:b", want: 0},
		{field: "x-01:02:03", want: 0},
	}
	for _, tt := range tests {
		t.Run(tt.field, func(t *testing.T) {
			t.Parallel()
			entries := domain.ParseProcessList("1 1 " + tt.field + " cmd\n")
			if len(entries) != 1 {
				t.Fatalf("読めた行 = %d, want 1", len(entries))
			}
			if got := entries[0].Elapsed; got != tt.want {
				t.Errorf("Elapsed(%q) = %v, want %v", tt.field, got, tt.want)
			}
		})
	}
}

// TestZombieServersSparesYoungServers は起動しかけのサーバを掴まない
// ことを確かめる。
//
// zellij のサーバは起動してから list-sessions に載るまでに一瞬の間があり、
// その隙に見ると「一覧に出ないのに動いている」に見える。実機でも走査した
// 瞬間だけ見えて次には消えている短命なサーバを観測した。ここで撃つと、
// 利用者が今まさに開こうとしているセッションを落とす。
func TestZombieServersSparesYoungServers(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		elapsed time.Duration
		want    int
	}{
		{name: "起動直後", elapsed: 0, want: 0},
		{name: "59 秒", elapsed: 59 * time.Second, want: 0},
		{name: "60 秒ちょうど", elapsed: domain.ZombieMinAge, want: 1},
		{name: "十分に古い", elapsed: time.Hour, want: 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			servers := []domain.ZellijServer{{PID: 9, Elapsed: tt.elapsed, Session: "unknown"}}
			if got := len(domain.ZombieServers(servers, nil)); got != tt.want {
				t.Errorf("ゾンビ = %d 件, want %d 件", got, tt.want)
			}
		})
	}
}

// TestZellijServersRejectsLookAlikes は zellij サーバでないプロセスを
// サーバとみなさないことを確かめる。
//
// 部分一致で見ていたときは、`nvim --server /tmp/zellij-x` のような無関係の
// プロセスまで拾い、掃除が kill しにいく恐れがあった。
func TestZellijServersRejectsLookAlikes(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		command string
	}{
		{name: "nvim の --server", command: "nvim --server /tmp/zellij-501/x"},
		{name: "実行ファイル名が違う", command: "/usr/bin/zellijctl --server /tmp/z/a"},
		{name: "--server ではない", command: "/opt/homebrew/bin/zellij --serverish /tmp/z/a"},
		{name: "パスが無い", command: "/opt/homebrew/bin/zellij --server"},
		{name: "クライアント", command: "zellij --new-session-with-layout /l.kdl --session a"},
		{name: "名前に zellij を含むだけ", command: "grep zellij --server /tmp/z/a"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			entries := domain.ParseProcessList("100 1 10:00 " + tt.command + "\n")
			if got := domain.ZellijServers(entries); len(got) != 0 {
				t.Errorf("ZellijServers = %#v, want 空", got)
			}
		})
	}
}

// TestZellijServersWithSpacedSocketPath は空白を含むソケットのパスから
// 正しくセッション名を取ることを確かめる。
//
// 最後の語だけを見ると、名前が途中で切れて別のセッションを指す。
func TestZellijServersWithSpacedSocketPath(t *testing.T) {
	t.Parallel()

	entries := domain.ParseProcessList(
		"100 1 10:00 /opt/homebrew/bin/zellij --server /tmp/my tmp/zellij-501/v1/my session\n")
	got := domain.ZellijServers(entries)
	want := []domain.ZellijServer{{PID: 100, Elapsed: 10 * time.Minute, Session: "my session"}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("ZellijServers = %#v, want %#v", got, want)
	}
}

// TestMdevManagedSessionsWithSpacedName は空白入りのセッション名でも
// mdev 管理の判定が働くことを確かめる。
func TestMdevManagedSessionsWithSpacedName(t *testing.T) {
	t.Parallel()

	got := domain.MdevManagedSessions(domain.ParseProcessList(
		"100 1 10:00 /opt/homebrew/bin/zellij --server /tmp/z/my session\n" +
			"101 100 10:00 /home/u/.claude-conductor/bin/mdev pane dashboard\n"))
	want := map[string]bool{"my session": true}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("MdevManagedSessions = %v, want %v", got, want)
	}
}
