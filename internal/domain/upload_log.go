package domain

import (
	"bytes"
	"encoding/json"
	"regexp"
	"strings"
)

// 作業ログの markdown で使う既定値と印。
const (
	// uploadUnknown は tab / session / model が取れなかったときの値。
	uploadUnknown = "unknown"
	// uploadZero は数値フィールドが取れなかったときの値。
	uploadZero = "0"
	// uploadMarkerOn / uploadMarkerOff は markers の表示。
	uploadMarkerOn  = "✅"
	uploadMarkerOff = "-"
	// uploadDefaultTaskName は taskname が全部落ちたときのファイル名。
	uploadDefaultTaskName = "task"
)

// logPathUnsafePattern はファイル名に使えない文字の並びである。
// 現行 upload-log.sh の `sed -E 's/[^A-Za-z0-9._-]+/-/g'` に対応する。
var logPathUnsafePattern = regexp.MustCompile(`[^A-Za-z0-9._-]+`)

// logPathTrimPattern は整形後の先頭・末尾に残るハイフンである。
var logPathTrimPattern = regexp.MustCompile(`^-+|-+$`)

// BuildLogPath は作業ログの置き場所(リポジトリ内の相対パス)を組み立てる。
//
//	<base_dir>/YYYY/MM/DD/HHMMSS_<taskname>.md
//
// 日付は completedAt の **固定オフセット**で切り出す(現行版の
// `${completed_at:0:4}` など)。パースはしないので、想定外の形の文字列が来ても
// エラーにはならず、切り出せた分だけが入った短いパスになる。
//
// taskname はファイル名に使える文字だけに畳み、前後のハイフンを落とす。
// 全部落ちて空になった場合は "task" にする(ディレクトリ名で終わるパスを
// 作らないため)。
func BuildLogPath(baseDir, completedAt, taskname string) string {
	year := substring(completedAt, 0, 4)
	month := substring(completedAt, 5, 2)
	day := substring(completedAt, 8, 2)
	stamp := substring(completedAt, 11, 2) + substring(completedAt, 14, 2) + substring(completedAt, 17, 2)

	safe := logPathTrimPattern.ReplaceAllString(logPathUnsafePattern.ReplaceAllString(taskname, "-"), "")
	if safe == "" {
		safe = uploadDefaultTaskName
	}
	return baseDir + "/" + year + "/" + month + "/" + day + "/" + stamp + "_" + safe + ".md"
}

// substring は bash の `${s:offset:length}` と同じ切り出しを行う。
// 範囲を外れた場合はエラーではなく、取れた分だけ(または空)を返す。
func substring(s string, offset, length int) string {
	if offset >= len(s) {
		return ""
	}
	end := offset + length
	if end > len(s) {
		end = len(s)
	}
	return s[offset:end]
}

// uploadRecord は作業ログの markdown に出す 11 個の値である。
//
// 現行版は jq 1 パスで @tsv にしたものを `IFS=$'\t' read` で受けている。
// すべて文字列なのは、jq が tostring / join を通した後の表記をそのまま
// 使うためである(数値の書き方は入力の字面が保たれる)。
type uploadRecord struct {
	Tab         string
	Session     string
	CompletedAt string
	Model       string
	Turns       string
	Calls       string
	Cost        string
	Tools       string
	Merged      string
	Slack       string
	Doc         string
}

