package app_test

import (
	"reflect"
	"strings"
	"testing"

	"github.com/k-kudo-hub/mdev-go/internal/app"
	"github.com/k-kudo-hub/mdev-go/internal/domain"
)

// restoreFixture は SessionRestorer と観測用の fake をまとめて組み立てる。
type restoreFixture struct {
	restorer *app.SessionRestorer
	journal  *paneJournal
	registry *fakeRegistryReader
	tabs     *fakeTabNames
	creator  *fakeTaskMaker
	paths    *fakePathChecker
	focuser  *fakePaneFocuser
	// warnings は直近の Restore が返した説明。
	warnings []string
}

func newRestoreFixture(entries []domain.RegistryEntry, existing []string) *restoreFixture {
	journal := &paneJournal{}
	f := &restoreFixture{
		journal:  journal,
		registry: &fakeRegistryReader{journal: journal, entries: map[string][]domain.RegistryEntry{"s1": entries}},
		tabs:     &fakeTabNames{names: existing},
		creator:  &fakeTaskMaker{journal: journal},
		paths:    &fakePathChecker{dirs: map[string]bool{}, files: map[string]bool{}},
		focuser:  &fakePaneFocuser{journal: journal},
	}
	f.restorer = &app.SessionRestorer{
		Registry: f.registry,
		Tabs:     f.tabs,
		Creator:  f.creator,
		Paths:    f.paths,
		Focuser:  f.focuser,
	}
	return f
}

// TestSessionRestorerRecreatesTasks は test.sh 26j の中心のケースである。
// transcript が残っていれば --resume 相当のセッション ID を渡し、
// 最後にダッシュボードへ戻る。
func TestSessionRestorerRecreatesTasks(t *testing.T) {
	t.Parallel()

	f := newRestoreFixture([]domain.RegistryEntry{
		{Tab: "alpha-dev", Dir: "/w/alpha", ClaudeSessionID: "sid-1",
			TranscriptPath: "/w/t.jsonl", TaskType: "dev", Agent: "codex", UpdatedAt: "2026-01-01"},
	}, []string{"Main"})
	f.paths.dirs["/w/alpha"] = true
	f.paths.files["/w/t.jsonl"] = true

	f.warnings = f.restorer.Restore(dashboardEnv)

	want := []app.TaskSpec{
		{Dir: "/w/alpha", Type: "dev", Name: "alpha-dev", Resume: "sid-1", Agent: "codex"},
	}
	if !reflect.DeepEqual(f.creator.specs, want) {
		t.Errorf("作り直したタスク = %+v, want %+v", f.creator.specs, want)
	}
	if want := []string{"create-task alpha-dev", "go-to-tab-name Main"}; !reflect.DeepEqual(f.journal.entries, want) {
		t.Errorf("副作用の並び = %v, want %v", f.journal.entries, want)
	}
}

// TestSessionRestorerResumeConditions は resume の 3 条件を固定する。
// 1 つでも欠ければ新規セッションで起動する(壊れた --resume をしない)。
func TestSessionRestorerResumeConditions(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		entry      domain.RegistryEntry
		transcript bool
		want       string
	}{
		{
			name:       "3 条件が揃えば再開する",
			entry:      domain.RegistryEntry{Tab: "t", Dir: "/w", ClaudeSessionID: "sid", TranscriptPath: "/w/t.jsonl"},
			transcript: true,
			want:       "sid",
		},
		{
			name:       "セッション ID が無い",
			entry:      domain.RegistryEntry{Tab: "t", Dir: "/w", TranscriptPath: "/w/t.jsonl"},
			transcript: true,
		},
		{
			name:  "transcript のパスが記録されていない",
			entry: domain.RegistryEntry{Tab: "t", Dir: "/w", ClaudeSessionID: "sid"},
		},
		{
			name:  "transcript が消えている",
			entry: domain.RegistryEntry{Tab: "t", Dir: "/w", ClaudeSessionID: "sid", TranscriptPath: "/w/gone.jsonl"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			f := newRestoreFixture([]domain.RegistryEntry{tt.entry}, []string{"Main"})
			f.paths.dirs["/w"] = true
			if tt.transcript {
				f.paths.files[tt.entry.TranscriptPath] = true
			}

			f.warnings = f.restorer.Restore(dashboardEnv)

			if len(f.creator.specs) != 1 {
				t.Fatalf("作り直したタスク = %d 件, want 1", len(f.creator.specs))
			}
			if got := f.creator.specs[0].Resume; got != tt.want {
				t.Errorf("Resume = %q, want %q", got, tt.want)
			}
		})
	}
}

