package shell_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/k-kudo-hub/mdev-go/internal/infra/shell"
)

// fakeClaude は PATH の先頭に置く claude の代役を作る。
//
// 現行 test.sh が MOCK_BIN へ置くモックと同じ考え方で、実際に exec される
// 経路(stdin の受け渡し・終了コードの扱い)ごと確かめられる。
func fakeClaude(t *testing.T, script string) {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "claude")
	if err := os.WriteFile(path, []byte("#!/bin/bash\n"+script), 0o700); err != nil { //nolint:gosec // テスト用の実行ファイル
		t.Fatal(err)
	}
	// 元の PATH は残す(モックの中で cat などを使うため)。先頭に置くので
	// 実環境に claude があってもこちらが選ばれる。
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
}

// TestSummarize は要約が取れる経路を確かめる。
func TestSummarize(t *testing.T) {
	fakeClaude(t, "cat >/dev/null\necho '- モックの作業要約1'\necho '- モックの作業要約2'\n")

	got, err := shell.NewSummaryGenerator().Summarize("会話")
	if err != nil {
		t.Fatalf("Summarize が失敗しました: %v", err)
	}
	// コマンド置換と同じく末尾の改行は落ちる。
	if want := "- モックの作業要約1\n- モックの作業要約2"; got != want {
		t.Errorf("Summarize = %q, want %q", got, want)
	}
}

// TestSummarizePassesConversationOnStdin は会話が stdin で渡ることを確かめる。
// 引数で渡すと長い会話でコマンドラインの上限に当たる。
func TestSummarizePassesConversationOnStdin(t *testing.T) {
	fakeClaude(t, "cat\n")

	got, err := shell.NewSummaryGenerator().Summarize("CONVOMARKER 会話本文")
	if err != nil {
		t.Fatalf("Summarize が失敗しました: %v", err)
	}
	if got != "CONVOMARKER 会話本文" {
		t.Errorf("stdin の内容が渡っていません: %q", got)
	}
}

// TestSummarizePassesPrompt は指示の文言が -p で渡ることを確かめる。
// 文言が変わると要約の体裁(箇条書きの点数・前置きの有無)が変わる。
func TestSummarizePassesPrompt(t *testing.T) {
	fakeClaude(t, "cat >/dev/null\nprintf '%s' \"$2\"\n")

	got, err := shell.NewSummaryGenerator().Summarize("会話")
	if err != nil {
		t.Fatalf("Summarize が失敗しました: %v", err)
	}
	want := "以下はあるタスクの作業会話ログです。" +
		"何を行ったかを日本語の箇条書き3〜6点で簡潔に要約してください。前置きや後書きは不要です。"
	if got != want {
		t.Errorf("プロンプト = %q, want %q", got, want)
	}
}

// TestSummarizeFailures は失敗が必ず error になることを確かめる。
// ここで成功を返すと、中身の無いログを残したままタブが消えてしまう。
func TestSummarizeFailures(t *testing.T) {
	tests := []struct {
		name   string
		script string
	}{
		{name: "異常終了", script: "cat >/dev/null\nexit 1\n"},
		{name: "出力が空", script: "cat >/dev/null\n"},
		{name: "出力が改行だけ", script: "cat >/dev/null\necho\n"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fakeClaude(t, tt.script)
			if got, err := shell.NewSummaryGenerator().Summarize("会話"); err == nil {
				t.Errorf("error になりませんでした(要約 = %q)", got)
			}
		})
	}
}

// TestSummarizeFailsWhenCommandMissing は claude が無い環境で error になる
// ことを確かめる。
func TestSummarizeFailsWhenCommandMissing(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	_, err := shell.NewSummaryGenerator().Summarize("会話")
	if err == nil {
		t.Fatal("claude が無いのに error になりませんでした")
	}
	if !strings.Contains(err.Error(), "claude") {
		t.Errorf("error にコマンド名が含まれていません: %v", err)
	}
}
