package cli

import (
	"errors"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/k-kudo-hub/mdev-go/internal/app"
)

// fakeSessionService はセッション起動のユースケースの代役である。
type fakeSessionService struct {
	requests []app.SessionRequest
	devNames []string
	attached []string
	cleared  int
	err      error
}

func (s *fakeSessionService) Start(_ io.Writer, req app.SessionRequest) error {
	s.requests = append(s.requests, req)
	return s.err
}

func (s *fakeSessionService) StartDev(name string) error {
	s.devNames = append(s.devNames, name)
	return s.err
}

func (s *fakeSessionService) Attach(name string) error {
	s.attached = append(s.attached, name)
	return s.err
}

func (s *fakeSessionService) ClearPending(io.Writer) error {
	s.cleared++
	return s.err
}

// sessionDeps はセッション系コマンドの依存を組み立てる。
func sessionDeps(svc *fakeSessionService) Deps {
	return Deps{
		Session: svc,
		Getwd:   func() string { return "/Users/dev/projects/myapp" },
		Now:     func() time.Time { return time.Date(2026, 8, 14, 17, 42, 41, 0, time.UTC) },
	}
}

// TestRootStartsSession は引数なしの `mdev` がセッションを開くことを確かめる。
//
// 起動の入口を 1 語に保つのがこのコマンドの要点である。
func TestRootStartsSession(t *testing.T) {
	t.Parallel()

	svc := &fakeSessionService{}
	code, _, stderr := runCLIWithOut(t, sessionDeps(svc))

	if code != exitOK {
		t.Errorf("終了コード = %d, want %d (stderr=%q)", code, exitOK, stderr)
	}
	want := app.SessionRequest{Dir: "/Users/dev/projects/myapp"}
	if len(svc.requests) != 1 || svc.requests[0] != want {
		t.Errorf("指定 = %+v, want %+v", svc.requests, want)
	}
}

// TestRootTreatsUnknownArgAsSessionName は未知の引数をセッション名として
// 扱うことを確かめる。
//
// 既知の子コマンドは cobra がそちらへ渡す。その結果、子コマンドと同じ名前の
// セッションは名前で開けない(`mdev news` は News の取得になる)。起動の
// 入口を 1 語で保つほうの利益が上回るという判断である。
func TestRootTreatsUnknownArgAsSessionName(t *testing.T) {
	t.Parallel()

	svc := &fakeSessionService{}
	code, _, _ := runCLIWithOut(t, sessionDeps(svc), "my-session")

	if code != exitOK {
		t.Errorf("終了コード = %d, want %d", code, exitOK)
	}
	if len(svc.requests) != 1 || svc.requests[0].Name != "my-session" {
		t.Errorf("指定 = %+v", svc.requests)
	}
}

// TestRootNewFlag は --new が時刻を付けることを確かめる。
func TestRootNewFlag(t *testing.T) {
	t.Parallel()

	svc := &fakeSessionService{}
	if code, _, _ := runCLIWithOut(t, sessionDeps(svc), "--new"); code != exitOK {
		t.Fatalf("終了コード = %d", code)
	}
	if got := svc.requests[0].Stamp; got != "174241" {
		t.Errorf("時刻 = %q, want 174241", got)
	}
}

// TestRootReportsFailure は起動の失敗を伝えることを確かめる。
func TestRootReportsFailure(t *testing.T) {
	t.Parallel()

	svc := &fakeSessionService{err: errors.New("zellij がありません")}
	code, _, stderr := runCLIWithOut(t, sessionDeps(svc))

	if code != exitError {
		t.Errorf("終了コード = %d, want %d", code, exitError)
	}
	if !strings.Contains(stderr, "zellij") {
		t.Errorf("標準エラー = %q", stderr)
	}
}

