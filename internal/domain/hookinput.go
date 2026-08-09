package domain

import (
	"bytes"
	"encoding/json"
	"path/filepath"
)

// HookInput は Claude Code の hook が標準入力で渡す JSON から、
// mdev が使う項目だけを取り出したものである。
//
// 現行 Shell 版が `jq -r` で個別に抜き出していた項目に 1:1 で対応し、
// 既定値(`// "Needs attention"`、`// "unknown"`)も jq 式に含まれていたものを
// そのまま持ち込んでいる。
type HookInput struct {
	SessionID      string
	Message        string
	HookEventName  string
	Cwd            string
	TranscriptPath string
}

// ParseHookInput は hook の標準入力 JSON を解釈する。
//
// 現行版は `jq ... 2>/dev/null` でエラーを握り潰し、解釈できない入力は
// 空文字として扱っていた。ここでも同様に error を返さず、解釈できない入力は
// 全項目が既定値になる。session_id が空になるため、呼び出し側の
// 「session_id が空なら何もしない」判定でそのまま no-op になる。
func ParseHookInput(raw []byte) HookInput {
	fields := map[string]json.RawMessage{}
	if err := json.Unmarshal(raw, &fields); err != nil {
		fields = nil
	}

	in := HookInput{
		SessionID:      jqString(fields, "session_id"),
		Message:        jqString(fields, "message"),
		HookEventName:  jqString(fields, "hook_event_name"),
		Cwd:            jqString(fields, "cwd"),
		TranscriptPath: jqString(fields, "transcript_path"),
	}
	if !jqHasValue(fields, "message") {
		in.Message = DefaultPendingMessage
	}
	if !jqHasValue(fields, "hook_event_name") {
		in.HookEventName = EventUnknown
	}
	return in
}

// jqHasValue は jq の `//` 演算子が「値がある」と見なすかを返す。
// jq は null と false のみを偽として扱うため、空文字列は値として扱われる。
func jqHasValue(fields map[string]json.RawMessage, key string) bool {
	raw, ok := fields[key]
	if !ok {
		return false
	}
	t := bytes.TrimSpace(raw)
	return len(t) != 0 && !bytes.Equal(t, []byte("null")) && !bytes.Equal(t, []byte("false"))
}

// jqString は `jq -r '.<key> // empty'` 相当の文字列化を行う。
//
// 文字列はそのまま返す。文字列以外(数値・true・配列・オブジェクト)は
// jq -r と同様に JSON 表記を返すが、jq が行う数値の正規化(1.50 → 1.5)や
// オブジェクトの圧縮までは再現していない。Claude Code はこれらのキーを
// 常に文字列で渡すため、この差が観測される経路は無い。
func jqString(fields map[string]json.RawMessage, key string) string {
	if !jqHasValue(fields, key) {
		return ""
	}
	raw := fields[key]

	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		return s
	}
	return string(bytes.TrimSpace(raw))
}

// ResolveTabName はタブ名を決める。
//
// 現行 pending-notify.sh の連鎖 `TASK_TAB_NAME` → `basename(cwd)` → `"unknown"`
// を移植したもので、cwd が空のときだけ "unknown" になる
// (`basename ""` は空文字を返し、`[ -z ]` 判定で "unknown" に落ちるため)。
func ResolveTabName(taskTabName, cwd string) string {
	if taskTabName != "" {
		return taskTabName
	}
	if base := shellBasename(cwd); base != "" {
		return base
	}
	return DefaultTabName
}

// DefaultTabName はタブ名を決められなかったときの名前。
const DefaultTabName = "unknown"

// shellBasename は POSIX の basename(1) と同じ結果を返す。
// filepath.Base は空文字に "." を返すため、そこだけ挙動を合わせている。
func shellBasename(path string) string {
	if path == "" {
		return ""
	}
	return filepath.Base(path)
}
