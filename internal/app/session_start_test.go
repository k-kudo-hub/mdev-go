package app_test

import (
	"bytes"
	"errors"
	"testing"

	"github.com/k-kudo-hub/mdev-go/internal/app"
	"github.com/k-kudo-hub/mdev-go/internal/domain"
)

// fakeSessionLister はセッション一覧の代役である。
type fakeSessionLister struct {
	listing string
	err     error
}

func (f fakeSessionLister) ListSessions() (string, error) { return f.listing, f.err }

// fakeSessionRemover はセッションの削除の代役である。
type fakeSessionRemover struct {
	deleted []string
	killed  []string
}

func (f *fakeSessionRemover) DeleteSession(name string) error {
	f.deleted = append(f.deleted, name)
	return nil
}

func (f *fakeSessionRemover) KillSession(name string) error {
	f.killed = append(f.killed, name)
	return nil
}

// fakeCleanService は掃除の代役である。
type fakeCleanService struct{ calls int }

func (f *fakeCleanService) Clean(bool) (app.CleanupResult, error) {
	f.calls++
	return app.CleanupResult{}, nil
}

// fakeNewsService は当日ニュースの代役である。
type fakeNewsRefreshService struct{ calls int }

func (f *fakeNewsRefreshService) Refresh(bool) { f.calls++ }

// fakeUpdateCheck は更新確認の代役である。
type fakeUpdateCheck struct{ notice string }

func (f fakeUpdateCheck) Check(bool) string { return f.notice }

// fakeSessionPending はセッション単位の pending 削除の代役である。
type fakeSessionPending struct {
	removed []string
	allGone bool
}

func (f *fakeSessionPending) RemoveSession(session string) error {
	f.removed = append(f.removed, session)
	return nil
}

func (f *fakeSessionPending) RemoveAll() error { f.allGone = true; return nil }

// fakeChooser は選択の代役である。
type fakeChooser struct {
	pick    string
	options []string
}

func (f *fakeChooser) Choose(_ string, options []string) (string, error) {
	f.options = options
	return f.pick, nil
}

// sessionFixture は SessionLauncher と観測用の fake をまとめたものである。
type sessionFixture struct {
	launcher *app.SessionLauncher
	execer   *fakeExecer
	remover  *fakeSessionRemover
	cleaner  *fakeCleanService
	news     *fakeNewsRefreshService
	pending  *fakeSessionPending
	chooser  *fakeChooser
	files    *fakeFileStore
}

// newSessionFixture はレイアウトが置いてある状態の一式を返す。
func newSessionFixture(listing string) *sessionFixture {
	files := newFakeFileStore()
	files.files[conductorPath("layouts/multi.kdl")] = "layout {}\n"
	files.files[conductorPath("layouts/dev.kdl")] = "layout {}\n"

	f := &sessionFixture{
		execer:  &fakeExecer{},
		remover: &fakeSessionRemover{},
		cleaner: &fakeCleanService{},
		news:    &fakeNewsRefreshService{},
		pending: &fakeSessionPending{},
		chooser: &fakeChooser{},
		files:   files,
	}
	f.launcher = &app.SessionLauncher{
		Sessions: fakeSessionLister{listing: listing},
		Remover:  f.remover,
		Cleaner:  f.cleaner,
		News:     f.news,
		Update:   fakeUpdateCheck{},
		Pending:  f.pending,
		Files:    files,
		Execer:   f.execer,
		Chooser:  f.chooser,
		Paths:    testInstallPaths,
		Clock:    fakeClock{now: codexNow},
		WorkDir:  "/Users/dev/projects/myapp",
	}
	return f
}

// TestSessionStartAttachesToAlive は動いているセッションへ入ることを確かめる。
//
// このとき掃除もニュースも走らせない。戻ってくるだけの操作を毎回重くしない。
func TestSessionStartAttachesToAlive(t *testing.T) {
	t.Parallel()

	f := newSessionFixture("myapp [Created 1h ago]\n")
	var out bytes.Buffer
	if err := f.launcher.Start(&out, app.SessionRequest{Dir: "/Users/dev/projects/myapp"}); err != nil {
		t.Fatalf("Start = %v", err)
	}

	want := []string{"zellij", "attach", "myapp"}
	if len(f.execer.commands) != 1 || !equalStrings(f.execer.commands[0], want) {
		t.Errorf("起動 = %q, want %q", f.execer.commands, want)
	}
	if f.cleaner.calls != 0 || f.news.calls != 0 {
		t.Errorf("attach で下ごしらえが走った: clean=%d news=%d", f.cleaner.calls, f.news.calls)
	}
}

