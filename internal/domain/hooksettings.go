package domain

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
)

// settings.json の `.hooks` 配下でコマンド文字列が入るキー。
// Claude Code の hook スキーマでは {"type":"command","command":"..."} と決まっている。
const hookCommandKey = "command"

// hooksKey は settings.json のトップレベルで hook 定義が入るキー。
const hooksKey = "hooks"

// hookCommandRule は conductor のスクリプト呼び出し 1 種類に対する置換規則である。
//
// コマンド文字列の「末尾」だけを対象にし、前置きは書き換えない。現行 hooks.json は
// `${CONDUCTOR_HOME:-$HOME/.claude-conductor}/scripts/pending-notify.sh` の形で、
// この環境変数展開が mdev-test の worktree 隔離を hooks にも効かせている。
// 絶対パスへ展開してしまうと隔離が壊れるため、前置きはそのまま残す。
type hookCommandRule struct {
	// scriptSuffix は切り替え前のコマンドの末尾。
	scriptSuffix string
	// mdevSuffix は切り替え後のコマンドの末尾。
	mdevSuffix string
}

// hookCommandRules は置き換える 3 種類のスクリプトである。
// pending-notify.sh は Notification と Stop の 2 箇所に現れる。
var hookCommandRules = []hookCommandRule{
	{scriptSuffix: "/scripts/pending-notify.sh", mdevSuffix: "/bin/mdev hook notify"},
	{scriptSuffix: "/scripts/pending-post-tool.sh", mdevSuffix: "/bin/mdev hook post-tool"},
	{scriptSuffix: "/scripts/pending-resolve.sh", mdevSuffix: "/bin/mdev hook resolve"},
}

// HookCommandChange は hook コマンド 1 件の置換内容である。表示用に使う。
type HookCommandChange struct {
	// Event は `.hooks` 直下のイベント名(Notification / Stop など)。
	Event string
	// Before / After は置換前後のコマンド文字列。
	Before string
	After  string
}

// SwitchHookCommands は settings.json のバイト列を受け取り、`.hooks` 配下の
// conductor スクリプト呼び出しを `mdev hook` サブコマンドへ置き換えたバイト列と、
// 置換した内容の一覧を返す。入力は変更しない。
//
// JSON を再シリアライズせず、置換対象の文字列リテラルのバイト範囲だけを
// 差し替える。そのためキー順・インデント・空白・未知キー・触っていない
// hook コマンドの表記は 1 バイトも変わらない。map 経由で書き戻すとキーが
// アルファベット順に並べ替えられ、ユーザーの settings.json が壊れる。
//
// 置換対象は「`.hooks` 配下のオブジェクトの `command` キーの値」に限る。
// `.hooks` の外に同じパスがあっても、イベント名などキーの位置に現れても触らない。
//
// 既に `mdev hook ...` を指しているコマンドはどの規則にも一致しないため、
// 2 回目以降の呼び出しは変更なしになる(冪等)。
func SwitchHookCommands(settings []byte) ([]byte, []HookCommandChange, error) {
	if !json.Valid(settings) {
		return nil, nil, errors.New("settings.json として解釈できる JSON ではありません")
	}

	edits, changes, err := findHookCommandEdits(settings)
	if err != nil {
		return nil, nil, err
	}
	return applyEdits(settings, edits), changes, nil
}

// byteEdit はバイト列の [start, end) を replacement で置き換える指示である。
type byteEdit struct {
	start       int
	end         int
	replacement []byte
}

// applyEdits は edits(start の昇順)を後ろから適用した新しいバイト列を返す。
// 後ろから適用するため、前方の edit のオフセットがずれない。
func applyEdits(data []byte, edits []byteEdit) []byte {
	out := bytes.Clone(data)
	for i := len(edits) - 1; i >= 0; i-- {
		e := edits[i]
		out = append(out[:e.start:e.start], append(e.replacement, out[e.end:]...)...)
	}
	return out
}

// jsonFrame は走査中の JSON コンテナ 1 つ分の状態である。
type jsonFrame struct {
	// isObject はオブジェクト(true)か配列(false)か。
	isObject bool
	// key はオブジェクトのとき、直近に読んだキー名。
	key string
	// expectKey はオブジェクトのとき、次のトークンがキーかどうか。
	expectKey bool
}

