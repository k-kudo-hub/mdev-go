package app

import (
	"fmt"

	"github.com/k-kudo-hub/mdev-go/internal/domain"
)

// DoneSnapshot は 1 回ぶんの Done の状態である。
//
// DashboardSnapshot と同じ考え方で、tui へは描画結果と番号での操作だけを渡す。
type DoneSnapshot struct {
	// Text は画面に出す文字列。
	Text string
	// Count は一覧に並んでいる件数。番号キーの範囲判定に使う。
	Count int

	// rows は表示順の行そのもの。restore へ渡す 3 つ組を取り出すために要る。
	rows []domain.DoneRow
}

// DonePane は Done ペインのユースケースである(現行 done-loop.sh 相当)。
type DonePane struct {
	Daily DailyReader
	Shell ShellRunner
	Clock Clock
}

// Refresh は当日の daily log から表示内容を組み立てる。
//
// 対象は当日ぶんの全セッションで、セッションをまたいで 1 つの一覧にまとめる。
func (p *DonePane) Refresh() DoneSnapshot {
	date := p.Clock.Now().Format(domain.DailyFileDateLayout)
	view := domain.BuildDoneView(p.Daily.ReadToday(date))
	return DoneSnapshot{Text: domain.RenderDone(view), Count: len(view.Rows), rows: view.Rows}
}

// Restore は snapshot の number 番目(1 始まり)のタスクをダッシュボードへ戻す。
//
// 終了コードは見ない。現行版も `2>/dev/null` で握り潰しており、失敗した場合は
// エントリが Done に残ったままになる(次のポーリングで再表示される)ことで
// 利用者が気づく作りになっている。
//
// 渡す 3 つ組は表示に使ったものと同じである。現行版の読み直しで値がずれた
// 行では、ずれたままの値が渡る(domain.DoneRow のコメントを参照)。
func (p *DonePane) Restore(snapshot DoneSnapshot, number int) {
	if number < 1 || number > len(snapshot.rows) {
		return
	}
	row := snapshot.rows[number-1]
	p.Shell.RestoreTask(row.Tab, row.Session, row.CompletedAt)
}

// WaitingPane は Waiting ペインのユースケースである(現行 waiting-loop.sh 相当)。
//
// キー入力も zellij の呼び出しも無く、pending を読むだけである。
type WaitingPane struct {
	Pending PendingLister
}

// Refresh は外部の返答待ちになっている pending を並べた画面を返す。
func (p *WaitingPane) Refresh(env PaneEnv) (string, error) {
	views, err := p.Pending.List(env.Session())
	if err != nil {
		return "", fmt.Errorf("pending の読み取りに失敗しました: %w", err)
	}
	return domain.RenderWaiting(domain.WaitingItems(views)), nil
}

// NewsSnapshot は 1 回ぶんの News の状態である。
type NewsSnapshot struct {
	// Text は画面に出す文字列。
	Text string
	// FetchingText はニュースを取り直している間に出す画面。
	FetchingText string
	// Count は並んでいる項目数。番号キーの範囲判定に使う。
	Count int

	// items は表示順の項目そのもの。開く URL を取り出すために要る。
	items []domain.NewsItem
}

// NewsPane は News ペインのユースケースである(現行 news-loop.sh 相当)。
type NewsPane struct {
	News   NewsReader
	Shell  ShellRunner
	Opener URLOpener
	Clock  Clock
}

// Refresh は当日のニュースを読んで画面を組み立てる。
func (p *NewsPane) Refresh() NewsSnapshot {
	date := p.Clock.Now().Format(domain.DailyFileDateLayout)
	items := domain.ParseNews(p.News.Read(date))
	return NewsSnapshot{
		Text:         domain.RenderNews(date, items),
		FetchingText: domain.RenderNewsFetching(date),
		Count:        len(items),
		items:        items,
	}
}

// Reload は当日のニュースを取り直す(fetch-news.sh --force)。
// 同期で走らせ、終わってから次の描画で新しい内容が出る。
func (p *NewsPane) Reload() {
	p.Shell.FetchNews()
}

// Open は snapshot の number 番目(1 始まり)の URL をブラウザで開く。
//
// URL が空、または jq が "null" を返した(url キーが無い)場合は何もしない。
func (p *NewsPane) Open(snapshot NewsSnapshot, number int) {
	if number < 1 || number > len(snapshot.items) {
		return
	}
	url := snapshot.items[number-1].URL
	if url == "" || url == domain.JQNullText {
		return
	}
	p.Opener.Open(url)
}
