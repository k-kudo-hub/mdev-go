package domain

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"regexp"
	"strings"
)

// codex の rollout(JSONL)の行 type。
const (
	codexLineEventMsg     = "event_msg"
	codexLineResponseItem = "response_item"
	codexLineTurnContext  = "turn_context"
)

// event_msg の payload.type。
//
// codex の rollout には書き方が 2 通りあり、codex 自身のバージョンでは見分け
// られない(同じ cli_version 0.147.0 が両方を書く)。旧い書き方(v1)は会話を
// user_message イベントで残し、新しい書き方(v2)は会話もツール実行も
// item_completed の item として残す。どちらの形でも読めるように両方を集め、
// 採用する側は内容から決める。
const (
	codexEventUserMessage   = "user_message"
	codexEventTokenCount    = "token_count"
	codexEventItemCompleted = "item_completed"
)

// item_completed が運ぶ item.type のうち、ターンとツール呼び出しに当たるもの。
const (
	codexItemUserMessage      = "UserMessage"
	codexItemCommandExecution = "CommandExecution"
	codexItemMcpToolCall      = "McpToolCall"
)

// codexToolItemTypes はツール呼び出しに当たる item.type である。
// Reasoning やメッセージの item はツール呼び出しではないので数えない。
var codexToolItemTypes = map[string]struct{}{
	codexItemCommandExecution: {},
	codexItemMcpToolCall:      {},
	"FileChange":              {},
	"Extension":               {},
}

// codexMergeToolSubstring は MCP 経由の PR マージを見分ける。
//
// 現行版(codex-rollout-lib.sh の codex_merged)は `test("merge_pull_request")`
// で照合する。メタ文字を含まない正規表現なので部分一致そのものである。
// claude 側(markers.go)がツール名の完全一致で見るのに対し、codex は
// mcp__github__merge_pull_request(呼び出しビュー)と merge_pull_request
// (item ビューの .tool)の両方を 1 つの式で拾うため部分一致になっている。
const codexMergeToolSubstring = "merge_pull_request"

// codexToolCallPattern はツール呼び出しの response_item を見分ける。
//
// codex は custom_tool_call / function_call / local_shell_call など複数の
// 呼び出し種別を持ち、結果は `_call_output` で終わる別の response_item に入る。
// 現行 record-output.sh:72 はこの命名規則を使って呼び出しだけを数えている。
// 末尾の `\n?$` は Oniguruma の `$` に合わせたものである(markers.go を参照)。
var codexToolCallPattern = regexp.MustCompile(`_call\n?$`)

// CodexToolCall は rollout 中のツール呼び出し 1 件である。
type CodexToolCall struct {
	// Name は tools_used に出す表示名で、現行版の codex_tool_name
	// (`.name // .tool // .kind // .type`)に対応する。呼び出しビューは .name を
	// 持ち、item ビューは McpToolCall が .tool(実データでは "js")、Extension が
	// .kind("web.search")を持つ。どれも無ければ item の型が名前になる。
	Name string

	// MergeCommand と MergeTool は merged 判定の走査対象である。走査するのは
	// 「何を起動したか」だけで、コマンドの出力や引数の本文は見ない。
	// CommandExecution item は stdout / aggregated_output も持つため、
	// gh pr merge と書かれたファイルを cat しただけで誤検知する実データがあり、
	// MCP の引数本文も Slack 投稿が gh pr merge を引用しただけで誤検知する。

	// MergeCommand は `gh pr merge` を探す対象。呼び出しビューは現行版の
	// `((.input // .arguments // "") | tostring)`、item ビューは
	// CommandExecution が実行したコマンド(codex_command_text)である。
	// ツールを起動しない item(FileChange / Extension)では空になる。
	MergeCommand string
	// MergeTool は `merge_pull_request` を探す対象。呼び出しビューは `.name`、
	// item ビューは McpToolCall の `.tool` で、それ以外では空になる。
	MergeTool string
}

// CodexTranscript は codex の rollout から集計した値である。
// 現行 record-output.sh:71-90 の jq 1 パスに対応する。
//
// claude と違い speed の概念が無く、summary では常に "standard" になる。
// トークンは各行の加算ではなく「最後の token_count に載っている累計」を使う。
type CodexTranscript struct {
	TotalTurns int
	// Tools は集計に採用したビューである。呼び出しビュー(response_item の
	// `_call`)に 1 件でもあればそちらを、無ければ item ビューを採る。
	// 同じ活動の別表現なので、合算すると二重計上になる。
	Tools []CodexToolCall
	// ItemTools は item ビューのツールで、採用されなかった場合も保持する。
	// merged 判定だけは両ビューを走査するためである(真偽値なので二重には数えない)。
	// 呼び出しビューが採用されなかったときは Tools と同じ内容になる。
	ItemTools         []CodexToolCall
	ToolsUsed         []string
	Model             string
	TotalInputTokens  int
	TotalOutputTokens int
	CacheReadTokens   int
	CacheWriteTokens  int
}