// findHookCommandEdits は settings を走査し、置換すべき文字列リテラルの
// バイト範囲と置換内容を先頭から順に集める。
func findHookCommandEdits(settings []byte) ([]byteEdit, []HookCommandChange, error) {
	dec := json.NewDecoder(bytes.NewReader(settings))

	var (
		stack   []jsonFrame
		edits   []byteEdit
		changes []HookCommandChange
	)
	for {
		prev := dec.InputOffset()
		tok, err := dec.Token()
		if errors.Is(err, io.EOF) {
			return edits, changes, nil
		}
		if err != nil {
			// json.Valid を通っているためここには来ない。
			return nil, nil, fmt.Errorf("settings.json の走査に失敗しました: %w", err)
		}

		if d, ok := tok.(json.Delim); ok {
			stack = pushOrPop(stack, d)
			continue
		}

		s, isString := tok.(string)
		if isKeyPosition(stack) {
			stack[len(stack)-1].key = s
			stack[len(stack)-1].expectKey = false
			continue
		}
		consumeValue(stack)

		if !isString || !isHookCommandValue(stack) {
			continue
		}
		after, ok := switchedHookCommand(s)
		if !ok {
			continue
		}
		literal, err := encodeJSONString(after)
		if err != nil {
			return nil, nil, err
		}
		start := int(prev) + bytes.IndexByte(settings[prev:], '"')
		edits = append(edits, byteEdit{start: start, end: int(dec.InputOffset()), replacement: literal})
		changes = append(changes, HookCommandChange{Event: stack[1].key, Before: s, After: after})
	}
}

// pushOrPop は区切りトークンに応じてスタックを積み下ろしする。
// コンテナの開始は親から見れば値なので、親の状態も進める。
func pushOrPop(stack []jsonFrame, d json.Delim) []jsonFrame {
	if d == '}' || d == ']' {
		return stack[:len(stack)-1]
	}
	consumeValue(stack)
	isObject := d == '{'
	return append(stack, jsonFrame{isObject: isObject, expectKey: isObject})
}

// isKeyPosition は次に読むトークンがオブジェクトのキーかどうかを返す。
func isKeyPosition(stack []jsonFrame) bool {
	if len(stack) == 0 {
		return false
	}
	top := stack[len(stack)-1]
	return top.isObject && top.expectKey
}

// consumeValue は値を 1 つ読み終えたことを親フレームに反映する。
func consumeValue(stack []jsonFrame) {
	if len(stack) == 0 {
		return
	}
	if top := &stack[len(stack)-1]; top.isObject {
		top.expectKey = true
	}
}

// isHookCommandValue は現在位置が `.hooks.<イベント名>` 配下の
// `command` キーの値かどうかを返す。
func isHookCommandValue(stack []jsonFrame) bool {
	if len(stack) < 2 {
		return false
	}
	if !stack[0].isObject || stack[0].key != hooksKey {
		return false
	}
	if !stack[1].isObject {
		return false
	}
	top := stack[len(stack)-1]
	return top.isObject && top.key == hookCommandKey
}

// switchedHookCommand は cmd を切り替え後のコマンドに変換する。
// どの規則にも当てはまらなければ ok=false を返す。
func switchedHookCommand(cmd string) (string, bool) {
	for _, r := range hookCommandRules {
		if strings.HasSuffix(cmd, r.scriptSuffix) {
			return strings.TrimSuffix(cmd, r.scriptSuffix) + r.mdevSuffix, true
		}
	}
	return "", false
}

// encodeJSONString は s を JSON の文字列リテラル(前後の " を含む)にする。
// `<` `>` `&` のエスケープは、元の表記との無用な差を生まないため無効にする。
func encodeJSONString(s string) ([]byte, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(s); err != nil {
		return nil, fmt.Errorf("コマンド文字列の JSON 化に失敗しました: %w", err)
	}
	return bytes.TrimRight(buf.Bytes(), "\n"), nil
}
