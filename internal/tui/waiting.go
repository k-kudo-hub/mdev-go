package tui

import (
	tea "charm.land/bubbletea/v2"

	"github.com/k-kudo-hub/mdev-go/internal/app"
)

// waitingRefreshedMsg は Waiting の一覧の読み直しが終わったことを表す。
type waitingRefreshedMsg struct {
	text string
	err  error
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
}

var (
	_ tea.Model = WaitingModel{}
	_ Once      = WaitingModel{}
)

// NewWaitingModel は Waiting のモデルを作る。
func NewWaitingModel(pane WaitingService, env app.PaneEnv) WaitingModel {
	return WaitingModel{pane: pane, env: env}
}

// Init は最初の一覧を読み、ポーリングを開始する。
//
// ポーリングのチェーンを張り出すのはここだけである(張り直しは tickMsg の
// ハンドラに一元化している)。
func (m WaitingModel) Init() tea.Cmd {
	return tea.Batch(m.refreshCmd(), tickCmd(WaitingInterval))
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
		if msg.err != nil {
			// 直前の一覧を残したままエラーだけを足す。空文字で上書きすると
			// 何も出ていない画面になり、何が起きたのか分からなくなる。
			m.err = msg.err
			return m, nil
		}
		// ポーリングは張り直さない(Init と tickMsg のハンドラだけが張る)。
		m.text, m.err = msg.text, nil
		return m, nil

	case tickMsg:
		return m, tea.Batch(m.refreshCmd(), tickCmd(WaitingInterval))
	}
	return m, nil
}

// refreshCmd は一覧を読み直す。
func (m WaitingModel) refreshCmd() tea.Cmd {
	return func() tea.Msg {
		text, err := m.pane.Refresh(m.env)
		return waitingRefreshedMsg{text: text, err: err}
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
