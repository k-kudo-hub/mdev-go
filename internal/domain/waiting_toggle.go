package domain

import (
	"bytes"
	"encoding/json"
	"errors"
)

// ToggleWaiting は pending の JSON を Waiting との間で往復させる。
//
// 現行 waiting-toggle.sh の 2 本の jq フィルタを移植したものである。
//
//	Waiting のとき: .event = (.prev_event // "Notification") | del(.prev_event) | .time = $time
//	それ以外      : .prev_event = .event | .event = "Waiting" | .time = $time
//
// Waiting へ入るときに元の event を prev_event へ退避するのは、完了した
// (Stop の)タスクが再開時に Notification ではなく done へ戻れるようにするため
// である。
//
// 入力は構造体を経由せず生の JSON として扱う。jq は知らないキーもそのまま
// 持ち越すため、構造体に写すと将来 pending にキーが増えたとき黙って落として
// しまう。キーの並びも jq と同じ規則(既存キーは位置を保ち、新しいキーだけを
// 末尾に足す)で保つ。
//
// ok が false のときは呼び出し側が**何も書き換えてはならない**。現行版も
// jq が失敗したら一時ファイルを捨てて元のファイルを残す。
func ToggleWaiting(raw []byte, now string) (result []byte, ok bool) {
	doc, err := parseOrderedObject(raw)
	if err != nil {
		return nil, false
	}

	if jqRawString(doc.get(pendingKeyEvent)) == EventWaiting {
		// 再開。退避してあった event へ戻す(無ければ Notification)。
		restored := doc.get(pendingKeyPrevEvent)
		if !JSONTruthy(restored) {
			restored = jsonQuoted(EventNotification)
		}
		doc.set(pendingKeyEvent, restored)
		doc.del(pendingKeyPrevEvent)
	} else {
		// Waiting へ入る。今の event を退避してから差し替える。
		// event キーが無い場合、jq の `.prev_event = .event` は null を書く。
		previous := doc.get(pendingKeyEvent)
		if len(previous) == 0 {
			previous = json.RawMessage("null")
		}
		doc.set(pendingKeyPrevEvent, previous)
		doc.set(pendingKeyEvent, jsonQuoted(EventWaiting))
	}
	doc.set(pendingKeyTime, jsonQuoted(now))

	encoded, err := doc.encode()
	if err != nil {
		return nil, false
	}
	return encoded, true
}

// pending の JSON で ToggleWaiting が触るキー。
const (
	pendingKeyEvent     = "event"
	pendingKeyPrevEvent = "prev_event"
	pendingKeyTime      = "time"
)

// jsonQuoted は文字列を JSON の値(引用符つき)として符号化する。
func jsonQuoted(s string) json.RawMessage {
	// json.Marshal が string で失敗することはない。
	b, _ := json.Marshal(s)
	return b
}

// orderedObject はキーの並びを保った JSON オブジェクトである。
//
// Go の map は順序を持たないため、キーの並びを別に持つ。jq の出力と同じ
// 並びで書き戻すためだけに使う。
type orderedObject struct {
	keys   []string
	values map[string]json.RawMessage
}

// parseOrderedObject は JSON オブジェクトをキーの並びごと読む。
// オブジェクトでない場合はエラーを返す。
func parseOrderedObject(raw []byte) (*orderedObject, error) {
	var values map[string]json.RawMessage
	if err := json.Unmarshal(raw, &values); err != nil {
		return nil, err
	}
	if values == nil {
		// `null` は map への Unmarshal が成功してしまう(nil になる)。
		// オブジェクトではないので受け付けない。
		//
		// 現行版の jq はこの入力でも書き換えに成功するが、その文書が
		// waiting-toggle に選ばれることはない(`.tab` が "null" になり
		// タブ名と一致しないため)。安全側の no-op に倒す。
		return nil, errNotObject
	}

	// キーが重複した JSON では map に残るのが最後の 1 つだけなので、
	// 並びのほうも最初に現れた位置で 1 つに畳む(jq も 1 つだけ出力する)。
	seen := make(map[string]bool, len(values))
	keys := make([]string, 0, len(values))
	for _, key := range objectKeys(raw) {
		if seen[key] {
			continue
		}
		seen[key] = true
		keys = append(keys, key)
	}
	return &orderedObject{keys: keys, values: values}, nil
}

// errNotObject は JSON オブジェクトでない入力を表す。
var errNotObject = errors.New("JSON オブジェクトではありません")

// get はキーの値を返す。無ければ長さ 0 の RawMessage を返す。
func (o *orderedObject) get(key string) json.RawMessage { return o.values[key] }

// set はキーへ値を書く。無いキーは末尾に足す(jq の代入と同じ)。
func (o *orderedObject) set(key string, value json.RawMessage) {
	if _, ok := o.values[key]; !ok {
		o.keys = append(o.keys, key)
	}
	o.values[key] = value
}

// del はキーを取り除く(jq の del と同じ)。
func (o *orderedObject) del(key string) {
	if _, ok := o.values[key]; !ok {
		return
	}
	delete(o.values, key)
	for i, k := range o.keys {
		if k == key {
			o.keys = append(o.keys[:i], o.keys[i+1:]...)
			return
		}
	}
}

// encode はキーの並びを保ったまま compact な JSON にする。
//
// jq の既定は 2 スペースのプリティ出力なのでバイト列は一致しない。現行版との
// 突き合わせは JSON としての等価比較で行う(evidence §5)。
func (o *orderedObject) encode() ([]byte, error) {
	buf := &bytes.Buffer{}
	buf.WriteByte('{')
	written := 0
	for _, key := range o.keys {
		value, ok := o.values[key]
		if !ok {
			continue
		}
		if written > 0 {
			buf.WriteByte(',')
		}
		written++
		encodedKey, err := json.Marshal(key)
		if err != nil {
			return nil, err
		}
		buf.Write(encodedKey)
		buf.WriteByte(':')
		buf.Write(bytes.TrimSpace(value))
	}
	buf.WriteByte('}')
	return buf.Bytes(), nil
}
