package tui

import (
	"fmt"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/k-kudo-hub/mdev-go/internal/app"
)

// 各ペインのポーリング間隔。現行 Shell 版の sleep / read -t に合わせている。
const (
	// DashboardInterval は Dashboard の再描画間隔。
	DashboardInterval = 2 * time.Second
	// WaitingInterval は Waiting の再描画間隔。
	WaitingInterval = 2 * time.Second
	// DoneInterval は Done の再描画間隔。
	DoneInterval = 5 * time.Second
	// NewsInterval は News の再描画間隔。
	NewsInterval = 5 * time.Second
)

// PromptTimeout は 2 打鍵目(d+番号 / r+番号)を待つ時間。
// 現行版の `read -t 3` に対応する。
const PromptTimeout = 3 * time.Second

// noticeDuration は一時的な通知(アップロード結果・失敗)を出しておく時間。
// 現行版の `sleep 2` に対応する。
const noticeDuration = 2 * time.Second

// quitKeys はペインを終了させるキー。
//
// 現行の Shell 版ペインは終了キーを持たず、zellij がペインごと落とすまで
// 回り続ける。Bubble Tea は端末を raw モードにするため、Ctrl+C が素通り
// しないと手動で止められなくなる。移行期の運用しやすさを優先して受け付ける
// (挙動差として evidence に記録している)。
var quitKeys = map[string]bool{"ctrl+c": true}

// tickMsg はポーリングの合図である。
type tickMsg time.Time

// tickCmd は d 後に tickMsg を送る。
func tickCmd(d time.Duration) tea.Cmd {
	return tea.Tick(d, func(t time.Time) tea.Msg { return tickMsg(t) })
}

// ポーリングは「完了起点」で回る(4 ペイン共通)。
//
// 現行 Shell 版のループは「処理 → sleep 2」の逐次実行である。CLI が走るのは
// 処理中の T の間だけなので 1 周期は T + S になり、占有率は T/(T+S) に収まる。
// 処理が遅れた回はそのぶん周期が伸びるため、それ以上には上がらない(自己抑制)。
// Dashboard の 1 回の読み直しは zellij の CLI(スクリーン検出の list-panes と
// list-tabs)を呼ぶため実測で 1 秒以上かかり、固定間隔で回すと占有率が T/S まで
// 上がる。さらに T が S を超えると呼び出しが重なってサーバを劣化させ、T が
// もっと伸びるという正のフィードバックが閉じる。
//
// そこで、次の合図を張るのは「ポーリングで出した読み直しが着弾したとき」だけに
// する。これで Shell 版と同じ T + S の周期になり、自己抑制も同じように働く。
//
// 不変条件: ポーリングのチェーンは常にちょうど 1 本である。すなわち
// 「未着弾のポーリング読み直し」と「予約済みのタイマー」のどちらか一方だけが
// 存在し、着弾と発火で互いに入れ替わる。
//
//	Init ─→ [読み直し] ─着弾→ [タイマー] ─発火→ [読み直し] ─着弾→ …
//
// キー操作で出した読み直し(force 起源)はこのチェーンの一部ではなく、着弾しても
// 何も予約しない。予約するとチェーンが 1 本ずつ恒久的に増えていく。

// attachCheckedMsg は「誰か開いているか」の確認が返ったことを表す。
//
// この合図はポーリングのチェーンの一部では **ない**。受け取っても次の合図を
// 張らず、実行中の数も変えない。張ってしまうとチェーンが 2 本になる。
type attachCheckedMsg struct{ attached bool }

// AttachWatch はセッションを誰か開いているかを見張る。
//
// 実体は zellij の list-clients を叩く adapter である。ゼロ値(checker が
// nil)は「見張らない」を意味し、減速は一切かからない。zellij の外で
// 動かしたときや単発描画(--once)がこれに当たる。
type AttachWatch struct {
	Checker app.SessionAttachChecker
	Session string
}

// cmd は確認を 1 回行う tea.Cmd を返す。見張らない設定なら nil を返す。
func (w AttachWatch) cmd() tea.Cmd {
	if w.Checker == nil || w.Session == "" {
		return nil
	}
	return func() tea.Msg {
		return attachCheckedMsg{attached: w.Checker.IsAttached(w.Session)}
	}
}

