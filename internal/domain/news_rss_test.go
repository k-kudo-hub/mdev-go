package domain_test

import (
	"reflect"
	"strings"
	"testing"

	"github.com/k-kudo-hub/mdev-go/internal/domain"
)

// techCrunchRSS は test.sh「26. fetch-news.sh」のモック curl が返す RSS である。
const techCrunchRSS = `<?xml version="1.0" encoding="UTF-8"?>
<rss version="2.0">
<channel>
<title>TechCrunch AI</title>
<item><title><![CDATA[GPT-5 Released with Major Improvements]]></title><link>https://techcrunch.com/gpt5</link><description><![CDATA[OpenAI has released GPT-5 with significant performance gains across all benchmarks.]]></description></item>
<item><title><![CDATA[Claude 4.6 Beats All Benchmarks]]></title><link>https://techcrunch.com/claude</link><description><![CDATA[Anthropic Claude 4.6 sets new records in reasoning and coding tasks.]]></description></item>
<item><title><![CDATA[Open Source LLM Surpasses Commercial Models]]></title><link>https://techcrunch.com/open-llm?ref=rss&amp;utm=ai</link><description><![CDATA[A new open-source model outperforms proprietary alternatives.]]></description></item>
<item><title><![CDATA[AI Chip Startup Raises 1B]]></title><link>https://techcrunch.com/ai-chip</link><description><![CDATA[Startup secures <a href="https://example.com">massive funding</a> for next-gen AI processors.]]></description></item>
<item><title><![CDATA[New AI Safety Framework Proposed]]></title><link>https://techcrunch.com/ai-safety</link><description><![CDATA[Researchers propose
comprehensive guidelines for safe
AI deployment.]]></description></item>
</channel>
</rss>
`

// TestParseRSSItems は現行 fetch-news.sh の awk と同じ切り出しを確かめる。
// 期待値は現行版の awk へ同じ RSS を流して得たものである。
func TestParseRSSItems(t *testing.T) {
	got := domain.ParseRSSItems([]byte(techCrunchRSS))
	want := []domain.NewsItem{
		{
			Title:       "GPT-5 Released with Major Improvements",
			URL:         "https://techcrunch.com/gpt5",
			Description: "OpenAI has released GPT-5 with significant performance gains across all benchmarks.",
		},
		{
			Title:       "Claude 4.6 Beats All Benchmarks",
			URL:         "https://techcrunch.com/claude",
			Description: "Anthropic Claude 4.6 sets new records in reasoning and coding tasks.",
		},
		{
			// URL の実体参照はそのまま残す(現行版も置換しない)。
			Title:       "Open Source LLM Surpasses Commercial Models",
			URL:         "https://techcrunch.com/open-llm?ref=rss&amp;utm=ai",
			Description: "A new open-source model outperforms proprietary alternatives.",
		},
		{
			// 説明の HTML タグは落とす(引用符を含むタグで JSON が壊れないため)。
			Title:       "AI Chip Startup Raises 1B",
			URL:         "https://techcrunch.com/ai-chip",
			Description: "Startup secures massive funding for next-gen AI processors.",
		},
		{
			// CDATA の中の改行は空白に潰す。
			Title:       "New AI Safety Framework Proposed",
			URL:         "https://techcrunch.com/ai-safety",
			Description: "Researchers propose comprehensive guidelines for safe AI deployment.",
		},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("ParseRSSItems =\n%#v\nwant\n%#v", got, want)
	}
}

