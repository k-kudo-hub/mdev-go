package app_test

import (
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/k-kudo-hub/mdev-go/internal/app"
	"github.com/k-kudo-hub/mdev-go/internal/domain"
)

// create_task(task-lib.sh:320-406)の移植を固定する。
//
// 期待値の根拠は test.sh の 17b(TASK_TYPE / --resume の受け渡し)、
// 17b2(.agent.command)、17b3(名前付き agent と TASK_AGENT)、
// 17b8(タブ生成レースの防御)、17b10(全体予算)である。

// taskFixture は CreateTask の 1 回ぶんの実行環境である。
type taskFixture struct {
	creator *app.TaskCreator
	journal *paneJournal
	tabs    *fakeTabActor
	screen  *fakeScreenStateRemover
	clock   *fakeStopwatch
}

const defaultTaskConfig = `{
  "agent": {"command": "claude", "resume_args": "--resume"},
  "agents": {
    "claude": {"command": "claude", "resume_args": "--resume"},
    "codex":  {"command": "codex",  "resume_args": "resume"}
  },
  "task_types": {"dev": {"layout": [{"action": "move-focus", "direction": "left"}]},
                 "review": {"layout": []}}
}`

func newTaskFixture(t *testing.T, configJSON string) *taskFixture {
	t.Helper()

	var config domain.Config
	if err := json.Unmarshal([]byte(configJSON), &config); err != nil {
		t.Fatalf("設定の解釈に失敗: %v", err)
	}
	journal := &paneJournal{}
	clock := newStopwatch()
	tabs := &fakeTabActor{journal: journal, clock: clock, tabNames: []string{"Main"}}
	screen := &fakeScreenStateRemover{journal: journal}
	return &taskFixture{
		creator: &app.TaskCreator{
			Tabs:        tabs,
			ScreenState: screen,
			Config:      &fakeConfigLoader{config: config},
			Clock:       clock,
			Sleeper:     clock,
			Launcher:    fakeLauncher{},
		},
		journal: journal,
		tabs:    tabs,
		screen:  screen,
		clock:   clock,
	}
}

// registers はタブが即座に登録される(健全なサーバの)状態にする。
func (f *taskFixture) registers(name string) *taskFixture {
	f.tabs.tabToRegister = name
	f.tabs.registerAfter = 1
	return f
}

var taskEnv = app.PaneEnv{ZellijSession: "s1"}

// ---- 起動コマンドの組み立て ---------------------------------------------

func TestCreateTaskLaunchCommand(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		spec app.TaskSpec
		want string
	}{
		{
			name: "TASK_TAB_NAME と TASK_TYPE を渡す",
			spec: app.TaskSpec{Dir: "/tmp/proj", Type: "dev", Name: "restore-me"},
			want: "new-tab restore-me /tmp/proj -- env TASK_TAB_NAME=restore-me TASK_TYPE=dev claude",
		},
		{
			name: "resume 付きは resume_args とセッション ID が後ろに付く",
			spec: app.TaskSpec{Dir: "/tmp/proj", Type: "dev", Name: "resume-me", Resume: "sess-xyz"},
			want: "new-tab resume-me /tmp/proj -- env TASK_TAB_NAME=resume-me TASK_TYPE=dev claude --resume sess-xyz",
		},
		{
			name: "名前付き agent は TASK_AGENT が付き、そのコマンドで起動する",
			spec: app.TaskSpec{Dir: "/tmp/proj", Type: "dev", Name: "codex-tab", Agent: "codex"},
			want: "new-tab codex-tab /tmp/proj -- env TASK_TAB_NAME=codex-tab TASK_TYPE=dev TASK_AGENT=codex codex",
		},
		{
			name: "名前付き agent の resume は その agent の resume_args を使う",
			spec: app.TaskSpec{Dir: "/tmp/proj", Type: "dev", Name: "codex-res", Resume: "sess-abc", Agent: "codex"},
			want: "new-tab codex-res /tmp/proj -- env TASK_TAB_NAME=codex-res TASK_TYPE=dev TASK_AGENT=codex codex resume sess-abc",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			f := newTaskFixture(t, defaultTaskConfig).registers(tc.spec.Name)
			if _, err := f.creator.Execute(taskEnv, tc.spec); err != nil {
				t.Fatalf("Execute() = %v", err)
			}
			if got := f.journal.entries[f.journal.indexOf("new-tab")]; got != tc.want {
				t.Errorf("new-tab = %q\nwant %q", got, tc.want)
			}
		})
	}
}