// poller は 4 ペイン共通のポーリングの回し方である。
//
// 状態を変える入口を tick / arrive / force の 3 つに絞ってある。とくに arrive は
// 「実行中の数を減らす」と「次の合図を張る」を 1 つの操作にまとめてあり、片方
// だけを行うことができない。分けて書ける形にしておくと、分岐が増えたときに
// どこかで片方を落とし、ポーリングが二度と回らなくなる(または 1 本ずつ
// 増えていく)。
//
// tick / arrive / force はモデルを書き換えるため、呼び出し側は必ず
//
//	cmd := m.polling.tick(m.refreshCmd)
//	return m, cmd
//
// のように別の文へ分けること。`return m, m.polling.tick(...)` と書くと、返り値の
// m を評価する時点と呼び出しの時点の順序が Go の仕様で決まっておらず、数え上げが
// 反映されていないモデルが返りうる。
type poller struct {
	interval time.Duration
	// inFlight は発行済みでまだ着弾していない読み直しの本数である。
	//
	// 真偽値ではなく本数なのは、キー操作の読み直し(force 起源)とポーリングの
	// 読み直しが同時に走りうるためである。真偽値だと、先に着弾したほうが印を
	// 下ろしてしまい、まだ走っているほうへ次のポーリングが重なる。
	inFlight int

	// pace は「誰も開いていないなら遅く回す」ための状態である。
	pace app.IdlePace
	// watch は attach の確認手段。ゼロ値なら減速しない。
	watch AttachWatch
	// now は「今」を供給する。テストで差し替える。
	now func() time.Time
}

// newPoller は間隔 d のポーリングを返す。
//
// 生成直後を 1 本実行中として始めるのは、どのペインも Init で最初の読み直しを
// 発行するためである。Init は値レシーバでモデルを書き換えられないため、そこで
// 数えられない。
func newPoller(d time.Duration) poller {
	return poller{interval: d, inFlight: 1, now: time.Now}
}

// withAttachWatch は attach の見張りを付けた poller を返す。
func (p poller) withAttachWatch(watch AttachWatch) poller {
	p.watch = watch
	return p
}

// pollInterval は次に張る合図までの間隔を返す。
// 誰も開いていないと分かっているときだけ落ちる。
func (p poller) pollInterval() time.Duration {
	return p.pace.Interval(p.interval)
}

// observeAttach は確認の結果を取り込む。
//
// **次の合図は張らない。** これはポーリングのチェーンの外側の出来事であり、
// ここで張るとチェーンが 2 本になる。効くのは次に張られる合図からである。
func (p *poller) observeAttach(attached bool) {
	p.pace = p.pace.Observe(attached)
}

// armWithAttachCheck は次の合図を張り、頃合いなら attach の確認も足す。
//
// 確認をポーリングの着弾に相乗りさせているのは、独立したタイマーを持つと
// チェーンが 2 本になり、この設計が防いでいる「重なり」を自分で作って
// しまうためである。確認の合図(attachCheckedMsg)は何も予約しないので、
// チェーンは 1 本のままである。
func (p *poller) armWithAttachCheck() tea.Cmd {
	next := tickCmd(p.pollInterval())
	now := p.now()
	if !p.pace.ShouldCheck(now) {
		return next
	}
	check := p.watch.cmd()
	if check == nil {
		return next
	}
	// 結果を待たずに「確認を始めた」ことを記録する(重ねて出さないため)。
	p.pace = p.pace.MarkChecked(now)
	return tea.Batch(next, check)
}

// tick はポーリングの合図に対する応答を返す。
//
// 実行中の読み直しが 1 本も無いときだけ refresh を呼んで読み直しを発行する。
// このとき次の合図は張らない(その読み直しの着弾で張る = 完了起点)。実行中の
// ものがあれば重ねず、次の合図だけを予約する。
//
// refresh は読み直しのコマンドを組み立てるだけで、ポーリングの状態は見ない。
func (p *poller) tick(refresh func(poll bool) tea.Cmd) tea.Cmd {
	if p.inFlight > 0 {
		return tickCmd(p.pollInterval())
	}
	p.inFlight++
	return refresh(true)
}