// parseUploadRecord は daily レコードから markdown 用の 11 値を取り出す。
//
// 現行版の jq が落ちる入力(レコードや .summary / .markers がオブジェクトでない)
// では 11 値すべてが空文字になる。jq が何も出力せず、`read` が空のまま返るため
// である。ここでもその結果に揃える。
func parseUploadRecord(raw []byte) uploadRecord {
	record, isObject := jsonObject(raw)
	if !isObject {
		return uploadRecord{}
	}
	summary, isObject := jsonObject(record["summary"])
	if !isObject {
		return uploadRecord{}
	}
	markers, isObject := jsonObject(record["markers"])
	if !isObject {
		return uploadRecord{}
	}
	tools, ok := joinStrings(summary["tools_used"], ", ")
	if !ok {
		return uploadRecord{}
	}

	return uploadRecord{
		Tab:         jqFallbackString(record["tab"], uploadUnknown),
		Session:     jqFallbackString(record["session"], uploadUnknown),
		CompletedAt: jqFallbackString(record["completed_at"], ""),
		Model:       jqFallbackString(summary["model"], uploadUnknown),
		Turns:       jqFallbackString(summary["total_turns"], uploadZero),
		Calls:       jqFallbackString(summary["total_tool_calls"], uploadZero),
		Cost:        jqFallbackString(summary["total_cost_usd"], uploadZero),
		Tools:       tools,
		Merged:      uploadMarker(markers["merged"]),
		Slack:       uploadMarker(markers["slack"]),
		Doc:         uploadMarker(markers["doc"]),
	}
}

// jqFallbackString は `(<式> // <既定>) | tostring` を再現する。
func jqFallbackString(raw json.RawMessage, fallback string) string {
	if !jsonTruthy(raw) {
		return fallback
	}
	return jsonToString(raw)
}

// uploadMarker は `if <式> then "✅" else "-" end` を再現する。
func uploadMarker(raw json.RawMessage) string {
	if jsonTruthy(raw) {
		return uploadMarkerOn
	}
	return uploadMarkerOff
}

// joinStrings は `(<式> // []) | join(sep)` を再現する。
//
// null / false / 欠落は空配列として扱う。配列の要素は jq の join と同じく、
// null が空文字、文字列がそのまま、それ以外が tojson の表記になる。
// 配列でない値は jq の join がエラーになるため ok=false を返す。
func joinStrings(raw json.RawMessage, sep string) (string, bool) {
	if !jsonTruthy(raw) {
		return "", true
	}
	if !isJSONArray(raw) {
		return "", false
	}
	elements := jsonElements(raw)
	parts := make([]string, 0, len(elements))
	for _, element := range elements {
		parts = append(parts, jqJoinElement(element))
	}
	return strings.Join(parts, sep), true
}

// isJSONArray は raw が JSON の配列かどうかを返す。
func isJSONArray(raw json.RawMessage) bool {
	trimmed := strings.TrimSpace(string(raw))
	return len(trimmed) > 0 && trimmed[0] == '['
}

// RecordCompletedAt はレコードの completed_at を返す(`.completed_at // empty`)。
// 値が無い・null・false の場合は空文字になり、呼び出し側が現在時刻へ落とす。
func RecordCompletedAt(raw []byte) string {
	record, isObject := jsonObject(raw)
	if !isObject {
		return ""
	}
	if !jsonTruthy(record["completed_at"]) {
		return ""
	}
	return jsonToString(record["completed_at"])
}

