package domain

import (
	"strconv"
	"strings"
)

// ScreenState はタブ 1 つぶんのスクリーン検出の内部状態である。
//
// 現行版はこれを `$HOME/.claude-pending/<session>/.screen-state/<slug>` の
// **1 行**として持つ。形式は状態名だけ(`working`)か、状態名と epoch 秒を
// 空白で繋いだもの(`idle_pending 1754870400`)である。
//
// At を数値ではなく文字列で持つのは、現行版が `[[ =~ ^[0-9]+$ ]]` で数値かを
// 見てから使い、数値でなければ**確定側へ倒す**という分岐を持つためである。
// 読めなかったことを 0 と区別できないと、その分岐が再現できない。
type ScreenState struct {
	// State は ScreenWorking / ScreenIdle / ScreenBlocked / ScreenIdlePending。
	// 一度も観測していないタブでは空文字になる。
	State string
	// At は idle_pending に入った時刻(epoch 秒)の生の文字列。
	// 他の状態では空文字である。
	At string
}

// ParseScreenState は状態ファイルの中身を読む。
//
// 現行 screen-detect-lib.sh:138-141 と同じ分け方をする。
//
//   - 末尾の改行はすべて落とす(`$(cat ...)` のコマンド置換に相当)
//   - **最初の**空白までが状態名、その後ろがすべて時刻
//   - 空白が無ければ時刻は空文字
//
// ファイルが無い場合は空文字を渡すこと。ゼロ値(初回観測)になる。
func ParseScreenState(raw string) ScreenState {
	line := strings.TrimRight(raw, "\n")
	name, at, found := strings.Cut(line, " ")
	if !found {
		return ScreenState{State: line}
	}
	return ScreenState{State: name, At: at}
}

// Format は状態ファイルへ書く 1 行を返す(改行は含まない)。
// 書き込み側(infra)が現行版の `echo` と同じく末尾へ改行を足す。
func (s ScreenState) Format() string {
	if s.At == "" {
		return s.State
	}
	return s.State + " " + s.At
}

// PendingSince は idle_pending に入った時刻(epoch 秒)を返す。
//
// 現行版の `[[ "$prev_at" =~ ^[0-9]+$ ]]` に合わせ、数字だけで構成された
// 文字列のときにだけ ok=true を返す。ok=false のとき現行版は経過時間の判定を
// 飛ばして idle を確定させるため、呼び出し側はそちらへ倒す。
func (s ScreenState) PendingSince() (int64, bool) {
	if s.At == "" {
		return 0, false
	}
	for _, r := range s.At {
		if r < '0' || r > '9' {
			return 0, false
		}
	}
	seconds, err := strconv.ParseInt(s.At, 10, 64)
	if err != nil {
		return 0, false
	}
	return seconds, true
}
