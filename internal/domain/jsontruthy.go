package domain

import (
	"bytes"
	"encoding/json"
)

// JSONTruthy は jq の真偽判定に合わせる。偽になるのは null と false だけで、
// 空文字も 0 も真である(実測: `"" // "fb"` は "" を返す)。raw が無い
// (長さ 0)場合も偽とする。
//
// jq の `//` 演算子のフォールバック判定に対応する共通ヘルパーで、
// transcript の解釈と pricing の読み込み(infra/store)の両方が使う。
func JSONTruthy(raw json.RawMessage) bool {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 {
		return false
	}
	return !bytes.Equal(trimmed, []byte("null")) && !bytes.Equal(trimmed, []byte("false"))
}