// TestSessionRestorerSkipsAndDrops は「作り直さない」3 つの条件と、
// そのときレジストリのエントリを残すか捨てるかを固定する。
func TestSessionRestorerSkipsAndDrops(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		entry      domain.RegistryEntry
		existing   []string
		dirExists  bool
		wantRemove bool
	}{
		{
			name:     "既にタブがある(エントリは残す)",
			entry:    domain.RegistryEntry{Tab: "alive", Dir: "/w"},
			existing: []string{"Main", "alive"}, dirExists: true,
		},
		{
			name:       "dir が記録されていない(エントリを捨てる)",
			entry:      domain.RegistryEntry{Tab: "legacy"},
			existing:   []string{"Main"},
			wantRemove: true,
		},
		{
			name:       "dir が消えている(エントリを捨てる)",
			entry:      domain.RegistryEntry{Tab: "gone", Dir: "/w/removed"},
			existing:   []string{"Main"},
			wantRemove: true,
		},
		{
			name:     "タブ名が空",
			entry:    domain.RegistryEntry{Dir: "/w"},
			existing: []string{"Main"}, dirExists: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			f := newRestoreFixture([]domain.RegistryEntry{tt.entry}, tt.existing)
			if tt.dirExists {
				f.paths.dirs[tt.entry.Dir] = true
			}

			f.warnings = f.restorer.Restore(dashboardEnv)

			if len(f.creator.specs) != 0 {
				t.Errorf("作り直してしまった: %+v", f.creator.specs)
			}
			removed := f.registry.removed
			if tt.wantRemove {
				if want := []string{"s1/" + tt.entry.Tab}; !reflect.DeepEqual(removed, want) {
					t.Errorf("捨てたエントリ = %v, want %v", removed, want)
				}
			} else if len(removed) != 0 {
				t.Errorf("残すべきエントリを捨てた: %v", removed)
			}
			// 1 件も作り直していないので Main へは戻らない。
			for _, entry := range f.journal.entries {
				if entry == "go-to-tab-name Main" {
					t.Error("何も復元していないのに Main へ戻った")
				}
			}
		})
	}
}

// TestSessionRestorerCountsHalfBuiltTabs は create_task の rc=3 相当
// (タブは出来たがフォーカス未確認でペイン未構築)を復元済みとして数える
// ことを固定する。
//
// 硬い失敗と同じ扱いにすると、次回の起動ではこのタブが既存としてスキップ
// されるので永久に直らず、最後の Main 帰還も出ないためフォーカスが半端な
// タブに残ってしまう。
func TestSessionRestorerCountsHalfBuiltTabs(t *testing.T) {
	t.Parallel()

	for _, err := range []error{app.ErrTabNotRegistered, app.ErrFocusNotConfirmed} {
		t.Run(err.Error(), func(t *testing.T) {
			t.Parallel()

			f := newRestoreFixture([]domain.RegistryEntry{
				{Tab: "halfbuilt", Dir: "/w"},
			}, []string{"Main"})
			f.paths.dirs["/w"] = true
			f.creator.err = err

			f.warnings = f.restorer.Restore(dashboardEnv)

			if want := []string{"create-task halfbuilt", "go-to-tab-name Main"}; !reflect.DeepEqual(f.journal.entries, want) {
				t.Errorf("副作用の並び = %v, want %v", f.journal.entries, want)
			}
			if len(f.warnings) != 1 || !strings.Contains(f.warnings[0], "halfbuilt") {
				t.Errorf("警告が返っていない: %q", f.warnings)
			}
			// エントリは残す(次回の起動で既存タブとしてスキップされる)。
			if len(f.registry.removed) != 0 {
				t.Errorf("エントリを捨てた: %v", f.registry.removed)
			}
		})
	}
}

// TestSessionRestorerDoesNotCountHardFailures はタブそのものが作られなかった
// 場合を復元済みに数えないことを固定する。
func TestSessionRestorerDoesNotCountHardFailures(t *testing.T) {
	t.Parallel()

	f := newRestoreFixture([]domain.RegistryEntry{{Tab: "broken", Dir: "/w"}}, []string{"Main"})
	f.paths.dirs["/w"] = true
	f.creator.err = errCreateFailed

	f.warnings = f.restorer.Restore(dashboardEnv)

	if want := []string{"create-task broken"}; !reflect.DeepEqual(f.journal.entries, want) {
		t.Errorf("副作用の並び = %v, want %v", f.journal.entries, want)
	}
	if len(f.warnings) != 1 || !strings.Contains(f.warnings[0], "broken") {
		t.Errorf("警告が返っていない: %q", f.warnings)
	}
	// エントリは残す(次回の起動で再試行できるように)。
	if len(f.registry.removed) != 0 {
		t.Errorf("エントリを捨てた: %v", f.registry.removed)
	}
}

