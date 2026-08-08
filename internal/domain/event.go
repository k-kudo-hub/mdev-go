package domain

// ShouldOverwritePending は、既存の pending(event が existing)を
// next イベントで上書きしてよいかを返す。
//
// 現行 Shell 版(pending-notify.sh)の規則を移植したものである。
//
//   - Notification は無条件に上書きする。応答待ちは最優先で表示する必要があり、
//     Waiting(外部の返答待ちとして退避した状態)も潰す
//   - Stop は Notification と Waiting を上書きしない。ターン終了通知が
//     「未応答の許可要求」や「退避中のタスク」を done に見せてしまうため
//   - pending が存在しない場合と JSON が壊れている場合は existing が空文字になり、
//     どのイベントでも上書きされる(現行版で jq が空文字を返す挙動と同じ)
func ShouldOverwritePending(existing, next string) bool {
	if next != EventStop {
		return true
	}
	return existing != EventNotification && existing != EventWaiting
}
