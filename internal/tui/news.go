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
type NewsModel struct {
	pane NewsService

	snapshot app.NewsSnapshot

	// fetching は fetch-news.sh の実行中で、取得中の画面を出している状態。
	fetching bool
}

var (
	_ tea.Model = NewsModel{}
	_ Once      = NewsModel{}
)

// NewNewsModel は News のモデルを作る。
func NewNewsModel(pane NewsService) NewsModel {
	return NewsModel{pane: pane}
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
		m.snapshot, m.fetching = msg.snapshot, false
		return m, nil

	case newsReloadMsg:
		// 取得は同期で走る。終わったら読み直して通常の画面へ戻る。
		return m, func() tea.Msg {
			m.pane.Reload()
			return newsRefreshedMsg{snapshot: m.pane.Refresh()}
		}

	case tickMsg:
		if m.fetching {
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
