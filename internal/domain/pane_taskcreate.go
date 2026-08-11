package domain

// RenderTaskCreateMenu はタスク作成ペインの待ち受け画面を返す。
//
// 現行 task-create-loop.sh の main_loop 冒頭をそのまま写している。
//
//	echo -e "${BOLD}  New Task${NC}  ${DIM}[$SESSION_NAME]${NC}"
//	echo -e "${DIM}  ──────────────────────────${NC}"
//	echo ""
//	echo -e "  ${DIM}[n]${NC} Create task"
//	echo ""
func RenderTaskCreateMenu(session string) string {
	return ansiBold + "  New Task" + ansiReset + "  " + ansiDim + "[" + session + "]" + ansiReset + "\n" +
		divider(dividerWidth) +
		"\n" +
		"  " + ansiDim + "[n]" + ansiReset + " Create task\n" +
		"\n"
}

// TaskCreateSearchDirsMissing は search_dirs が 1 つも実在しないときの文言。
// 現行版は赤字でこれを出して 2 秒待ち、メニューへ戻る。
const TaskCreateSearchDirsMissing = "検索対象ディレクトリが見つかりません"

// RenderTaskCreateError はタスク作成ペインのエラー行を返す。
//
// 現行版は search_dirs が空のときだけ赤字を出し、create_task の失敗は
// 無言で握り潰していた。Go 版は失敗も同じ形で出す(意図的な改善)。
func RenderTaskCreateError(message string) string {
	return "  " + ansiRed + message + ansiReset + "\n"
}
