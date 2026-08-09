package domain

import "strings"

// 現行の各ペインスクリプトが定義している色コード。
//
// lipgloss は使わず、この文字列を組み立てて出力する(ADR-0002 の採用ライブラリ表)。
// 端末の能力に応じた減色や幅計算を挟むと現行版と 1 バイトも違わない出力を
// 保てないため、ANSI をそのまま書く方針にしている。
const (
	ansiRed    = "\033[0;31m"
	ansiGreen  = "\033[0;32m"
	ansiYellow = "\033[0;33m"
	ansiBold   = "\033[1m"
	ansiDim    = "\033[2m"
	ansiReset  = "\033[0m"
)

// 区切り線の長さ。Dashboard / Waiting / Done は 26 本、News だけ 22 本である
// (現行スクリプトの見た目をそのまま写している)。
const (
	dividerWidth     = 26
	newsDividerWidth = 22
)

// divider は DIM で囲んだ区切り線 1 行を返す(末尾の改行を含む)。
func divider(width int) string {
	return ansiDim + "  " + strings.Repeat("─", width) + ansiReset + "\n"
}
