package app

import (
	"fmt"

	"github.com/k-kudo-hub/mdev-go/internal/domain"
)

// DonePane は Done ペインのユースケースである(現行 done-loop.sh 相当)。
type DonePane struct {
	Daily DailyReader
	Shell ShellRunner
	Clock Clock
}

// Refresh は当日の daily log から表示内容を組み立てる。
//
// 対象は当日ぶんの全セッションで、セッションをまたいで 1 つの一覧にまとめる。
func (p *DonePane) Refresh() domain.DoneView {
	date := p.Clock.Now().Format(domain.DailyFileDateLayout)
	return domain.BuildDoneView(p.Daily.ReadToday(date))
}

// Restore は行のタスクをダッシュボードへ戻す。
//
// 終了コードは見ない。現行版も `2>/dev/null` で握り潰しており、失敗した場合は
// エントリが Done に残ったままになる(次のポーリングで再表示される)ことで
// 利用者が気づく作りになっている。
//
// 渡す 3 つ組は表示に使ったものと同じである。現行版の読み直しで値がずれた
// 行では、ずれたままの値が渡る(domain.DoneRow のコメントを参照)。
func (p *DonePane) Restore(row domain.DoneRow) {
	p.Shell.RestoreTask(row.Tab, row.Session, row.CompletedAt)
}

// WaitingPane は Waiting ペインのユースケースである(現行 waiting-loop.sh 相当)。
//
// キー入力も zellij の呼び出しも無く、pending を読むだけである。
type WaitingPane struct {
	Pending PendingLister
}

// Refresh は外部の返答待ちになっている pending をファイル名順で返す。
func (p *WaitingPane) Refresh(env PaneEnv) ([]domain.PendingView, error) {
	views, err := p.Pending.List(env.Session())
	if err != nil {
		return nil, fmt.Errorf("pending の読み取りに失敗しました: %w", err)
	}
	return domain.WaitingItems(views), nil
}

// NewsPane は News ペインのユースケースである(現行 news-loop.sh 相当)。
type NewsPane struct {
	News   NewsReader
	Shell  ShellRunner
	Opener URLOpener
	Clock  Clock
}

// Refresh は当日のニュースファイルを読み、見出しに出す日付と項目を返す。
func (p *NewsPane) Refresh() (string, []domain.NewsItem) {
	date := p.Clock.Now().Format(domain.DailyFileDateLayout)
	return date, domain.ParseNews(p.News.Read(date))
}

// Reload は当日のニュースを取り直す(fetch-news.sh --force)。
// 同期で走らせ、終わってから次の描画で新しい内容が出る。
func (p *NewsPane) Reload() {
	p.Shell.FetchNews()
}

// Open は項目の URL をブラウザで開く。
//
// URL が空、または jq が "null" を返した(url キーが無い)場合は何もしない。
func (p *NewsPane) Open(item domain.NewsItem) {
	if item.URL == "" || item.URL == domain.JQNullText {
		return
	}
	p.Opener.Open(item.URL)
}
