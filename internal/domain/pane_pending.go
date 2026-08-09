package domain

import (
	"bytes"
	"encoding/json"
	"strconv"
)

// PaneMessageLimit は Dashboard / Waiting が message を切り詰めるバイト数。
// 現行版の `jq -r '.message' | head -c 60` に対応する(文字数ではない)。
const PaneMessageLimit = 60

// jqNull は jq -r が null を出力するときの文字列。
//
// pending のキーが欠けていた場合、現行版は `jq -r '.message'` の出力である
// "null" をそのまま画面に出す。壊れた JSON では jq 自体が失敗して空文字になる
// ため、両者は区別しなければならない(空文字はタブ名の一致判定から外れる)。
const jqNull = "null"

// PendingView は pending ファイル 1 件を、現行版の jq が読み出した文字列の
// 組として表す。
//
// 現行の各ペインは 1 ファイルにつき jq を数回呼び、失敗はすべて空文字へ潰す。
// その結果だけが表示と絞り込みに使われるため、この型も「読み出された文字列」
// だけを持ち、型付きの値は持たない。
type PendingView struct {
	// Name はファイル名。glob の並び順(ファイル名の昇順)を決める鍵になる。
	Name string

	Tab     string
	Event   string
	Message string
	Time    string
	Agent   string
}

// ParsePendingView は pending ファイルの中身を現行版の jq と同じ規則で読む。
//
// JSON として解釈できない場合と、トップレベルがオブジェクトでない場合は
// 全フィールドを空文字にする(jq が非ゼロ終了し、コマンド置換が空になる)。
// オブジェクトなら、欠けたキーと null はいずれも "null" になり、文字列以外の
// スカラーは JSON としての表現がそのまま入る(jq -r の挙動)。
//
// 値が配列・オブジェクトの場合だけは現行版と一致しない。jq -r は複数行の
// 整形済み JSON を出すのに対し、ここでは 1 行の compact JSON を返す。これらの
// キーは hook が必ず文字列として書くため到達しない経路であり、表示互換より
// 単純さを取っている。
func ParsePendingView(name string, data []byte) PendingView {
	view := PendingView{Name: name}

	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return view
	}

	view.Tab = jqRawString(raw["tab"])
	view.Event = jqRawString(raw["event"])
	view.Message = jqRawString(raw["message"])
	view.Time = jqRawString(raw["time"])
	view.Agent = jqRawString(raw["agent"])
	return view
}

// jqRawString は `jq -r` が 1 つの値に対して出力する文字列を返す。
//
// キーが無い場合(raw が空)と null はどちらも "null" になる。文字列は
// 引用符を外した中身、それ以外は JSON の表現そのままである。
func jqRawString(raw json.RawMessage) string {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 {
		return jqNull
	}
	var value any
	if err := json.Unmarshal(trimmed, &value); err != nil {
		return jqNull
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
		// 配列・オブジェクトは jq が compact JSON を出す。
		compact := &bytes.Buffer{}
		if err := json.Compact(compact, trimmed); err != nil {
			return jqNull
		}
		return compact.String()
	}
}

// formatJQNumber は jq が数値を出力するときの表記に寄せる。
// 整数は小数点なし、それ以外は必要最小限の桁で出す。
func formatJQNumber(v float64) string {
	if v == float64(int64(v)) {
		return strconv.FormatInt(int64(v), 10)
	}
	return strconv.FormatFloat(v, 'g', -1, 64)
}

// DashboardItems は Dashboard に並べる pending を表示順で返す。
//
// 外側のループが zellij のタブ順、内側が pending のファイル名順である
// (現行版の `for tab_name in $tab_order` × `for f in "$PENDING_DIR"/*.json`)。
// タブ一覧に無いタブの pending は表示されず、Waiting は Waiting ペインの
// 担当なのでここでは飛ばす。
//
// entries はファイル名の昇順で渡すこと。タブ一覧に同じ名前が 2 度現れれば
// 同じ pending が 2 度並ぶ(現行版と同じで、畳み込みはしない)。
func DashboardItems(tabOrder []string, entries []PendingView) []PendingView {
	items := make([]PendingView, 0, len(entries))
	for _, tab := range tabOrder {
		for _, entry := range entries {
			if entry.Tab != tab || entry.Event == EventWaiting {
				continue
			}
			items = append(items, entry)
		}
	}
	return items
}

// WaitingItems は Waiting ペインに並べる pending をファイル名順で返す。
//
// Dashboard と違い zellij のタブ一覧は参照しない。閉じたタブの Waiting も
// 表示され続けるのが現行版の挙動である。
func WaitingItems(entries []PendingView) []PendingView {
	items := make([]PendingView, 0, len(entries))
	for _, entry := range entries {
		if entry.Event != EventWaiting {
			continue
		}
		items = append(items, entry)
	}
	return items
}

// TruncateBytes は s を先頭から limit バイトに切り詰める。
//
// 現行版の `head -c 60` に合わせてバイト単位で切るため、マルチバイト文字の
// 途中で切れて不正なバイト列になることがある。表示互換のためにその結果を
// そのまま返す。
func TruncateBytes(s string, limit int) string {
	if len(s) <= limit {
		return s
	}
	return s[:limit]
}
