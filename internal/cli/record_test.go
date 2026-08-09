package cli

import (
	"errors"
	"strings"
	"testing"

	"github.com/k-kudo-hub/mdev-go/internal/app"
)

// recordCall は RecordService への 1 回の呼び出しを記録する。
type recordCall struct {
	tab string
	env app.RecordEnv
}

type fakeRecordService struct {
	calls []recordCall
	err   error
}

func (s *fakeRecordService) Execute(tab string, env app.RecordEnv) error {
	s.calls = append(s.calls, recordCall{tab: tab, env: env})
	return s.err
}

func newRecordDeps(record *fakeRecordService) Deps {
	return Deps{
		Hooks:  &fakeHookService{},
		Record: record,
		Getenv: func(key string) string { return testEnv[key] },
	}
}

func TestRecordCommandPassesTabAndEnv(t *testing.T) {
	t.Parallel()

	record := &fakeRecordService{}
	code, stderr := runCLI(t, newRecordDeps(record), "", "record", "api-feature")

	if code != exitOK {
		t.Errorf("終了コード = %d, want %d (stderr=%q)", code, exitOK, stderr)
	}
	want := []recordCall{{tab: "api-feature", env: app.RecordEnv{ZellijSession: "test-session"}}}
	if len(record.calls) != 1 || record.calls[0] != want[0] {
		t.Errorf("呼び出し = %+v, want %+v", record.calls, want)
	}
}

func TestRecordCommandWithoutArgument(t *testing.T) {
	t.Parallel()

	// 現行版はタブ名が空なら何もせず正常終了する。判断はユースケース側に
	// あるため、cli は空文字をそのまま渡す。
	record := &fakeRecordService{}
	code, stderr := runCLI(t, newRecordDeps(record), "", "record")

	if code != exitOK {
		t.Errorf("終了コード = %d, want %d (stderr=%q)", code, exitOK, stderr)
	}
	if len(record.calls) != 1 || record.calls[0].tab != "" {
		t.Errorf("呼び出し = %+v, want タブ名が空の 1 件", record.calls)
	}
}

func TestRecordCommandRejectsExtraArguments(t *testing.T) {
	t.Parallel()

	record := &fakeRecordService{}
	code, _ := runCLI(t, newRecordDeps(record), "", "record", "a", "b")

	if code != exitError {
		t.Errorf("終了コード = %d, want %d", code, exitError)
	}
	if len(record.calls) != 0 {
		t.Errorf("引数過多でも呼び出された: %+v", record.calls)
	}
}

func TestRecordCommandReportsError(t *testing.T) {
	t.Parallel()

	record := &fakeRecordService{err: errors.New("追記できない")}
	code, stderr := runCLI(t, newRecordDeps(record), "", "record", "api-feature")

	if code != exitError {
		t.Errorf("終了コード = %d, want %d", code, exitError)
	}
	if !strings.Contains(stderr, "追記できない") {
		t.Errorf("stderr = %q, want 原因を含む", stderr)
	}
}
