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
//   - タイトルと説明は CDATA の囲みを外す(URL は外さない)
//   - **この時点で**タイトルか URL が空なら項目ごと捨てる
//   - 残った項目のタイトルと説明から HTML タグを落とし、改行を空白にする
//   - 説明は NewsDescriptionLimit 文字までにして "..." を付ける
//   - 先頭 NewsItemLimit 件で打ち切る
//
// 空判定と整形の順番、および URL に何もしないことは現行版のままである
// (実測で確認済み)。順番を入れ替えると、タグだけのタイトル(整形すると
// 空になる)を持つ項目が現行版では出るのにこちらでは消える、という差が出る。
func ParseRSSItems(data []byte) []NewsItem {
	chunks := strings.Split(string(data), newsItemSeparator)
	items := make([]NewsItem, 0, NewsItemLimit)
	for _, chunk := range chunks[1:] {
		if len(items) >= NewsItemLimit {
			break
		}
		title := stripCDATA(extractTag(chunk, "title"))
		// URL は CDATA の囲みも HTML タグも外さず、改行も潰さない。
		// 現行版が title / description にしか手を入れていないためである。
		link := extractTag(chunk, "link")
		if title == "" || link == "" {
			continue
		}
		items = append(items, NewsItem{
			Title:       cleanNewsText(title),
			URL:         link,
			Description: truncateRunes(cleanNewsText(stripCDATA(extractTag(chunk, "description")))),
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

// stripCDATA は CDATA の囲みだけを外す。
//
// 空判定より前に行う整形はこれだけである。囲みを外して空になるもの
// (<![CDATA[]]> だけのタイトル)は項目ごと捨てられる。
func stripCDATA(s string) string {
	s = strings.ReplaceAll(s, "<![CDATA[", "")
	return strings.ReplaceAll(s, "]]>", "")
}

// cleanNewsText は HTML タグを落とし、改行を空白に潰す。
//
// 空判定の **後** に行う。引用符を含むタグ(<a href="...">)をそのまま
// 出すと表示が崩れるので落とすが、それで空になっても項目は残す。
func cleanNewsText(s string) string {
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
