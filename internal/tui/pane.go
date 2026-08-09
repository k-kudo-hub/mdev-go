package tui

import (
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

// promptExpiredMsg は 2 打鍵目の待ち受けが時間切れになったことを表す。
// token は世代番号で、待ち受けをやり直した後に古いタイマーが効かないようにする。
type promptExpiredMsg struct{ token int }

// promptTimeoutCmd は PromptTimeout 後に待ち受けを打ち切る合図を送る。
func promptTimeoutCmd(token int) tea.Cmd {
	return tea.Tick(PromptTimeout, func(time.Time) tea.Msg {
		return promptExpiredMsg{token: token}
	})
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