func TestCreateTaskHonorsLegacyAgentCommand(t *testing.T) {
	t.Parallel()

	// .agents を選ばなかったタスクは .agent.command で起動する。
	// 複数語のコマンドは空白で分割される(ラッパー経由の起動)。
	f := newTaskFixture(t,
		`{"agent": {"command": "fdev exec wrapper -- claude", "resume_args": "--continue"}}`).
		registers("wrapped")
	if _, err := f.creator.Execute(taskEnv, app.TaskSpec{Dir: "/d", Type: "dev", Name: "wrapped"}); err != nil {
		t.Fatalf("Execute() = %v", err)
	}
	want := "new-tab wrapped /d -- env TASK_TAB_NAME=wrapped TASK_TYPE=dev fdev exec wrapper -- claude"
	if got := f.journal.entries[f.journal.indexOf("new-tab")]; got != want {
		t.Errorf("new-tab = %q\nwant %q", got, want)
	}
}

// ---- 防御シーケンス ------------------------------------------------------

func TestCreateTaskSequence(t *testing.T) {
	t.Parallel()

	f := newTaskFixture(t, defaultTaskConfig).registers("my-task")
	result, err := f.creator.Execute(taskEnv,
		app.TaskSpec{Dir: "/tmp/proj", Type: "dev", Name: "my-task"})
	if err != nil {
		t.Fatalf("Execute() = %v", err)
	}
	if result.Warning != "" {
		t.Errorf("警告 = %q, want 空", result.Warning)
	}

	// screen-state の削除は new-tab より前。前のタスクの状態を引き継がせない。
	got := f.journal.entries
	if len(got) < 4 {
		t.Fatalf("記録が短すぎる: %v", got)
	}
	head := got[:4]
	want := []string{
		"screen-state-remove " + domain.ScreenTabSlug("my-task"),
		"new-tab my-task /tmp/proj -- env TASK_TAB_NAME=my-task TASK_TYPE=dev claude",
		"query-tab-names",
		"go-to-tab-name my-task",
	}
	if !reflect.DeepEqual(head, want) {
		t.Errorf("先頭の並び = %v\nwant %v", head, want)
	}

	// フォーカス確認のあとに task-control ペイン → 縮小 30 回 →
	// focus-previous-pane → レイアウト、の順で続く。
	if got[4] != "new-pane down /tmp/proj -- /x/bin/mdev pane task-control my-task" {
		t.Errorf("task-control ペイン = %q", got[4])
	}
	if n := f.journal.count("resize decrease up"); n != 30 {
		t.Errorf("縮小の回数 = %d, want 30", n)
	}
	if last := got[len(got)-1]; last != "move-focus left" {
		t.Errorf("最後 = %q, want レイアウトの move-focus left", last)
	}
}

func TestCreateTaskHealthyServerCostsOneQueryAndOneFocus(t *testing.T) {
	t.Parallel()

	// test.sh 17b8「healthy server costs exactly one query + one focus」。
	f := newTaskFixture(t, defaultTaskConfig).registers("fast-tab")
	if _, err := f.creator.Execute(taskEnv,
		app.TaskSpec{Dir: "/d", Type: "review", Name: "fast-tab"}); err != nil {
		t.Fatalf("Execute() = %v", err)
	}
	if got := f.journal.count("query-tab-names"); got != 1 {
		t.Errorf("query-tab-names = %d 回, want 1", got)
	}
	if got := f.journal.count("go-to-tab-name"); got != 1 {
		t.Errorf("go-to-tab-name = %d 回, want 1", got)
	}
	if len(f.clock.slept) != 0 {
		t.Errorf("健全なサーバでは待たないこと: %v", f.clock.slept)
	}
}

