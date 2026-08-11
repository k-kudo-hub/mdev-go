package zellij

import (
	"encoding/json"
	"regexp"
	"strings"

	"github.com/k-kudo-hub/mdev-go/internal/app"
)

var (
	_ app.PaneLister   = (*TabController)(nil)
	_ app.ScreenDumper = (*TabController)(nil)
)

// taskAgentMarker はエージェントペインの目印である。
// create_task が `env TASK_AGENT=<name> ...` としてコマンド行へ入れる。
const taskAgentMarker = "TASK_AGENT="

// taskAgentPattern はコマンド行からエージェント名を取り出す。
// 現行版の jq `capture("TASK_AGENT=(?<a>[^ ]+)")` と同じく空白の手前までを採る。
var taskAgentPattern = regexp.MustCompile(taskAgentMarker + `([^ ]+)`)

// paneJSON は `zellij action list-panes -t -c -j` の 1 要素のうち、
// スクリーン検出が使うフィールドである。
type paneJSON struct {
	// ID は数値。現行版は `.id | tostring` で文字列にしている。
	ID json.Number `json:"id"`
	// IsPlugin はプラグインペイン(タブバー・ステータスバー)かどうか。
	IsPlugin bool `json:"is_plugin"`
	// TabName は所属するタブ名。
	TabName string `json:"tab_name"`
	// TerminalCommand はペインで動いているコマンド行。プラグインでは無い。
	TerminalCommand string `json:"terminal_command"`
}

// ListAgentPanes は `TASK_AGENT=` を持つ端末ペインを返す。
//
// 現行 screen-detect-lib.sh の screen_detect_tick と同じ絞り込みである。
//
//	.[] | select(.is_plugin == false)
//	    | select(.terminal_command != null)
//	    | select(.terminal_command | test("TASK_AGENT="))
//
// レジストリではなくコマンド行の目印で見分けるので、まだエントリの無いタブでも
// 最初のターンから走査できる(承認待ちで始まるタスクを取りこぼさない)。
//
// コマンドが失敗した場合や出力が JSON として読めない場合は空を返す。
// 劣化した zellij サーバでは上限で打ち切られて空になり、その回は何も
// 検出しなかった扱いになる(現行版の `2>/dev/null` と同じ)。
func (c *TabController) ListAgentPanes() []app.AgentPane {
	out, err := c.output(commandTimeout, binaryName, "action", "list-panes", "-t", "-c", "-j")
	if err != nil || out == "" {
		return nil
	}

	var raw []paneJSON
	if err := json.Unmarshal([]byte(out), &raw); err != nil {
		return nil
	}

	panes := make([]app.AgentPane, 0, len(raw))
	for _, pane := range raw {
		if pane.IsPlugin || !strings.Contains(pane.TerminalCommand, taskAgentMarker) {
			continue
		}
		match := taskAgentPattern.FindStringSubmatch(pane.TerminalCommand)
		if match == nil {
			continue
		}
		// タブ名か id が空のペインは現行版も飛ばす(TSV の空フィールドが
		// `[[ -n "$tab" && -n "$pane_id" ]]` で弾かれる)。
		if pane.TabName == "" || pane.ID.String() == "" {
			continue
		}
		panes = append(panes, app.AgentPane{
			Tab:   pane.TabName,
			ID:    pane.ID.String(),
			Agent: match[1],
		})
	}
	if len(panes) == 0 {
		return nil
	}
	return panes
}

// DumpScreen はペインの見えている内容を返す。
//
// 失敗した場合は空文字を返す。dump-screen はペインの数だけ毎周期叩くため、
// ハングしたときの蓄積が list-panes より速い。同じ上限で打ち切り、空なら
// 呼び出し側がそのペインを飛ばす(現行版の `|| true` と同じ扱い)。
func (c *TabController) DumpScreen(paneID string) string {
	// 失敗は空文字に潰す。呼び出し側はそのペインを飛ばす。
	out, _ := c.output(commandTimeout, binaryName, "action", "dump-screen", "-p", "terminal_"+paneID)
	return out
}
