package domain_test

import (
	"reflect"
	"strings"
	"testing"

	"github.com/k-kudo-hub/mdev-go/internal/domain"
)

// 現行 news-loop.sh の ONCE 出力を隔離環境で実測して写したもの。
// 区切り線だけ 22 本で、他のペイン(26 本)より短い。
const wantNewsThreeItems = "\x1b[1m  AI Tech News\x1b[0m \x1b[2m[2026-08-09]\x1b[0m\n" +
	"\x1b[2m  ──────────────────────\x1b[0m\n" +
	"\n" +
	"  \x1b[0;33m1.\x1b[0m \x1b[1mGPT-5 Released\x1b[0m\n" +
	"     \x1b[2mOpenAI has released GPT-5.\x1b[0m\n" +
	"\n" +
	"  \x1b[0;33m2.\x1b[0m \x1b[1mNo Desc Item\x1b[0m\n" +
	"\n" +
	"  \x1b[0;33m3.\x1b[0m \x1b[1mEmpty Desc\x1b[0m\n" +
	"\n" +
	"\x1b[2m  ──────────────────────\x1b[0m\n" +
	"  \x1b[2m[1-3]: open  ·  [r]: reload\x1b[0m\n"

const wantNewsEmpty = "\x1b[1m  AI Tech News\x1b[0m \x1b[2m[2026-08-09]\x1b[0m\n" +
	"\x1b[2m  ──────────────────────\x1b[0m\n" +
	"\n" +
	"  \x1b[2mNo news yet. Press [r] to reload.\x1b[0m\n"

func TestParseNews(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		data string
		want []domain.NewsItem
	}{
		{
			name: "通常",
			data: `{"items":[{"title":"A","url":"https://a","description":"desc a"}]}`,
			want: []domain.NewsItem{{Title: "A", URL: "https://a", Description: "desc a"}},
		},
		{
			// jq -r は欠けたキーを "null" という文字列にする。
			name: "description が null なら文字列 null になる",
			data: `{"items":[{"title":"A","url":"https://a","description":null}]}`,
			want: []domain.NewsItem{{Title: "A", URL: "https://a", Description: "null"}},
		},
		{name: "items が空", data: `{"items":[]}`, want: []domain.NewsItem{}},
		{name: "items が無い", data: `{}`, want: []domain.NewsItem{}},
		{name: "壊れた JSON", data: `{broken`, want: []domain.NewsItem{}},
		{name: "空ファイル", data: ``, want: []domain.NewsItem{}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := domain.ParseNews([]byte(tt.data)); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("ParseNews() = %+v, want %+v", got, tt.want)
			}
		})
	}
}

func TestRenderNews(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		items []domain.NewsItem
		want  string
	}{
		{
			// description が空、または文字列 "null" の項目は説明行を出さない。
			name: "3 件(説明あり / null / 空)",
			items: []domain.NewsItem{
				{Title: "GPT-5 Released", Description: "OpenAI has released GPT-5."},
				{Title: "No Desc Item", Description: "null"},
				{Title: "Empty Desc", Description: ""},
			},
			want: wantNewsThreeItems,
		},
		{
			name:  "0 件なら No news yet でフッタも出ない",
			items: nil,
			want:  wantNewsEmpty,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := domain.RenderNews("2026-08-09", tt.items); got != tt.want {
				t.Errorf("RenderNews() の出力が違う\n got: %q\nwant: %q", got, tt.want)
			}
		})
	}
}

func TestRenderNewsShowsAllItemsNotJustFive(t *testing.T) {
	t.Parallel()

	// 5 件に絞っているのは fetch-news.sh であって、表示側は items を
	// すべて出す。フッタの範囲も件数に追従する。
	items := make([]domain.NewsItem, 0, 7)
	for range 7 {
		items = append(items, domain.NewsItem{Title: "t"})
	}
	got := domain.RenderNews("2026-08-09", items)

	if !strings.Contains(got, "\x1b[0;33m7.\x1b[0m") {
		t.Errorf("7 件目が出ていない: %q", got)
	}
	if !strings.Contains(got, "[1-7]: open") {
		t.Errorf("フッタの範囲が 7 件になっていない: %q", got)
	}
}
