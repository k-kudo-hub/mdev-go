package tui

import (
	tea "charm.land/bubbletea/v2"

	"github.com/k-kudo-hub/mdev-go/internal/app"
)

// restorePrompt は r の後に番号を待っている間の表示。
const restorePrompt = "\033[0;33m\033[1mRestore number...\033[0m"

// doneRefreshedMsg は Done の集計が終わったことを表す。
type doneRefreshedMsg struct{ snapshot app.DoneSnapshot }

// DoneModel は Done ペインの Bubble Tea モデルである。
//
// Dashboard / Waiting と違いエラー行を持たない。集計は読めた daily log だけで
// 組み立て、restore-task.sh の終了コードは現行版と同じく見ないため、画面に
// 出すべきエラーがそもそも発生しない。
type DoneModel struct {
	pane DoneService

	snapshot app.DoneSnapshot

	// awaiting は r の後の番号入力を待っている状態。
	awaiting bool
	// token はタイマーの世代。
	token int
}

var (
	_ tea.Model = DoneModel{}
	_ Once      = DoneModel{}
)

// NewDoneModel は Done のモデルを作る。
func NewDoneModel(pane DoneService) DoneModel {
	return DoneModel{pane: pane}
}

// Init は最初の集計を行い、ポーリングを開始する。
//
// ポーリングのチェーンを張り出すのはここだけである(張り直しは tickMsg の
// ハンドラに一元化している)。
func (m DoneModel) Init() tea.Cmd {
	return tea.Batch(m.refreshCmd(), tickCmd(DoneInterval))
}

// Once は 1 回だけ描画した結果を返す(--once)。
func (m DoneModel) Once() (string, error) {
	return m.pane.Refresh().Text, nil
}

// Update はキー入力とポーリングに応じて状態を進める。
func (m DoneModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyPressMsg:
		return m.handleKey(msg.String())

	case doneRefreshedMsg:
		if m.awaiting {
			// 待ち受けに入る前に発行した集計が着弾した。ここで一覧を
			// 差し替えると押した番号が別の行を指すため、捨てる。
			return m, nil
		}
		// ポーリングは張り直さない(Init と tickMsg のハンドラだけが張る)。
		m.snapshot = msg.snapshot
		return m, nil

	case tickMsg:
		if m.awaiting {
			// 2 打鍵目の待ち受け中は集計し直さない。現行版は `read -t 3` が
			// ループを止めるため、この間は表示も番号の対応も動かない。
			return m, tickCmd(DoneInterval)
		}
		return m, tea.Batch(m.refreshCmd(), tickCmd(DoneInterval))

	case promptExpiredMsg:
		if msg.token == m.token {
			// 凍結を解いて、止めていた間の変化に表示を追いつかせる。
			m.awaiting = false
			return m, m.refreshCmd()
		}
		return m, nil
	}
	return m, nil
}

// handleKey は 1 打鍵ぶんの入力を処理する。
func (m DoneModel) handleKey(key string) (tea.Model, tea.Cmd) {
	if quitKeys[key] {
		return m, tea.Quit
	}

	if m.awaiting {
		// 凍結を解く。restore へ進まない分岐では、止めていた間の変化に
		// 表示を追いつかせるため集計し直す。
		m.awaiting = false
		number, ok := keyIndex(key)
		if !ok || number > m.snapshot.Count {
			return m, m.refreshCmd()
		}
		snapshot := m.snapshot
		return m, func() tea.Msg {
			// restore-task.sh の終了コードは見ない。失敗した場合は
			// エントリが Done に残り、次のポーリングで再表示される。
			m.pane.Restore(snapshot, number)
			return doneRefreshedMsg{snapshot: m.pane.Refresh()}
		}
	}

	if key == "r" && m.snapshot.Count > 0 {
		m.awaiting = true
		m.token++
		return m, promptTimeoutCmd(m.token)
	}
	return m, nil
}

// refreshCmd は集計をやり直す。
func (m DoneModel) refreshCmd() tea.Cmd {
	return func() tea.Msg {
		return doneRefreshedMsg{snapshot: m.pane.Refresh()}
	}
}

// View は画面を返す。
func (m DoneModel) View() tea.View {
	out := m.snapshot.Text
	if m.awaiting {
		out += "  " + restorePrompt + "\n"
	}
	return tea.NewView(out)
}
