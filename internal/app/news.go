package app

import "github.com/k-kudo-hub/mdev-go/internal/domain"

// NewsRefresher は `mdev news fetch` のユースケースである
// (現行 fetch-news.sh 相当)。
//
// セッションを開くたびに init.zsh から呼ばれるため、既に当日のニュースが
// あるなら取りに行かない。ニュースは 1 日分をまとめて置く作りで、開き直す
// たびに引き直しても中身はほぼ変わらない一方、通信の待ちは毎回発生する。
type NewsRefresher struct {
	Fetcher NewsFetcher
	Clock   Clock
}

// Refresh は当日のニュースを用意する。
//
// force が偽で当日のファイルが既にあるときは何もしない。force が真なら
// ファイルの有無に関わらず取り直す(News ペインの r キーと同じ扱い)。
//
// **失敗しても何も返さない。** ニュースは無くても作業は進むため、取得の
// 失敗でセッションの起動を止めるべきではない(現行版もすべての失敗経路で
// 黙って exit 0 する)。
func (r *NewsRefresher) Refresh(force bool) {
	date := r.Clock.Now().Format(domain.DailyFileDateLayout)
	if !force && r.Fetcher.HasNews(date) {
		return
	}
	r.Fetcher.FetchNews(date)
}
