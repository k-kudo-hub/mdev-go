package tui

import (
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/k-kudo-hub/mdev-go/internal/app"
)

// TaskCreateService はタスク作成ペインのユースケースである。
type TaskCreateService interface {
	Menu(env app.PaneEnv) string
	Directories() (dirs []string, rootsFound bool)
	TaskTypes() []app.TaskTypeChoice
	Agents() []string
	SkipNameInput() bool
	DefaultName(dir, taskType string) string
	ResolveName(defaultName, input string) string
	UniqueName(base string) string
	Create(env app.PaneEnv, dir, taskType, name, agent string) (warning string, err error)
}

// taskCreateStep はタスク作成の進行段階である。
type taskCreateStep int

const (
	// stepMenu は n の入力を待っている状態。ポーリングはしない。
	stepMenu taskCreateStep = iota
	// stepDir はディレクトリを選んでいる状態。
	stepDir
	// stepType はタスク種別を選んでいる状態。
	stepType
	// stepAgent はエージェントを選んでいる状態。
	stepAgent
	// stepName はタスク名を編集している状態。
	stepName
	// stepBusy は候補の収集または作成の実行中。
	stepBusy
	// stepNotice はエラーや警告を出している状態(時間で stepMenu へ戻る)。
	stepNotice
)

// task-create のメッセージ。
type (
	// dirsLoadedMsg はディレクトリ候補の収集が終わったことを表す。
	dirsLoadedMsg struct {
		dirs       []string
		rootsFound bool
	}
	// taskCreatedMsg はタスク作成が終わったことを表す。
	taskCreatedMsg struct {
		warning string
		err     error
	}
)

// taskCreateBusyLabel は候補の収集中・作成中に出す行。
const (
	taskCreateSearching = "\033[2m検索中...\033[0m"
	taskCreateCreating  = "\033[0;32m作成中...\033[0m"
)

// TaskCreateModel はタスク作成ペインの Bubble Tea モデルである。
//
// 他の 4 ペインと違い**ポーリングを持たない**。現行 task-create-loop.sh も
// `read -n 1 -s` でキーを待つだけで、定期的な再描画はしない。
type TaskCreateModel struct {
	pane TaskCreateService
	env  app.PaneEnv

	step taskCreateStep
	// list は今表示している選択 UI(stepDir / stepType / stepAgent)。
	list selectList
	// input はタスク名の編集欄(stepName)。
	input textField

	// 選択済みの値。
	dir      string
	taskType string
	agent    string

	// busyLabel は stepBusy のときに出す行。
	busyLabel string
	// notice は stepNotice のときに出す行。
	notice string
	// token は通知のタイマーの世代。
	token int
}

var _ tea.Model = TaskCreateModel{}

// NewTaskCreateModel はタスク作成ペインのモデルを作る。
func NewTaskCreateModel(pane TaskCreateService, env app.PaneEnv) TaskCreateModel {
	return TaskCreateModel{pane: pane, env: env, step: stepMenu}
}

// Init は何もしない。最初の描画はメニューで、キーが押されるまで動かない。
func (m TaskCreateModel) Init() tea.Cmd { return nil }

// Update はキー入力と各段階の完了に応じて状態を進める。
func (m TaskCreateModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyPressMsg:
		return m.handleKey(msg.String())

	case dirsLoadedMsg:
		if !msg.rootsFound {
			// 現行版と同じく赤字を出して 2 秒待ってからメニューへ戻る。
			return m.showNotice(app.TaskCreateError(app.SearchDirsMissingMessage))
		}
		m.step = stepDir
		m.list = newSelectList("Directory: ", msg.dirs, nil)
		return m, nil

	case taskCreatedMsg:
		if msg.err != nil {
			// 現行版は create_task の戻り値を捨てており、失敗しても何も出ない。
			// 何が起きたか分からない状態を残さないため出す(意図的な改善)。
			return m.showNotice(app.TaskCreateError(msg.err.Error()))
		}
		if msg.warning != "" {
			return m.showNotice(app.TaskCreateError(msg.warning))
		}
		return m.backToMenu(), nil

	case noticeExpiredMsg:
		if msg.token == m.token {
			return m.backToMenu(), nil
		}
		return m, nil
	}
	return m, nil
}

// handleKey は 1 打鍵ぶんの入力を処理する。
func (m TaskCreateModel) handleKey(key string) (tea.Model, tea.Cmd) {
	if quitKeys[key] {
		return m, tea.Quit
	}

	switch m.step {
	case stepMenu:
		if key != "n" && key != "N" {
			return m, nil
		}
		m.step, m.busyLabel = stepBusy, taskCreateSearching
		return m, func() tea.Msg {
			dirs, rootsFound := m.pane.Directories()
			return dirsLoadedMsg{dirs: dirs, rootsFound: rootsFound}
		}

	case stepDir:
		value, index, done := m.list.step(key)
		switch {
		case !done:
			return m, nil
		case index < 0:
			// どの段階でも ESC はメニューへ戻る(現行版の continue)。
			return m.backToMenu(), nil
		}
		return m.afterDir(value), nil
	case stepType:
		value, index, done := m.list.step(key)
		switch {
		case !done:
			return m, nil
		case index < 0:
			return m.backToMenu(), nil
		}
		return m.afterType(value)
	case stepAgent:
		value, index, done := m.list.step(key)
		switch {
		case !done:
			return m, nil
		case index < 0:
			return m.backToMenu(), nil
		}
		return m.afterAgent(value)
	case stepName:
		return m.handleNameKey(key)
	case stepBusy, stepNotice:
		// 実行中と通知中はキーを受け付けない。
		return m, nil
	}
	return m, nil
}

