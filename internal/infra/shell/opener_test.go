package shell

import (
	"errors"
	"reflect"
	"testing"
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
