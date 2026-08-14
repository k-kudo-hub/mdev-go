package domain

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sort"
	"strings"
)

// agentsKey は設定のエージェント定義が入るキー。
const agentsKey = "agents"

// detectionKey / patternsKey は install が既存の設定へ補う 2 つのキーである。
//
// どちらも後から足された項目で、これが無いとスクリーン検出が無言で無効の
// ままになる(conductor issue #28)。利用者が自分で書いた値は書き換えない。
const (
	detectionKey = "detection"
	patternsKey  = "patterns"
)

// AgentDefaultAddition は補ったキー 1 件である。何を足したかを画面へ出すために使う。
type AgentDefaultAddition struct {
	// Agent は補われたエージェント名(.agents のキー)。
	Agent string
	// Key は補われたキー。patterns の中のキーを補った場合は "patterns.<名前>"。
	Key string
}

// String は "codex.detection" の形にする。
func (a AgentDefaultAddition) String() string { return a.Agent + "." + a.Key }

// MergeAgentDefaults は既定の設定から不足しているエージェント項目を補う。
//
// 現行 install.sh:138-149 の jq 式を移したものである。補うのは既存の
// `.agents` にあるエージェントに対してだけで、既定にしかいないエージェントを
// 足したりはしない。利用者が設定済みの値は 1 つも書き換えない。
//
//   - `.agents.<名前>` に detection / patterns が無ければ既定から補う
//   - `.agents.<名前>.patterns` がオブジェクトで **1 件以上**あるなら、
//     既定にあってそこに無いキーだけを補う
//
// 空のオブジェクト `{}` を持つ patterns には手を入れない。「検出を止める」と
// いう明示の意思表示だからである(現行版の `length > 0` 判定と同じ)。
//
// # 現行版との差異: 書き換え方
//
// jq は入力を読み直して整形し直すため、触っていない箇所の空白やインデントも
// jq の流儀へ揃ってしまう。ここでは **足りないキーを挿し込むだけ** で、他の
// バイトは 1 つも動かさない。利用者が手で整えた設定の見た目が install の
// たびに変わるのは避けたい。
//
// この差の副産物として、補うものが無いときの出力は入力とバイト単位で同じに
// なる(2 回目の install が 1 バイトも書かない)。
//
// # 現行版との差異: 補ったキーの位置
//
// jq は既定側のキー(detection / patterns)を先に並べてから利用者のキーを
// 重ねるため、補われたキーはオブジェクトの **先頭** に来る。こちらは末尾へ
// 足す。中身は同じで、JSON のキー順に意味は無い。
func MergeAgentDefaults(config, defaults []byte) ([]byte, []AgentDefaultAddition, error) {
	var defaultAgents map[string]map[string]json.RawMessage
	if err := unmarshalAgents(defaults, &defaultAgents); err != nil {
		return nil, nil, fmt.Errorf("既定の設定を読めません: %w", err)
	}
	if len(defaultAgents) == 0 {
		return bytes.Clone(config), nil, nil
	}

	if !isJSONObject(config) {
		return nil, nil, errors.New("config.json のトップレベルがオブジェクトではありません")
	}
	layout, err := scanAgentsLayout(config)
	if err != nil {
		return nil, nil, err
	}
	if layout == nil {
		// `.agents` が無い(または オブジェクトでない)設定には何もしない。
		// 現行版の `if .agents then ... else . end` と同じ扱いである。
		return bytes.Clone(config), nil, nil
	}

	var (
		edits     []byteEdit
		additions []AgentDefaultAddition
	)
	for _, agent := range layout.agents {
		base := defaultAgents[agent.name]
		if base == nil {
			continue
		}

		var members []jsonMember
		for _, key := range []string{detectionKey, patternsKey} {
			raw, ok := base[key]
			// jq の with_entries(select(.value != null)) と同じく、既定側が
			// null のキーは「無い」ものとして扱う。
			if !ok || isJSONNull(raw) || agent.has(key) {
				continue
			}
			members = append(members, jsonMember{key: key, value: raw})
			additions = append(additions, AgentDefaultAddition{Agent: agent.name, Key: key})
		}
		if len(members) > 0 {
			edits = append(edits, insertMembers(config, agent.object, members))
		}

		// patterns がオブジェクトで 1 件以上あるときだけ、中を鍵単位で補う。
		if agent.patterns == nil || len(agent.patternKeys) == 0 {
			continue
		}
		defaultPatterns := patternMap(base[patternsKey])
		var patternMembers []jsonMember
		for _, key := range sortedKeys(defaultPatterns) {
			if agent.hasPattern(key) {
				continue
			}
			patternMembers = append(patternMembers, jsonMember{key: key, value: defaultPatterns[key]})
			additions = append(additions,
				AgentDefaultAddition{Agent: agent.name, Key: patternsKey + "." + key})
		}
		if len(patternMembers) > 0 {
			edits = append(edits, insertMembers(config, *agent.patterns, patternMembers))
		}
	}

	sortEdits(edits)
	return applyEdits(config, edits), additions, nil
}

