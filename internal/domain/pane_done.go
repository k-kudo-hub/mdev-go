package domain

import (
	"bytes"
	"encoding/json"
	"errors"
	"math"
	"sort"
	"strconv"
	"strings"
)

// Done ペインが completed_at から切り出す時刻の範囲(現行版の `.[11:16]`)。
// "2026-08-09T10:00:00+0900" の 11 文字目から 16 文字目の手前まで = "10:00"。
const (
	doneTimeStart = 11
	doneTimeEnd   = 16
)

// Done の行を組み立てる printf の桁数(現行版の `%-14s %3s t  %7s`)。
// bash の printf はバイト幅で詰めるため、Go の %-14s(ルーン幅)は使えない。
const (
	doneTabWidth   = 14
	doneTurnsWidth = 3
	doneCostWidth  = 7
)

// Done の markers に対応する記号。
const (
	markerMerged = "🚀"
	markerSlack  = "💬"
	markerDoc    = "📝"
)

// errJQFailed は現行版の jq がエラー終了する状況を表す。
//
// 現行は全ファイルを 1 つの `jq -s` に流すため、どこか 1 か所でも壊れていると
// 集計も一覧も丸ごと失敗し、正常なエントリまで表示されなくなる。この既知の
// 挙動を再現するために、途中経過ではなく全体を捨てる合図として使う。
var errJQFailed = errors.New("jq に相当する処理が失敗した")

// DoneRow は Done ペインの 1 行分の値である。
//
// フィールド名は現行版が `read -r tab session completed turns cost time markers`
// で受ける変数に対応する。ただし現行はタブ区切りの行を IFS で読み直すため、
// 空フィールドがあると値が 1 つずつ手前へずれる(BuildDoneView のコメント参照)。
// ここでもそのずれを再現するので、Session に completed_at が入ることがある。
type DoneRow struct {
	Tab         string
	Session     string
	CompletedAt string
	Turns       string
	Cost        string
	Time        string
	Markers     string
}

// DoneView は Done ペインの 1 画面分の材料である。
type DoneView struct {
	// Count は表示対象(restored でない)のエントリ数。
	Count int
	// Turns / Calls / Cost は見出しの統計行に出す文字列。
	Turns string
	Calls string
	Cost  string
	// Rows は表示順(completed_at の昇順)の行。
	Rows []DoneRow
}

// BuildDoneView は当日の daily log の全行から Done ペインの表示内容を組み立てる。
//
// lines には当日の全セッションの daily ファイルを連結した行を、ファイル順・
// 行順のまま渡す。空行は読み飛ばす。
//
// 現行版の再現として次の 3 つを守っている。
//
//   - 1 行でも JSON として壊れていれば全体を捨てる(`jq -s` が失敗し、
//     統計が空になり "No tasks completed yet" に落ちる)
//   - `restored` がちょうど true のエントリだけを除く
//   - completed_at の昇順に安定ソートする(同着なら入力順のまま)
func BuildDoneView(lines [][]byte) DoneView {
	view, err := buildDoneView(lines)
	if err != nil {
		return DoneView{}
	}
	return view
}

func buildDoneView(lines [][]byte) (DoneView, error) {
	entries, err := parseDoneEntries(lines)
	if err != nil {
		return DoneView{}, err
	}

	// completed_at の昇順に安定ソートする。jq の sort_by は安定で、
	// null は文字列より前に来る。
	sort.SliceStable(entries, func(i, j int) bool {
		return lessDoneKey(entries[i].sortKey, entries[j].sortKey)
	})

	var turns, calls, cost float64
	rows := make([]DoneRow, 0, len(entries))
	for _, entry := range entries {
		turns += entry.statTurns
		calls += entry.statCalls
		cost += entry.statCost

		row, err := entry.row()
		if err != nil {
			return DoneView{}, err
		}
		rows = append(rows, row)
	}

	return DoneView{
		Count: len(entries),
		Turns: formatJQNumber(turns),
		Calls: formatJQNumber(calls),
		Cost:  formatDoneTotalCost(cost),
		Rows:  rows,
	}, nil
}

