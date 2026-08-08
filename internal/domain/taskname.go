package domain

import "strconv"

// UniqueTaskName は existing のいずれとも一致しないタスク名を返す。
//
// base が existing に含まれない場合は base をそのまま返す。含まれる場合は
// "base-2"、"base-3" と連番を進め、最初に空いた候補を返す。
// 比較は完全一致で行うため、"myapp" は "myapp-dev" と衝突しない。
//
// 現行 Shell Script 版 (claude-conductor の task-create-loop.sh:
// ensure_unique_tab_name) の挙動を移植したもの。existing は変更しない。
func UniqueTaskName(base string, existing []string) string {
	taken := make(map[string]struct{}, len(existing))
	for _, name := range existing {
		taken[name] = struct{}{}
	}

	candidate := base
	for n := 2; ; n++ {
		if _, ok := taken[candidate]; !ok {
			return candidate
		}
		candidate = base + "-" + strconv.Itoa(n)
	}
}