// BuildMarkdown は作業ログの markdown を組み立てる。
//
// 戻り値は現行版の `MD=$(build_markdown ...)` が受け取る文字列と同じで、
// 末尾の改行は落としてある。中身は最後に FilterSecrets を通す。要約を書くのは
// モデルであり、会話に出てきた秘密をそのまま書き戻すことがあるためで、
// 要約へ渡す前のマスクと合わせて **2 回** かける必要がある。
//
// daily レコードの `.message`(作業の生の報告文)は **出さない**。
// アップロードするのはサマリと会話要約だけ、というのが現行版の方針である。
//
// 現行版との意図的な差異: 現行版は 11 値を @tsv にして
// `IFS=$'\t' read` で受けるが、bash はタブを IFS の空白文字として扱うため
// **空のフィールドがあると連続タブが 1 つに畳まれ、以降の値が 1 つずつ
// ずれる**(completed_at が空、または tools_used が空配列のときに起きる)。
// ここではずらさず、意図どおりの値を出す。
func BuildMarkdown(raw []byte, summaryText string) string {
	r := parseUploadRecord(raw)
	body := "# " + r.Tab + "\n" +
		"\n" +
		"- **Session**: " + r.Session + "\n" +
		"- **Completed**: " + r.CompletedAt + "\n" +
		"- **Model**: " + r.Model + "\n" +
		"\n" +
		"## サマリ\n" +
		"\n" +
		"| 項目 | 値 |\n" +
		"|---|---|\n" +
		"| ターン数 | " + r.Turns + " |\n" +
		"| ツール呼び出し | " + r.Calls + " |\n" +
		"| コスト(USD) | " + r.Cost + " |\n" +
		"| 使用ツール | " + r.Tools + " |\n" +
		"| マージ | " + r.Merged + " |\n" +
		"| Slack | " + r.Slack + " |\n" +
		"| ドキュメント | " + r.Doc + " |\n" +
		"\n" +
		"## 会話要約\n" +
		"\n" +
		summaryText + "\n"
	return strings.TrimRight(FilterSecrets(body), "\n")
}

// SelectUploadRecord は daily ファイル群からタブに一致するレコードを 1 件選ぶ。
//
// files は **ファイル名の昇順**(daily は YYYY-MM-DD.jsonl なので日付順)で
// 渡す。各ファイルの中では最後に一致した行、ファイル間では後ろのファイルが
// 勝つ。現行 upload-log.sh の
//
//	for df in "$DAILY_DIR"/*.jsonl; do
//	    r=$(jq -c 'select(.tab == $tab)' "$df" | tail -1)
//	    [ -n "$r" ] && RECORD="$r"
//	done
//
// に対応する。当日ぶんだけを見ないのは、完了と削除が別の日にまたがった場合や、
// record-output との間で日付が変わった場合でも記録を見つけるためである。
//
// 戻り値は一致した行そのもの(JSON)である。1 件も無ければ ok=false になり、
// 呼び出し側は PlaceholderUploadRecord へ落ちる。
func SelectUploadRecord(files [][]byte, tab string) ([]byte, bool) {
	var selected []byte
	for _, data := range files {
		if match, ok := lastRecordForTab(data, tab); ok {
			selected = match
		}
	}
	return selected, selected != nil
}

// lastRecordForTab は 1 ファイルの中でタブ名が一致する最後の行を返す。
//
// 壊れた行に当たったらそこで走査をやめ、それまでに見つけた分を返す。
// 現行版の jq は解釈できない行でその場で終了し、直前までの出力だけが
// tail へ渡るためである。
func lastRecordForTab(data []byte, tab string) ([]byte, bool) {
	var selected []byte
	dec := json.NewDecoder(bytes.NewReader(data))
	for {
		var raw json.RawMessage
		if err := dec.Decode(&raw); err != nil {
			break
		}
		record, isObject := jsonObject(raw)
		if !isObject {
			break
		}
		if name, isString := jsonString(record["tab"]); isString && name == tab {
			selected = raw
		}
	}
	return selected, selected != nil
}

// PlaceholderUploadRecord は daily に記録が 1 件も無かったときのレコードを作る。
//
// 現行版の `jq -n '{tab:$tab, session:$session, completed_at:"", summary:null,
// markers:{}}'` と同じ内容である。統計は空だが、タブ名とセッション名だけは
// 分かるので、そのぶんはログに残す。
func PlaceholderUploadRecord(tab, session string) []byte {
	record := struct {
		Tab         string          `json:"tab"`
		Session     string          `json:"session"`
		CompletedAt string          `json:"completed_at"`
		Summary     json.RawMessage `json:"summary"`
		Markers     struct{}        `json:"markers"`
	}{Tab: tab, Session: session, Summary: json.RawMessage("null")}
	// フィールドはすべて marshal 可能なので失敗しない。
	b, _ := json.Marshal(record) //nolint:errchkjson // 失敗しない構造体
	return b
}