// doneEntry は daily の 1 レコードのうち Done ペインが使う値である。
type doneEntry struct {
	raw json.RawMessage
	// sortKey は completed_at。存在しない/null の場合は hasKey が false になる。
	sortKey doneSortKey
	// stat* は統計行の合計に足す値(`// 0` 相当で欠損は 0)。
	statTurns float64
	statCalls float64
	statCost  float64
}

// doneSortKey は jq の値順序のうち、completed_at に現れうる範囲を表す。
// jq では null が文字列より前に並ぶ。
type doneSortKey struct {
	isString bool
	value    string
}

func lessDoneKey(a, b doneSortKey) bool {
	if a.isString != b.isString {
		return !a.isString
	}
	return a.value < b.value
}

func parseDoneEntries(lines [][]byte) ([]doneEntry, error) {
	entries := make([]doneEntry, 0, len(lines))
	for _, line := range lines {
		trimmed := bytes.TrimSpace(line)
		if len(trimmed) == 0 {
			continue
		}
		var raw json.RawMessage
		if err := json.Unmarshal(trimmed, &raw); err != nil {
			return nil, errJQFailed
		}

		restored, err := jqIndex(raw, "restored")
		if err != nil {
			return nil, err
		}
		// `(.restored // false) != true` なので、ちょうど true のときだけ除く。
		if bytes.Equal(bytes.TrimSpace(restored), []byte("true")) {
			continue
		}

		entry := doneEntry{raw: raw}
		if entry.sortKey, err = doneSortKeyOf(raw); err != nil {
			return nil, err
		}
		if entry.statTurns, err = doneStatNumber(raw, "total_turns"); err != nil {
			return nil, err
		}
		if entry.statCalls, err = doneStatNumber(raw, "total_tool_calls"); err != nil {
			return nil, err
		}
		if entry.statCost, err = doneStatNumber(raw, "total_cost_usd"); err != nil {
			return nil, err
		}
		entries = append(entries, entry)
	}
	return entries, nil
}

func doneSortKeyOf(raw json.RawMessage) (doneSortKey, error) {
	completed, err := jqIndex(raw, "completed_at")
	if err != nil {
		return doneSortKey{}, err
	}
	var value any
	if err := json.Unmarshal(completed, &value); err != nil {
		return doneSortKey{}, errJQFailed
	}
	if s, ok := value.(string); ok {
		return doneSortKey{isString: true, value: s}, nil
	}
	return doneSortKey{}, nil
}

// doneStatNumber は `.summary.<key> // 0` 相当の値を返す。
func doneStatNumber(raw json.RawMessage, key string) (float64, error) {
	summary, err := jqIndex(raw, "summary")
	if err != nil {
		return 0, err
	}
	value, err := jqIndex(summary, key)
	if err != nil {
		return 0, err
	}
	if !JSONTruthy(value) {
		return 0, nil
	}
	var number float64
	if err := json.Unmarshal(value, &number); err != nil {
		// 数値でない値を add に渡すと jq がエラーになる。
		return 0, errJQFailed
	}
	return number, nil
}

// row は 1 エントリからタブ区切りの行を組み立て、現行版と同じ規則で読み直す。
func (e doneEntry) row() (DoneRow, error) {
	fields := make([]string, 0, 7)
	for _, key := range []string{"tab", "session", "completed_at"} {
		value, err := jqIndex(e.raw, key)
		if err != nil {
			return DoneRow{}, err
		}
		fields = append(fields, jqJoinString(value))
	}

	turns, err := doneRowTurns(e.raw)
	if err != nil {
		return DoneRow{}, err
	}
	cost, err := doneRowCost(e.raw)
	if err != nil {
		return DoneRow{}, err
	}
	timeText, err := doneRowTime(e.raw)
	if err != nil {
		return DoneRow{}, err
	}
	markers, err := doneRowMarkers(e.raw)
	if err != nil {
		return DoneRow{}, err
	}
	fields = append(fields, turns, cost, timeText, markers)

	return splitDoneRow(strings.Join(fields, "\t")), nil
}

