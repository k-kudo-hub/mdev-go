package tui

import (
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/k-kudo-hub/mdev-go/internal/app"
)

// TaskControlInterval は操作バーの再描画間隔である。
// 現行 task-control.sh の `read -t 2` に対応する。
const TaskControlInterval = 2 * time.Second

// TaskControlPromptTimeout は dd の 2 打鍵目を待つ時間である。
//
// Dashboard の 2 打鍵目(PromptTimeout = 3 秒)とは**別の値**で、現行版も
// task-control.sh だけ `read -t 2` になっている。片方に合わせると、
// どちらかの利用者の手が覚えた間合いが変わる。
const TaskControlPromptTimeout = 2 * time.Second

// 削除フロー中に出す文言。現行 task-control.sh が 1 行を上書きして出していた
// ものに対応する。
const (
	taskControlDeletePrompt = "\033[0;31m\033[1mPress d to confirm delete...\033[0m"
	taskControlUploading    = "\033[2mUploading log...\033[0m"
)

// TaskControlService は task-control ペインのユースケースである。
type TaskControlService interface {
	Refresh(env app.PaneEnv, tab string) (string, error)
	GoToMain() error
	ToggleWaiting(env app.PaneEnv, tab string) error
	PrepareDelete(env app.PaneEnv, tab string) (app.DeletePreparation, error)
	CommitDelete(env app.PaneEnv, tab string) error
	// ForceDelete はアップロードを飛ばして削除する。
	// 中止の理由を見せたうえで利用者が明示的に選んだときだけ呼ぶ。
	ForceDelete(env app.PaneEnv, tab string) error
}

// task-control のメッセージ。
type (
	// taskControlRefreshedMsg は操作バーの組み立てが終わったことを表す。
	taskControlRefreshedMsg struct {
		text string
		err  error
		poll bool
	}
	// taskControlActedMsg はキー操作(m / w)が終わったことを表す。
	taskControlActedMsg struct{ err error }
)

// TaskControlModel はタスクタブ下部の操作バーの Bubble Tea モデルである。
type TaskControlModel struct {
	pane TaskControlService
	env  app.PaneEnv
	tab  string

	text string
	err  error

	// awaiting は d の後の 2 打鍵目を待っている状態。
	awaiting bool
	// forceTab は「アップロードせずに削除する」を選べるタブである。
	// Dashboard と同じ扱いで、中止の直後だけ入る。
	forceTab string
	// token はタイマーの世代。古いタイマーの発火を無視するために使う。
	token int
	// notice は一時的に出す通知(アップロード結果・失敗)。
	notice string
	// busy は削除の処理中で、ポーリングによる再描画を止める状態。
	busy bool
	// polling はポーリングの回し方(完了起点・重なりの防止)。
	polling poller
}

var (
	_ tea.Model = TaskControlModel{}
	_ Once      = TaskControlModel{}
)

// NewTaskControlModel は task-control のモデルを作る。
func NewTaskControlModel(pane TaskControlService, env app.PaneEnv, tab string) TaskControlModel {
	return TaskControlModel{
		pane: pane, env: env, tab: tab,
		polling: newPoller(TaskControlInterval),
	}
}

// Init は最初の描画を行い、ポーリングを開始する。
func (m TaskControlModel) Init() tea.Cmd { return m.refreshCmd(true) }

// Once は 1 回だけ描画した結果を返す(--once)。
// 現行版の CONDUCTOR_TASKCTL_ONCE=1 と同じ出力になる。
func (m TaskControlModel) Once() (string, error) {
	return m.pane.Refresh(m.env, m.tab)
}

// Update はキー入力とポーリングに応じて状態を進める。
func (m TaskControlModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyPressMsg:
		return m.handleKey(msg.String())

	case taskControlRefreshedMsg:
		// 内容を捨てる場合も必ずここを通す(実行中の数を減らし、ポーリング
		// 起源なら次の合図を張る)。
		next := m.polling.arrive(msg.poll)
		if msg.err != nil {
			m.err = msg.err
			return m, next
		}
		m.text, m.err = msg.text, nil
		return m, next

	case taskControlActedMsg:
		if msg.err != nil {
			m.err = msg.err
		}
		cmd := m.forceRefreshCmd()
		return m, cmd

	case attachCheckedMsg:
		// 減速の判断を更新し、減速から復帰したときだけ読み直しを 1 本出す。
		// 次の合図は張らない(pane.go のチェーン 1 本の不変条件)。
		cmd := m.polling.observeAttach(msg, m.refreshCmd)
		return m, cmd

	case tickMsg:
		if m.busy || m.awaiting {
			// 削除の途中と 2 打鍵目の待ち受け中は読み直さない。現行版は
			// `read -t 2` がループを止めるため、この間は表示も動かない。
			return m, m.polling.rearm()
		}
		cmd := m.polling.tick(m.refreshCmd)
		return m, cmd

	case promptExpiredMsg:
		if msg.token == m.token {
			m.awaiting = false
			cmd := m.forceRefreshCmd()
			return m, cmd
		}
		return m, nil

	case deletePreparedMsg:
		return m.handlePrepared(msg)

	case forceDeleteMsg:
		tab := msg.tab
		return m, func() tea.Msg {
			return deleteFinishedMsg{err: m.pane.ForceDelete(m.env, tab)}
		}

	case commitDeleteMsg:
		tab := msg.tab
		return m, func() tea.Msg {
			return deleteFinishedMsg{err: m.pane.CommitDelete(m.env, tab)}
		}

	case deleteFinishedMsg:
		if msg.err != nil {
			// 片付けの途中で失敗した。理由を出してから通常の表示へ戻る。
			m.notice = errorLine(msg.err)
			m.token++
			return m, noticeCmd(m.token)
		}
		// タブごと閉じたのでペインも終わる(現行版の `exit 0`)。
		return m, tea.Quit

	case noticeExpiredMsg:
		if msg.token == m.token {
			m.busy, m.notice = false, ""
			cmd := m.forceRefreshCmd()
			return m, cmd
		}
		return m, nil
	}
	return m, nil
}

