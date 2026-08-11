package domain_test

import (
	"reflect"
	"testing"

	"github.com/k-kudo-hub/mdev-go/internal/domain"
)

// realProcessList は実機(macOS)の `ps -axo pid,ppid,command` から
// zellij 関連の行を抜き出したものである。
//
// サーバが PPID=1 で動き、ペインのプロセスがその直接の子になる。
const realProcessList = "  PID  PPID COMMAND\n" +
	"18001     1 /opt/homebrew/bin/zellij --server /var/folders/5w/T/zellij-501/contract_version_1/mdev-go-224042\n" +
	"17997 17339 zellij --new-session-with-layout /Users/kazuto/.claude-conductor/layouts/multi.kdl --session mdev-go-224042\n" +
	"18002 18001 /Users/kazuto/.claude-conductor/bin/mdev pane dashboard\n" +
	"18005 18001 /Users/kazuto/.claude-conductor/bin/mdev pane news\n" +
	"27682 18001 /Users/kazuto/.claude-conductor/bin/mdev pane task-control -- blogs-dev\n" +
	"28468 18001 nvim\n"

func TestParseProcessList(t *testing.T) {
	t.Parallel()

	got := domain.ParseProcessList("  PID  PPID COMMAND\n" +
		"18001     1 /opt/homebrew/bin/zellij --server /tmp/z/mdev-go-1\n" +
		"18002 18001 /home/u/.claude-conductor/bin/mdev pane dashboard\n" +
		"ゴミ行\n")
	want := []domain.ProcessEntry{
		{PID: 18001, PPID: 1, Command: "/opt/homebrew/bin/zellij --server /tmp/z/mdev-go-1"},
		{PID: 18002, PPID: 18001, Command: "/home/u/.claude-conductor/bin/mdev pane dashboard"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("ParseProcessList = %#v, want %#v", got, want)
	}
}

func TestZellijServers(t *testing.T) {
	t.Parallel()

	got := domain.ZellijServers(domain.ParseProcessList(realProcessList))
	want := []domain.ZellijServer{{PID: 18001, Session: "mdev-go-224042"}}
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
		"17997 17339 zellij --new-session-with-layout /l/multi.kdl --session mdev-go-1\n" +
			"28468 18001 nvim\n" +
			"18099     1 /opt/homebrew/bin/zellij --server\n")
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
			out: "18001     1 /opt/homebrew/bin/zellij --server /tmp/z/dev\n" +
				"18002 18001 -zsh\n",
			want: map[string]bool{},
		},
		{
			// 別のサーバの子である mdev pane を取り違えない。
			name: "サーバごとに分けて数える",
			out: "1001     1 /opt/homebrew/bin/zellij --server /tmp/z/mdev-a\n" +
				"1002     1 /opt/homebrew/bin/zellij --server /tmp/z/manual-b\n" +
				"1003  1001 /home/u/.claude-conductor/bin/mdev pane dashboard\n" +
				"1004  1002 -zsh\n",
			want: map[string]bool{"mdev-a": true},
		},
		{
			// 親が見つからない mdev pane はどのセッションにも紐づけない。
			name: "親のサーバが居ない",
			out:  "1003  9999 /home/u/.claude-conductor/bin/mdev pane dashboard\n",
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

	servers := []domain.ZellijServer{
		{PID: 1, Session: "in-use"},
		{PID: 2, Session: "exited-but-running"},
		{PID: 3, Session: "unknown-to-zellij"},
	}
	sessions := []domain.SessionEntry{
		{Name: "in-use"},
		{Name: "exited-but-running", Exited: true},
	}

	got := domain.ZombieServers(servers, sessions)
	want := []domain.ZellijServer{
		{PID: 2, Session: "exited-but-running"},
		{PID: 3, Session: "unknown-to-zellij"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("ZombieServers = %#v, want %#v", got, want)
	}
}

// TestOrphanZellijClients は親を失った zellij action の検出を固定する。
func TestOrphanZellijClients(t *testing.T) {
	t.Parallel()

	entries := domain.ParseProcessList(
		"2001     1 zellij action list-tabs\n" +
			"2002  1500 zellij action list-tabs\n" + // 親が生きている = 実行中
			"2003     1 /opt/homebrew/bin/zellij --server /tmp/z/a\n" + // サーバは別扱い
			"2004     1 nvim\n")
	got := domain.OrphanZellijClients(entries)
	want := []domain.ProcessEntry{{PID: 2001, PPID: 1, Command: "zellij action list-tabs"}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("OrphanZellijClients = %#v, want %#v", got, want)
	}
}
