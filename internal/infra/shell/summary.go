package shell

import (
	"errors"
	"fmt"
	"os/exec"
	"strings"

	"github.com/k-kudo-hub/mdev-go/internal/app"
)

// summaryCommand は要約に使う CLI。
//
// API を直接叩かないのは、認証(API キーか Claude の購読か)とモデルの選択が
// 新しい設計になるためである。CLI ならその環境が既に持っている認証設定を
// そのまま使える。
const summaryCommand = "claude"

// summaryPrompt は要約の指示である。現行 upload-log.sh:103 の文字列そのもの。
// 出力の体裁(箇条書きの点数・前置きの有無)がログの読みやすさを決めるため、
// 文言は 1 バイトも変えていない。
const summaryPrompt = "以下はあるタスクの作業会話ログです。" +
	"何を行ったかを日本語の箇条書き3〜6点で簡潔に要約してください。前置きや後書きは不要です。"

// SummaryGenerator は会話テキストから作業要約を作る。
type SummaryGenerator struct {
	// run は要約コマンドを実行して標準出力を返す。テストで差し替える。
	run func(stdin string, name string, args ...string) (string, error)
	// lookCommand は要約コマンドが使えるかを確かめる。テストで差し替える。
	lookCommand func() error
}

var _ app.SummaryGenerator = (*SummaryGenerator)(nil)

// NewSummaryGenerator は claude CLI を使う SummaryGenerator を返す。
func NewSummaryGenerator() *SummaryGenerator {
	return &SummaryGenerator{
		run:         runWithStdin,
		lookCommand: func() error { _, err := exec.LookPath(summaryCommand); return err },
	}
}

// Summarize は会話テキストを要約して返す。
//
// 上限は設けない。要約は数十秒かかることがあり、途中で切ると作業ログを
// 残せないままタブの削除だけが止まる(利用者から見て何も進まない)。
// モデルの指定もしない。利用者の設定した既定のモデルに従う。
//
// コマンドが無い・異常終了した・出力が空、のいずれでも error を返す。
// 中身の無い要約でログを残すと、失敗したことに気づけないまま会話が失われる。
func (g *SummaryGenerator) Summarize(conversation string) (string, error) {
	if err := g.lookCommand(); err != nil {
		return "", fmt.Errorf("%s コマンドが見つかりません: %w", summaryCommand, err)
	}
	out, err := g.run(conversation, summaryCommand, "-p", summaryPrompt)
	if err != nil {
		return "", fmt.Errorf("%s による要約に失敗しました: %w", summaryCommand, err)
	}
	// コマンド置換と同じく末尾の改行を落としてから空かどうかを見る。
	summary := strings.TrimRight(out, "\n")
	if summary == "" {
		return "", errors.New("要約が空でした")
	}
	return summary, nil
}

// runWithStdin は stdin を与えてコマンドを実行し、標準出力を返す。
// 標準エラー出力は捨てる(現行版の `2>/dev/null`)。上限は設けない。
func runWithStdin(stdin string, name string, args ...string) (string, error) {
	cmd := exec.Command(name, args...) //nolint:gosec // name は定数、args は固定の並び
	cmd.Stdin = strings.NewReader(stdin)
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("%s の実行に失敗しました: %w", name, err)
	}
	return string(out), nil
}
