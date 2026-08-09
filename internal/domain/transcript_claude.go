package domain

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"sort"
)

// UnknownModel は transcript からモデル名を特定できなかったときの値
// (現行 record-output.sh の `.[0] // "unknown"`)。
const UnknownModel = "unknown"

// StandardSpeed は speed が記録されていないときの既定値
// (現行版の `.[0] // "standard"`)。
const StandardSpeed = "standard"

// markers の判定対象になるツール名。ここでしか使わない値だが、判定より前の
// パース段階で「どのツールの input を文字列として読むか」を決めるために必要になる。
const (
	toolWrite = "Write"
	toolEdit  = "Edit"
	toolBash  = "Bash"
)

// contentTypeToolUse はツール呼び出しを表す content ブロックの type。
const contentTypeToolUse = "tool_use"

// lineTypeUser はユーザーの発話を表す transcript 行の type。
// ターン数はこの行数で数える。
const lineTypeUser = "user"

// ClaudeToolUse は transcript 中のツール呼び出し 1 件である。
//
// FilePath / Command は markers の判定に使う値だけを取り出したものである。
// 現行版が Write / Edit の file_path と Bash の command しか参照しないため、
// それ以外のツールでは空のままになる(値が入っていないことに意味はない)。
type ClaudeToolUse struct {
	Name     string
	FilePath string
	Command  string
}

// ClaudeTranscript は claude の transcript(JSONL)から集計した値である。
// 現行 record-output.sh:149-177 の jq 1 パスに対応する。
type ClaudeTranscript struct {
	TotalTurns         int
	Tools              []ClaudeToolUse
	ToolsUsed          []string
	Model              string
	Speed              string
	TotalInputTokens   int
	TotalOutputTokens  int
	CacheReadTokens    int
	CacheWrite5mTokens int
	CacheWrite1hTokens int
}

// claudeLine は transcript の 1 行。
//
// type は数値などでも現行版は素通りする(比較が偽になるだけ)ため
// json.RawMessage で受ける。一方 message は、オブジェクト以外だと現行版が
// `.message.content[]?` で落ちるため、型付きのポインタで受けて
// アンマーシャルの失敗をそのままパース失敗に写す。
type claudeLine struct {
	Type    json.RawMessage `json:"type"`
	Message *claudeMessage  `json:"message"`
}

type claudeMessage struct {
	Model   json.RawMessage `json:"model"`
	Content json.RawMessage `json:"content"`
	Usage   *claudeUsage    `json:"usage"`
}

type claudeUsage struct {
	InputTokens          *int                 `json:"input_tokens"`
	OutputTokens         *int                 `json:"output_tokens"`
	CacheReadInputTokens *int                 `json:"cache_read_input_tokens"`
	CacheCreation        *claudeCacheCreation `json:"cache_creation"`
	Speed                json.RawMessage      `json:"speed"`
}

type claudeCacheCreation struct {
	Ephemeral5mInputTokens *int `json:"ephemeral_5m_input_tokens"`
	Ephemeral1hInputTokens *int `json:"ephemeral_1h_input_tokens"`
}

type claudeContentBlock struct {
	Type  json.RawMessage `json:"type"`
	Name  json.RawMessage `json:"name"`
	Input json.RawMessage `json:"input"`
}

// ParseClaudeTranscript は claude の transcript を集計する。
//
// 現行版は `jq -s` の出力が空かどうかでフォールバックへ落ちるかを決めている。
// ここでも同じ切り分けになるよう、jq がエラーになる入力では ok=false を返す。
// 具体的には「JSON として読めない」「トップレベルがオブジェクト以外(null は可)」
// 「message や usage の型が違う」「tool_use に文字列の name が無い」場合である。
//
// 空ファイルはエラーではない。現行版も 0 と "unknown" の並んだ summary を出す。
func ParseClaudeTranscript(data []byte) (ClaudeTranscript, bool) {
	result := ClaudeTranscript{
		Tools:     []ClaudeToolUse{},
		ToolsUsed: []string{},
		Model:     UnknownModel,
		Speed:     StandardSpeed,
	}
	modelFound, speedFound := false, false

	dec := json.NewDecoder(bytes.NewReader(data))
	for {
		var line claudeLine
		err := dec.Decode(&line)
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return ClaudeTranscript{}, false
		}

		if typ, ok := jsonString(line.Type); ok && typ == lineTypeUser {
			result.TotalTurns++
		}
		if line.Message == nil {
			continue
		}

		tools, ok := parseClaudeTools(line.Message.Content)
		if !ok {
			return ClaudeTranscript{}, false
		}
		result.Tools = append(result.Tools, tools...)

		if !modelFound && jsonTruthy(line.Message.Model) {
			model, ok := jsonString(line.Message.Model)
			if !ok {
				// 現行版は文字列でないモデル名を $pricing の添字に使って落ちる。
				return ClaudeTranscript{}, false
			}
			result.Model, modelFound = model, true
		}

		usage := line.Message.Usage
		if usage == nil {
			continue
		}
		result.TotalInputTokens += intOrZero(usage.InputTokens)
		result.TotalOutputTokens += intOrZero(usage.OutputTokens)
		result.CacheReadTokens += intOrZero(usage.CacheReadInputTokens)
		if usage.CacheCreation != nil {
			result.CacheWrite5mTokens += intOrZero(usage.CacheCreation.Ephemeral5mInputTokens)
			result.CacheWrite1hTokens += intOrZero(usage.CacheCreation.Ephemeral1hInputTokens)
		}
		if !speedFound && jsonTruthy(usage.Speed) {
			speed, ok := jsonString(usage.Speed)
			if !ok {
				return ClaudeTranscript{}, false
			}
			result.Speed, speedFound = speed, true
		}
	}

	names := make([]string, 0, len(result.Tools))
	for _, tool := range result.Tools {
		names = append(names, tool.Name)
	}
	result.ToolsUsed = uniqueSortedStrings(names)
	return result, true
}

