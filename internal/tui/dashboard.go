package tui

import (
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/k-kudo-hub/mdev-go/internal/app"
)

// 削除フロー中に出す文言。現行版が 1 行を上書きして出していたものに対応する。
const (
	deletePrompt   = "\033[0;31m\033[1mDelete tab number...\033[0m"
	uploadingLabel = "\033[2mUploading log...\033[0m"
	uploadFailed   = "\033[0;31m\033[1mUpload failed. Deletion cancelled.\033[0m"
)

// Dashboard のメッセージ。
type (
	// dashboardRefreshedMsg は 1 回ぶんの一覧の組み立てが終わったことを表す。
	dashboardRefreshedMsg struct {
		snapshot app.DashboardSnapshot
		err      error
	}
	// deletePreparedMsg は record と upload-log が終わったことを表す。
	deletePreparedMsg struct {
		tab  string
		prep app.DeletePreparation
		err  error
	}
	// commitDeleteMsg は削除の後半へ進む合図である。
	commitDeleteMsg struct{ tab string }
	// deleteFinishedMsg は削除の後半が終わったことを表す。
	deleteFinishedMsg struct{ err error }
	// noticeExpiredMsg は一時的な通知の表示時間が過ぎたことを表す。
	noticeExpiredMsg struct{ token int }
)

// DashboardModel は Dashboard ペインの Bubble Tea モデルである。
//
// 表示は domain.RenderDashboard に委ねる。このモデルが持つのは「どの状態を
// 表示するか」と「キーとタイマーにどう反応するか」だけである。
type DashboardModel struct {
	pane DashboardService
	env  app.PaneEnv

	snapshot app.DashboardSnapshot
	err      error

	// awaiting は d の後の番号入力を待っている状態。
	awaiting bool
	// token はタイマーの世代。古いタイマーの発火を無視するために使う。
	token int
	// notice は一時的に出す通知(アップロード結果・失敗)。
	notice string
	// busy は削除の処理中で、ポーリングによる再描画を止める状態。
	busy bool
}

var (
	_ tea.Model = DashboardModel{}
	_ Once      = DashboardModel{}
)

// NewDashboardModel は Dashboard のモデルを作る。
func NewDashboardModel(pane DashboardService, env app.PaneEnv) DashboardModel {
	return DashboardModel{pane: pane, env: env}
}

// Init は起動時の復元と最初の一覧の組み立てを行う。
func (m DashboardModel) Init() tea.Cmd {
	return func() tea.Msg {
		m.pane.Startup()
		snapshot, err := m.pane.Refresh(m.env)
		return dashboardRefreshedMsg{snapshot: snapshot, err: err}
	}
}

// Once は 1 回だけ描画した結果を返す(--once)。
//
// 起動時の復元とスクリーン検出は現行の ONCE 経路と同じく走らせる。
func (m DashboardModel) Once() (string, error) {
	m.pane.Startup()
	snapshot, err := m.pane.Refresh(m.env)
	if err != nil {
		return "", err
	}
	m.snapshot = snapshot
	return m.body(), nil
}

// Update はキー入力とタイマーに応じて状態を進める。
func (m DashboardModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyPressMsg:
		return m.handleKey(msg.String())

	case dashboardRefreshedMsg:
		m.snapshot, m.err = msg.snapshot, msg.err
		return m, tickCmd(DashboardInterval)

	case tickMsg:
		if m.busy {
			// 削除の途中は再描画しない。現行版もキー処理が終わるまで
			// 次のポーリングへ進まない。
			return m, tickCmd(DashboardInterval)
		}
		return m, m.refreshCmd()

	case promptExpiredMsg:
		if msg.token == m.token {
			m.awaiting = false
		}
		return m, nil

	case deletePreparedMsg:
		return m.handlePrepared(msg)

	case commitDeleteMsg:
		tab := msg.tab
		return m, func() tea.Msg {
			return deleteFinishedMsg{err: m.pane.CommitDelete(m.env, tab)}
		}

	case deleteFinishedMsg:
		m.busy, m.notice = false, ""
		if msg.err != nil {
			m.err = msg.err
		}
		return m, m.refreshCmd()

	case noticeExpiredMsg:
		if msg.token == m.token {
			m.busy, m.notice = false, ""
			return m, m.refreshCmd()
		}
		return m, nil
	}
	return m, nil
}

