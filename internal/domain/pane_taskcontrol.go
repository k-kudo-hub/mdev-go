package domain

// task-control の操作バーに出す文言。
//
// 現行 task-control.sh の render_bar をそのまま写している。行頭の 2 スペースも
// 現行の文字列に含まれるもので、他のペインの字下げとは別物である。
const (
	taskControlKeysNormal  = "  m: Main  |  w: Waiting  |  dd: Delete tab"
	taskControlKeysWaiting = "  |  m: Main  |  w: Resume  |  dd: Delete tab"
	taskControlWaitingMark = "  ● WAITING"
)

// RenderTaskControlBar はタスクタブ下部の操作バー 1 行を返す(末尾に改行を含む)。
//
// waiting が真のときは黄色の WAITING 表示が前に付き、w キーの説明が
// "Resume" に変わる。タスクが外部の返答待ちであることをタスクタブ側からも
// 見えるようにするためである。
func RenderTaskControlBar(waiting bool) string {
	if waiting {
		return ansiYellow + ansiBold + taskControlWaitingMark + ansiReset +
			ansiDim + taskControlKeysWaiting + ansiReset + "\n"
	}
	return ansiDim + taskControlKeysNormal + ansiReset + "\n"
}

// TaskControlWaiting は pending の event が Waiting かどうかを返す。
//
// 現行 task-control.sh の `[[ "$(current_event)" == "Waiting" ]]` に対応する。
// pending が無い場合は空文字が渡り、偽になる。
func TaskControlWaiting(event string) bool { return event == EventWaiting }

// PendingEvent は pending の中身から event を取り出す。
//
// 現行 task-control.sh の `current_event`(`jq -r '.event'`)に対応する。
// 欠けたキーと null はどちらも "null" になるが、いずれも "Waiting" とは
// 一致しないので通常表示に落ちる。
func PendingEvent(raw []byte) string {
	return ParsePendingView("", raw).Event
}
