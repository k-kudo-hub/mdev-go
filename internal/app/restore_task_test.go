package app_test

import (
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/k-kudo-hub/mdev-go/internal/app"
	"github.com/k-kudo-hub/mdev-go/internal/domain"
)

const restoreAt = "2026-08-11T10:00:00+0900"

// taskRestoreFixture は TaskRestorer と観測用の fake をまとめて組み立てる。
type taskRestoreFixture struct {
	restorer *app.TaskRestorer
	journal  *paneJournal
	daily    *fakeDailyRestore
	creator  *fakeTaskMaker
	paths    *fakePathChecker
}

func newTaskRestoreFixture(target domain.DailyRestoreTarget, found bool) *taskRestoreFixture {
	journal := &paneJournal{}
	f := &taskRestoreFixture{
		journal: journal,
		daily:   &fakeDailyRestore{journal: journal, target: target, found: found},
		creator: &fakeTaskMaker{journal: journal},
		paths:   &fakePathChecker{dirs: map[string]bool{}, files: map[string]bool{}},
	}
	f.restorer = &app.TaskRestorer{
		Daily:   f.daily,
		Creator: f.creator,
		Paths:   f.paths,
	}
	return f
}

// TestTaskRestorerRestoresEntry は Done のタスクを作り直して daily へ
// restored を付けるまでの一巡を固定する(test.sh 26e の中心のケース)。
func TestTaskRestorerRestoresEntry(t *testing.T) {
	t.Parallel()

	f := newTaskRestoreFixture(domain.DailyRestoreTarget{
		Dir: "/w/proj", TaskType: "dev", ClaudeSessionID: "sess-1",
		TranscriptPath: "/w/t.jsonl", Agent: "codex",
	}, true)
	f.paths.dirs["/w/proj"] = true
	f.paths.files["/w/t.jsonl"] = true

	if _, err := f.restorer.Restore(dashboardEnv, "restore-me", "done-sess", restoreAt); err != nil {
		t.Fatalf("Restore() = %v", err)
	}

	want := []app.TaskSpec{
		{Dir: "/w/proj", Type: "dev", Name: "restore-me", Resume: "sess-1", Agent: "codex"},
	}
	if !reflect.DeepEqual(f.creator.specs, want) {
		t.Errorf("作り直したタスク = %+v, want %+v", f.creator.specs, want)
	}
	// 探すのが先、作るのが次、restored を付けるのが最後。順序が入れ替わると
	// 「タブは出来ていないのに Done から消える」ことが起こりうる。
	wantJournal := []string{
		"daily-find done-sess 2026-08-11 restore-me",
		"create-task restore-me",
		"daily-mark done-sess 2026-08-11 restore-me",
	}
	if !reflect.DeepEqual(f.journal.entries, wantJournal) {
		t.Errorf("副作用の並び = %v, want %v", f.journal.entries, wantJournal)
	}
}

// TestTaskRestorerResumeConditions は resume の判定を固定する。
//
// **screen 由来の合成セッション ID を除く条件は Go 版で追加したものである**
// (evidence §5-1)。現行 Shell 版はここを弾かないため、スクリーン検出だけで
// 完了を記録した codex タスクを Done から戻すと
// `codex resume screen-<slug>` という存在しない ID で起動してしまう。
func TestTaskRestorerResumeConditions(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		target     domain.DailyRestoreTarget
		transcript bool
		want       string
	}{
		{
			name: "3 条件が揃えば再開する",
			target: domain.DailyRestoreTarget{Dir: "/w", ClaudeSessionID: "sess-1",
				TranscriptPath: "/w/t.jsonl"},
			transcript: true,
			want:       "sess-1",
		},
		{
			name:       "セッション ID が無い",
			target:     domain.DailyRestoreTarget{Dir: "/w", TranscriptPath: "/w/t.jsonl"},
			transcript: true,
		},
		{
			name:   "transcript のパスが記録されていない",
			target: domain.DailyRestoreTarget{Dir: "/w", ClaudeSessionID: "sess-1"},
		},
		{
			name: "transcript が消えている",
			target: domain.DailyRestoreTarget{Dir: "/w", ClaudeSessionID: "sess-1",
				TranscriptPath: "/w/gone.jsonl"},
		},
		{
			name: "screen 由来の合成セッション ID は再開に使わない",
			target: domain.DailyRestoreTarget{Dir: "/w",
				ClaudeSessionID: domain.ScreenPendingSessionID("cx-task"),
				TranscriptPath:  "/w/t.jsonl"},
			transcript: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			f := newTaskRestoreFixture(tt.target, true)
			f.paths.dirs["/w"] = true
			if tt.transcript {
				f.paths.files[tt.target.TranscriptPath] = true
			}

			if _, err := f.restorer.Restore(dashboardEnv, "t", "s", restoreAt); err != nil {
				t.Fatalf("Restore() = %v", err)
			}
			if len(f.creator.specs) != 1 {
				t.Fatalf("作り直したタスク = %d 件, want 1", len(f.creator.specs))
			}
			if got := f.creator.specs[0].Resume; got != tt.want {
				t.Errorf("Resume = %q, want %q", got, tt.want)
			}
		})
	}
}