// rearm は読み直しを出さずに次の合図だけを予約する。
// 凍結中(削除の途中・2 打鍵目の待ち受け中・取得中)の tick が使う。
//
// 実行中の数を変えないので、凍結が解けた後の tick は通常どおり発行できる。
func (p poller) rearm() tea.Cmd { return tickCmd(p.pollInterval()) }

// arrive は読み直しの着弾に対する応答を返す。
//
// 実行中の本数を 1 つ減らし、ポーリング起源ならば次の合図を張る。エラーで
// 返ってきた場合も、待ち受け中で内容を捨てる場合も必ず通すこと。
func (p *poller) arrive(poll bool) tea.Cmd {
	if p.inFlight > 0 {
		p.inFlight--
	}
	if !poll {
		return nil
	}
	return p.armWithAttachCheck()
}

// force はキー操作起源の読み直しを実行中として数える。
//
// 利用者の操作への反応は前回の完了を待たずに出す。数えるのは、その読み直しが
// 走っている間にポーリングが重ならないようにするためである。
func (p *poller) force() { p.inFlight++ }

// promptExpiredMsg は 2 打鍵目の待ち受けが時間切れになったことを表す。
// token は世代番号で、待ち受けをやり直した後に古いタイマーが効かないようにする。
type promptExpiredMsg struct{ token int }

// promptTimeoutCmd は PromptTimeout 後に待ち受けを打ち切る合図を送る。
func promptTimeoutCmd(token int) tea.Cmd {
	return tea.Tick(PromptTimeout, func(time.Time) tea.Msg {
		return promptExpiredMsg{token: token}
	})
}

// errorLine はエラーを画面へ出す 1 行にする。
//
// 現行 Shell 版はこの種のエラーを画面に出さない(そもそも失敗を検出せず、
// 空の一覧として描き直す)。無言のまま内容が消えるのを避けることだけを目的に、
// 本体の下へ赤字で簡潔に足す。挙動差として evidence に記録している。
//
// 単発描画(--once)はこの経路を通らない。エラーは戻り値として呼び出し元へ
// 返すため、ゴールデンテストが見る出力は変わらない。
func errorLine(err error) string {
	return "\033[0;31m\033[1mError: " + err.Error() + "\033[0m"
}

// warningLine は警告を画面へ出す 1 行にする。
//
// エラー(赤)と分けるのは、警告が「処理は進んだが完全ではない」ことを表す
// ためである。復元で言えば、タブは出来ているので押し直す必要は無い。
// ユースケースが標準エラーへ書かずに文字列で返すのは、ペインが動作中の
// Bubble Tea プログラムであり、同じ端末へ直接書くと描画が崩れるからである。
func warningLine(message string) string {
	return "\033[0;33m\033[1mWarning: " + message + "\033[0m"
}

// keyIndex はキーが 1-9 のときに 1 始まりの番号を返す。
//
// 現行版はいずれのペインも `[[ "$key" =~ [1-9] ]]` で 1 文字だけを見るため、
// 10 件目以降は番号が振られていてもキーでは選べない。その制限も同じである。
func keyIndex(key string) (int, bool) {
	if len(key) != 1 || key[0] < '1' || key[0] > '9' {
		return 0, false
	}
	return int(key[0] - '0'), true
}

// Once は 1 回だけ描画して終わるペインである(現行版の CONDUCTOR_*_ONCE 相当)。
//
// Bubble Tea のプログラムを起動せずに View() と同じ文字列を返す。端末を
// 必要としないため、ゴールデンテストからも同じ経路を通せる。
type Once interface {
	Once() (string, error)
}

