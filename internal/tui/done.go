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
	// notice は直前の復元が返した知らせ。空なら出すものが無い。
	notice string
	// poll はこの集計がポーリング起源かどうかを表す。真のときだけ着弾で
	// 次の合図を張る(pane.go の「完了起点」の説明を参照)。
	poll bool
}

// DoneModel は Done ペインの Bubble Tea モデルである。
//
// 集計そのものは読めた daily log だけで組み立てるため失敗しない。画面に出る
// のは復元(r + 番号)の結果だけで、これは一時的な通知として 2 秒出す。
type DoneModel struct {
	pane DoneService
	env  app.PaneEnv

	snapshot app.DoneSnapshot

	// notice は復元の結果を一時的に出す 1 行。空なら出さない。
	//
	// 復元の失敗を無反応にすると利用者はキーを押し直し、同じ名前のタブが
	// 増えるだけになる。現行 Shell 版は終了コードを捨てていたので何も出て
	// いなかった(意図的な改善)。
	notice string

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
//
// env を持つのは復元がタスクタブを作り直すためである(作り直すタブの
// スクリーン検出の状態は今の zellij セッションの下にある)。
func NewDoneModel(pane DoneService, env app.PaneEnv) DoneModel {
	return DoneModel{pane: pane, env: env, polling: newPoller(DoneInterval)}
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
		if msg.notice == "" {
			return m, next
		}
		// 復元の結果を 2 秒出す。世代を進めて、前の通知の打ち切りが
		// この通知を消してしまわないようにする。
		m.notice = msg.notice
		m.token++
		return m, tea.Batch(next, noticeCmd(m.token))

	case noticeExpiredMsg:
		if msg.token == m.token {
			m.notice = ""
			cmd := m.forceRefreshCmd()
			return m, cmd
		}
		return m, nil

	case attachCheckedMsg:
		// 減速の判断を更新し、減速から復帰したときだけ読み直しを 1 本出す。
		// 次の合図は張らない(pane.go のチェーン 1 本の不変条件)。
		cmd := m.polling.observeAttach(msg, m.refreshCmd)
		return m, cmd

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
		//
		// 世代を進めて、待ち受けに入るときに仕掛けた打ち切りのタイマーを
		// 無効にする(Dashboard と同じ理由。進めないと restore の直後に
		// 古い promptExpiredMsg が発火して余計な集計が走る)。
		m.awaiting = false
		m.token++
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
			// 失敗したエントリは Done に残り、次のポーリングで再表示される。
			// それだけでは押し直しを誘って同名のタブが増えるので、理由も出す。
			warning, err := m.pane.Restore(m.env, snapshot, number)
			return doneRefreshedMsg{
				snapshot: m.pane.Refresh(),
				notice:   restoreNotice(warning, err),
			}
		}
	}

	if key == "r" && m.snapshot.Count > 0 {
		m.awaiting = true
		m.token++
		return m, promptTimeoutCmd(m.token)
	}
	return m, nil
}

// restoreNotice は復元の結果を画面へ出す 1 行にする。
// 失敗を優先し、どちらも無ければ空を返す。
func restoreNotice(warning string, err error) string {
	if err != nil {
		return errorLine(err)
	}
	if warning != "" {
		return warningLine(warning)
	}
	return ""
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
	switch {
	case m.notice != "":
		out += "  " + m.notice + "\n"
	case m.awaiting:
		out += "  " + restorePrompt + "\n"
	}
	return paneView(out)
}
