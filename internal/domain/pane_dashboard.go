package domain

import (
	"strconv"
	"strings"
)

// DashboardInput は Dashboard ペインの描画に必要な値である。
type DashboardInput struct {
	// Session は見出しに出すセッション名(ZELLIJ_SESSION_NAME)。
	Session string
	// Items は表示順に並んだ pending。DashboardItems の戻り値をそのまま渡す。
	Items []PendingView
}

// RenderDashboard は Dashboard ペインの 1 画面分を組み立てる。
//
// 現行 dashboard-loop.sh の render() が echo で出力する文字列と 1 バイトも
// 違わない結果を返す(末尾の改行まで含む)。
//
// 現行版との差異が 1 つある。現行は `echo -e "      $msg"` で message を出すため、
// message に含まれる `\n` や `\t` がバックスラッシュ列として解釈される。ここでは
// 解釈せずそのまま出す。hook が書く message は transcript の 1 行であり、
// バックスラッシュ列を意図的に含める経路が無いため、素直な方を選んでいる。
func RenderDashboard(in DashboardInput) string {
	var b strings.Builder

	b.WriteString(ansiBold + "  Current Tasks" + ansiReset + " " + ansiDim + "[" + in.Session + "]" + ansiReset + "\n")
	b.WriteString(divider(dividerWidth))
	b.WriteString("\n")

	for i, item := range in.Items {
		number := ansiYellow + "[" + strconv.Itoa(i+1) + "]" + ansiReset
		mark := ansiRed + "■" + ansiReset
		suffix := ""
		if item.Event == EventStop {
			mark = ansiGreen + "■" + ansiReset
			suffix = " done"
		}
		b.WriteString("  " + number + " " + mark + " " +
			ansiBold + item.Tab + ansiReset + " " +
			ansiDim + "[" + item.Time + "]" + ansiReset + suffix + "\n")
		b.WriteString("      " + TruncateBytes(item.Message, PaneMessageLimit) + "\n")
		b.WriteString("\n")
	}

	if len(in.Items) == 0 {
		b.WriteString("  " + ansiGreen + "All tasks running" + ansiReset + "\n")
		b.WriteString("\n")
		b.WriteString(divider(dividerWidth))
		return b.String()
	}

	b.WriteString(divider(dividerWidth))
	b.WriteString("  " + ansiBold + "Pending: " + strconv.Itoa(len(in.Items)) + ansiReset +
		"  " + ansiDim + "[num]: jump / d+[num]: delete" + ansiReset + "\n")
	b.WriteString(divider(dividerWidth))
	return b.String()
}
