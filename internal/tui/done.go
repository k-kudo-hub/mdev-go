package tui

import (
	tea "charm.land/bubbletea/v2"

	"github.com/k-kudo-hub/mdev-go/internal/app"
)

// restorePrompt は r の後に番号を待っている間の表示。
const restorePrompt = "\033[0;33m\033[1mRestore number...\033[0m"

// doneRefreshedMsg は Done の集計が終わったことを表す。
type doneRefreshedMsg struct {
	snapshot app.DoneSnapshot
	// poll はこの集計がポーリング起源かどうかを表す。真のときだけ着弾で
	// 次の合図を張る(pane.go の「完了起点」の説明を参照)。
	poll bool
}

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
	// polling はポーリングの回し方(完了起点・重なりの防止)。
	polling poller
}

var (
	_ tea.Model = DoneModel{}
	_ Once      = DoneModel{}
)

// NewDoneModel は Done のモデルを作る。
func NewDoneModel(pane DoneService) DoneModel {
	return DoneModel{pane: pane, polling: newPoller(DoneInterval)}
}

// Init は最初の集計を行い、ポーリングを開始する。
//
// 返すのは最初の集計だけである。次の合図はその着弾で張る(完了起点の
// ペーシング。pane.go を参照)。
func (m DoneModel) Init() tea.Cmd {
	return m.refreshCmd(true)
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
		// 内容を捨てる場合も必ずここを通す(実行中の数を減らし、ポーリング
		// 起源なら次の合図を張る)。
		next := m.polling.arrive(msg.poll)
		if m.awaiting {
			// 待ち受けに入る前に発行した集計が着弾した。ここで一覧を
			// 差し替えると押した番号が別の行を指すため、捨てる。
			return m, next
		}
		m.snapshot = msg.snapshot
		return m, next

	case tickMsg:
		if m.awaiting {
			// 2 打鍵目の待ち受け中は集計し直さない。現行版は `read -t 3` が
			// ループを止めるため、この間は表示も番号の対応も動かない。
			return m, m.polling.rearm()
		}
		cmd := m.polling.tick(m.refreshCmd)
		return m, cmd

	case promptExpiredMsg:
		if msg.token == m.token {
			// 凍結を解いて、止めていた間の変化に表示を追いつかせる。
			m.awaiting = false
			cmd := m.forceRefreshCmd()
			return m, cmd
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
			cmd := m.forceRefreshCmd()
			return m, cmd
		}
		snapshot := m.snapshot
		// このコマンドも最後に doneRefreshedMsg を返すので実行中として数える。
		// キー操作起源なので poll は立てない(着弾しても合図は張らない)。
		m.polling.force()
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

// forceRefreshCmd は実行中として数えてから集計のコマンドを返す。
//
// キー操作に対する反応は前回の完了を待たずに出す(poller.force を参照)。
// ポーリングのチェーンとは別なので、着弾しても次の合図は張らない。
//
// モデルを書き換えるため、呼び出し側は `cmd := m.forceRefreshCmd()` と
// 別の文に分けてから return すること(poller の注記を参照)。
func (m *DoneModel) forceRefreshCmd() tea.Cmd {
	m.polling.force()
	return m.refreshCmd(false)
}

// refreshCmd は集計をやり直す。
// poll はポーリング起源かどうかで、着弾で次の合図を張るかを決める。
func (m DoneModel) refreshCmd(poll bool) tea.Cmd {
	return func() tea.Msg {
		return doneRefreshedMsg{snapshot: m.pane.Refresh(), poll: poll}
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
