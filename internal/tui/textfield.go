package tui

// textField は 1 行の文字入力欄である。
//
// 現行 task-create-loop.sh は bash 4 なら `read -e -i "$default"` で既定値を
// 編集可能に出し、bash 3.2(macOS の既定)では `[候補]` を見せて Enter で
// 確定するだけだった。Go 版は常に編集できる形に揃える(意図的な改善)。
//
// 使う機能は「末尾への追記」と「末尾の 1 文字削除」だけである。カーソルの
// 移動は持たない。タスク名は短く、末尾から直すのがほとんどであるため、
// 実装を増やして取りこぼしを作るより単純さを取る。
type textField struct {
	// prompt は見出し(例 "Task name: ")。
	prompt string
	// defaultValue は入力が空だったときに採る既定値。
	defaultValue string
	// value は今の入力内容。初期値は defaultValue と同じで、編集できる。
	value string
}

// newTextField は既定値を入れた状態の入力欄を作る。
func newTextField(prompt, defaultValue string) textField {
	return textField{prompt: prompt, defaultValue: defaultValue, value: defaultValue}
}

// handleKey は 1 打鍵ぶんの入力を処理する。
// enter / esc は呼び出し側が先に扱うため、ここへは来ない。
func (f *textField) handleKey(key string) {
	switch key {
	case "backspace":
		if f.value != "" {
			f.value = f.value[:len(f.value)-len(lastRune(f.value))]
		}
	case "ctrl+u":
		// 行ごと消す。既定値を捨てて全部打ち直したいときに使う。
		f.value = ""
	default:
		// スペースは名前が "space" で届くため 1 文字かどうかでは判定できない。
		// タブ名にスペースは入りうるので typedText が個別に受ける。
		if text, ok := typedText(key); ok {
			f.value += text
		}
	}
}

// View は入力欄の表示を返す。
func (f *textField) View() string {
	return "  " + ansiBold + f.prompt + ansiReset + f.value + ansiCyan + "▏" + ansiReset + "\n"
}