func TestCreateTaskWaitsForDelayedRegistration(t *testing.T) {
	t.Parallel()

	// test.sh 17b8「create_task succeeds when tab registration is delayed」。
	f := newTaskFixture(t, defaultTaskConfig)
	f.tabs.tabToRegister = "slow-tab"
	f.tabs.registerAfter = 3

	if _, err := f.creator.Execute(taskEnv,
		app.TaskSpec{Dir: "/d", Type: "review", Name: "slow-tab"}); err != nil {
		t.Fatalf("Execute() = %v", err)
	}
	if got := f.journal.count("query-tab-names"); got < 3 {
		t.Errorf("query-tab-names = %d 回, want 3 以上", got)
	}
	// フォーカスは 3 回目の query より後でなければならない。
	goto3 := f.journal.indexOf("go-to-tab-name")
	if goto3 < 3 {
		t.Errorf("登録の確認前にフォーカスを撃っている(位置 %d)", goto3)
	}
	if f.journal.indexOf("new-pane") < goto3 {
		t.Error("フォーカス確認前にペインを作っている")
	}
}

func TestCreateTaskRetriesFocusUntilConfirmed(t *testing.T) {
	t.Parallel()

	// test.sh 17b8「create_task retries focus until stdout confirms it」。
	f := newTaskFixture(t, defaultTaskConfig).registers("retry-focus")
	f.tabs.focusEmptyUntil = 2

	if _, err := f.creator.Execute(taskEnv,
		app.TaskSpec{Dir: "/d", Type: "review", Name: "retry-focus"}); err != nil {
		t.Fatalf("Execute() = %v", err)
	}
	if got := f.journal.count("go-to-tab-name"); got != 3 {
		t.Errorf("go-to-tab-name = %d 回, want 3", got)
	}
	if f.journal.indexOf("new-pane") < 0 {
		t.Error("フォーカス確認後にペインが作られていない")
	}
}

func TestCreateTaskBuildsNoPaneWhenTabNeverRegisters(t *testing.T) {
	t.Parallel()

	// test.sh 17b8。フォーカスが Main のままなら new-pane は Main を割る。
	// Main 保護が最優先なので、ペインは 1 枚も作らずに失敗を返す。
	f := newTaskFixture(t, defaultTaskConfig)

	_, err := f.creator.Execute(taskEnv,
		app.TaskSpec{Dir: "/d", Type: "dev", Name: "never-tab"})
	if !errors.Is(err, app.ErrTabNotRegistered) {
		t.Fatalf("Execute() = %v, want ErrTabNotRegistered", err)
	}
	assertNoPaneWork(t, f.journal)
	// go-to-tab-name も撃たない(登録が確認できていないため)。
	if f.journal.indexOf("go-to-tab-name") >= 0 {
		t.Error("登録されていないタブへフォーカスを撃っている")
	}
}

func TestCreateTaskBuildsNoPaneWhenFocusCannotBeConfirmed(t *testing.T) {
	t.Parallel()

	// test.sh 17b8「create_task returns non-zero when focus cannot be confirmed」。
	f := newTaskFixture(t, defaultTaskConfig).registers("unfocusable")
	f.tabs.focusEmptyUntil = 1 << 30

	_, err := f.creator.Execute(taskEnv,
		app.TaskSpec{Dir: "/d", Type: "dev", Name: "unfocusable"})
	if !errors.Is(err, app.ErrFocusNotConfirmed) {
		t.Fatalf("Execute() = %v, want ErrFocusNotConfirmed", err)
	}
	assertNoPaneWork(t, f.journal)
}

func TestCreateTaskBuildsNoPaneWhenNewTabFails(t *testing.T) {
	t.Parallel()

	// test.sh 17b「create_task returns non-zero when new-tab fails」。
	// 復元処理がこの戻り値に依存している。
	f := newTaskFixture(t, defaultTaskConfig)
	f.tabs.newTabErr = errors.New("zellij が失敗")

	_, err := f.creator.Execute(taskEnv,
		app.TaskSpec{Dir: "/d", Type: "dev", Name: "failed-tab"})
	if err == nil {
		t.Fatal("Execute() が成功してしまった")
	}
	assertNoPaneWork(t, f.journal)
	if f.journal.indexOf("go-to-tab-name") >= 0 {
		t.Error("作られていないタブへフォーカスを撃っている")
	}
	if f.journal.indexOf("query-tab-names") >= 0 {
		t.Error("new-tab が失敗したのに登録を待っている")
	}
}

