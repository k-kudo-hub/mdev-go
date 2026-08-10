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

	// gate は読み直しの逐次化。前回が着弾するまでポーリングで重ねて出さない。
	gate refreshGate
}

var (
	_ tea.Model = WaitingModel{}
	_ Once      = WaitingModel{}
)

// NewWaitingModel は Waiting のモデルを作る。
//
// 逐次化の印は実行中で始める(Init が最初の読み直しを必ず発行するため。
// refreshGate を参照)。
func NewWaitingModel(pane WaitingService, env app.PaneEnv) WaitingModel {
	return WaitingModel{pane: pane, env: env, gate: refreshGate{inFlight: true}}
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
		// 失敗していても逐次化の印はここで必ず下ろす。
		m.gate.release()
		// ポーリング起源ならここで次の合図を張る(完了起点のペーシング)。
		// エラーでも張る。絶やすと二度と回らない。
		next := rearmCmd(msg.poll, WaitingInterval)
		if msg.err != nil {
			// 直前の一覧を残したままエラーだけを足す。空文字で上書きすると
			// 何も出ていない画面になり、何が起きたのか分からなくなる。
			m.err = msg.err
			return m, next
		}
		m.text, m.err = msg.text, nil
		return m, next

	case tickMsg:
		if !m.gate.take() {
			// 前回の読み直しがまだ着弾していない。重ねて発行せず、次の合図
			// だけを予約する。
			return m, tickCmd(WaitingInterval)
		}
		// 次の合図はこの読み直しの着弾で張るため、ここでは張らない。
		return m, m.refreshCmd(true)
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
