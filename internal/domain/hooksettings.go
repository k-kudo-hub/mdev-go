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

// hookCommandRule はコマンド文字列 1 種類に対する置換規則である。
//
// コマンド文字列の「末尾」だけを対象にし、前置きは書き換えない。現行 hooks.json は
// `${CONDUCTOR_HOME:-$HOME/.claude-conductor}/scripts/pending-notify.sh` の形で、
// この環境変数展開が mdev-test の worktree 隔離を hooks にも効かせている。
// 絶対パスへ展開してしまうと隔離が壊れるため、前置きはそのまま残す。
type hookCommandRule struct {
	// from は置換前のコマンドの末尾。
	from string
	// to は置換後のコマンドの末尾。
	to string
}

// switchHookCommandRules は置き換える 3 種類のスクリプトである。
// pending-notify.sh は Notification と Stop の 2 箇所に現れる。
var switchHookCommandRules = []hookCommandRule{
	{from: "/scripts/pending-notify.sh", to: "/bin/mdev hook notify"},
	{from: "/scripts/pending-post-tool.sh", to: "/bin/mdev hook post-tool"},
	{from: "/scripts/pending-resolve.sh", to: "/bin/mdev hook resolve"},
}

// restoreHookCommandRules は switchHookCommandRules の from と to を入れ替えた
// 逆向きの規則である。復元をバックアップの全文書き戻しではなく
// 「切り替えと逆向きの外科的な書き換え」で行うために使う。
//
// 全文を書き戻すと、切り替え後に Claude Code 自身が settings.json へ書いた
// 変更(permissions.allow の追加が典型)が黙って消える。逆変換であれば
// hooks 以外の差分は一切触らずに残る。
var restoreHookCommandRules = reverseHookCommandRules(switchHookCommandRules)

// pendingScriptMarker は conductor の pending スクリプト呼び出しの目印である。
// switchHookCommandRules の from はすべてこの文字列を含む。
const pendingScriptMarker = "/scripts/pending-"

// reverseHookCommandRules は規則の向きを入れ替えた新しい規則列を返す。
func reverseHookCommandRules(rules []hookCommandRule) []hookCommandRule {
	out := make([]hookCommandRule, 0, len(rules))
	for _, r := range rules {
		out = append(out, hookCommandRule{from: r.to, to: r.from})
	}
	return out
}

// SwitchedHookCommandSuffixes は切り替え後のコマンドの末尾を、規則の順で返す。
//
// `/bin/mdev hook notify` の形の文字列であり、ここに現れるコマンド名は
// mdev が実際に受け付けるサブコマンドと一致していなければならない。
// 置換規則(domain)と cobra のコマンドツリー(cli)は互いを参照しないため、
// 片方だけを直しても両方ともコンパイルは通ってしまう。全パッケージを
// 参照できる cmd/mdev のテストが、この一覧をコマンドツリーと突き合わせる。
func SwitchedHookCommandSuffixes() []string {
	out := make([]string, 0, len(switchHookCommandRules))
	for _, r := range switchHookCommandRules {
		out = append(out, r.to)
	}
	return out
}

// HookCommandChange は hook コマンド 1 件の置換内容である。表示用に使う。
type HookCommandChange struct {
	// Event は `.hooks` 直下のイベント名(Notification / Stop の類)。
	Event string
	// Before / After は置換前後のコマンド文字列。
	Before string
	After  string
}

// HookCommand は `.hooks` 配下のコマンド 1 件である。表示用に使う。
type HookCommand struct {
	// Event は `.hooks` 直下のイベント名。
	Event string
	// Command はコマンド文字列。
	Command string
}