type codexLine struct {
	Type    json.RawMessage `json:"type"`
	Payload json.RawMessage `json:"payload"`
}

// codexUsage は token_count イベントが持つ累計トークン数である。
type codexUsage struct {
	InputTokens           *int `json:"input_tokens"`
	CachedInputTokens     *int `json:"cached_input_tokens"`
	CacheWriteInputTokens *int `json:"cache_write_input_tokens"`
	OutputTokens          *int `json:"output_tokens"`
}

// ParseCodexTranscript は codex の rollout を集計する。
//
// claude 版と同じく、現行版(jq)がエラーになる入力では ok=false を返して
// フォールバックレコードへ落ちるようにしている。
func ParseCodexTranscript(data []byte) (CodexTranscript, bool) {
	result := CodexTranscript{
		Tools:     []CodexToolCall{},
		ItemTools: []CodexToolCall{},
		ToolsUsed: []string{},
		Model:     UnknownModel,
	}
	var usage *codexUsage
	// 2 通りの書き方それぞれのターン数とツール呼び出しを別々に集め、
	// どちらを採るかは読み終えてから決める。
	var legacyTurns, itemTurns int
	callTools := []CodexToolCall{}

	dec := json.NewDecoder(bytes.NewReader(data))
	for {
		var line codexLine
		err := dec.Decode(&line)
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return CodexTranscript{}, false
		}

		lineType, _ := jsonString(line.Type)
		if lineType != codexLineEventMsg &&
			lineType != codexLineResponseItem &&
			lineType != codexLineTurnContext {
			continue
		}

		// 現行版はいずれの行種別でも payload に添字を付けるため、
		// payload がオブジェクトでも null でもなければエラーになる。
		payload, ok := jsonObject(line.Payload)
		if !ok {
			return CodexTranscript{}, false
		}

		switch lineType {
		case codexLineEventMsg:
			eventType, _ := jsonString(payload["type"])
			switch eventType {
			case codexEventUserMessage:
				legacyTurns++
			case codexEventItemCompleted:
				isTurn, tool, isTool, ok := codexItemOf(payload["item"])
				if !ok {
					return CodexTranscript{}, false
				}
				if isTurn {
					itemTurns++
				}
				if isTool {
					result.ItemTools = append(result.ItemTools, tool)
				}
			case codexEventTokenCount:
				parsed, ok := codexUsageOf(payload)
				if !ok {
					return CodexTranscript{}, false
				}
				// 最後の非 null が残る(現行版の `| last`)。
				if parsed != nil {
					usage = parsed
				}
			}
		case codexLineResponseItem:
			tool, isCall, ok := codexToolOf(payload)
			if !ok {
				return CodexTranscript{}, false
			}
			if isCall {
				callTools = append(callTools, tool)
			}
		case codexLineTurnContext:
			model := payload["model"]
			if !jsonNonNull(model) {
				continue
			}
			name, ok := jsonString(model)
			if !ok {
				// 現行版は文字列でないモデル名を $pricing の添字に使って落ちる。
				return CodexTranscript{}, false
			}
			result.Model = name
		}
	}

	// ターンは片方だけを採る。同じターンの別表現なので、両方に現れる rollout が
	// あったとしても合算してはいけない。
	result.TotalTurns = legacyTurns
	if itemTurns > 0 {
		result.TotalTurns = itemTurns
	}
	// ツールも同じく片方だけを採る。実機には response_item を 1 つも持たない
	// rollout があるため、item ビューは呼び出しビューが空のときの受け皿になる。
	result.Tools = callTools
	if len(callTools) == 0 {
		result.Tools = result.ItemTools
	}

	names := make([]string, 0, len(result.Tools))
	for _, tool := range result.Tools {
		names = append(names, tool.Name)
	}
	result.ToolsUsed = uniqueSortedStrings(names)

	if usage != nil {
		// input_tokens はキャッシュ済みを含む総量なので、実消費はその差である。
		result.TotalInputTokens = intOrZero(usage.InputTokens) - intOrZero(usage.CachedInputTokens)
		result.TotalOutputTokens = intOrZero(usage.OutputTokens)
		result.CacheReadTokens = intOrZero(usage.CachedInputTokens)
		result.CacheWriteTokens = intOrZero(usage.CacheWriteInputTokens)
	}
	return result, true
}

