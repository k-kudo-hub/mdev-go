package store

import (
	"errors"
	"reflect"
	"testing"
)

// TestTaskControlCommandUsesRunningBinary は今動いているバイナリを起動する
// ことを確かめる(ADR D7-2)。
//
// CONDUCTOR_HOME から組み立てていたときは、mdev test で worktree のバイナリを
// 動かしていても操作バーだけが設置済みのものになりえた。同じセッションで
// 2 つの版が混ざると、原因の特定ができなくなる。
func TestTaskControlCommandUsesRunningBinary(t *testing.T) {
	t.Parallel()

	s := &MdevBinaryStore{
		conductorHome: "/home/dev/.claude-conductor",
		executable:    func() (string, error) { return "/w/worktree/bin/mdev", nil },
	}
	want := []string{"/w/worktree/bin/mdev", "pane", "task-control", "--", "my-tab"}
	if got := s.TaskControlCommand("my-tab"); !reflect.DeepEqual(got, want) {
		t.Errorf("TaskControlCommand = %q, want %q", got, want)
	}
}

// TestTaskControlCommandFallsBackToConductorHome は自分の場所を引けないときに
// 設置済みのものへ落ちることを確かめる。
func TestTaskControlCommandFallsBackToConductorHome(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		exec func() (string, error)
	}{
		{name: "引けない", exec: func() (string, error) { return "", errors.New("no") }},
		{name: "空が返る", exec: func() (string, error) { return "", nil }},
		{name: "そもそも無い", exec: nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			s := &MdevBinaryStore{conductorHome: "/home/dev/.claude-conductor", executable: tt.exec}
			want := "/home/dev/.claude-conductor/bin/mdev"
			if got := s.TaskControlCommand("t")[0]; got != want {
				t.Errorf("パス = %q, want %q", got, want)
			}
		})
	}
}

// TestTaskControlCommandSeparatesTabName はタブ名を `--` の後ろへ置くことを
// 確かめる。`-wip` のような名前をフラグと解釈させない。
func TestTaskControlCommandSeparatesTabName(t *testing.T) {
	t.Parallel()

	s := &MdevBinaryStore{executable: func() (string, error) { return "/bin/mdev", nil }}
	got := s.TaskControlCommand("-wip")
	if got[len(got)-2] != "--" || got[len(got)-1] != "-wip" {
		t.Errorf("TaskControlCommand = %q", got)
	}
}
