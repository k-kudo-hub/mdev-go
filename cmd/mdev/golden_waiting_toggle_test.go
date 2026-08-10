package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/k-kudo-hub/mdev-go/internal/app"
	"github.com/k-kudo-hub/mdev-go/internal/infra/store"
)

// waiting-toggle のゴールデンテスト。
//
// testdata/golden-waiting-toggle/cases.json が入力(pending 1 件)を定義し、
// <case>/expected.json には現行 Shell 版の waiting-toggle.sh に同じ入力を
// 与えて生成させた結果が入っている。fixture の作り方は
// scripts/gen-golden-waiting-toggle.sh を参照(このテストは fixture を
// 読むだけで、Shell 版には依存しない)。
//
// 比較は **JSON としての等価** で行う。jq の既定は 2 スペースのプリティ出力、
// Go 版は compact なので、バイト列では一致しない。キーと値がすべて同じで
// あることを確かめる(evidence §5)。
//
// time は Shell が date で決めるため差し替えられない。生成時の値を time.txt に
// 記録してあり、Go 版にも同じ文字列を渡す。

const goldenWaitingToggleDir = "testdata/golden-waiting-toggle"

// goldenToggleCase は 1 件の入力定義。cases.json の 1 要素に対応する。
type goldenToggleCase struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	// Tab は waiting-toggle.sh に渡すタブ名。
	Tab string `json:"tab"`
	// Input は pending ファイルの中身。
	Input string `json:"input"`
}

func loadGoldenToggleCases(t *testing.T) []goldenToggleCase {
	t.Helper()

	b, err := os.ReadFile(filepath.Join(goldenWaitingToggleDir, "cases.json"))
	if err != nil {
		t.Fatalf("cases.json が読めない: %v", err)
	}
	var cases []goldenToggleCase
	if err := json.Unmarshal(b, &cases); err != nil {
		t.Fatalf("cases.json の解釈に失敗: %v", err)
	}
	if len(cases) == 0 {
		t.Fatal("cases.json が空")
	}
	return cases
}

// frozenClock は fixture の生成時刻を返す時計である。
type frozenClock struct{ at time.Time }

func (c frozenClock) Now() time.Time { return c.at }

func TestGoldenWaitingToggleMatchesShellVersion(t *testing.T) {
	t.Parallel()

	const session = "golden"

	for _, tc := range loadGoldenToggleCases(t) {
		t.Run(tc.Name, func(t *testing.T) {
			t.Parallel()

			caseDir := filepath.Join(goldenWaitingToggleDir, tc.Name)
			wantRaw, err := os.ReadFile(filepath.Join(caseDir, "expected.json"))
			if err != nil {
				t.Fatalf("expected.json が読めない(scripts/gen-golden-waiting-toggle.sh で生成する): %v", err)
			}
			timeText, err := os.ReadFile(filepath.Join(caseDir, "time.txt"))
			if err != nil {
				t.Fatalf("time.txt が読めない: %v", err)
			}
			// Shell が date で決めた時刻をそのまま Go 版へ渡す。
			at, err := time.Parse("15:04:05", strings.TrimSpace(string(timeText)))
			if err != nil {
				t.Fatalf("time.txt の時刻を解釈できない: %v", err)
			}

			// 実行時と同じ依存グラフ(実ファイルを読み書きする store)を通す。
			root := t.TempDir()
			pendingRoot := store.PendingRoot(root)
			dir := filepath.Join(pendingRoot, session)
			if err := os.MkdirAll(dir, 0o755); err != nil {
				t.Fatalf("pending ディレクトリの作成に失敗: %v", err)
			}
			path := filepath.Join(dir, "a.json")
			if err := os.WriteFile(path, []byte(tc.Input), 0o600); err != nil {
				t.Fatalf("pending の作成に失敗: %v", err)
			}

			pending := store.NewPendingStore(pendingRoot)
			pane := &app.TaskControlPane{
				Raw:   pending,
				Clock: frozenClock{at: at},
			}
			if err := pane.ToggleWaiting(app.PaneEnv{ZellijSession: session}, tc.Tab); err != nil {
				t.Fatalf("ToggleWaiting() = %v", err)
			}

			gotRaw, err := os.ReadFile(path) //nolint:gosec // テストの一時ディレクトリ
			if err != nil {
				t.Fatalf("書き換え後の pending が読めない: %v", err)
			}
			if diff := jsonDiff(t, gotRaw, wantRaw); diff != "" {
				t.Errorf("%s: Shell 版の結果と一致しない\n%s", tc.Description, diff)
			}
		})
	}
}

// jsonDiff は 2 つの JSON が等価かを調べ、違えば説明を返す。
func jsonDiff(t *testing.T, got, want []byte) string {
	t.Helper()

	var gotValue, wantValue any
	if err := json.Unmarshal(got, &gotValue); err != nil {
		return "got が JSON として読めない: " + err.Error() + "\n" + string(got)
	}
	if err := json.Unmarshal(want, &wantValue); err != nil {
		return "want が JSON として読めない: " + err.Error() + "\n" + string(want)
	}
	if reflect.DeepEqual(gotValue, wantValue) {
		return ""
	}
	return "--- got ---\n" + string(got) + "--- want ---\n" + string(want)
}

// TestGoldenWaitingToggleIsReversible は 2 回続けて切り替えると
// 元の状態へ戻ることを確かめる。
//
// ゴールデンは 1 回ぶんの変換しか固定しないため、往復の不変条件は別に見る。
// prev_event の退避と復元がずれると、完了(Stop)のタスクが Waiting から
// 戻ったときに Notification になり、Done ではなく Dashboard に出てしまう。
func TestGoldenWaitingToggleIsReversible(t *testing.T) {
	t.Parallel()

	const session = "golden"
	const input = `{"tab":"t","session":"golden","event":"Stop","time":"10:00:00","agent":"claude"}`

	root := t.TempDir()
	pendingRoot := store.PendingRoot(root)
	dir := filepath.Join(pendingRoot, session)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("pending ディレクトリの作成に失敗: %v", err)
	}
	path := filepath.Join(dir, "a.json")
	if err := os.WriteFile(path, []byte(input), 0o600); err != nil {
		t.Fatalf("pending の作成に失敗: %v", err)
	}

	pending := store.NewPendingStore(pendingRoot)
	at, err := time.Parse("15:04:05", "11:22:33")
	if err != nil {
		t.Fatalf("時刻の解釈に失敗: %v", err)
	}
	pane := &app.TaskControlPane{Raw: pending, Clock: frozenClock{at: at}}
	env := app.PaneEnv{ZellijSession: session}

	for range 2 {
		if err := pane.ToggleWaiting(env, "t"); err != nil {
			t.Fatalf("ToggleWaiting() = %v", err)
		}
	}

	got, err := os.ReadFile(path) //nolint:gosec // テストの一時ディレクトリ
	if err != nil {
		t.Fatalf("読み直しに失敗: %v", err)
	}
	// time だけは進んでいる(現行版も毎回書き換える)。
	want := `{"tab":"t","session":"golden","event":"Stop","time":"11:22:33","agent":"claude"}`
	if diff := jsonDiff(t, got, []byte(want)); diff != "" {
		t.Errorf("往復で元へ戻らない\n%s", diff)
	}
}