// codexUsageOf は token_count イベントから累計トークン数を取り出す。
// total_token_usage が null(または無い)場合は nil を返し、直前の値を残す。
func codexUsageOf(payload map[string]json.RawMessage) (*codexUsage, bool) {
	info, ok := jsonObject(payload["info"])
	if !ok {
		return nil, false
	}
	raw := info["total_token_usage"]
	if !jsonNonNull(raw) {
		return nil, true
	}
	var usage codexUsage
	if err := json.Unmarshal(raw, &usage); err != nil {
		return nil, false
	}
	return &usage, true
}

// codexToolOf は response_item の payload からツール呼び出しを取り出す。
// 呼び出しでない場合は isCall=false を返す。
func codexToolOf(payload map[string]json.RawMessage) (tool CodexToolCall, isCall, ok bool) {
	rawType := payload["type"]
	if !jsonNonNull(rawType) {
		return CodexToolCall{}, false, true
	}
	payloadType, isString := jsonString(rawType)
	if !isString {
		// 現行版は type を test() に渡すため文字列でなければ落ちる。
		return CodexToolCall{}, false, false
	}
	if !codexToolCallPattern.MatchString(payloadType) {
		return CodexToolCall{}, false, true
	}

	name, named := codexToolName(payload, payloadType)
	if !named {
		return CodexToolCall{}, false, false
	}
	tool = CodexToolCall{Name: name}
	for _, key := range []string{"input", "arguments"} {
		if raw := payload[key]; jsonTruthy(raw) {
			tool.MergeCommand = jsonToString(raw)
			break
		}
	}
	// MCP 経由のマージは引数の本文ではなく呼び出したツール名で見分ける。
	if raw := payload["name"]; jsonTruthy(raw) {
		tool.MergeTool = jsonToString(raw)
	}
	return tool, true, true
}

// codexToolName は現行版の codex_tool_name(`.name // .tool // .kind // .type`)
// を再現する。fallback は呼び出しビューでは payload.type、item ビューでは
// item.type である。
//
// 最初に見つかった非偽値が文字列でなければ ok=false を返す。これは現行仕様との
// 意図的な差異である(CodexTranscript のコメントを参照)。
func codexToolName(fields map[string]json.RawMessage, fallback string) (string, bool) {
	for _, key := range []string{"name", "tool", "kind"} {
		raw := fields[key]
		if !jsonTruthy(raw) {
			continue
		}
		name, isString := jsonString(raw)
		if !isString {
			return "", false
		}
		return name, true
	}
	return fallback, true
}

// codexItemOf は item_completed の item を読む。
//
// 戻り値は (isTurn, tool, isTool, ok)。isTurn は UserMessage item(1 ターン)、
// isTool はツール呼び出しの item かどうかである。
func codexItemOf(rawItem json.RawMessage) (isTurn bool, tool CodexToolCall, isTool, ok bool) {
	// 現行版は item に `.type` で添字を付けるため、オブジェクトでも null でも
	// なければ落ちる。
	item, isObject := jsonObject(rawItem)
	if !isObject {
		return false, CodexToolCall{}, false, false
	}

	// 型が文字列でなければ、どの比較にも一致しないだけでエラーにはならない。
	itemType, _ := jsonString(item["type"])
	if itemType == codexItemUserMessage {
		return true, CodexToolCall{}, false, true
	}
	if _, isToolItem := codexToolItemTypes[itemType]; !isToolItem {
		return false, CodexToolCall{}, false, true
	}

	name, named := codexToolName(item, itemType)
	if !named {
		return false, CodexToolCall{}, false, false
	}
	tool = CodexToolCall{Name: name}

	// 走査対象は item の種類ごとに決まっている。CommandExecution は実行した
	// コマンド、McpToolCall は呼び出したツール名で、FileChange と Extension は
	// マージの手段になり得ないので何も走査しない。
	switch itemType {
	case codexItemCommandExecution:
		tool.MergeCommand = codexCommandText(item["command"])
	case codexItemMcpToolCall:
		if raw := item["tool"]; jsonTruthy(raw) {
			tool.MergeTool = jsonToString(raw)
		}
	}
	return false, tool, true, true
}

// codexCommandText は CommandExecution item が実行したコマンドを 1 本の文字列に
// する。現行版の codex_command_text に対応する。
//
//	if (.command | type) == "array" then (.command | map(tostring) | join(" "))
//	else ((.command // "") | tostring) end
//
// 型で分岐するのは、形の揺れでレコードを丸ごと落とさないためである。ここで
// jq が落ちると daily レコードが summary: null へ退避してしまう。配列なら
// 要素ごとに tostring して空白で繋ぎ(null は "null" になる)、配列でなければ
// 文字列や数値やオブジェクトをそのまま tostring する。偽値は空文字になる。
//
// 数値の表記は jq の正規化(1.0 を 1 にする)までは真似ていない。実在の rollout
// の command は文字列の配列だけで、数値が入る例が無いためである。
func codexCommandText(raw json.RawMessage) string {
	if trimmed := bytes.TrimSpace(raw); len(trimmed) > 0 && trimmed[0] == '[' {
		var elements []json.RawMessage
		if err := json.Unmarshal(raw, &elements); err != nil {
			return ""
		}
		parts := make([]string, 0, len(elements))
		for _, element := range elements {
			parts = append(parts, jsonToString(element))
		}
		return strings.Join(parts, " ")
	}
	if !jsonTruthy(raw) {
		return ""
	}
	return jsonToString(raw)
}