// assertNoPaneWork はペインを触る操作が 1 つも無いことを確かめる。
func assertNoPaneWork(t *testing.T, journal *paneJournal) {
	t.Helper()
	for _, prefix := range []string{"new-pane", "resize", "move-focus", "focus-previous-pane"} {
		if journal.indexOf(prefix) >= 0 {
			t.Errorf("%s を撃っている(Main を壊しうる): %v", prefix, journal.entries)
		}
	}
}

// ---- 予算 ----------------------------------------------------------------

func TestCreateTaskStaysWithinTheSetupBudget(t *testing.T) {
	t.Parallel()

	// test.sh 17b10。1 呼び出しが上限いっぱいかかるサーバでも、
	// 全体は予算 + 1 回ぶんの上限に収まり、rc は成功になる。
	f := newTaskFixture(t, defaultTaskConfig).registers("budget-tab")
	f.tabs.spend = time.Minute

	result, err := f.creator.Execute(taskEnv,
		app.TaskSpec{Dir: "/tmp/proj", Type: "dev", Name: "budget-tab"})
	if err != nil {
		t.Fatalf("Execute() = %v(予算切れは成功として返すこと)", err)
	}
	if result.Warning == "" {
		t.Error("予算切れの警告が返っていない")
	}
	if got := f.journal.count("resize decrease up"); got >= 30 {
		t.Errorf("縮小を最後まで回している: %d 回", got)
	}
	// タスクの中核である task-control ペインだけは予算切れでも作る。
	if f.journal.indexOf("new-pane down /tmp/proj -- /x/bin/mdev") < 0 {
		t.Error("予算が厳しくても task-control ペインは作ること")
	}
	limit := app.TaskSetupBudget + app.TabReadyBudget*2 + app.ZellijCallTimeout*2
	if f.clock.elapsed() > limit {
		t.Errorf("経過 = %v, want %v 以下", f.clock.elapsed(), limit)
	}
}

func TestCreateTaskGivesTaskControlAtLeastOneSecond(t *testing.T) {
	t.Parallel()

	// 予算が尽きた状態でも task-control ペインには最低 1 秒を与える。
	f := newTaskFixture(t, defaultTaskConfig).registers("t")
	f.tabs.spend = 40 * time.Second

	if _, err := f.creator.Execute(taskEnv, app.TaskSpec{Dir: "/d", Type: "dev", Name: "t"}); err != nil {
		t.Fatalf("Execute() = %v", err)
	}
	// new-tab / query / focus / task-control の 4 回目に渡した上限。
	if got := f.tabs.caps[3]; got != time.Second {
		t.Errorf("task-control へ渡した上限 = %v, want 1s", got)
	}
}

// ---- 一意な名前 ----------------------------------------------------------

func TestUniqueName(t *testing.T) {
	t.Parallel()

	// test.sh 35。既存タブと衝突する名前は空いている連番まで進む。
	f := newTaskFixture(t, defaultTaskConfig)
	f.tabs.tabNames = []string{"Main", "myapp-dev", "myapp-dev-2"}

	tests := []struct{ base, want string }{
		{"other-dev", "other-dev"},
		{"myapp-dev", "myapp-dev-3"},
		// 部分一致は衝突として扱わない(完全一致のみ)。
		{"myapp", "myapp"},
	}
	for _, tc := range tests {
		if got := f.creator.UniqueName(tc.base); got != tc.want {
			t.Errorf("UniqueName(%q) = %q, want %q", tc.base, got, tc.want)
		}
	}
}

func TestUniqueNameKeepsBaseWhenQueryFails(t *testing.T) {
	t.Parallel()

	// zellij の外や呼び出しの失敗では候補が空になり、元の名前が通る。
	f := newTaskFixture(t, defaultTaskConfig)
	f.tabs.tabNames = nil
	if got := f.creator.UniqueName("myapp-dev"); got != "myapp-dev" {
		t.Errorf("UniqueName() = %q, want myapp-dev", got)
	}
}

// ---- 付随する副作用 ------------------------------------------------------

