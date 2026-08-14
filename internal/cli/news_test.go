package cli

import (
	"strings"
	"testing"
)

// fakeNewsService は `mdev news fetch` のユースケースの代役である。
type fakeNewsService struct {
	// forces は Refresh に渡された force。呼ばれた回数も分かる。
	forces []bool
}

func (s *fakeNewsService) Refresh(force bool) { s.forces = append(s.forces, force) }

// TestNewsFetchCommand は `mdev news fetch` の引数の受け渡しを確かめる。
//
// 無印と --force の違いはユースケース側の分岐にそのまま効くため、
// フラグの取り違えは「毎回通信する」か「一日取れない」に直結する。
func TestNewsFetchCommand(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		args []string
		want bool
	}{
		{name: "無印は force なし", args: []string{"news", "fetch"}, want: false},
		{name: "--force は force あり", args: []string{"news", "fetch", "--force"}, want: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			svc := &fakeNewsService{}
			code, stdout, stderr := runCLIWithOut(t, Deps{News: svc}, tt.args...)

			if code != exitOK {
				t.Errorf("終了コード = %d, want %d (stderr=%q)", code, exitOK, stderr)
			}
			if len(svc.forces) != 1 {
				t.Fatalf("呼び出し = %d 回, want 1", len(svc.forces))
			}
			if svc.forces[0] != tt.want {
				t.Errorf("force = %v, want %v", svc.forces[0], tt.want)
			}
			// 起動のたびに走るため、成功時は何も出さない(現行版と同じ)。
			if stdout != "" {
				t.Errorf("標準出力 = %q, want 空", stdout)
			}
		})
	}
}

// TestNewsFetchCommandRejectsArguments は余計な引数を弾くことを確かめる。
// 取り違えたまま黙って走ると、意図と違う動きに気付けない。
func TestNewsFetchCommandRejectsArguments(t *testing.T) {
	t.Parallel()

	svc := &fakeNewsService{}
	code, _, stderr := runCLIWithOut(t, Deps{News: svc}, "news", "fetch", "extra")

	if code != exitError {
		t.Errorf("終了コード = %d, want %d", code, exitError)
	}
	if len(svc.forces) != 0 {
		t.Errorf("弾くはずが %d 回呼ばれた", len(svc.forces))
	}
	if !strings.Contains(stderr, "extra") {
		t.Errorf("標準エラー = %q", stderr)
	}
}