// 各ペインが呼ぶユースケースの形。実体は internal/app の *Pane 型である。
//
// tui が具象型ではなく interface に依存するのは、テストでユースケースを
// 差し替えられるようにするためである。ここを具象型にすると、tui のテストが
// app の port(domain の型を受け渡しする)を実装せざるを得なくなり、
// tui から domain への依存が生まれてしまう(ADR-0002 で禁じている方向)。
type (
	// DashboardService は Dashboard ペインのユースケース。
	DashboardService interface {
		Startup(app.PaneEnv) []string
		Refresh(app.PaneEnv) (app.DashboardSnapshot, error)
		Jump(app.PaneEnv, app.DashboardSnapshot, int) error
		PrepareDelete(app.PaneEnv, string) (app.DeletePreparation, error)
		CommitDelete(app.PaneEnv, string) error
	}

	// WaitingService は Waiting ペインのユースケース。
	WaitingService interface {
		Refresh(app.PaneEnv) (string, error)
	}

	// DoneService は Done ペインのユースケース。
	DoneService interface {
		Refresh() app.DoneSnapshot
		Restore(app.PaneEnv, app.DoneSnapshot, int) (string, error)
	}

	// NewsService は News ペインのユースケース。
	NewsService interface {
		Refresh() app.NewsSnapshot
		Reload()
		Open(app.NewsSnapshot, int)
	}
)

// ペインの名前(`mdev pane <name>` の引数)。
const (
	NameDashboard = "dashboard"
	NameWaiting   = "waiting"
	NameDone      = "done"
	NameNews      = "news"
	// NameTaskCreate は Main タブ下部のタスク作成ペイン。
	NameTaskCreate = "task-create"
	// NameTaskControl は各タスクタブ下部の操作バー。引数にタブ名を取る。
	NameTaskControl = "task-control"
)

// Panes は 4 つのペインをまとめて起動できるようにしたものである。
//
// internal/cli は internal/tui を参照できない(ADR-0002)ため、cli 側は
// Run / Once だけを持つ interface としてこれを受け取り、実体の組み立ては
// cmd/mdev が行う。
type Panes struct {
	Dashboard   DashboardService
	Waiting     WaitingService
	Done        DoneService
	News        NewsService
	TaskCreate  TaskCreateService
	TaskControl TaskControlService
	Env         app.PaneEnv
	// Attach は「誰か開いているか」の見張りである。ゼロ値なら減速しない。
	Attach AttachWatch
}

// model は名前に対応するモデルを返す。
//
// arg はペインが取る引数である。task-control だけがタブ名を必要とし、
// 他のペインは無視する。
func (p Panes) model(name, arg string) (tea.Model, error) {
	// 減速の対象はポーリングで回り続ける 4 ペインだけである。
	// task-create と task-control は利用者のキー入力で動くので、誰も
	// 開いていなければそもそも何も起きない。
	switch name {
	case NameDashboard:
		m := NewDashboardModel(p.Dashboard, p.Env)
		m.polling = m.polling.withAttachWatch(p.Attach)
		return m, nil
	case NameWaiting:
		m := NewWaitingModel(p.Waiting, p.Env)
		m.polling = m.polling.withAttachWatch(p.Attach)
		return m, nil
	case NameDone:
		m := NewDoneModel(p.Done, p.Env)
		m.polling = m.polling.withAttachWatch(p.Attach)
		return m, nil
	case NameNews:
		m := NewNewsModel(p.News)
		m.polling = m.polling.withAttachWatch(p.Attach)
		return m, nil
	case NameTaskCreate:
		return NewTaskCreateModel(p.TaskCreate, p.Env), nil
	case NameTaskControl:
		return NewTaskControlModel(p.TaskControl, p.Env, arg), nil
	default:
		return nil, fmt.Errorf("不明なペインです: %s", name)
	}
}

// Run は名前のペインを対話モードで動かす。ペインが終了するまで戻らない。
func (p Panes) Run(name, arg string) error {
	model, err := p.model(name, arg)
	if err != nil {
		return err
	}
	if _, err := tea.NewProgram(model).Run(); err != nil {
		return fmt.Errorf("ペイン %s の実行に失敗しました: %w", name, err)
	}
	return nil
}

// Once は名前のペインを 1 回だけ描画した文字列を返す。
//
// Bubble Tea のプログラムは起動しないため端末を必要としない。現行 Shell 版の
// CONDUCTOR_*_ONCE と同じ経路で、ゴールデンテストもここを通る。
func (p Panes) Once(name, arg string) (string, error) {
	model, err := p.model(name, arg)
	if err != nil {
		return "", err
	}
	once, ok := model.(Once)
	if !ok {
		return "", fmt.Errorf("ペイン %s は単発描画に対応していません", name)
	}
	return once.Once()
}