// unmarshalAgents は設定の `.agents` だけを取り出す。
func unmarshalAgents(data []byte, out *map[string]map[string]json.RawMessage) error {
	var doc struct {
		Agents map[string]map[string]json.RawMessage `json:"agents"`
	}
	if err := json.Unmarshal(data, &doc); err != nil {
		return err
	}
	*out = doc.Agents
	return nil
}

// patternMap は patterns の中身をキーごとに割る。オブジェクトでなければ空を返す。
func patternMap(raw json.RawMessage) map[string]json.RawMessage {
	var out map[string]json.RawMessage
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil
	}
	return out
}

// sortedKeys はキーを昇順で返す。補う順を決めるためだけに使う。
//
// 既定側の記述順を採らないのは、map に落とした時点で順が失われているためで
// ある。補われるのは既定にしか無いキーなので、並びに意味は無い。
func sortedKeys(m map[string]json.RawMessage) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// isJSONNull は raw が null かどうかを返す。
func isJSONNull(raw json.RawMessage) bool {
	return bytes.Equal(bytes.TrimSpace(raw), []byte("null"))
}

// objectSpan は JSON オブジェクト 1 つが元のバイト列で占める範囲である。
// open は `{` の位置、close は `}` の位置(いずれも 0 始まり)。
type objectSpan struct {
	open  int
	close int
	// empty はメンバーが 1 つも無いかどうか。挿し込むときのカンマの要否を決める。
	empty bool
}

// agentLayout は `.agents.<名前>` 1 件の位置と、そこに在るキーである。
type agentLayout struct {
	name   string
	object objectSpan
	keys   map[string]struct{}
	// patterns は patterns がオブジェクトのときだけ非 nil。
	patterns    *objectSpan
	patternKeys map[string]struct{}
}

func (a agentLayout) has(key string) bool {
	_, ok := a.keys[key]
	return ok
}

func (a agentLayout) hasPattern(key string) bool {
	_, ok := a.patternKeys[key]
	return ok
}

// agentsLayout は `.agents` 配下の位置情報である。
type agentsLayout struct {
	agents []agentLayout
}

// scanAgentsLayout は config を 1 回走査して `.agents` 配下の位置を集める。
//
// `.agents` が無い、オブジェクトでない、というときは nil を返す(呼び出し側は
// 何もしない)。JSON として解釈できない入力はエラーにする。現行版も jq が
// 落ちてマージを飛ばし、既存の設定をそのまま残していた。
func scanAgentsLayout(config []byte) (*agentsLayout, error) {
	if !json.Valid(config) {
		return nil, errors.New("config.json として解釈できる JSON ではありません")
	}
	dec := json.NewDecoder(bytes.NewReader(config))

	var (
		stack   []jsonFrame
		layout  agentsLayout
		opens   []int
		members []int
	)
	for {
		prev := dec.InputOffset()
		tok, err := dec.Token()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			// json.Valid を通っているためここには来ない。
			return nil, fmt.Errorf("config.json の走査に失敗しました: %w", err)
		}

		if d, ok := tok.(json.Delim); ok {
			switch d {
			case '{', '[':
				opens = append(opens, int(prev)+bytes.IndexByte(config[prev:], byte(d)))
				members = append(members, 0)
				stack = pushOrPop(stack, d)
			case '}', ']':
				open, count := opens[len(opens)-1], members[len(members)-1]
				opens, members = opens[:len(opens)-1], members[:len(members)-1]
				closePos := int(dec.InputOffset()) - 1
				collectAgentSpan(&layout, stack, d,
					objectSpan{open: open, close: closePos, empty: count == 0})
				stack = pushOrPop(stack, d)
			}
			continue
		}

		s, isString := tok.(string)
		if isKeyPosition(stack) {
			stack[len(stack)-1].key = s
			stack[len(stack)-1].expectKey = false
			if len(members) > 0 {
				members[len(members)-1]++
			}
			recordAgentKey(&layout, stack, s)
			continue
		}
		consumeValue(stack)
		if !isString && len(members) > 0 && !isObjectTop(stack) {
			// 配列の要素を数える。オブジェクトのメンバー数はキーで数える。
			members[len(members)-1]++
		}
	}
	if len(layout.agents) == 0 {
		return nil, nil
	}
	return &layout, nil
}

