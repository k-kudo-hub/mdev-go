package cli

import (
	"errors"
	"strings"
	"testing"

	"github.com/k-kudo-hub/mdev-go/internal/app"
)

// fakeSessionCleanService は掃除ユースケースの代役である。
type fakeSessionCleanService struct {
	dryRuns []bool
	result  app.CleanupResult
	err     error
}

func (s *fakeSessionCleanService) Clean(dryRun bool) (app.CleanupResult, error) {
	s.dryRuns = append(s.dryRuns, dryRun)
	return s.result, s.err
}

// fullPlan は 4 種類すべてに対象がある掃除の計画である。
func fullPlan() app.CleanupPlan {
	return app.CleanupPlan{
		ExitedSessions:   []string{"gone-1", "gone-2"},
		DetachedSessions: []string{"idle"},
		ZombieServers:    []app.ZombieServer{{PID: 400, Session: "zombie"}},
		OrphanClients:    []app.OrphanClient{{PID: 500}},
	}
}

func newCleanDeps(clean *fakeSessionCleanService) Deps {
	return Deps{SessionClean: clean, Getenv: func(string) string { return "" }}
}

// TestSessionsCleanShowsTargets は手で実行したときに対象を名前まで出す
// ことを確かめる。dry-run はこれを見て消してよいかを判断する。
func TestSessionsCleanShowsTargets(t *testing.T) {
	t.Parallel()

	clean := &fakeSessionCleanService{result: app.CleanupResult{Plan: fullPlan()}}
	code, out, stderr := runCLIWithOut(t, newCleanDeps(clean), "sessions", "clean")

	if code != exitOK {
		t.Fatalf("終了コード = %d, want %d (stderr=%q)", code, exitOK, stderr)
	}
	for _, want := range []string{"gone-1", "gone-2", "idle", "pid=400", "session=zombie", "pid=500", "片付けました"} {
		if !strings.Contains(out, want) {
			t.Errorf("出力に %q がありません:\n%s", want, out)
		}
	}
}

// TestSessionsCleanDryRun は --dry-run が実行しないことを伝えることを
// 確かめる。
func TestSessionsCleanDryRun(t *testing.T) {
	t.Parallel()

	clean := &fakeSessionCleanService{result: app.CleanupResult{Plan: fullPlan(), DryRun: true}}
	code, out, _ := runCLIWithOut(t, newCleanDeps(clean), "sessions", "clean", "--dry-run")

	if code != exitOK {
		t.Fatalf("終了コード = %d, want %d", code, exitOK)
	}
	if want := []bool{true}; len(clean.dryRuns) != 1 || clean.dryRuns[0] != want[0] {
		t.Errorf("dryRun = %v, want %v", clean.dryRuns, want)
	}
	if !strings.Contains(out, "--dry-run のため何も実行していません") {
		t.Errorf("dry-run の断りがありません:\n%s", out)
	}
	if strings.Contains(out, "片付けました") {
		t.Errorf("実行していないのに完了と出ています:\n%s", out)
	}
}

// TestSessionsCleanWithNothingToDo は対象が無いときの出力を確かめる。
func TestSessionsCleanWithNothingToDo(t *testing.T) {
	t.Parallel()

	clean := &fakeSessionCleanService{}
	code, out, _ := runCLIWithOut(t, newCleanDeps(clean), "sessions", "clean")

	if code != exitOK {
		t.Errorf("終了コード = %d, want %d", code, exitOK)
	}
	if !strings.Contains(out, "片付けるものはありません") {
		t.Errorf("出力 = %q", out)
	}
}

// TestSessionsCleanReportsError は手で実行したときの失敗が非 0 になる
// ことを確かめる。
func TestSessionsCleanReportsError(t *testing.T) {
	t.Parallel()

	clean := &fakeSessionCleanService{err: errors.New("zellij が無い")}
	code, _, stderr := runCLIWithOut(t, newCleanDeps(clean), "sessions", "clean")

	if code != exitError {
		t.Errorf("終了コード = %d, want %d", code, exitError)
	}
	if !strings.Contains(stderr, "zellij が無い") {
		t.Errorf("stderr = %q, want 原因を含む", stderr)
	}
}

// TestSessionsCleanAuto は --auto の契約を固定する。
//
// これはセッションを開くたびに走るので、要約 1 行に抑え、何があっても
// 起動を止めない。
func TestSessionsCleanAuto(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		clean    *fakeSessionCleanService
		wantOut  string
		wantNone bool
	}{
		{
			// 数えるのは **実際に片付けた件数** である。
			name: "掃除した",
			clean: &fakeSessionCleanService{result: app.CleanupResult{
				Plan: fullPlan(),
				Done: app.CleanupCounts{
					ExitedSessions: 2, DetachedSessions: 1, ZombieServers: 1, OrphanClients: 1,
				},
			}},
			wantOut: "掃除: 終了済み 2 件, 未使用 1 件, ゾンビ 1 件, 残骸 1 件\n",
		},
		{
			// 計画に載っていても、消す直前の再確認で全部飛ばされることが
			// ある。ここで計画の件数を出すと「掃除した」と嘘になる。
			name:     "計画はあるが 1 件も片付かなければ無言",
			clean:    &fakeSessionCleanService{result: app.CleanupResult{Plan: fullPlan()}},
			wantNone: true,
		},
		{
			// 毎回何か出ると起動時の画面が埋まる。
			name:     "掃除するものが無ければ無言",
			clean:    &fakeSessionCleanService{},
			wantNone: true,
		},
		{
			// 失敗を理由に起動を止める価値は無い。
			name:     "失敗しても無言で正常終了",
			clean:    &fakeSessionCleanService{err: errors.New("zellij が無い")},
			wantNone: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			code, out, stderr := runCLIWithOut(t, newCleanDeps(tt.clean), "sessions", "clean", "--auto")

			// **どの経路でも exit 0。** 起動を止めないための契約である。
			if code != exitOK {
				t.Fatalf("終了コード = %d, want %d (stderr=%q)", code, exitOK, stderr)
			}
			if tt.wantNone {
				if out != "" || stderr != "" {
					t.Errorf("無言のはずが出力しました: out=%q stderr=%q", out, stderr)
				}
				return
			}
			if out != tt.wantOut {
				t.Errorf("出力 = %q, want %q", out, tt.wantOut)
			}
		})
	}
}

// TestSessionsCleanAutoWithDryRun は --auto と --dry-run を併用しても
// ユースケースへ dry-run が伝わることを確かめる。
func TestSessionsCleanAutoWithDryRun(t *testing.T) {
	t.Parallel()

	clean := &fakeSessionCleanService{result: app.CleanupResult{Plan: fullPlan(), DryRun: true}}
	if code, _, _ := runCLIWithOut(t, newCleanDeps(clean), "sessions", "clean", "--auto", "--dry-run"); code != exitOK {
		t.Fatalf("終了コード = %d, want %d", code, exitOK)
	}
	if len(clean.dryRuns) != 1 || !clean.dryRuns[0] {
		t.Errorf("dryRun = %v, want [true]", clean.dryRuns)
	}
}
