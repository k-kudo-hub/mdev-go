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

// refreshGate は読み直しの重なりを防ぐ印である。
//
// 完了起点のペーシングでは、ポーリングの読み直しが走っている間はタイマーが
// 存在しないため tickMsg 自体が来ない。この印が実際に効くのは、キー操作で出した
// 読み直し(force 起源)が走っている最中にタイマーが発火した場合である。
// そのときは読み直しを重ねず、次の合図だけを予約し直す。
//
// ゼロ値は「実行中ではない」である。ただし各ペインの New*Model は実行中で
// 始める。Init が必ず 1 回目の読み直しを発行するのに対し、Init は値レシーバで
// モデルを書き換えられず、そこで印を立てられないためである。
type refreshGate struct {
	// inFlight は発行済みでまだ着弾していない読み直しがあることを表す。
	inFlight bool
}

// take は読み直しを発行してよいかを返す。発行できるときは印を立てる。
// ポーリング(tickMsg)だけが使う。
func (g *refreshGate) take() bool {
	if g.inFlight {
		return false
	}
	g.inFlight = true
	return true
}

// force は完了を待たずに読み直しを発行するときに印を立てる。
//
// キー操作と削除・取得の後始末が使う。利用者の操作に対する反応は遅らせずに
// 出したいので、実行中でも発行する(そのぶん重なるのは押した瞬間だけである)。
func (g *refreshGate) force() { g.inFlight = true }

// release は読み直しの着弾で印を下ろす。
//
// 呼ぶのは *RefreshedMsg のハンドラの先頭だけである。エラーで返ってきた場合も、
// 待ち受け中で内容を捨てる場合も必ず通す。下ろし忘れるとポーリングが二度と
// 読み直さなくなる。
func (g *refreshGate) release() { g.inFlight = false }

// rearmCmd は着弾がポーリング起源のときだけ次の合図を予約する。
//
// *RefreshedMsg のハンドラは、内容を捨てる場合もエラーで返った場合も必ず
// ここを通す。チェーンを絶やすとポーリングが二度と回らない。逆に force 起源の
// 着弾で予約するとチェーンが増えるため、そのときは何も返さない。
func rearmCmd(poll bool, d time.Duration) tea.Cmd {
	if !poll {
		return nil
	}
	return tickCmd(d)
}

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
		Startup()
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
		Restore(app.DoneSnapshot, int)
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
)

// Panes は 4 つのペインをまとめて起動できるようにしたものである。
//
// internal/cli は internal/tui を参照できない(ADR-0002)ため、cli 側は
// Run / Once だけを持つ interface としてこれを受け取り、実体の組み立ては
// cmd/mdev が行う。
type Panes struct {
	Dashboard DashboardService
	Waiting   WaitingService
	Done      DoneService
	News      NewsService
	Env       app.PaneEnv
}

// model は名前に対応するモデルを返す。
func (p Panes) model(name string) (tea.Model, error) {
	switch name {
	case NameDashboard:
		return NewDashboardModel(p.Dashboard, p.Env), nil
	case NameWaiting:
		return NewWaitingModel(p.Waiting, p.Env), nil
	case NameDone:
		return NewDoneModel(p.Done), nil
	case NameNews:
		return NewNewsModel(p.News), nil
	default:
		return nil, fmt.Errorf("不明なペインです: %s", name)
	}
}

// Run は名前のペインを対話モードで動かす。ペインが終了するまで戻らない。
func (p Panes) Run(name string) error {
	model, err := p.model(name)
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
func (p Panes) Once(name string) (string, error) {
	model, err := p.model(name)
	if err != nil {
		return "", err
	}
	once, ok := model.(Once)
	if !ok {
		return "", fmt.Errorf("ペイン %s は単発描画に対応していません", name)
	}
	return once.Once()
}
