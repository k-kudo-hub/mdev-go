package tui_test

import (
	"errors"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/k-kudo-hub/mdev-go/internal/app"
	"github.com/k-kudo-hub/mdev-go/internal/tui"
)

// タスク作成ペイン(n フロー)の進み方を確かめる。設定の読み方と
// create_task の中身は internal/app のテストが持つ。

type stubTaskCreate struct {
	dirs       []string
	rootsFound bool
	types      []app.TaskTypeChoice
	agents     []string
	skipName   bool
	createErr  error
	warning    string

	calls []string
}

var _ tui.TaskCreateService = (*stubTaskCreate)(nil)

func (s *stubTaskCreate) Menu(app.PaneEnv) string { return "MENU\n" }

func (s *stubTaskCreate) Directories() ([]string, bool) {
	s.calls = append(s.calls, "directories")
	return s.dirs, s.rootsFound
}

func (s *stubTaskCreate) TaskTypes() []app.TaskTypeChoice { return s.types }
func (s *stubTaskCreate) Agents() []string                { return s.agents }
func (s *stubTaskCreate) SkipNameInput() bool             { return s.skipName }

func (s *stubTaskCreate) DefaultName(dir, taskType string) string {
	return "default-" + taskType
}

func (s *stubTaskCreate) ResolveName(defaultName, input string) string {
	if input == "" {
		return defaultName
	}
	return input
}

func (s *stubTaskCreate) UniqueName(base string) string {
	s.calls = append(s.calls, "unique "+base)
	return base + "-uniq"
}

func (s *stubTaskCreate) Create(_ app.PaneEnv, dir, taskType, name, agent string) (string, error) {
	s.calls = append(s.calls, strings.Join([]string{"create", dir, taskType, name, agent}, " "))
	return s.warning, s.createErr
}

// newTaskCreate は既定の候補を持つモデルを組み立てる。
func newTaskCreate(pane *stubTaskCreate) tui.TaskCreateModel {
	return tui.NewTaskCreateModel(pane, app.PaneEnv{ZellijSession: "s1"})
}

// defaultTaskCreateStub は dev / review の 2 種別を持つスタブを返す。
func defaultTaskCreateStub() *stubTaskCreate {
	return &stubTaskCreate{
		dirs:       []string{"/p/alpha", "/p/beta"},
		rootsFound: true,
		types: []app.TaskTypeChoice{
			{Name: "dev", Description: "Claude Code + LazyVim"},
			{Name: "review", Description: "Claude Code only"},
		},
	}
}

// startFlow は n を押してディレクトリ選択まで進めたモデルを返す。
func startFlow(t *testing.T, pane *stubTaskCreate) tea.Model {
	t.Helper()
	m := newTaskCreate(pane)
	next, cmd := m.Update(key('n'))
	if cmd == nil {
		t.Fatal("n で候補の収集が始まらない")
	}
	next, _ = next.Update(cmd())
	return next
}

func TestTaskCreateMenuWaitsForN(t *testing.T) {
	t.Parallel()

	// メニューはキーを待つだけで、ポーリングを持たない。
	pane := defaultTaskCreateStub()
	m := newTaskCreate(pane)
	if cmd := m.Init(); cmd != nil {
		t.Error("Init が何かを発行している(ポーリングは持たない)")
	}
	if got := content(m); got != "MENU\n" {
		t.Errorf("表示 = %q, want MENU", got)
	}
	// n 以外は何もしない。
	if _, cmd := m.Update(key('x')); cmd != nil {
		t.Error("n 以外のキーで動いている")
	}
	if len(pane.calls) != 0 {
		t.Errorf("呼び出し = %v, want 空", pane.calls)
	}
}