// afterDir はディレクトリが決まったあとタスク種別の選択へ進む。
//
// 並びは設定の記述順である(現行版の `jq ... to_entries` と同じ)。表示は
// 「キー + 説明」で、選ばれるのはキーだけである。
func (m TaskCreateModel) afterDir(dir string) TaskCreateModel {
	m.dir = dir
	choices := m.pane.TaskTypes()
	names := make([]string, 0, len(choices))
	labels := make([]string, 0, len(choices))
	for _, choice := range choices {
		names = append(names, choice.Name)
		labels = append(labels, choice.Name+"  "+ansiDim+choice.Description+ansiReset)
	}
	m.step = stepType
	m.list = newSelectList("Task type: ", names, labels)
	return m
}

// afterType はタスク種別が決まったあとエージェントを決める。
//
// 設定済みエージェントが 0 件なら選択を飛ばし、TASK_AGENT も渡さない
// (旧来の単一エージェント経路)。1 件なら選択 UI を出さずに即決する。
func (m TaskCreateModel) afterType(taskType string) (tea.Model, tea.Cmd) {
	m.taskType = taskType
	agents := m.pane.Agents()
	switch len(agents) {
	case 0:
		return m.startName()
	case 1:
		return m.afterAgent(agents[0])
	}
	m.step = stepAgent
	m.list = newSelectList("Agent: ", agents, nil)
	return m, nil
}

// afterAgent はエージェントが決まったあとタスク名へ進む。
func (m TaskCreateModel) afterAgent(agent string) (tea.Model, tea.Cmd) {
	m.agent = agent
	return m.startName()
}

// startName はタスク名の入力へ進む。
//
// skip_task_name_input が真なら入力を出さずに既定名で作る。そうでなければ
// 既定名を編集できる状態で提示する(現行版の bash 4 経路 `read -e -i` に
// 相当する。bash 3.2 では候補の提示だけで編集できなかった)。
func (m TaskCreateModel) startName() (tea.Model, tea.Cmd) {
	defaultName := m.pane.DefaultName(m.dir, m.taskType)
	if m.pane.SkipNameInput() {
		return m.create(defaultName)
	}
	m.step = stepName
	m.input = newTextField("Task name: ", defaultName)
	return m, nil
}

// handleNameKey はタスク名の編集中の入力を処理する。
func (m TaskCreateModel) handleNameKey(key string) (tea.Model, tea.Cmd) {
	switch key {
	case "esc":
		return m.backToMenu(), nil
	case "enter":
		return m.create(m.pane.ResolveName(m.input.defaultValue, m.input.value))
	}
	m.input.handleKey(key)
	return m, nil
}

// create は名前を一意化してからタスクを作る。
func (m TaskCreateModel) create(name string) (tea.Model, tea.Cmd) {
	dir, taskType, agent := m.dir, m.taskType, m.agent
	m.step, m.busyLabel = stepBusy, taskCreateCreating
	return m, func() tea.Msg {
		unique := m.pane.UniqueName(name)
		warning, err := m.pane.Create(m.env, dir, taskType, unique, agent)
		return taskCreatedMsg{warning: warning, err: err}
	}
}

// showNotice は通知を出し、noticeDuration 後にメニューへ戻す。
func (m TaskCreateModel) showNotice(text string) (tea.Model, tea.Cmd) {
	m.step, m.notice = stepNotice, text
	m.token++
	return m, tea.Tick(noticeDuration, func(time.Time) tea.Msg {
		return noticeExpiredMsg{token: m.token}
	})
}

// backToMenu は選択途中の値を捨ててメニューへ戻す。
func (m TaskCreateModel) backToMenu() TaskCreateModel {
	m.step = stepMenu
	m.dir, m.taskType, m.agent = "", "", ""
	m.notice, m.busyLabel = "", ""
	m.list = selectList{}
	m.input = textField{}
	return m
}

// View は画面を返す。
func (m TaskCreateModel) View() tea.View {
	menu := m.pane.Menu(m.env)
	switch m.step {
	case stepMenu:
		return tea.NewView(menu)
	case stepBusy:
		return tea.NewView(menu + "  " + m.busyLabel + "\n")
	case stepNotice:
		return tea.NewView(menu + m.notice)
	case stepName:
		return tea.NewView(menu + m.input.View())
	case stepDir, stepType, stepAgent:
		return tea.NewView(menu + m.list.View())
	}
	return tea.NewView(menu)
}
