package domain

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"regexp"
)

// codex の rollout(JSONL)の行 type。
const (
	codexLineEventMsg     = "event_msg"
	codexLineResponseItem = "response_item"
	codexLineTurnContext  = "turn_context"
)

// event_msg の payload.type。
const (
	codexEventUserMessage = "user_message"
	codexEventTokenCount  = "token_count"
)

// codexToolCallPattern はツール呼び出しの response_item を見分ける。
//
// codex は custom_tool_call / function_call / local_shell_call など複数の
// 呼び出し種別を持ち、結果は `_call_output` で終わる別の response_item に入る。
// 現行 record-output.sh:72 はこの命名規則を使って呼び出しだけを数えている。
// 末尾の `\n?$` は Oniguruma の `$` に合わせたものである(markers.go を参照)。
var codexToolCallPattern = regexp.MustCompile(`_call\n?$`)

// CodexToolCall は rollout 中のツール呼び出し 1 件である。
type CodexToolCall struct {
	// Name はツール名。payload.name が無ければ payload.type を使う
	// (現行版の `.name // .type`)。
	Name string
	// Input は merged 判定に使う文字列。現行版の
	// `((.input // .arguments // "") | tostring)` に対応し、
	// オブジェクトなどは compact JSON の文字列になる。
	Input string
}

// CodexTranscript は codex の rollout から集計した値である。
// 現行 record-output.sh:71-90 の jq 1 パスに対応する。
//
// claude と違い speed の概念が無く、summary では常に "standard" になる。
// トークンは各行の加算ではなく「最後の token_count に載っている累計」を使う。
type CodexTranscript struct {
	TotalTurns        int
	Tools             []CodexToolCall
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
		ToolsUsed: []string{},
		Model:     UnknownModel,
	}
	var usage *codexUsage

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
				result.TotalTurns++
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
				result.Tools = append(result.Tools, tool)
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

// CodexMarkers は codex のツール呼び出しから markers を判定する。
//
// 判定するのは merged だけで、slack と doc は常に false である。codex の
// rollout にはツール名が残らない(すべて exec などの汎用呼び出しに畳まれる)ため、
// 現行版もこの 2 つを判定していない。
func CodexMarkers(tools []CodexToolCall) DailyMarkers {
	var markers DailyMarkers
	for _, tool := range tools {
		if mergeCommandPattern.MatchString(tool.Input) {
			markers.Merged = true
		}
	}
	return markers
}

// CodexCost は codex のセッションの利用料金(USD)を返す。
//
// 単価が分からないモデルでは ok=false を返す。claude と違って別のモデルの
// 単価へフォールバックしないためで、呼び出し側は cost を null にする。
//
// 現行版(record-output.sh:82-89)は 4 項を足してから 100 万で割る。claude 側
// (項ごとに割る)と式の形が違うため、丸め誤差の出方まで揃えて写している。
func CodexCost(transcript CodexTranscript, pricing Pricing) (float64, bool) {
	model, ok := pricing.ForCodex(transcript.Model)
	if !ok {
		return 0, false
	}
	cost := (float64(transcript.TotalInputTokens)*model.Input +
		float64(transcript.TotalOutputTokens)*model.Output +
		float64(transcript.CacheReadTokens)*model.CacheHit +
		float64(transcript.CacheWriteTokens)*model.CacheWrite) / tokensPerPriceUnit
	return RoundCost(cost), true
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
