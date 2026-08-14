package domain

import "strings"

// zellij (>=0.44) はセッション名の長さを制限する。超えた名前は拒否される。
const (
	// SessionNameLimit は zellij が受け付けるセッション名の上限(文字数)。
	SessionNameLimit = 24
	// sessionNamePrefix は切り詰めたときに残す先頭の文字数。
	sessionNamePrefix = 19
	// sessionNameHashDigits は末尾へ付ける短縮ハッシュの桁数。
	sessionNameHashDigits = 4
)

// ZellijSessionName は zellij に渡せる長さのセッション名を返す。
//
// 現行 init.zsh の `_conductor_session_name` を移したものである。
//
//	local name="$1" hash_src="${2:-$1}"
//	if (( ${#name} > 24 )); then
//	    local h
//	    h=$(printf '%s' "$hash_src" | cksum | cut -d' ' -f1)
//	    h=${h: -4}
//	    name="${name:0:19}"
//	    name="${name%-}-$h"
//	fi
//	echo "$name"
//
// hashSrc が空なら name 自身を使う(`${2:-$1}` と同じで、空文字も未指定と
// 同じ扱いになる)。長さが上限以内ならそのまま返す。
//
// # 互換が要る理由
//
// この関数の出力がそのまま既存セッションの名前である。1 文字でも変えると、
// 利用者が今 attach しているセッションへ二度と戻れなくなり、代わりに同じ
// ディレクトリで新しいセッションが作られる(タスクは残ったまま画面から消える)。
//
// # バイト単位で写した箇所
//
//   - 長さの判定と切り出しは **文字数**。zsh は UTF-8 のロケールで
//     `${#name}` を文字数として数え、`${name:0:19}` も文字で切る
//   - ハッシュ源に流すのは **バイト列**。cksum はバイトを読む
//   - 短縮は cksum が出す **10 進表記の下 4 桁**であって、値の剰余ではない。
//     `${h: -4}` は文字列の後方参照で、4 桁に満たなければ全体が残る
//   - 切り出した 19 文字が `-` で終わる場合は `${name%-}` がそれを 1 つ落とす。
//     結果は 24 文字ではなく 23 文字になる
//
// 期待値は現行版へ同じ入力を流して作った表(testdata/session-names.tsv)で
// 固定してある。
func ZellijSessionName(name, hashSrc string) string {
	runes := []rune(name)
	if len(runes) <= SessionNameLimit {
		return name
	}
	if hashSrc == "" {
		hashSrc = name
	}

	digits := itoa32(posixCksum([]byte(hashSrc)))
	if len(digits) > sessionNameHashDigits {
		digits = digits[len(digits)-sessionNameHashDigits:]
	}

	head := strings.TrimSuffix(string(runes[:sessionNamePrefix]), "-")
	return head + "-" + digits
}
