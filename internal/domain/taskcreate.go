package domain

import (
	"path"
	"strings"
	"unicode"
)

// DefaultTaskName はディレクトリとタスク種別から既定のタスク名を作る。
//
// 現行 task-create-loop.sh の generate_default_name
// (`echo "$(basename "$dir")-$type"`)と同じ結果を返す。
//
// `basename` と同じく末尾のスラッシュは無視し、ルート("/")はそのまま残す。
// path.Base はどちらも同じ規則で、filepath.Base と違い区切りが常に "/" である
// (タスク作成の対象は POSIX のパスなので、こちらのほうが移植元に近い)。
func DefaultTaskName(dir, taskType string) string {
	return path.Base(dir) + "-" + taskType
}

// ResolveTaskName は入力されたタスク名を解決する。
//
// 現行 task-create-loop.sh の resolve_name と同じく、入力が空文字なら既定名を
// 採る。空白だけの入力は「入力あり」である(現行版の `[[ -z ]]` も空白を空とは
// 見ない)。
func ResolveTaskName(defaultName, input string) string {
	if input == "" {
		return defaultName
	}
	return input
}

// ExpandHome は先頭の `~` をホームディレクトリへ展開する。
//
// 現行 task-create-loop.sh の `"${d/#\~/$HOME}"` と同じで、**先頭の 1 文字が
// `~` のときだけ**その 1 文字を置き換える。途中の `~` や `~user` の形は
// 展開しない。
func ExpandHome(dir, home string) string {
	if !strings.HasPrefix(dir, "~") {
		return dir
	}
	return home + dir[len("~"):]
}

// FilterCandidates は query に部分列として一致する候補だけを元の順で返す。
//
// fzf の既定の絞り込み(入力した文字がこの順で現れるものを残す)に相当する
// 動作である。大文字小文字は区別せず、判定はルーン単位で行う(バイト単位だと
// 多バイト文字の途中で一致してしまう)。query が空なら全件を返す。
//
// スコアによる並べ替えは行わない。並びが入力のたびに動くと、方向キーで選んで
// いる最中に対象が入れ替わるためである(fzf との差異)。
func FilterCandidates(items []string, query string) []string {
	if query == "" {
		return items
	}
	indexes := FilterCandidateIndexes(items, query)
	matched := make([]string, 0, len(indexes))
	for _, i := range indexes {
		matched = append(matched, items[i])
	}
	return matched
}

// FilterCandidateIndexes は FilterCandidates が残す候補の**位置**を返す。
//
// 選択 UI は「表示する文字列」と「選ばれた値が元の何番目か」の両方を要る
// (タスク種別はキーで選び、説明を添えて見せる)。位置で受け取れば、絞り込んだ
// 結果を元の一覧と突き合わせて位置を引き直す必要がなくなる。
//
// query が空なら全件の位置を返す。
func FilterCandidateIndexes(items []string, query string) []int {
	pattern := []rune(strings.ToLower(query))
	matched := make([]int, 0, len(items))
	for i, item := range items {
		if subsequenceFold(item, pattern) {
			matched = append(matched, i)
		}
	}
	return matched
}

// subsequenceFold は pattern(小文字化済み)が s の部分列かを返す。
func subsequenceFold(s string, pattern []rune) bool {
	i := 0
	for _, r := range s {
		if i == len(pattern) {
			return true
		}
		if unicode.ToLower(r) == pattern[i] {
			i++
		}
	}
	return i == len(pattern)
}
