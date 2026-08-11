package domain

import (
	"bytes"
	"encoding/json"
	"regexp"
	"strings"
)

// NewsItemLimit はニュースファイルに残す件数の上限(現行版の `count < 5`)。
const NewsItemLimit = 5

// NewsDescriptionLimit は説明文の長さの上限。超えた分は落として "..." を付ける。
const NewsDescriptionLimit = 120

// newsItemSeparator は RSS を項目ごとに切る目印である。
// 現行 fetch-news.sh の awk が `RS = "<item>"` で使うものと同じ。
const newsItemSeparator = "<item>"

// htmlTagPattern は説明文に混ざる HTML タグである。
// 現行版の `gsub(/<[^>]*>/, "")` に対応する。
var htmlTagPattern = regexp.MustCompile(`<[^>]*>`)

// ParseRSSItems は RSS(XML)から表示する項目を取り出す。
//
// XML パーサではなく現行 fetch-news.sh の awk と同じ文字列操作で切り出す。
// 実在のフィードには CDATA・生の HTML・実体参照が混ざっており、厳密な
// パースにすると 1 か所の崩れでニュースが丸ごと消える。表示するのは
// タイトル・URL・短い説明の 3 つだけなので、緩く拾って落とさないほうがよい。
//
// 手順(いずれも現行版と同じ):
//
//   - "<item>" で区切り、2 つ目以降の断片を 1 項目として読む
//   - 各断片の最初の <title> / <link> / <description> の中身を取る
//   - CDATA の囲みを外し、HTML タグを落とし、改行を空白にする
//   - タイトルか URL が空の項目は捨てる
//   - 説明は NewsDescriptionLimit 文字までにして "..." を付ける
//   - 先頭 NewsItemLimit 件で打ち切る
func ParseRSSItems(data []byte) []NewsItem {
	chunks := strings.Split(string(data), newsItemSeparator)
	items := make([]NewsItem, 0, NewsItemLimit)
	for _, chunk := range chunks[1:] {
		if len(items) >= NewsItemLimit {
			break
		}
		title := cleanNewsText(extractTag(chunk, "title"))
		link := cleanNewsText(extractTag(chunk, "link"))
		if title == "" || link == "" {
			continue
		}
		items = append(items, NewsItem{
			Title:       title,
			URL:         link,
			Description: truncateRunes(cleanNewsText(extractTag(chunk, "description"))),
		})
	}
	return items
}

// extractTag は最初の <name> と </name> の間を返す。
// どちらかが無ければ空文字を返す(現行版の extract() と同じ)。
func extractTag(chunk, name string) string {
	open := "<" + name + ">"
	start := strings.Index(chunk, open)
	if start < 0 {
		return ""
	}
	rest := chunk[start+len(open):]
	end := strings.Index(rest, "</"+name+">")
	if end < 0 {
		// 現行版は split の結果が 1 要素になり、その全体を値として返す。
		return rest
	}
	return rest[:end]
}

// cleanNewsText は CDATA の囲みと HTML タグを外し、改行を空白に潰す。
func cleanNewsText(s string) string {
	s = strings.ReplaceAll(s, "<![CDATA[", "")
	s = strings.ReplaceAll(s, "]]>", "")
	s = htmlTagPattern.ReplaceAllString(s, "")
	s = strings.ReplaceAll(s, "\r", "")
	return strings.ReplaceAll(s, "\n", " ")
}

// truncateRunes は説明文を上限までに切り詰める。
//
// 現行版は awk の length() / substr() で切るため、環境によっては
// マルチバイト文字の途中で切れて壊れた文字が残る。ここでは文字単位で切る
// (意図的な差異。表示が壊れないほうを採る)。
func truncateRunes(s string) string {
	runes := []rune(s)
	if len(runes) <= NewsDescriptionLimit {
		return s
	}
	return string(runes[:NewsDescriptionLimit]) + "..."
}

// newsFileItem はニュースファイルに書く 1 項目である。
// キーの並びは現行版が出す JSON と同じにする。
type newsFileItem struct {
	Title       string `json:"title"`
	URL         string `json:"url"`
	Description string `json:"description"`
}

// BuildNewsFile はニュースファイルの中身を組み立てる。
//
// 現行版は awk が組んだ JSON を `jq '.'` で整形して書いているため、
// 2 スペースのインデントと末尾の改行が付く。読むのは ParseNews だけなので
// 体裁に意味は無いが、人が覗いたときに読める形を保つ。
//
// 現行版との意図的な差異: 現行版は awk の gsub で引用符とバックスラッシュを
// 自前で退避しており、awk の実装によってはバックスラッシュが二重にならず
// 壊れた JSON を書きうる(その場合 jq の検証で落ちてファイルが更新されない)。
// ここでは標準の JSON 符号化に任せる。HTML のエスケープは行わない
// (`<` が < にならない)。
func BuildNewsFile(items []NewsItem) []byte {
	file := struct {
		Items []newsFileItem `json:"items"`
	}{Items: make([]newsFileItem, 0, len(items))}
	for _, item := range items {
		file.Items = append(file.Items, newsFileItem{
			Title:       item.Title,
			URL:         item.URL,
			Description: item.Description,
		})
	}

	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetIndent("", "  ")
	enc.SetEscapeHTML(false)
	// 文字列だけの構造体なので符号化は失敗しない。
	_ = enc.Encode(file)
	return buf.Bytes()
}
