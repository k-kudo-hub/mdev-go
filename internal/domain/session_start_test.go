package domain_test

import (
	"testing"

	"github.com/k-kudo-hub/mdev-go/internal/domain"
)

// TestSessionRequestSessionName は開くセッションの名前の決め方を確かめる。
//
// 現行 init.zsh の mdev() を移したもので、**同じディレクトリから何度実行
// しても同じ名前になる**ことが attach-or-create の土台である。
func TestSessionRequestSessionName(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		req  domain.SessionRequest
		want string
	}{
		{
			name: "ディレクトリ名から決める",
			req:  domain.SessionRequest{Dir: "/Users/dev/projects/myapp"},
			want: "myapp",
		},
		{
			name: "名前の指定があればそれを使う",
			req:  domain.SessionRequest{Name: "review", Dir: "/Users/dev/projects/myapp"},
			want: "review",
		},
		{
			name: "--new は時刻を足す",
			req:  domain.SessionRequest{Dir: "/Users/dev/projects/myapp", Stamp: "174241"},
			want: "myapp-174241",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := tt.req.SessionName(); got != tt.want {
				t.Errorf("SessionName = %q, want %q", got, tt.want)
			}
		})
	}
}

// TestSessionRequestHashesFullPath は同じディレクトリ名でも別のセッションに
// なることを確かめる。
//
// 名前は basename、ハッシュ源はパス全体である。ここを取り違えると、
// 別のプロジェクトの同名ディレクトリが同じセッションへ潰れる。
func TestSessionRequestHashesFullPath(t *testing.T) {
	t.Parallel()

	long := "very-long-directory-name-here"
	a := domain.SessionRequest{Dir: "/Users/dev/alpha/" + long}.SessionName()
	b := domain.SessionRequest{Dir: "/Users/dev/beta/" + long}.SessionName()
	if a == b {
		t.Errorf("別のディレクトリが同じ名前になった: %q", a)
	}
}

// TestSessionRequestNewStampsHashToo は --new が長い名前でも別のセッションに
// なることを確かめる。
//
// 時刻は切り詰めで消えるため、ハッシュ源にも入れないと既定のセッションと
// 同じ名前になってしまう。
func TestSessionRequestNewStampsHashToo(t *testing.T) {
	t.Parallel()

	dir := "/Users/dev/projects/a-very-long-project-directory-name"
	normal := domain.SessionRequest{Dir: dir}.SessionName()
	fresh := domain.SessionRequest{Dir: dir, Stamp: "174241"}.SessionName()
	if normal == fresh {
		t.Errorf("--new が既定と同じ名前になった: %q", normal)
	}
}

// TestParseSessionState はセッションの状態の読み取りを確かめる。
func TestParseSessionState(t *testing.T) {
	t.Parallel()

	const listing = "alive-one [Created 1h ago]\n" +
		"dead-one [Created 2h ago] (EXITED - attach to resurrect)\n"

	tests := []struct {
		name string
		want domain.SessionState
	}{
		{name: "alive-one", want: domain.SessionAlive},
		{name: "dead-one", want: domain.SessionExited},
		{name: "missing", want: domain.SessionAbsent},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := domain.ParseSessionState(listing, tt.name); got != tt.want {
				t.Errorf("ParseSessionState(%q) = %v, want %v", tt.name, got, tt.want)
			}
		})
	}
}

// TestParseSessionStateEmpty は一覧が空のときを確かめる。
func TestParseSessionStateEmpty(t *testing.T) {
	t.Parallel()

	if got := domain.ParseSessionState("", "any"); got != domain.SessionAbsent {
		t.Errorf("ParseSessionState = %v, want SessionAbsent", got)
	}
}

// TestInitZshScriptDefinesAliases は出力する定義を確かめる。
//
// mdev そのものを定義してはいけない(PATH 上のバイナリが受ける)。
func TestInitZshScriptDefinesAliases(t *testing.T) {
	t.Parallel()

	script := domain.InitZshScript
	for _, want := range []string{
		"alias zj=", "alias zja=", "alias zjl=", "alias zjk=",
		"alias dev='mdev dev'", "alias zs='mdev attach'",
		"alias pending-clear='mdev pending clear'",
	} {
		if !containsString(script, want) {
			t.Errorf("%q が無い:\n%s", want, script)
		}
	}
	for _, forbidden := range []string{"mdev()", "alias mdev=", "/scripts/"} {
		if containsString(script, forbidden) {
			t.Errorf("%q を定義している:\n%s", forbidden, script)
		}
	}
}

// containsString は s に substr が含まれるかを返す。
func containsString(s, substr string) bool {
	for i := 0; i+len(substr) <= len(s); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
