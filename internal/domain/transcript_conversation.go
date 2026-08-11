package domain

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"strings"
)

// lineTypeAssistant はエージェントの発話を表す claude transcript 行の type。
const lineTypeAssistant = "assistant"

// contentTypeText はテキストを運ぶ content ブロックの type(claude 形式)。
const contentTypeText = "text"

// codex の会話に当たる payload.type / item.type のうち、
// transcript_codex.go がまだ持っていないもの。
const (
	codexEventAgentMessage = "agent_message"
	codexItemAgentMessage  = "AgentMessage"
)

// ConversationText は transcript から要約に渡す会話テキストを取り出す。
//
// 現行 upload-log.sh の generate_summary が使う jq 1 パス(claude 形式の抽出と
// codex-rollout-lib.sh の codex_texts の連結)に対応する。**形式の判定は
// 行わない**。1 つのレコードは高々どちらか一方の選択条件にしか当たらないため、
// 両方を集めて連結すれば、pending に agent が記録されていない(古い)場合でも
// 誤った agent が記録されている場合でも正しく取れる。
//
// 並びは「claude 形式のぶんを全部 → codex 形式のぶんを全部」で、それぞれの
// 中はファイル順である(現行版の配列連結と同じ)。
//
// ok=false は現行版で jq が落ちるか会話が 1 件も取れなかった場合で、
// 呼び出し側はアップロードを中止しなければならない(会話が無いまま要約すると
// 中身の無いログが残り、失敗に気づけない)。
func ConversationText(data []byte) (string, bool) {
	var claudeTexts, codexTexts []string

	dec := json.NewDecoder(bytes.NewReader(data))
	for {
		var raw json.RawMessage
		err := dec.Decode(&raw)
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return "", false
		}
		// 現行版はどのレコードにも `.type` で添字を付けるため、
		// オブジェクトでも null でもないレコードがあれば jq ごと落ちる。
		record, isObject := jsonObject(raw)
		if !isObject {
			return "", false
		}

		lineType, _ := jsonString(record["type"])
		switch lineType {
		case lineTypeUser, lineTypeAssistant:
			text, ok := claudeMessageText(record["message"])
			if !ok {
				return "", false
			}
			if text != "" {
				claudeTexts = append(claudeTexts, text)
			}
		case codexLineEventMsg:
			text, ok := codexEventText(record["payload"])
			if !ok {
				return "", false
			}
			if text != "" {
				codexTexts = append(codexTexts, text)
			}
		}
	}

	joined := strings.Join(append(claudeTexts, codexTexts...), "\n")
	if joined == "" {
		return "", false
	}
	return joined, true
}

// claudeMessageText は claude の 1 レコードから発話テキストを取り出す。
//
//	.message.content as $c
//	| (if ($c | type) == "string" then $c
//	   else ([ $c[]? | select(.type == "text") | .text ] | join("\n")) end)
//
// content が文字列ならそのまま、配列ならテキストブロックだけを繋ぐ。
// ツール呼び出しだけのレコードは空文字になり、呼び出し側が捨てる。
func claudeMessageText(rawMessage json.RawMessage) (string, bool) {
	message, isObject := jsonObject(rawMessage)
	if !isObject {
		return "", false
	}
	content := message["content"]
	if text, isString := jsonString(content); isString {
		return text, true
	}

	var parts []string
	for _, element := range jsonElements(content) {
		// 現行版は要素へ `.type` で添字を付けるため、スカラー要素があると落ちる
		// (`[]?` が抑えるのは反復そのものの失敗だけである)。
		fields, isObject := jsonObject(element)
		if !isObject {
			return "", false
		}
		blockType, _ := jsonString(fields["type"])
		if blockType != contentTypeText {
			continue
		}
		parts = append(parts, jqJoinElement(fields["text"]))
	}
	return strings.Join(parts, "\n"), true
}

// codexEventText は codex の event_msg 1 件から発話テキストを取り出す。
//
// 旧い書き方(user_message / agent_message)は payload.message をそのまま、
// 新しい書き方(item_completed)は item.content の読める要素だけを繋ぐ。
// Reasoning の item は内部の思考であって会話ではないので拾わない。
func codexEventText(rawPayload json.RawMessage) (string, bool) {
	payload, isObject := jsonObject(rawPayload)
	if !isObject {
		return "", false
	}

	eventType, _ := jsonString(payload["type"])
	switch eventType {
	case codexEventUserMessage, codexEventAgentMessage:
		// 文字列でない message は select で落ちるだけでエラーにはならない。
		text, _ := jsonString(payload["message"])
		return text, true
	case codexEventItemCompleted:
		item, isObject := jsonObject(payload["item"])
		if !isObject {
			return "", false
		}
		itemType, _ := jsonString(item["type"])
		if itemType != codexItemUserMessage && itemType != codexItemAgentMessage {
			return "", true
		}
		return codexItemContentText(item["content"]), true
	}
	return "", true
}

// codexItemContentText は item.content の要素から読めるテキストだけを繋ぐ。
//
//	[ .item.content[]?
//	  | (if type == "string" then . else .text? end)
//	  | select(type == "string" and . != "") ] | join("\n")
//
// 実データでは UserMessage の要素が type "text"、AgentMessage の要素が "Text" と
// 揺れるため、type は見ずに .text を取る。素の文字列要素も受ける。読めない形の
// 要素は飛ばすだけで失敗させない(ここで落とすとタブの削除ごと止まるため)。
func codexItemContentText(rawContent json.RawMessage) string {
	var parts []string
	for _, element := range jsonElements(rawContent) {
		text, isString := jsonString(element)
		if !isString {
			fields, isObject := jsonObject(element)
			if !isObject {
				continue
			}
			text, isString = jsonString(fields["text"])
			if !isString {
				continue
			}
		}
		if text == "" {
			continue
		}
		parts = append(parts, text)
	}
	return strings.Join(parts, "\n")
}

// jsonElements は raw を配列として反復する(jq の `.[]?` に対応する)。
//
// 配列でなければ要素なしとして扱う。jq の `.[]?` はオブジェクトの値も反復
// するが、content がオブジェクトである transcript は claude にも codex にも
// 存在しないため、ここでは配列だけを扱う(意図的な差異)。
func jsonElements(raw json.RawMessage) []json.RawMessage {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || trimmed[0] != '[' {
		return nil
	}
	var elements []json.RawMessage
	if err := json.Unmarshal(trimmed, &elements); err != nil {
		return nil
	}
	return elements
}

// jqJoinElement は jq の join が 1 要素を文字列にする規則を再現する。
// null は空文字、文字列はそのまま、それ以外は tojson の表記になる。
//
// jsonToString(tostring 相当)との違いは null の扱いだけで、tostring が
// "null" を返すのに対し join は空文字を挟む。
func jqJoinElement(raw json.RawMessage) string {
	if !jsonNonNull(raw) {
		return ""
	}
	return jsonToString(raw)
}