// TestSessionStartCreatesWhenAbsent は無いセッションを作ることを確かめる。
func TestSessionStartCreatesWhenAbsent(t *testing.T) {
	t.Parallel()

	f := newSessionFixture("")
	var out bytes.Buffer
	if err := f.launcher.Start(&out, app.SessionRequest{Dir: "/Users/dev/projects/myapp"}); err != nil {
		t.Fatalf("Start = %v", err)
	}

	want := []string{
		"zellij", "--new-session-with-layout",
		conductorPath("layouts/multi.kdl"), "--session", "myapp",
	}
	if len(f.execer.commands) != 1 || !equalStrings(f.execer.commands[0], want) {
		t.Errorf("起動 = %q, want %q", f.execer.commands, want)
	}
	// 作る前に下ごしらえをする。
	if f.cleaner.calls != 1 || f.news.calls != 1 {
		t.Errorf("下ごしらえが走っていない: clean=%d news=%d", f.cleaner.calls, f.news.calls)
	}
	if len(f.pending.removed) != 0 {
		t.Errorf("pending を消した: %v", f.pending.removed)
	}
}

// TestSessionStartRebuildsExited は落ちたセッションを作り直すことを確かめる。
//
// zellij の復元に任せると、タスクのペインが会話を失った新しいエージェントと
// して立ち上がる。pending も消す。そのセッションより前の記録なので、残すと
// レジストリが復元したタスクを古い行が覆い隠す。
func TestSessionStartRebuildsExited(t *testing.T) {
	t.Parallel()

	f := newSessionFixture("myapp [Created 2h ago] (EXITED - attach to resurrect)\n")
	var out bytes.Buffer
	if err := f.launcher.Start(&out, app.SessionRequest{Dir: "/Users/dev/projects/myapp"}); err != nil {
		t.Fatalf("Start = %v", err)
	}

	if len(f.remover.deleted) != 1 || f.remover.deleted[0] != "myapp" {
		t.Errorf("削除 = %v, want [myapp]", f.remover.deleted)
	}
	if len(f.pending.removed) != 1 || f.pending.removed[0] != "myapp" {
		t.Errorf("pending の削除 = %v, want [myapp]", f.pending.removed)
	}
	if len(f.execer.commands) != 1 || f.execer.commands[0][1] != "--new-session-with-layout" {
		t.Errorf("作り直していない: %q", f.execer.commands)
	}
}

// TestSessionStartShowsUpdateNotice は更新の案内を出すことを確かめる。
func TestSessionStartShowsUpdateNotice(t *testing.T) {
	t.Parallel()

	f := newSessionFixture("")
	f.launcher.Update = fakeUpdateCheck{notice: "新しい版があります\n"}

	var out bytes.Buffer
	if err := f.launcher.Start(&out, app.SessionRequest{Dir: "/Users/dev/projects/myapp"}); err != nil {
		t.Fatalf("Start = %v", err)
	}
	if out.String() != "新しい版があります\n" {
		t.Errorf("出力 = %q", out.String())
	}
}

// TestSessionStartFailsWhenListingFails は一覧を引けないときに止まることを
// 確かめる。
//
// 「無い」と見なして作りに行くと、実は動いているセッションを二重に作る。
// 掃除の実事故と同じ形(rc=0 かつ空)を作らないための線引きである。
func TestSessionStartFailsWhenListingFails(t *testing.T) {
	t.Parallel()

	f := newSessionFixture("")
	f.launcher.Sessions = fakeSessionLister{err: errors.New("引けない")}

	var out bytes.Buffer
	if err := f.launcher.Start(&out, app.SessionRequest{Dir: "/w/myapp"}); err == nil {
		t.Fatal("エラーを返すはず")
	}
	if len(f.execer.commands) != 0 {
		t.Errorf("起動した: %q", f.execer.commands)
	}
}

// TestSessionStartRequiresLayout はレイアウトが無いときの説明を確かめる。
func TestSessionStartRequiresLayout(t *testing.T) {
	t.Parallel()

	f := newSessionFixture("")
	delete(f.files.files, conductorPath("layouts/multi.kdl"))

	var out bytes.Buffer
	err := f.launcher.Start(&out, app.SessionRequest{Dir: "/w/myapp"})
	if err == nil {
		t.Fatal("エラーを返すはず")
	}
	if !bytes.Contains([]byte(err.Error()), []byte("mdev install")) {
		t.Errorf("直し方が出ていない: %v", err)
	}
}