// isObjectTop は今いるコンテナがオブジェクトかどうかを返す。
func isObjectTop(stack []jsonFrame) bool {
	return len(stack) > 0 && stack[len(stack)-1].isObject
}

// recordAgentKey は `.agents.<名前>` とその patterns に在るキーを覚える。
func recordAgentKey(layout *agentsLayout, stack []jsonFrame, key string) {
	switch {
	case isAgentObject(stack):
		agent := findAgent(layout, stack[1].key)
		agent.keys[key] = struct{}{}
	case isAgentPatternsObject(stack):
		agent := findAgent(layout, stack[1].key)
		agent.patternKeys[key] = struct{}{}
	}
}

// collectAgentSpan は閉じ括弧の位置で `.agents.<名前>` と patterns の範囲を記録する。
func collectAgentSpan(layout *agentsLayout, stack []jsonFrame, d json.Delim, span objectSpan) {
	if d != '}' {
		return
	}
	switch {
	case isAgentObject(stack):
		findAgent(layout, stack[1].key).object = span
	case isAgentPatternsObject(stack):
		agent := findAgent(layout, stack[1].key)
		copied := span
		agent.patterns = &copied
	}
}

// isAgentObject は今いるコンテナが `.agents.<名前>` かどうかを返す。
func isAgentObject(stack []jsonFrame) bool {
	return len(stack) == 3 && stack[0].isObject && stack[0].key == agentsKey &&
		stack[1].isObject && stack[2].isObject
}

// isAgentPatternsObject は今いるコンテナが `.agents.<名前>.patterns` かを返す。
func isAgentPatternsObject(stack []jsonFrame) bool {
	return len(stack) == 4 && stack[0].isObject && stack[0].key == agentsKey &&
		stack[1].isObject && stack[2].isObject && stack[2].key == patternsKey &&
		stack[3].isObject
}

// findAgent は名前でエージェントを引く。初出なら作る(走査順に並ぶ)。
func findAgent(layout *agentsLayout, name string) *agentLayout {
	for i := range layout.agents {
		if layout.agents[i].name == name {
			return &layout.agents[i]
		}
	}
	layout.agents = append(layout.agents, agentLayout{
		name:        name,
		keys:        map[string]struct{}{},
		patternKeys: map[string]struct{}{},
	})
	return &layout.agents[len(layout.agents)-1]
}

// jsonMember は挿し込むキーと値の組である。
type jsonMember struct {
	key   string
	value json.RawMessage
}

// insertMembers は obj へ members を挿し込む編集を返す。
//
// インデントは閉じ括弧が乗っている行から読む。整形済みの設定に足したときに
// 周りと段が揃い、1 行に詰めて書かれた設定ではそのまま 1 行に収まる。
//
// # 挿し込む位置
//
// 整形済みで既存のメンバーがある場合は、**最後のメンバーの直後**へ挿す。
// 閉じ括弧の直前(= 改行と字下げの後ろ)へ挿すと、カンマだけの行ができて
// しかも閉じ括弧が挿した値の末尾に張り付く。
//
//	"command": "codex"      ← 元の最後のメンバー
//	,                       ← カンマだけの行
//	  "detection": "screen"}  ← 閉じ括弧が潰れる
//
// 中身が空のときだけは閉じ括弧の直前へ挿し、閉じ括弧が自分の行に戻るよう
// 改行と字下げを添える。
func insertMembers(data []byte, obj objectSpan, members []jsonMember) byteEdit {
	indent, pretty := objectIndent(data, obj)

	at := obj.close
	if pretty && !obj.empty {
		at = lastNonSpace(data, obj.open+1, obj.close) + 1
	}

	var buf bytes.Buffer
	for i, m := range members {
		if i > 0 || !obj.empty {
			buf.WriteByte(',')
		}
		if pretty {
			buf.WriteString("\n")
			buf.WriteString(indent)
		} else if i > 0 || !obj.empty {
			buf.WriteByte(' ')
		}
		buf.Write(encodeMemberKey(m.key))
		buf.WriteString(": ")
		buf.Write(indentJSONValue(m.value, indent, pretty))
	}
	if pretty && at == obj.close {
		// 閉じ括弧の直前へ挿した = 中身が空だった。閉じ括弧を自分の行へ戻す。
		buf.WriteString("\n")
		buf.WriteString(strings.TrimSuffix(indent, "  "))
	}
	return byteEdit{start: at, end: at, replacement: buf.Bytes()}
}