// splitDoneRow は現行版の `while IFS="$(printf '\t')" read -r ...` を再現する。
//
// タブは IFS の空白文字なので、連続するタブは 1 つの区切りに畳まれ、行頭と
// 行末のタブは無視される。そのため空フィールドがあると以降の値が 1 つずつ
// 手前へずれる。表示だけでなく restore に渡す引数もずれる既知バグである。
func splitDoneRow(line string) DoneRow {
	fields := strings.FieldsFunc(line, func(r rune) bool { return r == '\t' })
	at := func(i int) string {
		if i < len(fields) {
			return fields[i]
		}
		return ""
	}
	return DoneRow{
		Tab:         at(0),
		Session:     at(1),
		CompletedAt: at(2),
		Turns:       at(3),
		Cost:        at(4),
		Time:        at(5),
		Markers:     at(6),
	}
}

// doneRowTurns は `(.summary.total_turns // "-" | tostring)` 相当。
func doneRowTurns(raw json.RawMessage) (string, error) {
	summary, err := jqIndex(raw, "summary")
	if err != nil {
		return "", err
	}
	value, err := jqIndex(summary, "total_turns")
	if err != nil {
		return "", err
	}
	if !JSONTruthy(value) {
		return "-", nil
	}
	return jqToString(value), nil
}

// doneRowCost は `(.summary.total_cost_usd // null | if . != null then ... else "-" end)` 相当。
// 統計行と違い `. > 0` の条件が無いため、0 でも "$0.00" が出る。
func doneRowCost(raw json.RawMessage) (string, error) {
	summary, err := jqIndex(raw, "summary")
	if err != nil {
		return "", err
	}
	value, err := jqIndex(summary, "total_cost_usd")
	if err != nil {
		return "", err
	}
	if !JSONTruthy(value) {
		return "-", nil
	}
	var number float64
	if err := json.Unmarshal(value, &number); err != nil {
		return "", errJQFailed
	}
	return formatDoneCost(number), nil
}

// doneRowTime は `(.completed_at | .[11:16])` 相当。
func doneRowTime(raw json.RawMessage) (string, error) {
	completed, err := jqIndex(raw, "completed_at")
	if err != nil {
		return "", err
	}
	var value any
	if err := json.Unmarshal(completed, &value); err != nil {
		return "", errJQFailed
	}
	s, ok := value.(string)
	if !ok {
		if value == nil {
			// null[11:16] は null で、join では空文字になる。
			return "", nil
		}
		return "", errJQFailed
	}
	// jq の文字列スライスはコードポイント単位で、範囲外は切り詰められる。
	runes := []rune(s)
	start := min(doneTimeStart, len(runes))
	end := min(doneTimeEnd, len(runes))
	return string(runes[start:end]), nil
}

// doneRowMarkers は markers の真偽に応じた記号を連結する。
// jq の if は null と false だけを偽とするため、0 でも記号が出る。
func doneRowMarkers(raw json.RawMessage) (string, error) {
	markers, err := jqIndex(raw, "markers")
	if err != nil {
		return "", err
	}
	var b strings.Builder
	for _, m := range []struct {
		key    string
		symbol string
	}{
		{"merged", markerMerged},
		{"slack", markerSlack},
		{"doc", markerDoc},
	} {
		value, err := jqIndex(markers, m.key)
		if err != nil {
			return "", err
		}
		if JSONTruthy(value) {
			b.WriteString(m.symbol)
		}
	}
	return b.String(), nil
}

// jqIndex は `.key` 相当の参照を行う。
//
// null(および存在しないキー)を引くと null になり、オブジェクト以外を引くと
// jq はエラーになる。後者は errJQFailed として全体を捨てる合図にする。
func jqIndex(raw json.RawMessage, key string) (json.RawMessage, error) {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null")) {
		return json.RawMessage("null"), nil
	}
	var object map[string]json.RawMessage
	if err := json.Unmarshal(trimmed, &object); err != nil {
		return nil, errJQFailed
	}
	value, ok := object[key]
	if !ok {
		return json.RawMessage("null"), nil
	}
	return value, nil
}