// handleKey は 1 打鍵ぶんの入力を処理する。
func (m DashboardModel) handleKey(key string) (tea.Model, tea.Cmd) {
	if quitKeys[key] {
		return m, tea.Quit
	}
	// 削除の処理中はキーを受け付けない。
	if m.busy {
		return m, nil
	}

	if m.awaiting {
		m.awaiting = false
		number, ok := keyIndex(key)
		if !ok {
			return m, nil
		}
		if number > len(m.snapshot.Tabs) {
			// 範囲外の番号は何もしない(現行版も同じ)。
			return m, nil
		}
		tab := m.snapshot.Tabs[number-1]
		m.busy, m.notice = true, uploadingLabel
		return m, func() tea.Msg {
			prep, err := m.pane.PrepareDelete(m.env, tab)
			return deletePreparedMsg{tab: tab, prep: prep, err: err}
		}
	}

	if key == "d" {
		if len(m.snapshot.Tabs) == 0 {
			return m, nil
		}
		m.awaiting = true
		m.token++
		return m, promptTimeoutCmd(m.token)
	}

	number, ok := keyIndex(key)
	if !ok || number > len(m.snapshot.Tabs) {
		return m, nil
	}
	snapshot := m.snapshot
	return m, func() tea.Msg {
		if err := m.pane.Jump(m.env, snapshot, number); err != nil {
			return dashboardRefreshedMsg{snapshot: snapshot, err: err}
		}
		next, err := m.pane.Refresh(m.env)
		return dashboardRefreshedMsg{snapshot: next, err: err}
	}
}

// handlePrepared は record と upload-log が終わった後の分岐を決める。
func (m DashboardModel) handlePrepared(msg deletePreparedMsg) (tea.Model, tea.Cmd) {
	if msg.err != nil {
		m.busy, m.notice, m.err = false, "", msg.err
		return m, m.refreshCmd()
	}

	if msg.prep.Cancelled {
		// アップロードに失敗したので何も消さない。表示だけ出して元に戻る。
		m.notice = uploadFailed
		m.token++
		return m, noticeCmd(m.token)
	}

	if msg.prep.Message == "" {
		// 表示するものが無いのでそのまま削除へ進む。
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

// refreshCmd は一覧を組み立て直す。
func (m DashboardModel) refreshCmd() tea.Cmd {
	return func() tea.Msg {
		snapshot, err := m.pane.Refresh(m.env)
		return dashboardRefreshedMsg{snapshot: snapshot, err: err}
	}
}

// noticeCmd は noticeDuration 後に通知の表示を終える。
func noticeCmd(token int) tea.Cmd {
	return tea.Tick(noticeDuration, func(time.Time) tea.Msg {
		return noticeExpiredMsg{token: token}
	})
}

// View は画面を返す。本体は domain のレンダリング関数が組み立てる。
func (m DashboardModel) View() tea.View {
	return tea.NewView(m.body())
}

// body は表示する文字列を組み立てる。
//
// 現行版は削除の進行状況を最終行に `\r` で上書きして出していたが、Bubble Tea は
// 画面全体を差分描画するため、ここでは本体の下に 1 行足す形にしている
// (挙動差として evidence に記録)。--once では通知が出ないため出力は変わらない。
func (m DashboardModel) body() string {
	out := m.snapshot.Text
	switch {
	case m.notice != "":
		out += "  " + m.notice + "\n"
	case m.awaiting:
		out += "  " + deletePrompt + "\n"
	}
	return out
}
