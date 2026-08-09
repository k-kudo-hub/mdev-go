package domain

import (
	"encoding/json"
	"strconv"
	"strings"
)

// NewsItem は news ファイルの items 1 件である。
//
// 値はすべて現行版の `jq -r ".items[$i].title"` が出す文字列として持つ。
// そのためキーが無い場合は空文字ではなく "null" が入る。
type NewsItem struct {
	Title       string
	Description string
	URL         string
}

// ParseNews は news ファイルの中身から表示対象の items を取り出す。
//
// ファイルが読めない・JSON が壊れている・items が無い・items が空、の
// いずれでも空スライスを返す。現行版はこれらをすべて
// 「No news yet. Press [r] to reload.」の 1 経路に潰している。
func ParseNews(data []byte) []NewsItem {
	var file struct {
		Items []map[string]json.RawMessage `json:"items"`
	}
	if err := json.Unmarshal(data, &file); err != nil {
		return []NewsItem{}
	}

	items := make([]NewsItem, 0, len(file.Items))
	for _, raw := range file.Items {
		items = append(items, NewsItem{
			Title:       jqRawString(raw["title"]),
			Description: jqRawString(raw["description"]),
			URL:         jqRawString(raw["url"]),
		})
	}
	return items
}

// RenderNews は News ペインの 1 画面分を組み立てる。
//
// date は見出しに出す日付(現行版の `date '+%Y-%m-%d'`)。
//
// 説明行は description が空、または文字列 "null" のときに省かれる。後者は
// 現行版が `[[ "$description" != "null" ]]` で文字列比較しているためで、
// 本当に "null" という説明文が来ても省かれる。
func RenderNews(date string, items []NewsItem) string {
	var b strings.Builder

	b.WriteString(ansiBold + "  AI Tech News" + ansiReset + " " + ansiDim + "[" + date + "]" + ansiReset + "\n")
	b.WriteString(divider(newsDividerWidth))
	b.WriteString("\n")

	if len(items) == 0 {
		b.WriteString("  " + ansiDim + "No news yet. Press [r] to reload." + ansiReset + "\n")
		return b.String()
	}

	for i, item := range items {
		b.WriteString("  " + ansiYellow + strconv.Itoa(i+1) + "." + ansiReset + " " +
			ansiBold + item.Title + ansiReset + "\n")
		if item.Description != "" && item.Description != jqNull {
			b.WriteString("     " + ansiDim + item.Description + ansiReset + "\n")
		}
		b.WriteString("\n")
	}

	b.WriteString(divider(newsDividerWidth))
	b.WriteString("  " + ansiDim + "[1-" + strconv.Itoa(len(items)) + "]: open  ·  [r]: reload" + ansiReset + "\n")
	return b.String()
}