// jqJoinString は join("\t") に渡された値の文字列表現を返す。
// join は null を空文字として扱う。
func jqJoinString(raw json.RawMessage) string {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null")) {
		return ""
	}
	return jqToString(raw)
}

// jqToString は tostring 相当の文字列化を行う。
func jqToString(raw json.RawMessage) string {
	var value any
	if err := json.Unmarshal(raw, &value); err != nil {
		return ""
	}
	switch v := value.(type) {
	case nil:
		return jqNull
	case string:
		return v
	case bool:
		return strconv.FormatBool(v)
	case float64:
		return formatJQNumber(v)
	default:
		compact := &bytes.Buffer{}
		if err := json.Compact(compact, raw); err != nil {
			return ""
		}
		return compact.String()
	}
}

// formatDoneCost は現行版の jq 式が作る金額表記を返す。
//
//	(. * 100 | round | . / 100 | tostring
//	 | if test("\\.") then . else . + ".00" end
//	 | if test("\\.[0-9]$") then . + "0" else . end
//	 | "$" + .)
//
// 100 倍して四捨五入した整数を 100 で割り、小数第 2 位まで埋める処理なので、
// 結果は小数 2 桁固定の表記に一致する。
func formatDoneCost(v float64) string {
	return "$" + strconv.FormatFloat(math.Round(v*100)/100, 'f', 2, 64)
}

// formatDoneTotalCost は統計行の合計金額を返す。
// 行と違い `if . > 0` の条件が付くため、0 以下はすべて "$0.00" になる。
func formatDoneTotalCost(v float64) string {
	if v > 0 {
		return formatDoneCost(v)
	}
	return "$0.00"
}

// RenderDone は Done ペインの 1 画面分を組み立てる。
func RenderDone(view DoneView) string {
	var b strings.Builder

	b.WriteString(ansiBold + "  Done Tasks" + ansiReset + "\n")
	b.WriteString(divider(dividerWidth))
	b.WriteString("\n")

	if view.Count == 0 {
		b.WriteString("  " + ansiDim + "No tasks completed yet" + ansiReset + "\n")
		return b.String()
	}

	b.WriteString("  " + ansiYellow + ansiBold + strconv.Itoa(view.Count) + ansiReset +
		" tasks  " + ansiDim + view.Turns + " turns / " + view.Calls + " calls / " + view.Cost + ansiReset + "\n")
	b.WriteString("\n")

	for i, row := range view.Rows {
		b.WriteString("  " + ansiYellow + "[" + strconv.Itoa(i+1) + "]" + ansiReset + " " +
			ansiGreen + "⚡" + ansiReset + " " +
			padRightBytes(row.Tab, doneTabWidth) + " " +
			padLeftBytes(row.Turns, doneTurnsWidth) + " t  " +
			padLeftBytes(row.Cost, doneCostWidth) + "  " +
			ansiDim + "[" + row.Time + "]" + ansiReset)
		if row.Markers != "" {
			b.WriteString(" " + row.Markers)
		}
		b.WriteString("\n")
	}

	b.WriteString("\n")
	b.WriteString(divider(dividerWidth))
	b.WriteString("  " + ansiDim + "r+[num]: restore to dashboard" + ansiReset + "\n")
	return b.String()
}

// padRightBytes / padLeftBytes は bash の printf と同じくバイト幅で詰める。
// Go の fmt はルーン数で詰めるため、日本語タブ名で桁がずれてしまう。
func padRightBytes(s string, width int) string {
	if len(s) >= width {
		return s
	}
	return s + strings.Repeat(" ", width-len(s))
}

func padLeftBytes(s string, width int) string {
	if len(s) >= width {
		return s
	}
	return strings.Repeat(" ", width-len(s)) + s
}
