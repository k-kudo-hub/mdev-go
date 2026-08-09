package domain

import (
	"strconv"
	"strings"
)

// RenderWaiting は Waiting ペインの 1 画面分を組み立てる。
//
// 現行 waiting-loop.sh の render() が出す文字列と一致する。Dashboard と違い
// 行番号は振らず(キー入力を受け付けないため)、■ は常に黄色である。
//
// items は WaitingItems の戻り値をそのまま渡す。
func RenderWaiting(items []PendingView) string {
	var b strings.Builder

	b.WriteString(ansiBold + "  Waiting" + ansiReset + " " + ansiDim + "[external]" + ansiReset + "\n")
	b.WriteString(divider(dividerWidth))
	b.WriteString("\n")

	for _, item := range items {
		b.WriteString("  " + ansiYellow + "■" + ansiReset + " " +
			ansiBold + item.Tab + ansiReset + " " +
			ansiDim + "[" + item.Time + "]" + ansiReset + "\n")
		b.WriteString("      " + TruncateBytes(item.Message, PaneMessageLimit) + "\n")
		b.WriteString("\n")
	}

	if len(items) == 0 {
		b.WriteString("  " + ansiDim + "No waiting tasks" + ansiReset + "\n")
		return b.String()
	}

	b.WriteString(divider(dividerWidth))
	b.WriteString("  " + ansiBold + "Waiting: " + strconv.Itoa(len(items)) + ansiReset + "\n")
	return b.String()
}
