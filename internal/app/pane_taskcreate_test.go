package app_test

import (
	"encoding/json"
	"reflect"
	"testing"

	"github.com/k-kudo-hub/mdev-go/internal/app"
	"github.com/k-kudo-hub/mdev-go/internal/domain"
)

// task-create-loop.sh の n フローが読む設定と候補の組み立てを固定する。
// 期待値の根拠は test.sh 15(task_types / search_dirs / search_depth)、
// 15b(agent 選択)、33(名前入力スキップ)。

// fakeDirLister は起点ごとの候補を持つ。
type fakeDirLister struct {
	// byRoot は起点ごとに返す候補。ここに無い起点は存在しない扱い。
	byRoot map[string][]string
	// depths は ListDirs に渡された深さ。
	depths []int
	// roots は ListDirs に渡された起点。
	roots [][]string
}

var _ app.DirLister = (*fakeDirLister)(nil)

func (f *fakeDirLister) ListDirs(roots []string, depth int) []string {
	f.depths = append(f.depths, depth)
	f.roots = append(f.roots, roots)
	dirs := []string{}
	for _, root := range roots {
		dirs = append(dirs, f.byRoot[root]...)
	}
	return dirs
}

func (f *fakeDirLister) IsDir(path string) bool {
	_, ok := f.byRoot[path]
	return ok
}

func newTaskCreateFixture(t *testing.T, configJSON string, dirs *fakeDirLister) *app.TaskCreatePane {
	t.Helper()

	var config domain.Config
	if err := json.Unmarshal([]byte(configJSON), &config); err != nil {
		t.Fatalf("設定の解釈に失敗: %v", err)
	}
	journal := &paneJournal{}
	clock := newStopwatch()
	return &app.TaskCreatePane{
		Config: &fakeConfigLoader{config: config},
		Dirs:   dirs,
		Home:   "/home/u",
		Creator: &app.TaskCreator{
			Tabs:        &fakeTabActor{journal: journal, clock: clock},
			ScreenState: &fakeScreenStateRemover{journal: journal},
			Config:      &fakeConfigLoader{config: config},
			Clock:       clock,
			Sleeper:     clock,
			Launcher:    fakeLauncher{},
		},
	}
}

func TestTaskCreateDirectories(t *testing.T) {
	t.Parallel()

	dirs := &fakeDirLister{byRoot: map[string][]string{
		"/home/u/projects": {"/home/u/projects/alpha", "/home/u/projects/beta"},
		"/home/u/works":    {"/home/u/works/gamma"},
	}}
	pane := newTaskCreateFixture(t,
		`{"search_dirs": ["~/projects", "~/works", "~/missing"], "search_depth": 2}`, dirs)

	got, rootsFound := pane.Directories()
	if !rootsFound {
		t.Fatal("起点が見つからない扱いになっている")
	}
	want := []string{"/home/u/projects/alpha", "/home/u/projects/beta", "/home/u/works/gamma"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Directories() = %v, want %v", got, want)
	}
	// 実在しない起点は落とす(現行版の `[[ -d "$expanded" ]]`)。
	if want := []string{"/home/u/projects", "/home/u/works"}; !reflect.DeepEqual(dirs.roots[0], want) {
		t.Errorf("起点 = %v, want %v", dirs.roots[0], want)
	}
	if dirs.depths[0] != 2 {
		t.Errorf("深さ = %d, want 2", dirs.depths[0])
	}
}

func TestTaskCreateDirectoriesReportsMissingRoots(t *testing.T) {
	t.Parallel()

	// 起点が 1 つも実在しないときだけ「検索対象ディレクトリが見つかりません」。
	pane := newTaskCreateFixture(t,
		`{"search_dirs": ["~/nope"]}`, &fakeDirLister{byRoot: map[string][]string{}})

	dirs, rootsFound := pane.Directories()
	if rootsFound {
		t.Error("起点が見つかった扱いになっている")
	}
	if len(dirs) != 0 {
		t.Errorf("候補 = %v, want 空", dirs)
	}
}

func TestTaskCreateDirectoriesWithRootsButNoChildren(t *testing.T) {
	t.Parallel()

	// 起点はあるが配下が空。現行版は fzf に何も渡らず黙ってメニューへ戻る
	// (赤字は出さない)ので、rootsFound は真のままにする。
	pane := newTaskCreateFixture(t,
		`{"search_dirs": ["~/projects"]}`,
		&fakeDirLister{byRoot: map[string][]string{"/home/u/projects": nil}})

	dirs, rootsFound := pane.Directories()
	if !rootsFound {
		t.Error("起点はあるのに見つからない扱いになっている")
	}
	if len(dirs) != 0 {
		t.Errorf("候補 = %v, want 空", dirs)
	}
}

