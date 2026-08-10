package tui

import (
	tea "charm.land/bubbletea/v2"

	"github.com/k-kudo-hub/mdev-go/internal/app"
)

// News のメッセージ。
type (
	// newsRefreshedMsg はニュースの読み直しが終わったことを表す。
	newsRefreshedMsg struct{ snapshot app.NewsSnapshot }
	// newsReloadMsg は取得中の画面を出した後に実際の取得へ進む合図である。
	newsReloadMsg struct{}
)

// NewsModel は News ペインの Bubble Tea モデルである。
//
// DoneModel と同じくエラー行を持たない。ニュースは読めたファイルだけで
// 組み立て、fetch-news.sh とブラウザ起動の終了コードは現行版と同じく
// 見ないため、画面に出すべきエラーが発生しない。
type NewsModel struct {
	pane NewsService

	snapshot app.NewsSnapshot

	// fetching は fetch-news.sh の実行中で、取得中の画面を出している状態。
	fetching bool
	// gate は読み直しの逐次化。前回が着弾するまでポーリングで重ねて出さない。
	gate refreshGate
}

var (
	_ tea.Model = NewsModel{}
	_ Once      = NewsModel{}
)

// NewNewsModel は News のモデルを作る。
//
// 逐次化の印は実行中で始める(Init が最初の読み直しを必ず発行するため。
// refreshGate を参照)。
func NewNewsModel(pane NewsService) NewsModel {
	return NewsModel{pane: pane, gate: refreshGate{inFlight: true}}
}

// Init は最初のニュースを読み、ポーリングを開始する。
//
// ポーリングのチェーンを張り出すのはここだけである(張り直しは tickMsg の
// ハンドラに一元化している)。
func (m NewsModel) Init() tea.Cmd {
	return tea.Batch(m.refreshCmd(), tickCmd(NewsInterval))
}

// Once は 1 回だけ描画した結果を返す(--once)。
func (m NewsModel) Once() (string, error) {
	return m.pane.Refresh().Text, nil
}

// Update はキー入力とポーリングに応じて状態を進める。
func (m NewsModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyPressMsg:
		return m.handleKey(msg.String())

	case newsRefreshedMsg:
		// ポーリングは張り直さない(Init と tickMsg のハンドラだけが張る)。
		// 逐次化の印はここで必ず下ろす。
		m.gate.release()
		m.snapshot, m.fetching = msg.snapshot, false
		return m, nil

	case newsReloadMsg:
		// 取得は同期で走る。終わったら読み直して通常の画面へ戻る。
		// このコマンドも最後に newsRefreshedMsg を返すので、印を立てる。
		m.gate.force()
		return m, func() tea.Msg {
			m.pane.Reload()
			return newsRefreshedMsg{snapshot: m.pane.Refresh()}
		}

	case tickMsg:
		if m.fetching {
			return m, tickCmd(NewsInterval)
		}
		if !m.gate.take() {
			// 前回の読み直しがまだ着弾していない。重ねて発行せず、次の合図
			// だけを予約する。
			return m, tickCmd(NewsInterval)
		}
		return m, tea.Batch(m.refreshCmd(), tickCmd(NewsInterval))
	}
	return m, nil
}

// handleKey は 1 打鍵ぶんの入力を処理する。
func (m NewsModel) handleKey(key string) (tea.Model, tea.Cmd) {
	if quitKeys[key] {
		return m, tea.Quit
	}
	if m.fetching {
		return m, nil
	}

	if key == "r" || key == "R" {
		// 先に取得中の画面を出してから取得へ進む。
		m.fetching = true
		return m, func() tea.Msg { return newsReloadMsg{} }
	}

	number, ok := keyIndex(key)
	if !ok || number > m.snapshot.Count {
		return m, nil
	}
	snapshot := m.snapshot
	return m, func() tea.Msg {
		m.pane.Open(snapshot, number)
		return nil
	}
}

// refreshCmd はニュースを読み直す。
func (m NewsModel) refreshCmd() tea.Cmd {
	return func() tea.Msg {
		return newsRefreshedMsg{snapshot: m.pane.Refresh()}
	}
}

// View は画面を返す。
func (m NewsModel) View() tea.View {
	if m.fetching {
		return tea.NewView(m.snapshot.FetchingText)
	}
	return tea.NewView(m.snapshot.Text)
}