// lastNonSpace は data[from:to) の最後の非空白バイトの位置を返す。
// 見つからなければ from-1 を返す。
func lastNonSpace(data []byte, from, to int) int {
	for i := to - 1; i >= from; i-- {
		switch data[i] {
		case ' ', '\t', '\n', '\r':
			continue
		default:
			return i
		}
	}
	return from - 1
}

// objectIndent は挿し込む行の字下げと、整形して書かれているかどうかを返す。
func objectIndent(data []byte, obj objectSpan) (string, bool) {
	lineStart := bytes.LastIndexByte(data[:obj.close], '\n') + 1
	head := data[lineStart:obj.close]
	if len(bytes.TrimLeft(head, " \t")) != 0 {
		// 閉じ括弧の前に中身がある = 1 行で書かれている。
		return "", false
	}
	closeIndent := string(head)
	// 中身の段は閉じ括弧より 1 段深い。既存のメンバーがあればその段に揃える。
	if inner, ok := firstMemberIndent(data, obj); ok {
		return inner, true
	}
	return closeIndent + "  ", true
}

// firstMemberIndent は最初のメンバーが乗っている行の字下げを返す。
func firstMemberIndent(data []byte, obj objectSpan) (string, bool) {
	rest := data[obj.open+1 : obj.close]
	nl := bytes.IndexByte(rest, '\n')
	if nl < 0 {
		return "", false
	}
	line := rest[nl+1:]
	indent := line[:len(line)-len(bytes.TrimLeft(line, " \t"))]
	if len(bytes.TrimSpace(line)) == 0 {
		return "", false
	}
	return string(indent), true
}

// encodeMemberKey はキーを JSON の文字列リテラルにする。
func encodeMemberKey(key string) []byte {
	literal, err := encodeJSONString(key)
	if err != nil {
		// キーは既定の設定から来る短い ASCII 文字列なので失敗しない。
		return []byte(`"` + key + `"`)
	}
	return literal
}

// indentJSONValue は値を挿し込み先の段に合わせて整形する。
//
// 1 行で書かれた設定へ足すときは詰めた表記にする。整形済みの設定へ足すときは
// 2 スペース刻みで、周りと同じ深さから始まるようにする。
func indentJSONValue(raw json.RawMessage, indent string, pretty bool) []byte {
	var compact bytes.Buffer
	if err := json.Compact(&compact, raw); err != nil {
		return raw
	}
	if !pretty {
		return compact.Bytes()
	}
	var out bytes.Buffer
	if err := json.Indent(&out, compact.Bytes(), indent, "  "); err != nil {
		return compact.Bytes()
	}
	return out.Bytes()
}

// sortEdits は編集を start の昇順に並べる。applyEdits がその順を前提にする。
func sortEdits(edits []byteEdit) {
	sort.Slice(edits, func(i, j int) bool { return edits[i].start < edits[j].start })
}

// RenderAgentDefaultAdditions は補ったキーの一覧を 1 行にまとめる。
// 何も補わなかった場合は空文字を返す。
func RenderAgentDefaultAdditions(additions []AgentDefaultAddition) string {
	if len(additions) == 0 {
		return ""
	}
	names := make([]string, 0, len(additions))
	for _, a := range additions {
		names = append(names, a.String())
	}
	return strings.Join(names, ", ")
}
