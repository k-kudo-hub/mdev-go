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
		// warnings は起動時の復元で作り直せなかったタスクの説明。
		// 起動の 1 回だけ入り、それ以降の読み直しでは空である。
		warnings []string
		// poll はこの読み直しがポーリング起源かどうかを表す。真のときだけ
		// 着弾で次の合図を張る(pane.go の「完了起点」の説明を参照)。
		poll bool
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

	// warnings は起動時の復元で作り直せなかったタスクの説明である。
	//
	// 一時的な通知(notice)ではなく出しっぱなしにする。作り直せなかった
	// タスクは画面に出てこないままなので、2 秒で消すと「そのタスクが無い」
	// ことに気づく手掛かりが残らない。ペインを開き直すまで残す。
	warnings []string

	// awaiting は d の後の番号入力を待っている状態。
	awaiting bool
	// token はタイマーの世代。古いタイマーの発火を無視するために使う。
	token int
	// notice は一時的に出す通知(アップロード結果・失敗)。
	notice string
	// busy は削除の処理中で、ポーリングによる再描画を止める状態。
	busy bool
	// polling はポーリングの回し方(完了起点・重なりの防止)。
	polling poller
}

var (
	_ tea.Model = DashboardModel{}
	_ Once      = DashboardModel{}
)

// NewDashboardModel は Dashboard のモデルを作る。
func NewDashboardModel(pane DashboardService, env app.PaneEnv) DashboardModel {
	return DashboardModel{pane: pane, env: env, polling: newPoller(DashboardInterval)}
}

// Init は起動時の復元と最初の一覧の組み立てを行い、ポーリングを開始する。
//
// 返すのは最初の読み直しだけである。次の合図はその着弾で張る(完了起点の
// ペーシング。pane.go を参照)。ここでタイマーも一緒に張ると、チェーンが
// 2 本になってしまう。
func (m DashboardModel) Init() tea.Cmd {
	return m.startupCmd()
}

// startupCmd は起動時の復元をしてから最初の一覧を組み立てる。
// チェーンの起点なのでポーリング起源として返す。
func (m DashboardModel) startupCmd() tea.Cmd {
	return func() tea.Msg {
		warnings := m.pane.Startup(m.env)
		snapshot, err := m.pane.Refresh(m.env)
		return dashboardRefreshedMsg{snapshot: snapshot, err: err, warnings: warnings, poll: true}
	}
}

// Once は 1 回だけ描画した結果を返す(--once)。
//
// 起動時の復元とスクリーン検出は現行の ONCE 経路と同じく走らせる。
func (m DashboardModel) Once() (string, error) {
	m.warnings = m.pane.Startup(m.env)
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
		// 発行した読み直しが 1 つ片付いた。内容を使うかどうかに関わらず、
		// また失敗していても、必ずここを通す(実行中の数を減らし、ポーリング
		// 起源なら次の合図を張る)。
		next := m.polling.arrive(msg.poll)
		if len(msg.warnings) > 0 {
			m.warnings = msg.warnings
		}
		if m.awaiting {
			// 待ち受けに入る前に発行した読み直しが着弾した。ここで一覧を
			// 差し替えると押した番号が別のタブを指すため、捨てる。
			return m, next
		}
		if msg.err != nil {
			// 直前の一覧を残したままエラーだけを足す。ゼロ値で上書きすると
			// 何も出ていない画面になり、何が起きたのか分からなくなる。
			m.err = msg.err
			return m, next
		}
		m.snapshot, m.err = msg.snapshot, nil
		return m, next

	case attachCheckedMsg:
		// 減速の判断を更新し、減速から復帰したときだけ読み直しを 1 本出す。
		// 次の合図は張らない(pane.go のチェーン 1 本の不変条件)。
		cmd := m.polling.observeAttach(msg.attached, m.refreshCmd)
		return m, cmd

	case tickMsg:
		if m.busy || m.awaiting {
			// 削除の途中と 2 打鍵目の待ち受け中は読み直さない。現行版は
			// `read -t 3` がループを止めるため、この間は表示も番号の対応も
			// 動かない。同じ意味になるようポーリングだけを空回りさせる。
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

	case deletePreparedMsg:
		return m.handlePrepared(msg)

	case commitDeleteMsg:
		tab := msg.tab
		return m, func() tea.Msg {
			return deleteFinishedMsg{err: m.pane.CommitDelete(m.env, tab)}
		}

	case deleteFinishedMsg:
		if msg.err != nil {
			// 片付けの途中で失敗した。理由を出してから通常の表示へ戻る。
			m.notice = errorLine(msg.err)
			m.token++
			return m, noticeCmd(m.token)
		}
		m.busy, m.notice = false, ""
		cmd := m.forceRefreshCmd()
		return m, cmd

	case noticeExpiredMsg:
		if msg.token == m.token {
			m.busy, m.notice = false, ""
			cmd := m.forceRefreshCmd()
			return m, cmd
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
		// 凍結を解く。削除へ進まない分岐では、止めていた間の変化に表示を
		// 追いつかせるため読み直す。
		//
		// 世代を進めるのは、2 打鍵目を待つ間に仕掛けた打ち切りのタイマーを
		// 無効にするためである。進めないと、削除の処理中(busy)に古い
		// promptExpiredMsg が発火して余計な読み直しが走る。現行版の
		// `read -t 3` はキーを受け取った時点で終わっており、その後に
		// 時間切れが起きることはない。
		m.awaiting = false
		m.token++
		number, ok := keyIndex(key)
		if !ok {
			cmd := m.forceRefreshCmd()
			return m, cmd
		}
		if number > len(m.snapshot.Tabs) {
			// 範囲外の番号は何もしない(現行版も同じ)。
			cmd := m.forceRefreshCmd()
			return m, cmd
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
	// このコマンドも最後に dashboardRefreshedMsg を返すので実行中として数える。
	// キー操作起源なので poll は立てない(着弾しても合図は張らない)。
	m.polling.force()
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
		// 記録かアップロードの手前で失敗した。何も消していないので、
		// 中止(Cancelled)と同じく理由を出してから元に戻る。
		m.notice = errorLine(msg.err)
		m.token++
		return m, noticeCmd(m.token)
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

// forceRefreshCmd は実行中として数えてから読み直しのコマンドを返す。
//
// キー操作と後始末(削除の完了・通知の期限切れ)が使う。利用者の操作への
// 反応は前回の完了を待たずに出す(poller.force を参照)。ポーリングのチェーンとは
// 別なので、着弾しても次の合図は張らない。
//
// モデルを書き換えるため、呼び出し側は `cmd := m.forceRefreshCmd()` と
// 別の文に分けてから return すること(poller の注記を参照)。
func (m *DashboardModel) forceRefreshCmd() tea.Cmd {
	m.polling.force()
	return m.refreshCmd(false)
}

// refreshCmd は一覧を組み立て直す。
// poll はポーリング起源かどうかで、着弾で次の合図を張るかを決める。
func (m DashboardModel) refreshCmd(poll bool) tea.Cmd {
	return func() tea.Msg {
		snapshot, err := m.pane.Refresh(m.env)
		return dashboardRefreshedMsg{snapshot: snapshot, err: err, poll: poll}
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
	if m.err != nil {
		out += "  " + errorLine(m.err) + "\n"
	}
	for _, warning := range m.warnings {
		out += "  " + warningLine(warning) + "\n"
	}
	return out
}