// handleKey は 1 打鍵ぶんの入力を処理する。
func (m TaskControlModel) handleKey(key string) (tea.Model, tea.Cmd) {
	if quitKeys[key] {
		return m, tea.Quit
	}
	if m.busy {
		return m, nil
	}

	// 中止の直後だけ、強制削除を選べる(Dashboard と同じ契約)。
	if m.forceTab != "" {
		tab := m.forceTab
		m.forceTab, m.notice = "", ""
		m.token++
		if key != forceDeleteKey {
			return m, m.forceRefreshCmd()
		}
		m.busy = true
		return m, func() tea.Msg { return forceDeleteMsg{tab: tab} }
	}

	if m.awaiting {
		m.awaiting = false
		m.token++
		if key != "d" {
			// 2 打鍵目が d 以外なら削除しない。止めていた間の変化に
			// 表示を追いつかせる。
			cmd := m.forceRefreshCmd()
			return m, cmd
		}
		tab := m.tab
		m.busy, m.notice = true, taskControlUploading
		return m, func() tea.Msg {
			prep, err := m.pane.PrepareDelete(m.env, tab)
			return deletePreparedMsg{tab: tab, prep: prep, err: err}
		}
	}

	switch key {
	case "m":
		return m, func() tea.Msg { return taskControlActedMsg{err: m.pane.GoToMain()} }
	case "w":
		tab := m.tab
		m.polling.force()
		return m, func() tea.Msg {
			if err := m.pane.ToggleWaiting(m.env, tab); err != nil {
				return taskControlRefreshedMsg{text: m.text, err: err}
			}
			text, err := m.pane.Refresh(m.env, tab)
			return taskControlRefreshedMsg{text: text, err: err}
		}
	case "d":
		m.awaiting = true
		m.token++
		return m, taskControlPromptTimeoutCmd(m.token)
	}
	return m, nil
}

// handlePrepared は record と upload-log が終わった後の分岐を決める。
//
// Dashboard と同じ契約である。アップロードが失敗したら何も消さず、
// "Upload failed. Deletion cancelled." と理由を出して元に戻る。
func (m TaskControlModel) handlePrepared(msg deletePreparedMsg) (tea.Model, tea.Cmd) {
	if msg.err != nil {
		m.notice = errorLine(msg.err)
		m.token++
		return m, noticeCmd(m.token)
	}
	if msg.prep.Cancelled {
		// 時間で消さない。理由を読んで判断してもらうためである
		// (Dashboard と同じ扱い)。
		//
		// **busy はここで解く。** 従来は通知のタイマーが解いていたが、この
		// 通知は時間で消えないため、そのままだとペインが固まる(キーも
		// ポーリングも止まる)。削除は既に中止されており、処理中ではない。
		m.busy = false
		m.notice = uploadFailedNotice(msg.prep.Reason)
		m.forceTab = msg.tab
		m.token++
		return m, nil
	}
	if msg.prep.Message == "" {
		m.notice = ""
		return m, func() tea.Msg { return commitDeleteMsg{tab: msg.tab} }
	}

	// タブが閉じる前にログの URL を確認できるよう、少し出してから削除する。
	m.notice = "\033[0;32m\033[1m" + msg.prep.Message + "\033[0m"
	tab := msg.tab
	return m, tea.Tick(noticeDuration, func(time.Time) tea.Msg {
		return commitDeleteMsg{tab: tab}
	})
}

// forceRefreshCmd は実行中として数えてから読み直しのコマンドを返す。
func (m *TaskControlModel) forceRefreshCmd() tea.Cmd {
	m.polling.force()
	return m.refreshCmd(false)
}

// refreshCmd は操作バーを組み立て直す。
func (m TaskControlModel) refreshCmd(poll bool) tea.Cmd {
	return func() tea.Msg {
		text, err := m.pane.Refresh(m.env, m.tab)
		return taskControlRefreshedMsg{text: text, err: err, poll: poll}
	}
}

// taskControlPromptTimeoutCmd は 2 打鍵目の待ち受けを打ち切る合図を送る。
// Dashboard の promptTimeoutCmd と待ち時間だけが違う。
func taskControlPromptTimeoutCmd(token int) tea.Cmd {
	return tea.Tick(TaskControlPromptTimeout, func(time.Time) tea.Msg {
		return promptExpiredMsg{token: token}
	})
}

// View は画面を返す。
//
// 現行版は 1 行を `\r` で上書きして進行状況を出していたが、Bubble Tea は
// 画面全体を差分描画するため、ここでは操作バーの代わりに 1 行を差し替える形に
// している(挙動差として evidence に記録)。
func (m TaskControlModel) View() tea.View {
	switch {
	case m.notice != "":
		return tea.NewView("  " + m.notice + "\n")
	case m.awaiting:
		return tea.NewView("  " + taskControlDeletePrompt + "\n")
	}
	out := m.text
	if m.err != nil {
		out += "  " + errorLine(m.err) + "\n"
	}
	return tea.NewView(out)
}
