package domain

import "regexp"

// mergePullRequestTool は PR をマージする MCP ツールの名前。
const mergePullRequestTool = "mcp__github__merge_pull_request"

// onigurumaSpace は jq(Oniguruma)の `\s` と同じ文字集合である。
//
// Oniguruma の `\s` は Unicode の White_Space プロパティ全体を指すのに対し、
// Go の RE2 の `\s` は `[\t\n\f\r ]` しかない(垂直タブすら含まない)。
// RE2 は `\p{White_Space}` を解釈できないため、実測したコードポイント
// (evidence の 6 節)を明示的な文字クラスへ展開して一致させる。
const onigurumaSpace = `[\t\n\v\f\r \x{0085}\x{00A0}\x{1680}\x{2000}-\x{200A}\x{2028}\x{2029}\x{202F}\x{205F}\x{3000}]`

var (
	// mergeCommandPattern は PR をマージするシェルコマンドを見つける。
	// 現行 record-output.sh:176 の `test("gh\\s+pr\\s+merge")` に対応し、
	// アンカーが無いため部分一致である(`echo gh pr merge` も真になる)。
	mergeCommandPattern = regexp.MustCompile(`gh` + onigurumaSpace + `+pr` + onigurumaSpace + `+merge`)

	// slackToolPattern は Slack の MCP ツールを見つける(前方一致)。
	slackToolPattern = regexp.MustCompile(`^mcp__slack`)

	// docFilePattern はドキュメントとみなす拡張子を見つける。
	//
	// 末尾の `\n?$` は Oniguruma の `$` に合わせるためである。Oniguruma の `$` は
	// 文字列末尾の改行 1 個の手前にも一致するが、Go の `$`(非マルチライン)は
	// 文字列の末尾だけに一致する。
	docFilePattern = regexp.MustCompile(`\.(md|mdx|txt|rst|adoc)\n?$`)
)

// ClaudeMarkers は claude のツール呼び出しから markers を判定する
// (現行 record-output.sh:171-177)。
//
// 判定に使うツールは種類ごとに決まっている。merged は MCP のマージツールか
// Bash の `gh pr merge`、slack は `mcp__slack` で始まるツール、doc は
// Write / Edit が書いたファイルの拡張子で決まる。
func ClaudeMarkers(tools []ClaudeToolUse) DailyMarkers {
	var markers DailyMarkers
	for _, tool := range tools {
		switch {
		case tool.Name == mergePullRequestTool:
			markers.Merged = true
		case tool.Name == toolBash && mergeCommandPattern.MatchString(tool.Command):
			markers.Merged = true
		}
		if slackToolPattern.MatchString(tool.Name) {
			markers.Slack = true
		}
		if (tool.Name == toolWrite || tool.Name == toolEdit) && docFilePattern.MatchString(tool.FilePath) {
			markers.Doc = true
		}
	}
	return markers
}