// parseClaudeTools は 1 行分の content からツール呼び出しを取り出す。
//
// content が配列でない場合(文字列など)はツール 0 件として扱う。現行版の
// `.message.content[]?` が `[]?` で吸収するためである。一方、配列の要素が
// オブジェクトでない場合は現行版が `.type` の添字で落ちるため ok=false を返す。
func parseClaudeTools(content json.RawMessage) ([]ClaudeToolUse, bool) {
	var blocks []json.RawMessage
	if err := json.Unmarshal(content, &blocks); err != nil {
		return nil, true
	}

	tools := make([]ClaudeToolUse, 0, len(blocks))
	for _, raw := range blocks {
		var block claudeContentBlock
		if err := json.Unmarshal(raw, &block); err != nil {
			return nil, false
		}
		if typ, ok := jsonString(block.Type); !ok || typ != contentTypeToolUse {
			continue
		}
		// 現行版は markers の判定で `.name | test(...)` を呼ぶため、name が
		// 文字列でなければ必ず落ちる。
		name, ok := jsonString(block.Name)
		if !ok {
			return nil, false
		}

		tool := ClaudeToolUse{Name: name}
		switch name {
		case toolWrite, toolEdit:
			value, ok := toolInputString(block.Input, "file_path")
			if !ok {
				return nil, false
			}
			tool.FilePath = value
		case toolBash:
			value, ok := toolInputString(block.Input, "command")
			if !ok {
				return nil, false
			}
			tool.Command = value
		}
		tools = append(tools, tool)
	}
	return tools, true
}

// toolInputString は現行版の `.input? // {} | .<key>? // ""` を再現する。
//
// input がオブジェクトでない、key が無い、値が null か false のときは空文字を返す。
// 値が文字列以外(数値など)のときだけ ok=false を返す。現行版がその値を
// `test()` に渡して「文字列でないので照合できない」と落ちるためである。
func toolInputString(input json.RawMessage, key string) (string, bool) {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(input, &fields); err != nil {
		return "", true
	}
	raw, present := fields[key]
	if !present || !jsonTruthy(raw) {
		return "", true
	}
	return jsonString(raw)
}

// uniqueSortedStrings は重複を除いて昇順に並べ替える(現行版の `unique` 相当)。
// jq の並びはコードポイント順で、UTF-8 のバイト順と一致する。
func uniqueSortedStrings(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	unique := make([]string, 0, len(values))
	for _, value := range values {
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		unique = append(unique, value)
	}
	sort.Strings(unique)
	return unique
}

// jsonString は raw が JSON 文字列ならその値を返す。
func jsonString(raw json.RawMessage) (string, bool) {
	var value string
	if err := json.Unmarshal(raw, &value); err != nil {
		return "", false
	}
	return value, true
}

// jsonTruthy は jq の真偽判定に合わせる。偽になるのは null と false だけで、
// 空文字も 0 も真である(実測: `"" // "fb"` は "" を返す)。
func jsonTruthy(raw json.RawMessage) bool {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 {
		return false
	}
	return !bytes.Equal(trimmed, []byte("null")) && !bytes.Equal(trimmed, []byte("false"))
}

// intOrZero は欠けているトークン数を 0 として扱う(現行版の `// 0` と `add`)。
func intOrZero(value *int) int {
	if value == nil {
		return 0
	}
	return *value
}