// TestTaskRestorerExitContract は現行 restore-task.sh の終了コード 0-5 の
// 契約を、対応する sentinel エラーとして固定する。
func TestTaskRestorerExitContract(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		tab        string
		session    string
		at         string
		target     domain.DailyRestoreTarget
		found      bool
		dirExists  bool
		createErr  error
		markErr    error
		want       error
		wantCreate bool
		wantMark   bool
	}{
		{
			name: "引数が足りない(exit 1)",
			tab:  "", session: "s", at: restoreAt,
			want: app.ErrRestoreEntryNotFound,
		},
		{
			name: "セッションが空(exit 1)",
			tab:  "t", session: "", at: restoreAt,
			want: app.ErrRestoreEntryNotFound,
		},
		{
			name: "完了時刻が空(exit 1)",
			tab:  "t", session: "s", at: "",
			want: app.ErrRestoreEntryNotFound,
		},
		{
			name: "エントリが見つからない(exit 1)",
			tab:  "t", session: "s", at: restoreAt,
			want: app.ErrRestoreEntryNotFound,
		},
		{
			name: "dir が記録されていない(exit 2)",
			tab:  "t", session: "s", at: restoreAt,
			target: domain.DailyRestoreTarget{}, found: true,
			want: app.ErrRestoreDirUnknown,
		},
		{
			name: "dir が消えている(exit 3)",
			tab:  "t", session: "s", at: restoreAt,
			target: domain.DailyRestoreTarget{Dir: "/w/removed"}, found: true,
			want: app.ErrRestoreDirMissing,
		},
		{
			name: "タブを作れなかった(exit 4)",
			tab:  "t", session: "s", at: restoreAt,
			target: domain.DailyRestoreTarget{Dir: "/w"}, found: true, dirExists: true,
			createErr: errCreateFailed,
			want:      app.ErrRestoreTabFailed, wantCreate: true,
		},
		{
			name: "daily を更新できなかった(exit 5)",
			tab:  "t", session: "s", at: restoreAt,
			target: domain.DailyRestoreTarget{Dir: "/w"}, found: true, dirExists: true,
			markErr: errDailyWrite,
			want:    app.ErrRestoreDailyUpdate, wantCreate: true, wantMark: true,
		},
		{
			name: "成功(exit 0)",
			tab:  "t", session: "s", at: restoreAt,
			target: domain.DailyRestoreTarget{Dir: "/w"}, found: true, dirExists: true,
			wantCreate: true, wantMark: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			f := newTaskRestoreFixture(tt.target, tt.found)
			if tt.dirExists {
				f.paths.dirs[tt.target.Dir] = true
			}
			f.creator.err = tt.createErr
			f.daily.markErr = tt.markErr

			_, err := f.restorer.Restore(dashboardEnv, tt.tab, tt.session, tt.at)
			if tt.want == nil && err != nil {
				t.Fatalf("Restore() = %v, want nil", err)
			}
			if tt.want != nil && !errors.Is(err, tt.want) {
				t.Fatalf("Restore() = %v, want %v", err, tt.want)
			}
			if got := len(f.creator.specs) > 0; got != tt.wantCreate {
				t.Errorf("タブ作成の有無 = %v, want %v", got, tt.wantCreate)
			}
			if got := f.daily.marked > 0; got != tt.wantMark {
				t.Errorf("restored の書き込みの有無 = %v, want %v", got, tt.wantMark)
			}
		})
	}
}

// TestTaskRestorerCountsHalfBuiltTabAsSuccess は create_task の rc=3 相当を
// 成功として扱うことを固定する(test.sh 26e の "half-built restore")。
//
// タブは出来ているので、Done に残して再試行させると同名タブが増えるだけになる。
func TestTaskRestorerCountsHalfBuiltTabAsSuccess(t *testing.T) {
	t.Parallel()

	for _, createErr := range []error{app.ErrTabNotRegistered, app.ErrFocusNotConfirmed} {
		t.Run(createErr.Error(), func(t *testing.T) {
			t.Parallel()

			f := newTaskRestoreFixture(domain.DailyRestoreTarget{Dir: "/w"}, true)
			f.paths.dirs["/w"] = true
			f.creator.err = createErr

			warning, err := f.restorer.Restore(dashboardEnv, "halfbuilt", "s", restoreAt)
			if err != nil {
				t.Fatalf("Restore() = %v", err)
			}
			if f.daily.marked != 1 {
				t.Errorf("restored を付けていない: %d", f.daily.marked)
			}
			if !strings.Contains(warning, "halfbuilt") {
				t.Errorf("警告が返っていない: %q", warning)
			}
		})
	}
}

// TestTaskRestorerUsesCompletionDate は daily ファイルを完了時刻の日付で
// 引くことを固定する(現行版の `${COMPLETED_AT:0:10}`)。
//
// Done は当日ぶんを表示するが、日付をまたいだ直後は前日の完了エントリも
// 並ぶ。今日の日付で引くと、その 1 件だけが復元できなくなる。
func TestTaskRestorerUsesCompletionDate(t *testing.T) {
	t.Parallel()

	f := newTaskRestoreFixture(domain.DailyRestoreTarget{Dir: "/w"}, true)
	f.paths.dirs["/w"] = true

	if _, err := f.restorer.Restore(dashboardEnv, "t", "s", "2026-08-10T23:59:00+0900"); err != nil {
		t.Fatalf("Restore() = %v", err)
	}
	if want := "2026-08-10"; f.daily.dates[0] != want {
		t.Errorf("引いた日付 = %q, want %q", f.daily.dates[0], want)
	}
}

// TestTaskRestorerWithShortCompletedAt は日付として使えない完了時刻でも
// 落ちないことを固定する。
func TestTaskRestorerWithShortCompletedAt(t *testing.T) {
	t.Parallel()

	f := newTaskRestoreFixture(domain.DailyRestoreTarget{}, false)
	_, err := f.restorer.Restore(dashboardEnv, "t", "s", "2026")
	if !errors.Is(err, app.ErrRestoreEntryNotFound) {
		t.Errorf("Restore() = %v, want %v", err, app.ErrRestoreEntryNotFound)
	}
}