func TestCreateTaskRemovesScreenStateBeforeCreatingTheTab(t *testing.T) {
	t.Parallel()

	// 同じ名前で作り直したタブが前のタスクの状態を引き継がないようにする。
	// 削除は new-tab より前でなければならない(後だと新しいタブの最初の
	// 観測結果を消してしまう)。
	f := newTaskFixture(t, defaultTaskConfig).registers("reused")
	if _, err := f.creator.Execute(taskEnv, app.TaskSpec{Dir: "/d", Type: "review", Name: "reused"}); err != nil {
		t.Fatalf("Execute() = %v", err)
	}
	if f.journal.indexOf("screen-state-remove") != 0 {
		t.Errorf("screen-state の削除が先頭でない: %v", f.journal.entries)
	}
	if want := "s1/" + domain.ScreenTabSlug("reused"); !strings.Contains(strings.Join(f.screen.removed, " "), want) {
		t.Errorf("削除したスラグ = %v, want %s", f.screen.removed, want)
	}
}

func TestCreateTaskFailsWhenScreenStateCannotBeRemoved(t *testing.T) {
	t.Parallel()

	// 消せないまま作ると、古い "working" が新しいタブの最初のポーリングで
	// 即 Stop に見える。タブを作る前に止める。
	f := newTaskFixture(t, defaultTaskConfig).registers("t")
	f.screen.err = errors.New("権限がない")

	if _, err := f.creator.Execute(taskEnv, app.TaskSpec{Dir: "/d", Type: "dev", Name: "t"}); err == nil {
		t.Fatal("Execute() が成功してしまった")
	}
	if f.journal.indexOf("new-tab") >= 0 {
		t.Error("screen-state を消せていないのにタブを作っている")
	}
}

func TestCreateTaskSkipsLayoutWhenTheBudgetIsGone(t *testing.T) {
	t.Parallel()

	// 予算を使い切った状態で ApplyLayout へ入ってはならない。
	//
	// ApplyLayout は「0 以下の予算 = 無制限」と解釈するため、使い切った残り
	// (負の値)をそのまま渡すと意味が反転し、レイアウト操作が 1 回あたり
	// 最大 10 秒かけて最後まで走ってしまう。
	f := newTaskFixture(t, `{"task_types": {"dev": {"layout": [
	  {"action": "new-pane", "direction": "right", "command": "nvim"},
	  {"action": "new-pane", "direction": "down", "command": "lazygit"},
	  {"action": "move-focus", "direction": "left"}
	]}}}`).registers("over")
	// 予算 30 秒をぎりぎりまで使い、最後の 1 本が上限を超えるようにする。
	//   resize 0.9 秒 × 30 = 27 秒(残り 3 秒)
	//   focus-previous-pane は上限 3 秒で撃たれるが後始末に 5 秒かかる
	//   → 経過 32 秒 = 残り -2 秒でレイアウトへ進もうとする
	f.tabs.overrunOn = map[string]time.Duration{
		"resize":              900 * time.Millisecond,
		"focus-previous-pane": 5 * time.Second,
	}

	result, err := f.creator.Execute(taskEnv,
		app.TaskSpec{Dir: "/tmp/proj", Type: "dev", Name: "over"})
	if err != nil {
		t.Fatalf("Execute() = %v(予算切れは成功として返すこと)", err)
	}
	if result.Warning == "" {
		t.Error("予算切れの警告が返っていない")
	}
	for _, prefix := range []string{"new-pane right", "new-pane down /tmp/proj -- lazygit", "move-focus"} {
		if f.journal.indexOf(prefix) >= 0 {
			t.Errorf("予算切れなのにレイアウトを当てている(%s): %v", prefix, f.journal.entries)
		}
	}
}

func TestCreateTaskMeasuresTheBudgetOncePerStep(t *testing.T) {
	t.Parallel()

	// 判定に使った残り予算と、実際にコマンドへ渡した上限は同じ値でなければ
	// ならない。別々に測ると、その間に時間が進んで「判定は通ったのに
	// 渡した上限は 0 以下」という組み合わせが起こりうる。
	f := newTaskFixture(t, defaultTaskConfig).registers("t")
	f.tabs.spend = 900 * time.Millisecond

	if _, err := f.creator.Execute(taskEnv, app.TaskSpec{Dir: "/d", Type: "dev", Name: "t"}); err != nil {
		t.Fatalf("Execute() = %v", err)
	}
	for i, limit := range f.tabs.caps {
		if limit <= 0 {
			t.Fatalf("%d 回目に 0 以下の上限を渡している: %v", i, f.tabs.caps)
		}
	}
}