func TestTaskCreateDirectoriesDefaultDepth(t *testing.T) {
	t.Parallel()

	// search_depth が無ければ 1(現行版は fd が失敗して候補ゼロになる。
	// 意図的な挙動差)。
	dirs := &fakeDirLister{byRoot: map[string][]string{"/home/u/projects": {"/home/u/projects/a"}}}
	pane := newTaskCreateFixture(t, `{"search_dirs": ["~/projects"]}`, dirs)

	if _, ok := pane.Directories(); !ok {
		t.Fatal("起点が見つからない扱いになっている")
	}
	if dirs.depths[0] != 1 {
		t.Errorf("深さ = %d, want 1", dirs.depths[0])
	}
}

func TestTaskCreateTaskTypesKeepConfigOrder(t *testing.T) {
	t.Parallel()

	pane := newTaskCreateFixture(t, `{"task_types": {
	  "dev":    {"description": "Claude Code + LazyVim"},
	  "review": {"description": "Claude Code only"},
	  "k8s":    {"description": "Claude Code + k9s"}
	}}`, &fakeDirLister{})

	want := []app.TaskTypeChoice{
		{Name: "dev", Description: "Claude Code + LazyVim"},
		{Name: "review", Description: "Claude Code only"},
		{Name: "k8s", Description: "Claude Code + k9s"},
	}
	if got := pane.TaskTypes(); !reflect.DeepEqual(got, want) {
		t.Errorf("TaskTypes() = %+v, want %+v", got, want)
	}
}

func TestTaskCreateAgents(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		config string
		want   []string
	}{
		{"設定が無ければ空(旧来の単一エージェント経路)", `{}`, nil},
		{"1 件なら 1 件", `{"agents": {"claude": {}}}`, []string{"claude"}},
		{"複数は記述順", `{"agents": {"claude": {}, "codex": {}}}`, []string{"claude", "codex"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			pane := newTaskCreateFixture(t, tc.config, &fakeDirLister{})
			if got := pane.Agents(); !reflect.DeepEqual(got, tc.want) {
				t.Errorf("Agents() = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestTaskCreateSkipNameInput(t *testing.T) {
	t.Parallel()

	tests := []struct {
		config string
		want   bool
	}{
		{`{"skip_task_name_input": true}`, true},
		{`{"skip_task_name_input": false}`, false},
		{`{}`, false},
	}
	for _, tc := range tests {
		t.Run(tc.config, func(t *testing.T) {
			t.Parallel()
			pane := newTaskCreateFixture(t, tc.config, &fakeDirLister{})
			if got := pane.SkipNameInput(); got != tc.want {
				t.Errorf("SkipNameInput() = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestTaskCreateNameHelpers(t *testing.T) {
	t.Parallel()

	pane := newTaskCreateFixture(t, `{}`, &fakeDirLister{})
	if got := pane.DefaultName("/home/u/myapp", "dev"); got != "myapp-dev" {
		t.Errorf("DefaultName() = %q, want myapp-dev", got)
	}
	if got := pane.ResolveName("myapp-dev", ""); got != "myapp-dev" {
		t.Errorf("ResolveName(空) = %q, want myapp-dev", got)
	}
	if got := pane.ResolveName("myapp-dev", "typed"); got != "typed" {
		t.Errorf("ResolveName(入力あり) = %q, want typed", got)
	}
}

func TestTaskCreateCreate(t *testing.T) {
	t.Parallel()

	pane := newTaskCreateFixture(t,
		`{"agents": {"codex": {"command": "codex"}}, "task_types": {"dev": {"layout": []}}}`,
		&fakeDirLister{})
	actor, ok := pane.Creator.Tabs.(*fakeTabActor)
	if !ok {
		t.Fatal("fake の型が違う")
	}
	actor.tabToRegister = "myapp-dev"
	actor.registerAfter = 1

	warning, err := pane.Create(app.PaneEnv{ZellijSession: "s1"}, "/d", "dev", "myapp-dev", "codex")
	if err != nil {
		t.Fatalf("Create() = %v", err)
	}
	if warning != "" {
		t.Errorf("警告 = %q, want 空", warning)
	}
}

func TestTaskCreateCreateReportsFailure(t *testing.T) {
	t.Parallel()

	// タブが登録されなければ失敗が返る。このときペインは 1 枚も作られていない。
	pane := newTaskCreateFixture(t, `{"task_types": {"dev": {"layout": []}}}`, &fakeDirLister{})
	if _, err := pane.Create(app.PaneEnv{ZellijSession: "s1"}, "/d", "dev", "never", ""); err == nil {
		t.Fatal("Create() が成功してしまった")
	}
}

func TestFilterCandidatesEntryPoint(t *testing.T) {
	t.Parallel()

	// tui は domain を参照できないため、絞り込みは app 経由で呼ぶ。
	items := []string{"/a/alpha", "/a/beta"}
	if got := app.FilterCandidates(items, "alp"); !reflect.DeepEqual(got, []string{"/a/alpha"}) {
		t.Errorf("FilterCandidates() = %v", got)
	}
}
