package domain

import "regexp"

// 画面へ出す文字列から資格情報を伏せる。
//
// # FilterSecrets と分ける理由
//
// FilterSecrets は現行 Shell 版の awk / sed を移植したもので、**その出力は
// golden テストでバイト単位に固定されている**(作業ログの中身が Shell 版と
// 一致することの根拠になっている)。表示のために伏せる範囲を足すと、その
// 一致が崩れる。用途が違うので関数も分ける。
//
// こちらが守るのは画面とスクロールバックである。作業ログのように後から
// 読み返すものではないので、読みやすさより伏せ漏れの少なさを優先する。

// maskedPlaceholder は伏せた箇所に置く印である。
const maskedPlaceholder = "***"

// urlCredentials は `scheme://利用者[:秘密]@ホスト` の資格情報部分を捉える。
//
// upload.repo に `https://x-access-token:<トークン>@github.com/o/r.git` の形を
// 書いている利用者がいる。push の失敗メッセージにはこの URL がそのまま載る
// ため、画面へ出す前に伏せる。
var urlCredentials = regexp.MustCompile(`([a-zA-Z][a-zA-Z0-9+.-]*://)[^/\s:@]+(?::[^/\s@]*)?@`)

// bearerTokens は URL の外に現れる代表的なトークンを捉える。
//
// 網羅は狙わない。**画面に出る経路で実際に見かける形**だけを対象にする。
// 増やすときはここへ足す(FilterSecrets 側は触らない)。
var bearerTokens = regexp.MustCompile(
	`\b(?:gh[pousr]_[A-Za-z0-9]{16,}|glpat-[A-Za-z0-9_-]{16,}|sk-[A-Za-z0-9_-]{16,})`)

// MaskURLCredentials は表示用に資格情報を伏せた文字列を返す。
//
// 伏せるのは 2 つである。
//
//   - URL に埋め込まれた利用者名と秘密(`scheme://user:pass@host` → `scheme://***@host`)
//   - 単体で現れる代表的なトークン
//
// **何が失敗したのかは読めるまま残す。** 伏せすぎると診断に使えなくなり、
// 画面へ理由を出す意味が無くなる。
func MaskURLCredentials(s string) string {
	s = urlCredentials.ReplaceAllString(s, "${1}"+maskedPlaceholder+"@")
	return bearerTokens.ReplaceAllString(s, maskedPlaceholder)
}