// TestSessionAliasCommands は dev / attach / pending clear の配線を確かめる。
//
// これらは `mdev init zsh` が出すエイリアス(dev / zs / pending-clear)の
// 実体である。
func TestSessionAliasCommands(t *testing.T) {
	t.Parallel()

	t.Run("dev", func(t *testing.T) {
		t.Parallel()
		svc := &fakeSessionService{}
		runCLIWithOut(t, sessionDeps(svc), "dev", "myname")
		if len(svc.devNames) != 1 || svc.devNames[0] != "myname" {
			t.Errorf("名前 = %v", svc.devNames)
		}
	})

	t.Run("dev は名前を省ける", func(t *testing.T) {
		t.Parallel()
		svc := &fakeSessionService{}
		runCLIWithOut(t, sessionDeps(svc), "dev")
		if len(svc.devNames) != 1 || svc.devNames[0] != "" {
			t.Errorf("名前 = %v", svc.devNames)
		}
	})

	t.Run("attach", func(t *testing.T) {
		t.Parallel()
		svc := &fakeSessionService{}
		runCLIWithOut(t, sessionDeps(svc), "attach", "other")
		if len(svc.attached) != 1 || svc.attached[0] != "other" {
			t.Errorf("名前 = %v", svc.attached)
		}
	})

	t.Run("pending clear", func(t *testing.T) {
		t.Parallel()
		svc := &fakeSessionService{}
		runCLIWithOut(t, sessionDeps(svc), "pending", "clear")
		if svc.cleared != 1 {
			t.Errorf("呼び出し = %d 回, want 1", svc.cleared)
		}
	})
}

// TestInitZshCommand は出力がそのまま eval できる形であることを確かめる。
func TestInitZshCommand(t *testing.T) {
	t.Parallel()

	code, stdout, stderr := runCLIWithOut(t, Deps{}, "init", "zsh")

	if code != exitOK {
		t.Errorf("終了コード = %d, want %d (stderr=%q)", code, exitOK, stderr)
	}
	if stdout != app.InitZshScript {
		t.Errorf("標準出力 = %q", stdout)
	}
	// mdev 自身は定義しない(PATH 上のバイナリが受ける)。
	if strings.Contains(stdout, "alias mdev=") || strings.Contains(stdout, "mdev()") {
		t.Errorf("mdev を定義している:\n%s", stdout)
	}
}

// TestRootRejectsCommandTypo は既知のコマンドと 1 文字違いの引数を
// 差し戻すことを確かめる。
//
// 未知の引数をセッション名として扱う以上、`mdev instal` が黙って
// 「instal というセッションを開く」に化ける。
func TestRootRejectsCommandTypo(t *testing.T) {
	t.Parallel()

	for _, typo := range []string{"instal", "uninstal", "nws", "tes", "updat"} {
		t.Run(typo, func(t *testing.T) {
			t.Parallel()
			svc := &fakeSessionService{}
			code, _, stderr := runCLIWithOut(t, sessionDeps(svc), typo)

			if code != exitError {
				t.Errorf("終了コード = %d, want %d", code, exitError)
			}
			if len(svc.requests) != 0 {
				t.Errorf("セッションを開いた: %+v", svc.requests)
			}
			// 本当にその名前で開きたい場合の逃げ道を案内する。
			if !strings.Contains(stderr, "mdev attach "+typo) {
				t.Errorf("逃げ道が案内されていない: %q", stderr)
			}
		})
	}
}

// TestRootOpensIntentionalNames は打ち間違いでない名前をそのまま開くことを
// 確かめる。
//
// **差し戻しを広げすぎないことが要点である。** 承認済みの「未知引数 =
// セッション名」が使えなくなっては本末転倒になる。
func TestRootOpensIntentionalNames(t *testing.T) {
	t.Parallel()

	for _, name := range []string{"my-project", "api-server", "fix-bug", "レビュー", "newsroom"} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			svc := &fakeSessionService{}
			code, stdout, stderr := runCLIWithOut(t, sessionDeps(svc), name)

			if code != exitOK {
				t.Errorf("終了コード = %d, want %d (stderr=%q)", code, exitOK, stderr)
			}
			if len(svc.requests) != 1 || svc.requests[0].Name != name {
				t.Fatalf("指定 = %+v", svc.requests)
			}
			// 開く直前に名前を出す。差し戻しをすり抜けた打ち間違いでも
			// 画面に出ていれば気づける。
			if !strings.Contains(stdout, name) {
				t.Errorf("開く名前を出していない: %q", stdout)
			}
		})
	}
}

// TestRootWithoutArgsSaysNothing は引数なしのときに余計な行を出さないことを
// 確かめる。一番よく使う経路なので静かにする。
func TestRootWithoutArgsSaysNothing(t *testing.T) {
	t.Parallel()

	svc := &fakeSessionService{}
	_, stdout, _ := runCLIWithOut(t, sessionDeps(svc))
	if stdout != "" {
		t.Errorf("標準出力 = %q, want 空", stdout)
	}
}