// CodexMarkers は codex のツール呼び出しから markers を判定する。
//
// 判定するのは merged だけで、slack と doc は常に false である。codex の
// rollout にはツール名が残らない(すべて exec などの汎用呼び出しに畳まれる)ため、
// 現行版もこの 2 つを判定していない。
//
// 走査するのは採用したビューと item ビューの両方である。呼び出しビューを採った
// rollout でも、gh pr merge が CommandExecution item にしか残っていないことが
// あるためで、真偽値なので二重に数える心配は無い。
func CodexMarkers(transcript CodexTranscript) DailyMarkers {
	var markers DailyMarkers
	for _, tools := range [][]CodexToolCall{transcript.Tools, transcript.ItemTools} {
		for _, tool := range tools {
			if mergeCommandPattern.MatchString(tool.MergeCommand) ||
				strings.Contains(tool.MergeTool, codexMergeToolSubstring) {
				markers.Merged = true
			}
		}
	}
	return markers
}

// codexRequiredPricingKeys は codex の cost 式が素の参照で使うキーである。
// jq(record-output.sh:82-89)では input / output だけが素の参照で、
// cache_hit / cache_write には `// 0` が付いている(欠けても 0 で計算が進む)。
var codexRequiredPricingKeys = []string{PricingKeyInput, PricingKeyOutput}

// CodexCost は codex のセッションの利用料金(USD)を返す。
//
// 戻り値は (cost, priced, ok)。単価が分からないモデルでは priced=false
// (claude と違いフォールバックせず、呼び出し側が cost を null にする)。
// 単価エントリはあるが input / output が欠けている場合は ok=false で、現行版は
// null との掛け算エラーでレコード全体が Parse failed に落ちる。cache_hit /
// cache_write の欠落は jq の `// 0` と同じくゼロ値のまま計算する。
//
// 現行版は 4 項を足してから 100 万で割る。claude 側(項ごとに割る)と式の形が
// 違うため、丸め誤差の出方まで揃えて写している。
func CodexCost(transcript CodexTranscript, pricing Pricing) (float64, bool, bool) {
	model, found := pricing.ForCodex(transcript.Model)
	if !found {
		return 0, false, true
	}
	if model.MissingAny(codexRequiredPricingKeys...) {
		return 0, false, false
	}
	cost := (float64(transcript.TotalInputTokens)*model.Input +
		float64(transcript.TotalOutputTokens)*model.Output +
		float64(transcript.CacheReadTokens)*model.CacheHit +
		float64(transcript.CacheWriteTokens)*model.CacheWrite) / tokensPerPriceUnit
	return RoundCost(cost), true, true
}

// jsonObject は raw をオブジェクトとして読む。
//
// raw が無い、または null の場合は nil マップと ok=true を返す。現行版は
// null への添字を null として扱うためである。オブジェクトでも null でもない
// 場合だけ ok=false を返す(現行版が「Cannot index ... 」で落ちる条件)。
func jsonObject(raw json.RawMessage) (map[string]json.RawMessage, bool) {
	if !jsonNonNull(raw) {
		return nil, true
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil {
		return nil, false
	}
	return fields, true
}

// jsonNonNull は値が存在して null でないかを返す(jq の `. != null`)。
// false は「非 null」である点に注意する。
func jsonNonNull(raw json.RawMessage) bool {
	trimmed := bytes.TrimSpace(raw)
	return len(trimmed) > 0 && !bytes.Equal(trimmed, []byte("null"))
}

// jsonToString は jq の tostring を再現する。
// 文字列はそのまま、それ以外は compact JSON の表記にする。
//
// null を先に弾くのは、Go の json.Unmarshal が JSON の null を文字列へ入れても
// エラーにせず空文字を残すためである。jq の `null | tostring` は "null" になる。
func jsonToString(raw json.RawMessage) string {
	if !jsonNonNull(raw) {
		return "null"
	}
	if value, ok := jsonString(raw); ok {
		return value
	}
	var buf bytes.Buffer
	if err := json.Compact(&buf, raw); err != nil {
		return string(raw)
	}
	return buf.String()
}