// TestSessionStartDev は単一の開発セッションを確かめる。
func TestSessionStartDev(t *testing.T) {
	t.Parallel()

	t.Run("名前を省くと時刻が付く", func(t *testing.T) {
		t.Parallel()
		f := newSessionFixture("")
		if err := f.launcher.StartDev(""); err != nil {
			t.Fatalf("StartDev = %v", err)
		}
		got := f.execer.commands[0]
		if want := "myapp-" + codexNow.Format(domain.NewSessionTimeLayout); got[4] != want {
			t.Errorf("セッション名 = %q, want %q", got[4], want)
		}
		if got[2] != conductorPath("layouts/dev.kdl") {
			t.Errorf("レイアウト = %q", got[2])
		}
	})

	t.Run("長い名前は切り詰める", func(t *testing.T) {
		t.Parallel()
		f := newSessionFixture("")
		long := "this-is-a-very-long-session-name"
		if err := f.launcher.StartDev(long); err != nil {
			t.Fatalf("StartDev = %v", err)
		}
		if got := f.execer.commands[0][4]; got != domain.ZellijSessionName(long, long) {
			t.Errorf("セッション名 = %q", got)
		}
	})
}

// TestSessionAttach は名前指定と一覧からの選択を確かめる。
func TestSessionAttach(t *testing.T) {
	t.Parallel()

	t.Run("動いていれば attach", func(t *testing.T) {
		t.Parallel()
		f := newSessionFixture("other [Created 1h ago]\n")
		if err := f.launcher.Attach("other"); err != nil {
			t.Fatalf("Attach = %v", err)
		}
		if want := []string{"zellij", "attach", "other"}; !equalStrings(f.execer.commands[0], want) {
			t.Errorf("起動 = %q, want %q", f.execer.commands[0], want)
		}
	})

	t.Run("無ければその名前で作る", func(t *testing.T) {
		t.Parallel()
		f := newSessionFixture("")
		if err := f.launcher.Attach("brand-new"); err != nil {
			t.Fatalf("Attach = %v", err)
		}
		if want := []string{"zellij", "--session", "brand-new"}; !equalStrings(f.execer.commands[0], want) {
			t.Errorf("起動 = %q, want %q", f.execer.commands[0], want)
		}
	})

	t.Run("名前を省くと一覧から選ぶ", func(t *testing.T) {
		t.Parallel()
		f := newSessionFixture("one [Created 1h ago]\ntwo [Created 2h ago]\n")
		f.chooser.pick = "two"
		if err := f.launcher.Attach(""); err != nil {
			t.Fatalf("Attach = %v", err)
		}
		if len(f.chooser.options) != 2 {
			t.Errorf("候補 = %v", f.chooser.options)
		}
		if want := []string{"zellij", "attach", "two"}; !equalStrings(f.execer.commands[0], want) {
			t.Errorf("起動 = %q, want %q", f.execer.commands[0], want)
		}
	})

	t.Run("何も選ばなければ何もしない", func(t *testing.T) {
		t.Parallel()
		f := newSessionFixture("one [Created 1h ago]\ntwo [Created 2h ago]\n")
		if err := f.launcher.Attach(""); err != nil {
			t.Fatalf("Attach = %v", err)
		}
		if len(f.execer.commands) != 0 {
			t.Errorf("起動した: %q", f.execer.commands)
		}
	})
}

// TestSessionClearPending は待ち状態の一括削除を確かめる。
func TestSessionClearPending(t *testing.T) {
	t.Parallel()

	f := newSessionFixture("")
	var out bytes.Buffer
	if err := f.launcher.ClearPending(&out); err != nil {
		t.Fatalf("ClearPending = %v", err)
	}
	if !f.pending.allGone {
		t.Error("消していない")
	}
	if out.Len() == 0 {
		t.Error("何も伝えていない")
	}
}

// TestSessionAttachRevivesExited は EXITED のセッションへも attach する
// ことを確かめる。
//
// zellij は EXITED のセッションを attach で復活させる。生きているものだけを
// attach の対象にすると、入りたくて zs を叩いた利用者に対して同じ名前の空の
// セッションを新しく作ってしまい、前のタブが行方不明になる(現行版の
// `zellij attach || zellij --session` はこの場合 attach が成功する)。
func TestSessionAttachRevivesExited(t *testing.T) {
	t.Parallel()

	f := newSessionFixture("gone [Created 2h ago] (EXITED - attach to resurrect)\n")
	if err := f.launcher.Attach("gone"); err != nil {
		t.Fatalf("Attach = %v", err)
	}
	want := []string{"zellij", "attach", "gone"}
	if len(f.execer.commands) != 1 || !equalStrings(f.execer.commands[0], want) {
		t.Errorf("起動 = %q, want %q", f.execer.commands, want)
	}
}