// RemainingPendingScriptCommands は `.hooks` 配下に残っている conductor の
// pending スクリプト呼び出しを返す。
//
// 切り替えた後にこれが空でなければ、置換規則に無い亜種(引数付きの呼び出しや
// 利用者が足した別のスクリプト)が残っているということである。切り替え自体は
// 成功しているが、そのイベントだけ Shell 版のまま取り残されるため、
// 呼び出し側は警告として提示する。
func RemainingPendingScriptCommands(settings []byte) ([]HookCommand, error) {
	refs, err := scanHookCommands(settings)
	if err != nil {
		return nil, err
	}

	var out []HookCommand
	for _, ref := range refs {
		if strings.Contains(ref.value, pendingScriptMarker) {
			out = append(out, HookCommand{Event: ref.event, Command: ref.value})
		}
	}
	return out, nil
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
	return rewriteHookCommands(settings, switchHookCommandRules)
}

// RestoreHookCommands は SwitchHookCommands と逆向きの置換を行う。
// `mdev hook` サブコマンドの呼び出しを conductor のスクリプト呼び出しへ戻す。
//
// 走査と編集の仕組みは SwitchHookCommands と同一で、規則の向きだけが違う。
// そのため switch → restore の往復は(対象の文字列リテラルが素直な表記で
// 書かれている限り)バイト単位で恒等になる。
//
// 既にスクリプトを指しているコマンドはどの規則にも一致しないため、
// 2 回目以降の呼び出しは変更なしになる(冪等)。
func RestoreHookCommands(settings []byte) ([]byte, []HookCommandChange, error) {
	return rewriteHookCommands(settings, restoreHookCommandRules)
}

// rewriteHookCommands は rules に従って `.hooks` 配下のコマンドを書き換える。
func rewriteHookCommands(settings []byte, rules []hookCommandRule) ([]byte, []HookCommandChange, error) {
	refs, err := scanHookCommands(settings)
	if err != nil {
		return nil, nil, err
	}

	var (
		edits   []byteEdit
		changes []HookCommandChange
	)
	for _, ref := range refs {
		after, ok := rewrittenHookCommand(ref.value, rules)
		if !ok {
			continue
		}
		literal, err := encodeJSONString(after)
		if err != nil {
			return nil, nil, err
		}
		edits = append(edits, byteEdit{start: ref.start, end: ref.end, replacement: literal})
		changes = append(changes, HookCommandChange{Event: ref.event, Before: ref.value, After: after})
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

// hookCommandRef は `.hooks` 配下で見つけたコマンド 1 件と、
// その値の文字列リテラルが元のバイト列で占める範囲 [start, end) である。
type hookCommandRef struct {
	event string
	value string
	start int
	end   int
}

// scanHookCommands は settings を 1 回走査し、`.hooks` 配下の `command` の値を
// 先頭から順に集める。JSON として解釈できない入力はエラーにする。
func scanHookCommands(settings []byte) ([]hookCommandRef, error) {
	if !json.Valid(settings) {
		return nil, errors.New("settings.json として解釈できる JSON ではありません")
	}
	dec := json.NewDecoder(bytes.NewReader(settings))

	var (
		stack []jsonFrame
		refs  []hookCommandRef
	)
	for {
		prev := dec.InputOffset()
		tok, err := dec.Token()
		if errors.Is(err, io.EOF) {
			return refs, nil
		}
		if err != nil {
			// json.Valid を通っているためここには来ない。
			return nil, fmt.Errorf("settings.json の走査に失敗しました: %w", err)
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
		start := int(prev) + bytes.IndexByte(settings[prev:], '"')
		refs = append(refs, hookCommandRef{
			event: stack[1].key,
			value: s,
			start: start,
			end:   int(dec.InputOffset()),
		})
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

// rewrittenHookCommand は cmd に rules を当てた結果を返す。
// どの規則にも当てはまらなければ ok=false を返す。
func rewrittenHookCommand(cmd string, rules []hookCommandRule) (string, bool) {
	for _, r := range rules {
		if strings.HasSuffix(cmd, r.from) {
			return strings.TrimSuffix(cmd, r.from) + r.to, true
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