func TestTaskCreateFullFlow(t *testing.T) {
	t.Parallel()

	pane := defaultTaskCreateStub()
	pane.agents = []string{"claude", "codex"}

	m := startFlow(t, pane)
	// ディレクトリ選択。↓ で 2 件目を選ぶ。
	if got := content(m); !strings.Contains(got, "/p/alpha") || !strings.Contains(got, "Directory") {
		t.Fatalf("ディレクトリ選択が出ていない: %q", got)
	}
	m, _ = m.Update(specialKey(tea.KeyDown))
	m, _ = m.Update(specialKey(tea.KeyEnter))

	// タスク種別。説明も並ぶ。
	if got := content(m); !strings.Contains(got, "Task type") || !strings.Contains(got, "LazyVim") {
		t.Fatalf("種別選択が出ていない: %q", got)
	}
	m, _ = m.Update(specialKey(tea.KeyEnter))

	// エージェント。2 件あるので選択 UI が出る。
	if got := content(m); !strings.Contains(got, "Agent") || !strings.Contains(got, "codex") {
		t.Fatalf("エージェント選択が出ていない: %q", got)
	}
	m, _ = m.Update(specialKey(tea.KeyDown))
	m, _ = m.Update(specialKey(tea.KeyEnter))

	// タスク名。既定値がプリフィルされ、編集できる。
	if got := content(m); !strings.Contains(got, "Task name") || !strings.Contains(got, "default-dev") {
		t.Fatalf("名前入力が出ていない: %q", got)
	}
	m, _ = m.Update(specialKey(tea.KeyBackspace))
	m, _ = m.Update(key('X'))
	_, cmd := m.Update(specialKey(tea.KeyEnter))
	if cmd == nil {
		t.Fatal("作成が始まらない")
	}
	cmd()

	want := []string{
		"directories",
		"unique default-deX",
		"create /p/beta dev default-deX-uniq codex",
	}
	if !equalStrings(pane.calls, want) {
		t.Errorf("呼び出し = %v\nwant %v", pane.calls, want)
	}
}

func TestTaskCreateSkipsAgentSelection(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		agents    []string
		wantAgent string
	}{
		// 設定が無ければ選択を飛ばし、TASK_AGENT も渡さない。
		{"0 件は選択なし", nil, ""},
		// 1 件なら選択 UI を出さずに即決する。
		{"1 件は即決", []string{"claude"}, "claude"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			pane := defaultTaskCreateStub()
			pane.agents = tc.agents

			m := startFlow(t, pane)
			m, _ = m.Update(specialKey(tea.KeyEnter)) // ディレクトリ
			m, _ = m.Update(specialKey(tea.KeyEnter)) // 種別
			// エージェント選択を飛ばして名前入力へ来ている。
			if got := content(m); !strings.Contains(got, "Task name") {
				t.Fatalf("名前入力へ進んでいない: %q", got)
			}
			_, cmd := m.Update(specialKey(tea.KeyEnter))
			if cmd == nil {
				t.Fatal("作成が始まらない")
			}
			cmd()

			want := "create /p/alpha dev default-dev-uniq " + tc.wantAgent
			if last := pane.calls[len(pane.calls)-1]; last != want {
				t.Errorf("作成の引数 = %q, want %q", last, want)
			}
		})
	}
}

func TestTaskCreateSkipsNameInput(t *testing.T) {
	t.Parallel()

	// skip_task_name_input が真なら入力を出さずに既定名で作る。
	pane := defaultTaskCreateStub()
	pane.skipName = true

	m := startFlow(t, pane)
	m, _ = m.Update(specialKey(tea.KeyEnter)) // ディレクトリ
	_, cmd := m.Update(specialKey(tea.KeyEnter))
	if cmd == nil {
		t.Fatal("種別の確定で作成が始まらない")
	}
	cmd()

	want := []string{"directories", "unique default-dev", "create /p/alpha dev default-dev-uniq "}
	if !equalStrings(pane.calls, want) {
		t.Errorf("呼び出し = %v\nwant %v", pane.calls, want)
	}
}

func TestTaskCreateEscapeReturnsToMenu(t *testing.T) {
	t.Parallel()

	// どの段階の ESC でもメニューへ戻り、何も作らない。
	steps := []struct {
		name   string
		enters int
	}{
		{"ディレクトリ選択", 0},
		{"種別選択", 1},
		{"エージェント選択", 2},
		{"名前入力", 3},
	}
	for _, tc := range steps {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			pane := defaultTaskCreateStub()
			pane.agents = []string{"claude", "codex"}

			m := startFlow(t, pane)
			for range tc.enters {
				m, _ = m.Update(specialKey(tea.KeyEnter))
			}
			m, _ = m.Update(specialKey(tea.KeyEscape))

			if got := content(m); got != "MENU\n" {
				t.Errorf("メニューへ戻っていない: %q", got)
			}
			for _, call := range pane.calls {
				if strings.HasPrefix(call, "create") {
					t.Errorf("取り消したのに作成している: %v", pane.calls)
				}
			}
		})
	}
}

