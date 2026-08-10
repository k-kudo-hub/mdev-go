package shell

import (
	"errors"
	"reflect"
	"testing"
	"time"
)

func TestOpenerPrefersOpenOverXdgOpen(t *testing.T) {
	t.Parallel()

	var opened []string
	o := &Opener{
		lookPath: func(string) (string, error) { return "/usr/bin/open", nil },
		run: func(name string, args ...string) error {
			opened = append(opened, name+" "+args[0])
			return nil
		},
	}
	o.Open("https://example.com")

	if want := []string{"open https://example.com"}; !reflect.DeepEqual(opened, want) {
		t.Errorf("実行 = %v, want %v", opened, want)
	}
}

func TestOpenerFallsBackToXdgOpen(t *testing.T) {
	t.Parallel()

	var opened []string
	o := &Opener{
		lookPath: func(name string) (string, error) {
			if name == "open" {
				return "", errors.New("見つからない")
			}
			return "/usr/bin/xdg-open", nil
		},
		run: func(name string, args ...string) error {
			opened = append(opened, name+" "+args[0])
			return nil
		},
	}
	o.Open("https://example.com")

	if want := []string{"xdg-open https://example.com"}; !reflect.DeepEqual(opened, want) {
		t.Errorf("実行 = %v, want %v", opened, want)
	}
}

func TestOpenerDoesNothingWithoutCommands(t *testing.T) {
	t.Parallel()

	called := false
	o := &Opener{
		lookPath: func(string) (string, error) { return "", errors.New("見つからない") },
		run: func(string, ...string) error {
			called = true
			return nil
		},
	}
	o.Open("https://example.com")

	if called {
		t.Error("使えるコマンドが無いのに実行している")
	}
}

func TestOpenerIgnoresFailure(t *testing.T) {
	t.Parallel()

	// 失敗しても何も返さない(現行版も 2>/dev/null で握り潰している)。
	o := &Opener{
		lookPath: func(string) (string, error) { return "/usr/bin/open", nil },
		run:      func(string, ...string) error { return errors.New("失敗") },
	}
	o.Open("https://example.com")
}

func TestRunOpenRunsRealCommand(t *testing.T) {
	t.Parallel()

	// 既定の実行関数が実プロセスを起動することを、確実に存在するコマンドと
	// 存在しないコマンドで確認する。
	if err := runOpen("true"); err != nil {
		t.Errorf("runOpen(true) = %v, want nil", err)
	}
	if err := runOpen("mdev-no-such-command-for-test"); err == nil {
		t.Error("存在しないコマンドで nil が返った")
	}
}

func TestOpenTimeoutIsTenSeconds(t *testing.T) {
	t.Parallel()

	// `open` は即座に返るコマンドなので、10 秒かかる時点で異常である。
	// 上限が無いと返らないコマンドが goroutine ごと mdev の終了まで残る。
	if openTimeout != 10*time.Second {
		t.Errorf("openTimeout = %v, want 10s", openTimeout)
	}
}
