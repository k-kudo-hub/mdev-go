package app_test

import (
	"testing"
	"time"

	"github.com/k-kudo-hub/mdev-go/internal/app"
)

// stubNewsFetcher は取得の呼ばれ方だけを記録する NewsFetcher である。
type stubNewsFetcher struct {
	// has は HasNews が返す値。
	has bool
	// fetched は FetchNews に渡された日付。呼ばれなければ空のままになる。
	fetched string
	// askedFor は HasNews に渡された日付。
	askedFor string
}

func (f *stubNewsFetcher) FetchNews(date string) { f.fetched = date }

func (f *stubNewsFetcher) HasNews(date string) bool {
	f.askedFor = date
	return f.has
}

// newsFixedClock は固定の時刻を返す Clock である。
type newsFixedClock struct{ at time.Time }

func (c newsFixedClock) Now() time.Time { return c.at }

// TestNewsRefresher は当日ファイルの有無と --force の組み合わせを確かめる。
//
// セッションを開くたびに走る処理なので、既にある日に取りに行くと毎回
// 通信の待ちが入る。逆に --force で取りに行かないと、News ペインから
// 引き直せなくなる。
func TestNewsRefresher(t *testing.T) {
	t.Parallel()

	const today = "2026-08-14"
	at := time.Date(2026, 8, 14, 9, 30, 0, 0, time.UTC)

	tests := []struct {
		name string
		// has は当日のニュースが既にあるか。
		has bool
		// force は --force が指定されたか。
		force bool
		// wantFetched は取りに行くべきか。
		wantFetched bool
	}{
		{name: "当日ファイルが無ければ取りに行く", has: false, force: false, wantFetched: true},
		{name: "当日ファイルがあれば取りに行かない", has: true, force: false, wantFetched: false},
		{name: "force なら当日ファイルがあっても取り直す", has: true, force: true, wantFetched: true},
		{name: "force で当日ファイルが無くても取りに行く", has: false, force: true, wantFetched: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			fetcher := &stubNewsFetcher{has: tt.has}
			refresher := &app.NewsRefresher{Fetcher: fetcher, Clock: newsFixedClock{at: at}}

			refresher.Refresh(tt.force)

			if tt.wantFetched && fetcher.fetched != today {
				t.Errorf("FetchNews(%q) を期待したが fetched = %q", today, fetcher.fetched)
			}
			if !tt.wantFetched && fetcher.fetched != "" {
				t.Errorf("取りに行かないはずが FetchNews(%q) が呼ばれた", fetcher.fetched)
			}
		})
	}
}

// TestNewsRefresherSkipsExistenceCheckWhenForced は force のとき有無を
// 見ないことを確かめる。見てから無視しても結果は同じだが、無駄な I/O を
// 増やさない。
func TestNewsRefresherSkipsExistenceCheckWhenForced(t *testing.T) {
	t.Parallel()

	fetcher := &stubNewsFetcher{has: true}
	refresher := &app.NewsRefresher{
		Fetcher: fetcher,
		Clock:   newsFixedClock{at: time.Date(2026, 8, 14, 9, 30, 0, 0, time.UTC)},
	}

	refresher.Refresh(true)

	if fetcher.askedFor != "" {
		t.Errorf("force のときは HasNews を呼ばないはずが %q で呼ばれた", fetcher.askedFor)
	}
}