// TestSessionRestorerPicksNewestEntryPerTab は同じタブに複数のエントリが
// あるとき、updated_at が最新のものだけを作り直すことを固定する。
//
// --resume での再開はエージェントのセッション ID を変えるため、古いエントリは
// 使えない ID を持っている。
func TestSessionRestorerPicksNewestEntryPerTab(t *testing.T) {
	t.Parallel()

	f := newRestoreFixture([]domain.RegistryEntry{
		{Tab: "dup", Dir: "/w", ClaudeSessionID: "old", TranscriptPath: "/w/t.jsonl",
			UpdatedAt: "2020-01-01T00:00:00+0000"},
		{Tab: "dup", Dir: "/w", ClaudeSessionID: "new", TranscriptPath: "/w/t.jsonl",
			UpdatedAt: "2026-08-11T00:00:00+0000"},
	}, []string{"Main"})
	f.paths.dirs["/w"] = true
	f.paths.files["/w/t.jsonl"] = true

	f.warnings = f.restorer.Restore(dashboardEnv)

	if len(f.creator.specs) != 1 {
		t.Fatalf("作り直したタスク = %d 件, want 1", len(f.creator.specs))
	}
	if got := f.creator.specs[0].Resume; got != "new" {
		t.Errorf("Resume = %q, want new", got)
	}
}

// TestSessionRestorerRestoresInTabNameOrder はタブ名の昇順で作り直すことを
// 固定する(現行版の jq group_by がキー昇順で並べるのと同じ)。
func TestSessionRestorerRestoresInTabNameOrder(t *testing.T) {
	t.Parallel()

	f := newRestoreFixture([]domain.RegistryEntry{
		{Tab: "zeta", Dir: "/w"},
		{Tab: "alpha", Dir: "/w"},
		{Tab: "mid", Dir: "/w"},
	}, []string{"Main"})
	f.paths.dirs["/w"] = true

	f.warnings = f.restorer.Restore(dashboardEnv)

	got := []string{}
	for _, spec := range f.creator.specs {
		got = append(got, spec.Name)
	}
	if want := []string{"alpha", "mid", "zeta"}; !reflect.DeepEqual(got, want) {
		t.Errorf("作り直した順 = %v, want %v", got, want)
	}
}

// TestSessionRestorerIsSilentWithoutEntries はレジストリが空のときに
// zellij を一切呼ばないことを固定する(test.sh の "empty registry is a
// silent no-op")。
func TestSessionRestorerIsSilentWithoutEntries(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		entries []domain.RegistryEntry
		err     error
	}{
		{name: "エントリが 1 件も無い"},
		{name: "レジストリを読めない", err: errRegistryRead},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			f := newRestoreFixture(tt.entries, []string{"Main"})
			f.registry.err = tt.err

			f.warnings = f.restorer.Restore(dashboardEnv)

			if len(f.journal.entries) != 0 {
				t.Errorf("副作用が起きた: %v", f.journal.entries)
			}
			if f.tabs.calls != 0 {
				t.Errorf("query-tab-names を呼んだ: %d 回", f.tabs.calls)
			}
		})
	}
}

// TestSessionRestorerQueriesTabsOnce は既存タブの一覧を 1 度だけ引くことを
// 固定する。
//
// 現行版も `EXISTING_TABS=$(zellij action query-tab-names)` をループの前で
// 1 度だけ評価している。エントリはタブごとに 1 件へ畳まれているので、
// ループ中に作ったタブを数え直す必要は無い。
func TestSessionRestorerQueriesTabsOnce(t *testing.T) {
	t.Parallel()

	f := newRestoreFixture([]domain.RegistryEntry{
		{Tab: "a", Dir: "/w"},
		{Tab: "b", Dir: "/w"},
	}, []string{"Main"})
	f.paths.dirs["/w"] = true

	f.warnings = f.restorer.Restore(dashboardEnv)

	if f.tabs.calls != 1 {
		t.Errorf("query-tab-names の呼び出し = %d 回, want 1", f.tabs.calls)
	}
}

// TestSessionRestorerSkipsWhenTabQueryFails は既存タブの一覧を引けなかった
// 回に復元そのものを見送ることを固定する。
//
// 上限で打ち切られた結果を「タブが 1 つも無い」と読むと、生きているタブを
// 作り直して同じ名前のタブが二重になる。Shell 版はここでハングしていた
// (復元が進まなかった)ので、上限を付けたことで初めて開く窓である。
func TestSessionRestorerSkipsWhenTabQueryFails(t *testing.T) {
	t.Parallel()

	f := newRestoreFixture([]domain.RegistryEntry{
		{Tab: "alive", Dir: "/w"},
	}, []string{"Main", "alive"})
	f.paths.dirs["/w"] = true
	f.tabs.err = errTabQuery

	f.warnings = f.restorer.Restore(dashboardEnv)

	if len(f.creator.specs) != 0 {
		t.Errorf("タブを作り直してしまった: %+v", f.creator.specs)
	}
	if len(f.registry.removed) != 0 {
		t.Errorf("エントリを捨てた: %v", f.registry.removed)
	}
	if len(f.warnings) != 1 || !strings.Contains(f.warnings[0], "既存のタブを確認できなかった") {
		t.Errorf("警告が返っていない: %q", f.warnings)
	}
	for _, entry := range f.journal.entries {
		if entry == "go-to-tab-name Main" {
			t.Error("何も復元していないのに Main へ戻った")
		}
	}
}
