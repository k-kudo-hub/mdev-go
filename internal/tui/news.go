package tui

import (
	tea "charm.land/bubbletea/v2"

	"github.com/k-kudo-hub/mdev-go/internal/app"
)

// News のメッセージ。
type (
	// newsRefreshedMsg はニュースの読み直しが終わったことを表す。
	newsRefreshedMsg struct {
		snapshot app.NewsSnapshot
		// poll はこの読み直しがポーリング起源かどうかを表す。真のときだけ
		// 着弾で次の合図を張る(pane.go の「完了起点」の説明を参照)。
		poll bool
	}
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
	// polling はポーリングの回し方(完了起点・重なりの防止)。
	polling poller
}

var (
	_ tea.Model = NewsModel{}
	_ Once      = NewsModel{}
)

// NewNewsModel は News のモデルを作る。
func NewNewsModel(pane NewsService) NewsModel {
	return NewsModel{pane: pane, polling: newPoller(NewsInterval)}
}

// Init は最初のニュースを読み、ポーリングを開始する。
//
// 返すのは最初の読み直しだけである。次の合図はその着弾で張る(完了起点の
// ペーシング。pane.go を参照)。
func (m NewsModel) Init() tea.Cmd {
	return m.refreshCmd(true)
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
		// 必ずここを通す(実行中の数を減らし、ポーリング起源なら次の合図を張る)。
		next := m.polling.arrive(msg.poll)
		if m.fetching && msg.poll {
			// 取得に入る前に発行したポーリングの読み直しが着弾した。ここで
			// 差し替えると取得中の画面が消え、まだ走っている取得が終わったように
			// 見える。r も再び効くようになり、取得が二重に走る。表示は取得の
			// 完了(force 起源の着弾)まで据え置く。
			return m, next
		}
		m.snapshot, m.fetching = msg.snapshot, false
		return m, next

	case newsReloadMsg:
		// 取得は同期で走る。終わったら読み直して通常の画面へ戻る。
		// このコマンドも最後に newsRefreshedMsg を返すので実行中として数える。
		// キー操作起源なので poll は立てない(着弾しても合図は張らない)。
		m.polling.force()
		return m, func() tea.Msg {
			m.pane.Reload()
			return newsRefreshedMsg{snapshot: m.pane.Refresh()}
		}

	case tickMsg:
		if m.fetching {
			// 取得中は読み直さない(取得中の画面を出したままにする)。
			return m, m.polling.rearm()
		}
		cmd := m.polling.tick(m.refreshCmd)
		return m, cmd
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
// poll はポーリング起源かどうかで、着弾で次の合図を張るかを決める。
func (m NewsModel) refreshCmd(poll bool) tea.Cmd {
	return func() tea.Msg {
		return newsRefreshedMsg{snapshot: m.pane.Refresh(), poll: poll}
	}
}

// View は画面を返す。
func (m NewsModel) View() tea.View {
	if m.fetching {
		return tea.NewView(m.snapshot.FetchingText)
	}
	return tea.NewView(m.snapshot.Text)
}
