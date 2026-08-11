package tui

import (
	tea "charm.land/bubbletea/v2"

	"github.com/k-kudo-hub/mdev-go/internal/app"
)

// waitingRefreshedMsg は Waiting の一覧の読み直しが終わったことを表す。
type waitingRefreshedMsg struct {
	text string
	err  error
	// poll はこの読み直しがポーリング起源かどうかを表す。真のときだけ着弾で
	// 次の合図を張る(pane.go の「完了起点」の説明を参照)。Waiting はキー
	// 操作を受け付けないため、実際に来るのはポーリング起源だけである。
	poll bool
}

// WaitingModel は Waiting ペインの Bubble Tea モデルである。
//
// 4 つのペインで唯一キー入力を受け付けない(終了キーを除く)。zellij の
// 呼び出しも無く、pending を読んで並べるだけである。
type WaitingModel struct {
	pane WaitingService
	env  app.PaneEnv

	text string
	err  error

	// polling はポーリングの回し方(完了起点・重なりの防止)。
	polling poller
}

var (
	_ tea.Model = WaitingModel{}
	_ Once      = WaitingModel{}
)

// NewWaitingModel は Waiting のモデルを作る。
func NewWaitingModel(pane WaitingService, env app.PaneEnv) WaitingModel {
	return WaitingModel{pane: pane, env: env, polling: newPoller(WaitingInterval)}
}

// Init は最初の一覧を読み、ポーリングを開始する。
//
// 返すのは最初の読み直しだけである。次の合図はその着弾で張る(完了起点の
// ペーシング。pane.go を参照)。
func (m WaitingModel) Init() tea.Cmd {
	return m.refreshCmd(true)
}

// Once は 1 回だけ描画した結果を返す(--once)。
func (m WaitingModel) Once() (string, error) {
	text, err := m.pane.Refresh(m.env)
	if err != nil {
		return "", err
	}
	return text, nil
}

// Update はポーリングに応じて一覧を読み直す。
func (m WaitingModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyPressMsg:
		if quitKeys[msg.String()] {
			return m, tea.Quit
		}
		return m, nil

	case waitingRefreshedMsg:
		// 失敗していても必ずここを通す(実行中の数を減らし、ポーリング起源なら
		// 次の合図を張る)。
		next := m.polling.arrive(msg.poll)
		if msg.err != nil {
			// 直前の一覧を残したままエラーだけを足す。空文字で上書きすると
			// 何も出ていない画面になり、何が起きたのか分からなくなる。
			m.err = msg.err
			return m, next
		}
		m.text, m.err = msg.text, nil
		return m, next

	case attachCheckedMsg:
		// 減速の判断だけを更新する。次の合図は張らない(pane.go の
		// チェーン 1 本の不変条件)。
		m.polling.observeAttach(msg.attached)
		return m, nil

	case tickMsg:
		cmd := m.polling.tick(m.refreshCmd)
		return m, cmd
	}
	return m, nil
}

// refreshCmd は一覧を読み直す。
// poll はポーリング起源かどうかで、着弾で次の合図を張るかを決める。
func (m WaitingModel) refreshCmd(poll bool) tea.Cmd {
	return func() tea.Msg {
		text, err := m.pane.Refresh(m.env)
		return waitingRefreshedMsg{text: text, err: err, poll: poll}
	}
}

// View は画面を返す。
func (m WaitingModel) View() tea.View {
	out := m.text
	if m.err != nil {
		out += "  " + errorLine(m.err) + "\n"
	}
	return tea.NewView(out)
}
