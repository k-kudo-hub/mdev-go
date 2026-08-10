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

// codexItemUserMessage は 1 ターンに当たる item.type である。
const codexItemUserMessage = "UserMessage"

// codexToolItemTypes はツール呼び出しに当たる item.type である。
// Reasoning やメッセージの item はツール呼び出しではないので数えない。
var codexToolItemTypes = map[string]struct{}{
	"CommandExecution": {},
	"McpToolCall":      {},
	"FileChange":       {},
	"Extension":        {},
}

// codexToolCallPattern はツール呼び出しの response_item を見分ける。
//
// codex は custom_tool_call / function_call / local_shell_call など複数の
// 呼び出し種別を持ち、結果は `_call_output` で終わる別の response_item に入る。
// 現行 record-output.sh:72 はこの命名規則を使って呼び出しだけを数えている。
// 末尾の `\n?$` は Oniguruma の `$` に合わせたものである(markers.go を参照)。
var codexToolCallPattern = regexp.MustCompile(`_call\n?$`)

// CodexToolCall は rollout 中のツール呼び出し 1 件である。
type CodexToolCall struct {
	// Name はツール名。name が無ければ type を使う(現行版の `.name // .type`)。
	// item ビューではツール名のフィールドが無いため、item の型がそのまま名前になる。
	Name string
	// Input は merged 判定に使う文字列。呼び出しビューでは現行版の
	// `((.input // .arguments // "") | tostring)`、item ビューでは
	// `((.command // []) | join(" ")) + " " + ((.arguments // "") | tostring)`
	// に対応する。オブジェクトなどは compact JSON の文字列になる。
	//
	// item ビューで実行したコマンドだけを見るのは、CommandExecution item が
	// stdout / aggregated_output も持つためである。gh pr merge と書かれた
	// ファイルを cat しただけのタスクを merged と誤判定する実データがある。
	Input string
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

	tool = CodexToolCall{Name: payloadType}
	if raw := payload["name"]; jsonTruthy(raw) {
		name, isString := jsonString(raw)
		if !isString {
			return CodexToolCall{}, false, false
		}
		tool.Name = name
	}
	for _, key := range []string{"input", "arguments"} {
		if raw := payload[key]; jsonTruthy(raw) {
			tool.Input = jsonToString(raw)
			break
		}
	}
	return tool, true, true
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

	tool = CodexToolCall{Name: itemType}
	if raw := item["name"]; jsonTruthy(raw) {
		name, isString := jsonString(raw)
		if !isString {
			return false, CodexToolCall{}, false, false
		}
		tool.Name = name
	}

	command, joined := codexJoinCommand(item["command"])
	if !joined {
		return false, CodexToolCall{}, false, false
	}
	arguments := ""
	if raw := item["arguments"]; jsonTruthy(raw) {
		arguments = jsonToString(raw)
	}
	tool.Input = command + " " + arguments
	return false, tool, true, true
}

// codexJoinCommand は item の command を jq の `(.command // []) | join(" ")`
// と同じ形で 1 本の文字列にする。
//
// 偽値(null や false)は空配列と同じ扱いになり、配列でなければ join が反復に
// 失敗するため ok=false を返す(現行版は「Cannot iterate over ...」で落ちる)。
func codexJoinCommand(raw json.RawMessage) (string, bool) {
	if !jsonTruthy(raw) {
		return "", true
	}
	var elements []json.RawMessage
	if err := json.Unmarshal(raw, &elements); err != nil {
		return "", false
	}
	parts := make([]string, 0, len(elements))
	for _, element := range elements {
		part, ok := codexJoinElement(element)
		if !ok {
			return "", false
		}
		parts = append(parts, part)
	}
	return strings.Join(parts, " "), true
}

// codexJoinElement は join が要素 1 つを文字列にする規則を再現する。
//
// null は空文字になり、数値と真偽値は表記になり、オブジェクトと配列は連結
// できずにエラーになる(現行版は「string と object cannot be added」で落ちる)。
// 数値の表記は jq の正規化(1.0 を 1 にする)までは真似ていない。実在の rollout
// の command は文字列の配列だけで、数値が入る例が無いためである。
func codexJoinElement(raw json.RawMessage) (string, bool) {
	if !jsonNonNull(raw) {
		return "", true
	}
	if value, isString := jsonString(raw); isString {
		return value, true
	}
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) > 0 && (trimmed[0] == '{' || trimmed[0] == '[') {
		return "", false
	}
	return jsonToString(raw), true
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
			if mergeCommandPattern.MatchString(tool.Input) {
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
func jsonToString(raw json.RawMessage) string {
	if value, ok := jsonString(raw); ok {
		return value
	}
	var buf bytes.Buffer
	if err := json.Compact(&buf, raw); err != nil {
		return string(raw)
	}
	return buf.String()
}