// TestParseRSSItemsEdgeCases は境界の扱いを固定する。
func TestParseRSSItemsEdgeCases(t *testing.T) {
	item := func(title, link, desc string) string {
		return "<item><title>" + title + "</title><link>" + link +
			"</link><description>" + desc + "</description></item>"
	}

	tests := []struct {
		name string
		rss  string
		want []domain.NewsItem
	}{
		{
			name: "RSS でない入力は 0 件になる",
			rss:  "not valid xml at all",
			want: []domain.NewsItem{},
		},
		{
			name: "空入力は 0 件になる",
			rss:  "",
			want: []domain.NewsItem{},
		},
		{
			name: "タイトルが無い項目は捨てる",
			rss:  "<channel>" + item("", "https://e/1", "d") + "</channel>",
			want: []domain.NewsItem{},
		},
		{
			name: "URL が無い項目は捨てる",
			rss:  "<channel>" + item("t", "", "d") + "</channel>",
			want: []domain.NewsItem{},
		},
		{
			// 空判定は HTML タグを落とす **前** に行う。現行版はこの項目を
			// タイトル空のまま出力する(実測で確認済み)。
			name: "タグだけのタイトルは空になっても項目として残る",
			rss:  "<channel>" + item("<b></b>", "https://e/1", "d") + "</channel>",
			want: []domain.NewsItem{{Title: "", URL: "https://e/1", Description: "d"}},
		},
		{
			// CDATA の囲みは空判定より前に外すので、これは空になって捨てられる。
			name: "CDATA が空のタイトルは項目ごと捨てる",
			rss:  "<channel>" + item("<![CDATA[]]>", "https://e/1", "d") + "</channel>",
			want: []domain.NewsItem{},
		},
		{
			// URL の CDATA と HTML タグには手を入れない。現行版が
			// title / description にしか gsub(/<[^>]*>/) をかけていないためである。
			name: "URL の CDATA の囲みは外さない",
			rss:  "<channel>" + item("t", "<![CDATA[https://e/2]]>", "d") + "</channel>",
			want: []domain.NewsItem{{Title: "t", URL: "<![CDATA[https://e/2]]>", Description: "d"}},
		},
		{
			// 折り返された <link> は実在のフィードにある。空白へ **置き換える**
			// と URL の途中に空白が入って開けなくなるため、落として詰める。
			name: "折り返された URL は改行とインデントを落として詰める",
			rss:  "<channel>" + item("t", "\n\thttps://e/3  \n", "d") + "</channel>",
			want: []domain.NewsItem{{Title: "t", URL: "https://e/3", Description: "d"}},
		},
		{
			// 現行版は LC_ALL=C の awk なので [[:space:]] はバイト指向で、
			// Unicode の空白は空白として扱われずそのまま残る。
			name: "URL の Unicode 空白は落とさない",
			rss:  "<channel>" + item("t", "　https://e/3", "d") + "</channel>",
			want: []domain.NewsItem{{Title: "t", URL: "　https://e/3", Description: "d"}},
		},
		{
			// タブは項目の区切りとして使われるため、現行版は空白に潰す。
			name: "タイトルと説明のタブは空白にする",
			rss:  "<channel>" + item("a\tb", "https://e/1", "c\td") + "</channel>",
			want: []domain.NewsItem{{Title: "a b", URL: "https://e/1", Description: "c d"}},
		},
		{
			name: "説明が無くても項目は残る",
			rss:  "<channel><item><title>t</title><link>https://e/1</link></item></channel>",
			want: []domain.NewsItem{{Title: "t", URL: "https://e/1"}},
		},
		{
			name: "6 件目以降は捨てる",
			rss: "<channel>" + strings.Repeat(item("t", "https://e/1", "d"), 7) +
				"</channel>",
			want: []domain.NewsItem{
				{Title: "t", URL: "https://e/1", Description: "d"},
				{Title: "t", URL: "https://e/1", Description: "d"},
				{Title: "t", URL: "https://e/1", Description: "d"},
				{Title: "t", URL: "https://e/1", Description: "d"},
				{Title: "t", URL: "https://e/1", Description: "d"},
			},
		},
		{
			name: "長い説明は 120 文字で切って ... を付ける",
			rss:  "<channel>" + item("t", "https://e/1", strings.Repeat("a", 130)) + "</channel>",
			want: []domain.NewsItem{
				{Title: "t", URL: "https://e/1", Description: strings.Repeat("a", 120) + "..."},
			},
		},
		{
			name: "ちょうど 120 文字なら切らない",
			rss:  "<channel>" + item("t", "https://e/1", strings.Repeat("a", 120)) + "</channel>",
			want: []domain.NewsItem{
				{Title: "t", URL: "https://e/1", Description: strings.Repeat("a", 120)},
			},
		},
		{
			name: "閉じタグが無ければ残り全部を値にする",
			rss:  "<channel><item><title>t</title><link>https://e/1</link><description>tail",
			want: []domain.NewsItem{{Title: "t", URL: "https://e/1", Description: "tail"}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := domain.ParseRSSItems([]byte(tt.rss)); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("ParseRSSItems = %#v, want %#v", got, tt.want)
			}
		})
	}
}

// TestBuildNewsFile は書き出す JSON が現行版と同じ形であることを確かめる。
// 中身は ParseNews(News ペイン)が読むため、キー名と入れ子が変わると
// ニュースが表示されなくなる。
func TestBuildNewsFile(t *testing.T) {
	got := string(domain.BuildNewsFile([]domain.NewsItem{
		{Title: "GPT-5 Released", URL: "https://e/1?a=1&b=2", Description: "説明 <b>"},
	}))
	want := `{
  "items": [
    {
      "title": "GPT-5 Released",
      "url": "https://e/1?a=1&b=2",
      "description": "説明 <b>"
    }
  ]
}
`
	if got != want {
		t.Errorf("BuildNewsFile =\n%s\nwant\n%s", got, want)
	}
}

// TestBuildNewsFileEmpty は 0 件でも空の items を持つ JSON になることを
// 確かめる。現行版も「項目が取れなかった」ときにこの形を書く。
func TestBuildNewsFileEmpty(t *testing.T) {
	got := string(domain.BuildNewsFile(nil))
	want := "{\n  \"items\": []\n}\n"
	if got != want {
		t.Errorf("BuildNewsFile = %q, want %q", got, want)
	}
}

// TestBuildNewsFileRoundTrip は書いたものを News ペインが読めることを確かめる。
func TestBuildNewsFileRoundTrip(t *testing.T) {
	items := domain.ParseRSSItems([]byte(techCrunchRSS))
	got := domain.ParseNews(domain.BuildNewsFile(items))
	if !reflect.DeepEqual(got, items) {
		t.Errorf("往復で内容が変わりました:\n%#v\n%#v", got, items)
	}
}
