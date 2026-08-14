package cli

import (
	"errors"
	"strings"
	"testing"
)

// fakeAgentService はエージェント起動のユースケースの代役である。
type fakeAgentService struct {
	calls int
	err   error
}

func (s *fakeAgentService) Launch() error {
	s.calls++
	return s.err
}

// TestAgentLaunchCommand は `mdev agent launch` がユースケースへ委ねることを
// 確かめる。
func TestAgentLaunchCommand(t *testing.T) {
	t.Parallel()

	svc := &fakeAgentService{}
	code, stdout, stderr := runCLIWithOut(t, Deps{Agent: svc}, "agent", "launch")

	if code != exitOK {
		t.Errorf("終了コード = %d, want %d (stderr=%q)", code, exitOK, stderr)
	}
	if svc.calls != 1 {
		t.Errorf("呼び出し = %d 回, want 1", svc.calls)
	}
	// 置き換わるだけなので何も出さない。
	if stdout != "" {
		t.Errorf("標準出力 = %q, want 空", stdout)
	}
}

// TestAgentLaunchCommandReportsFailure は起動に失敗したときの報告を確かめる。
//
// このコマンドはペインの中で動くため、失敗の説明が出ないとペインが理由も
// 分からず閉じる。
func TestAgentLaunchCommandReportsFailure(t *testing.T) {
	t.Parallel()

	svc := &fakeAgentService{err: errors.New("nosuchagent が見つかりません")}
	code, _, stderr := runCLIWithOut(t, Deps{Agent: svc}, "agent", "launch")

	if code != exitError {
		t.Errorf("終了コード = %d, want %d", code, exitError)
	}
	if !strings.Contains(stderr, "nosuchagent") {
		t.Errorf("標準エラー = %q", stderr)
	}
}

// TestAgentLaunchCommandRejectsArguments は余計な引数を弾くことを確かめる。
func TestAgentLaunchCommandRejectsArguments(t *testing.T) {
	t.Parallel()

	svc := &fakeAgentService{}
	code, _, _ := runCLIWithOut(t, Deps{Agent: svc}, "agent", "launch", "extra")

	if code != exitError {
		t.Errorf("終了コード = %d, want %d", code, exitError)
	}
	if svc.calls != 0 {
		t.Errorf("弾くはずが %d 回呼ばれた", svc.calls)
	}
}