func TestTaskCreateFilterNarrowsCandidates(t *testing.T) {
	t.Parallel()

	// 入力すると部分列で絞り込まれる。
	pane := defaultTaskCreateStub()
	m := startFlow(t, pane)
	m, _ = m.Update(key('b'))

	got := content(m)
	if !strings.Contains(got, "/p/beta") {
		t.Errorf("一致する候補が消えている: %q", got)
	}
	if strings.Contains(got, "/p/alpha") {
		t.Errorf("一致しない候補が残っている: %q", got)
	}

	// backspace で戻る。
	m, _ = m.Update(specialKey(tea.KeyBackspace))
	if got := content(m); !strings.Contains(got, "/p/alpha") {
		t.Errorf("backspace で候補が戻らない: %q", got)
	}
}

func TestTaskCreateShowsMissingSearchDirs(t *testing.T) {
	t.Parallel()

	// search_dirs が 1 つも実在しないときは赤字を出してメニューへ戻る。
	pane := &stubTaskCreate{rootsFound: false}
	m := startFlow(t, pane)

	if got := content(m); !strings.Contains(got, app.SearchDirsMissingMessage) {
		t.Errorf("警告が出ていない: %q", got)
	}
}

func TestTaskCreateShowsCreateFailure(t *testing.T) {
	t.Parallel()

	// 現行版は create_task の失敗を握り潰していた。Go 版は理由を出す。
	pane := defaultTaskCreateStub()
	pane.skipName = true
	pane.createErr = errors.New("タブが登録されませんでした")

	m := startFlow(t, pane)
	m, _ = m.Update(specialKey(tea.KeyEnter))
	m, cmd := m.Update(specialKey(tea.KeyEnter))
	if cmd == nil {
		t.Fatal("作成が始まらない")
	}
	next, _ := m.Update(cmd())
	if got := content(next); !strings.Contains(got, "タブが登録されませんでした") {
		t.Errorf("失敗が出ていない: %q", got)
	}
}

func TestTaskCreateShowsBudgetWarning(t *testing.T) {
	t.Parallel()

	// タブは作れたが予算切れでレイアウトを省いた場合も知らせる。
	pane := defaultTaskCreateStub()
	pane.skipName = true
	pane.warning = "予算を使い切りました"

	m := startFlow(t, pane)
	m, _ = m.Update(specialKey(tea.KeyEnter))
	m, cmd := m.Update(specialKey(tea.KeyEnter))
	if cmd == nil {
		t.Fatal("作成が始まらない")
	}
	next, _ := m.Update(cmd())
	if got := content(next); !strings.Contains(got, "予算を使い切りました") {
		t.Errorf("警告が出ていない: %q", got)
	}
}

func TestTaskCreateReturnsToMenuAfterSuccess(t *testing.T) {
	t.Parallel()

	pane := defaultTaskCreateStub()
	pane.skipName = true

	m := startFlow(t, pane)
	m, _ = m.Update(specialKey(tea.KeyEnter))
	m, cmd := m.Update(specialKey(tea.KeyEnter))
	if cmd == nil {
		t.Fatal("作成が始まらない")
	}
	next, _ := m.Update(cmd())
	if got := content(next); got != "MENU\n" {
		t.Errorf("メニューへ戻っていない: %q", got)
	}
}

func TestTaskCreateIgnoresKeysWhileBusy(t *testing.T) {
	t.Parallel()

	// 収集中・作成中はキーを受け付けない(二重に走らせない)。
	pane := defaultTaskCreateStub()
	m := newTaskCreate(pane)
	busy, cmd := m.Update(key('n'))
	if cmd == nil {
		t.Fatal("n で収集が始まらない")
	}
	if _, cmd := busy.Update(key('n')); cmd != nil {
		t.Error("収集中に n を受け付けている")
	}
	if got := content(busy); !strings.Contains(got, "検索中") {
		t.Errorf("進行の表示が出ていない: %q", got)
	}
}

func TestTaskCreateWithoutCandidates(t *testing.T) {
	t.Parallel()

	// 起点はあるが候補が 0 件。Enter しても何も起きず、ESC でメニューへ戻る。
	pane := &stubTaskCreate{rootsFound: true}
	m := startFlow(t, pane)

	if got := content(m); !strings.Contains(got, "候補なし") {
		t.Errorf("候補なしの表示が出ていない: %q", got)
	}
	if _, cmd := m.Update(specialKey(tea.KeyEnter)); cmd != nil {
		t.Error("候補が無いのに先へ進んでいる")
	}
}

func TestTaskCreateQuitsOnCtrlC(t *testing.T) {
	t.Parallel()

	if _, cmd := newTaskCreate(defaultTaskCreateStub()).Update(ctrlKey('c')); cmd == nil {
		t.Error("Ctrl+C で終了しない")
	}
}
