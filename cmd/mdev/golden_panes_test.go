package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/k-kudo-hub/mdev-go/internal/cli"
)

// ダッシュボード系 4 ペインのゴールデンテスト。
//
// testdata/golden-panes/cases.json が入力(pending / daily / news)を定義し、
// testdata/golden-panes/<case>/expected.txt には現行 Shell 版の同じスクリプトに
// 同じ入力を与えて生成させた ONCE 出力が入っている。fixture の作り方は
// scripts/gen-golden-panes.sh を参照(このテストは fixture を読むだけで、
// Shell 版には依存しない)。
//
// 比較はバイト列としての完全一致で行う。ANSI のエスケープ列・区切り線の本数・
// バイト幅の桁詰めまで含めて、現行と 1 バイトも違わないことを確かめる。
//
// このテストが cmd/mdev にあるのは、実行時と同じ依存グラフ(infra の実装まで
// 含む)を組み立てる必要があるためである。全パッケージを参照してよいのは
// ADR-0002 で cmd/mdev だけと決まっている。

const goldenPanesDir = "testdata/golden-panes"

// goldenPaneCase は 1 件の入力定義。cases.json の 1 要素に対応する。
type goldenPaneCase struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	// Pane は `mdev pane` に渡す名前。
	Pane string `json:"pane"`
	// Session は ZELLIJ_SESSION_NAME。
	Session string `json:"session"`
	// Tabs は zellij スタブが list-tabs で返すタブ名(空白区切り)。
	Tabs string `json:"tabs"`
	// Files は実行前に置くファイル。キーのパスに含まれる {TODAY} は
	// fixture の生成日に置き換える。
	Files map[string]string `json:"files"`
}

func loadGoldenPaneCases(t *testing.T) []goldenPaneCase {
	t.Helper()

	b, err := os.ReadFile(filepath.Join(goldenPanesDir, "cases.json"))
	if err != nil {
		t.Fatalf("cases.json が読めない: %v", err)
	}
	var cases []goldenPaneCase
	if err := json.Unmarshal(b, &cases); err != nil {
		t.Fatalf("cases.json の解釈に失敗: %v", err)
	}
	if len(cases) == 0 {
		t.Fatal("cases.json が空")
	}
	return cases
}

// goldenClock は fixture の生成日を返す時計である。
//
// daily とニュースのファイル名は「今日」の日付で決まる。Shell 版は date を
// 直接呼ぶため生成時の日付が焼き付いており、Go 側は同じ日付を返す時計を
// 差し込んで突き合わせる。
type goldenClock struct{ now time.Time }

func (c goldenClock) Now() time.Time { return c.now }

// writeGoldenFile は入力ファイルを 1 つ配置する。
func writeGoldenFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("ディレクトリの作成に失敗: %v", err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("ファイルの書き込みに失敗: %v", err)
	}
}

// setupGoldenSandbox は case の入力を一時ディレクトリへ展開し、
// ホームと CONDUCTOR_HOME を返す。
func setupGoldenSandbox(t *testing.T, tc goldenPaneCase, today string) (string, string) {
	t.Helper()

	root := t.TempDir()
	home := filepath.Join(root, "home")
	conductorHome := filepath.Join(root, "conductor")
	if err := os.MkdirAll(conductorHome, 0o755); err != nil {
		t.Fatalf("CONDUCTOR_HOME の作成に失敗: %v", err)
	}

	for rel, content := range tc.Files {
		resolved := strings.ReplaceAll(rel, "{TODAY}", today)
		var dest string
		switch {
		case strings.HasPrefix(resolved, "pending/"):
			dest = filepath.Join(home, ".claude-pending", strings.TrimPrefix(resolved, "pending/"))
		case strings.HasPrefix(resolved, "daily/"):
			dest = filepath.Join(conductorHome, "daily", strings.TrimPrefix(resolved, "daily/"))
		case strings.HasPrefix(resolved, "news/"):
			dest = filepath.Join(conductorHome, "news", strings.TrimPrefix(resolved, "news/"))
		case resolved == "config.json":
			dest = filepath.Join(conductorHome, "config.json")
		default:
			t.Fatalf("未知の入力パス: %s", resolved)
		}
		writeGoldenFile(t, dest, content)
	}
	return home, conductorHome
}

// installZellijStub は list-tabs だけに応える zellij のスタブを PATH の先頭へ置く。
//
// 実際の zellij は起動せず、タブの生成・移動・終了も起きない。Shell 版の
// fixture 生成でも同じ形のスタブを使っている(scripts/gen-golden-panes.sh)。
func installZellijStub(t *testing.T, tabs string) {
	t.Helper()

	dir := t.TempDir()
	stub := "#!/bin/bash\n" +
		"if [[ \"$1\" == \"action\" && \"$2\" == \"list-tabs\" ]]; then\n" +
		"    echo \"ID POSITION NAME\"\n" +
		"    for t in ${MOCK_TABS:-}; do\n" +
		"        echo \"1 x $t\"\n" +
		"    done\n" +
		"fi\n" +
		"exit 0\n"
	path := filepath.Join(dir, "zellij")
	if err := os.WriteFile(path, []byte(stub), 0o700); err != nil { //nolint:gosec // 実行するスタブ
		t.Fatalf("zellij スタブの作成に失敗: %v", err)
	}

	t.Setenv("MOCK_TABS", tabs)
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
}

func TestGoldenPanesMatchShellVersion(t *testing.T) {
	// t.Setenv を使うため並列にはしない。
	for _, tc := range loadGoldenPaneCases(t) {
		t.Run(tc.Name, func(t *testing.T) {
			caseDir := filepath.Join(goldenPanesDir, tc.Name)

			want, err := os.ReadFile(filepath.Join(caseDir, "expected.txt"))
			if err != nil {
				t.Fatalf("expected.txt が読めない(scripts/gen-golden-panes.sh で生成する): %v", err)
			}
			dateText, err := os.ReadFile(filepath.Join(caseDir, "date.txt"))
			if err != nil {
				t.Fatalf("date.txt が読めない: %v", err)
			}
			today := strings.TrimSpace(string(dateText))
			at, err := time.Parse("2006-01-02", today)
			if err != nil {
				t.Fatalf("date.txt の日付を解釈できない: %v", err)
			}

			home, conductorHome := setupGoldenSandbox(t, tc, today)
			installZellijStub(t, tc.Tabs)

			// 環境変数は case が定義するものだけを見せる。CONDUCTOR_HOME を
			// サンドボックスへ向けることで、Shell 呼び出し(restore-session /
			// スクリーン検出)は対象のスクリプトが無くて即座に失敗し、
			// 何の副作用も残さない。Shell 版の fixture 生成でもレジストリが
			// 空でパネルも無いため、どちらも表示に影響しない。
			env := map[string]string{
				"CONDUCTOR_HOME":      conductorHome,
				"ZELLIJ_SESSION_NAME": tc.Session,
			}
			deps := buildDeps(home, func(key string) string { return env[key] }, goldenClock{now: at})

			out := &bytes.Buffer{}
			cmd := cli.NewRootCommand(deps)
			cmd.SetOut(out)
			cmd.SetErr(out)
			cmd.SetArgs([]string{"pane", tc.Pane, "--once"})
			if err := cmd.Execute(); err != nil {
				t.Fatalf("mdev pane %s --once = %v", tc.Pane, err)
			}

			if out.String() != string(want) {
				t.Errorf("%s: Shell 版の出力と一致しない\n--- got ---\n%q\n--- want ---\n%q",
					tc.Description, out.String(), string(want))
			}
		})
	}
}
