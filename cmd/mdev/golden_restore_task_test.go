package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/k-kudo-hub/mdev-go/internal/app"
	"github.com/k-kudo-hub/mdev-go/internal/infra/store"
)

// Done からのタスク復元のゴールデンテスト。
//
// testdata/golden-restore-task/cases.json が入力(daily ログ 1 本)を定義し、
// <case>/ には現行 Shell 版の restore-task.sh に同じ入力を与えて生成させた
//
//	exit.txt    … 終了コード(0-5 の契約)
//	daily.jsonl … 実行後の daily ログ
//	zellij.log  … 実行された zellij コマンドの並び
//
// が入っている。fixture の作り方は scripts/gen-golden-restore-task.sh を参照
// (このテストは fixture を読むだけで、Shell 版には依存しない)。
//
// 比較は 3 つとも行う。daily は行ごとの JSON 等価(Go 版は触っていない行を
// バイト列のまま残すため、jq -c で出し直す Shell 版とは表記が揃わない。
// evidence §5-2)、zellij 呼び出しは行の完全一致である。
//
// 環境ごとに変わる文字列は両側で同じ印に潰してある。
//
//	<サンドボックス>              -> {SANDBOX}
//	-- bash <...>/task-control.sh -> -- {TASK_CONTROL}
//
// 後者はタスクタブの操作バーを何で起動するかの差(フェーズ 3 で決めた既知の
// 差異)で、復元の契約とは関係が無い。

const goldenRestoreTaskDir = "testdata/golden-restore-task"

// sandboxMark は環境ごとに変わるパスの印。
const sandboxMark = "{SANDBOX}"

// taskControlMark は操作バーの起動コマンドの印。
const taskControlMark = "{TASK_CONTROL}"

// goldenRestoreCase は 1 件の入力定義。cases.json の 1 要素に対応する。
type goldenRestoreCase struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	// KnownDifference が非空の case は Shell と結果が違うことが分かっている。
	// zellij 呼び出しの比較は行わず、代わりに差異そのものを確かめる。
	KnownDifference string `json:"known_difference"`
	Tab             string `json:"tab"`
	Session         string `json:"session"`
	CompletedAt     string `json:"completed_at"`
	// Daily は実行前に置く daily ログの中身({SANDBOX} を含む)。
	Daily string `json:"daily"`
}

func loadGoldenRestoreCases(t *testing.T) []goldenRestoreCase {
	t.Helper()

	b, err := os.ReadFile(filepath.Join(goldenRestoreTaskDir, "cases.json"))
	if err != nil {
		t.Fatalf("cases.json が読めない: %v", err)
	}
	var cases []goldenRestoreCase
	if err := json.Unmarshal(b, &cases); err != nil {
		t.Fatalf("cases.json の解釈に失敗: %v", err)
	}
	if len(cases) == 0 {
		t.Fatal("cases.json が空")
	}
	return cases
}

// recordingTabActor は zellij のスタブと同じ形で呼び出しを記録する。
//
// 記録の書式は gen-golden-restore-task.sh のスタブ(`action` を落として
// 残りを空白で連結する)に合わせてある。
type recordingTabActor struct {
	calls []string
	tabs  []string
}

func (a *recordingTabActor) add(parts ...string) {
	a.calls = append(a.calls, strings.Join(parts, " "))
}

func (a *recordingTabActor) QueryTabNames(time.Duration) []string {
	a.add("query-tab-names")
	return a.tabs
}

func (a *recordingTabActor) FocusTabVerified(_ time.Duration, name string) bool {
	a.add("go-to-tab-name", name)
	// zellij はヒットしたときだけ index を出す。スタブと同じ判定にする。
	for _, tab := range a.tabs {
		if tab == name {
			return true
		}
	}
	return false
}

func (a *recordingTabActor) NewTab(_ time.Duration, name, cwd string, command []string) error {
	a.add(append([]string{"new-tab", "-n", name, "--cwd", cwd}, dashed(command)...)...)
	a.tabs = append(a.tabs, name)
	return nil
}

func (a *recordingTabActor) NewPane(_ time.Duration, direction, cwd string, command []string) error {
	a.add(append([]string{"new-pane", "--direction", direction, "--cwd", cwd}, dashed(command)...)...)
	return nil
}

func (a *recordingTabActor) MoveFocus(_ time.Duration, direction string) error {
	a.add("move-focus", direction)
	return nil
}

func (a *recordingTabActor) FocusPreviousPane(time.Duration) error {
	a.add("focus-previous-pane")
	return nil
}

func (a *recordingTabActor) Resize(_ time.Duration, args ...string) error {
	a.add(append([]string{"resize"}, args...)...)
	return nil
}

// dashed は command が空でなければ先頭へ `--` を付ける。
func dashed(command []string) []string {
	if len(command) == 0 {
		return nil
	}
	return append([]string{"--"}, command...)
}

// markLauncher は操作バーの起動コマンドを印に置き換える。
// Shell 版は task-control.sh、Go 版は `mdev pane task-control` を使うため、
// この 1 行だけは両側で同じ印へ潰して比較する。
type markLauncher struct{}

func (markLauncher) TaskControlCommand(tab string) []string {
	return []string{taskControlMark, tab}
}

// restoreExitCode は TaskRestorer のエラーを現行版の終了コードへ写す。
func restoreExitCode(err error) int {
	switch {
	case err == nil:
		return 0
	case errors.Is(err, app.ErrRestoreEntryNotFound):
		return 1
	case errors.Is(err, app.ErrRestoreDirUnknown):
		return 2
	case errors.Is(err, app.ErrRestoreDirMissing):
		return 3
	case errors.Is(err, app.ErrRestoreTabFailed):
		return 4
	case errors.Is(err, app.ErrRestoreDailyUpdate):
		return 5
	default:
		return -1
	}
}

// goldenRestoreResult は 1 件を Go 版で走らせた結果である。
type goldenRestoreResult struct {
	exit  int
	daily string
	calls []string
}

// runGoldenRestore は case の入力を組み立てて Go 版の復元を 1 回走らせる。
func runGoldenRestore(t *testing.T, tc goldenRestoreCase) goldenRestoreResult {
	t.Helper()

	sandbox := t.TempDir()
	conductorHome := filepath.Join(sandbox, "conductor")
	for _, dir := range []string{"proj", "proj2"} {
		if err := os.MkdirAll(filepath.Join(sandbox, dir), 0o755); err != nil {
			t.Fatalf("作業ディレクトリの作成に失敗: %v", err)
		}
	}
	if err := os.WriteFile(filepath.Join(sandbox, "transcript.jsonl"), []byte("{}\n"), 0o600); err != nil {
		t.Fatalf("transcript の作成に失敗: %v", err)
	}

	// 設定は fixture と同じものを使う(gen スクリプトが持ち込んだ現行の既定値)。
	config, err := os.ReadFile(filepath.Join(goldenRestoreTaskDir, "config.default.json"))
	if err != nil {
		t.Fatalf("config.default.json が読めない: %v", err)
	}
	if err := os.MkdirAll(conductorHome, 0o755); err != nil {
		t.Fatalf("CONDUCTOR_HOME の作成に失敗: %v", err)
	}
	if err := os.WriteFile(filepath.Join(conductorHome, "config.default.json"), config, 0o600); err != nil {
		t.Fatalf("設定の配置に失敗: %v", err)
	}

	date := tc.CompletedAt[:10]
	dailyDir := filepath.Join(store.DailyRoot(conductorHome), tc.Session)
	if err := os.MkdirAll(dailyDir, 0o755); err != nil {
		t.Fatalf("daily ディレクトリの作成に失敗: %v", err)
	}
	dailyPath := filepath.Join(dailyDir, date+".jsonl")
	// gen スクリプトはコマンド置換で末尾の改行を落としてから書き込むため、
	// こちらも同じ形にする。
	daily := strings.TrimRight(strings.ReplaceAll(tc.Daily, sandboxMark, sandbox), "\n")
	if err := os.WriteFile(dailyPath, []byte(daily), 0o600); err != nil {
		t.Fatalf("daily ログの作成に失敗: %v", err)
	}

	// 実行時と同じ依存グラフ(実ファイルを読み書きする store と、
	// タスク作成をそのまま再利用する TaskCreator)を通す。
	paneStore := store.NewPaneStore(store.PendingRoot(sandbox), conductorHome)
	tabs := &recordingTabActor{}
	restorer := &app.TaskRestorer{
		Daily: store.NewDailyStore(store.DailyRoot(conductorHome), nil),
		Creator: &app.TaskCreator{
			Tabs:        tabs,
			ScreenState: paneStore,
			Config:      paneStore,
			Clock:       goldenClock{now: time.Date(2026, 8, 11, 10, 0, 0, 0, time.UTC)},
			Sleeper:     noSleep{},
			Launcher:    markLauncher{},
		},
		Paths: paneStore,
	}

	_, err = restorer.Restore(app.PaneEnv{ZellijSession: tc.Session}, tc.Tab, tc.Session, tc.CompletedAt)

	got, readErr := os.ReadFile(dailyPath) //nolint:gosec // テストの一時ディレクトリ
	if readErr != nil {
		t.Fatalf("daily ログの読み直しに失敗: %v", readErr)
	}
	calls := make([]string, 0, len(tabs.calls))
	for _, call := range tabs.calls {
		calls = append(calls, strings.ReplaceAll(call, sandbox, sandboxMark))
	}
	return goldenRestoreResult{
		exit:  restoreExitCode(err),
		daily: strings.ReplaceAll(string(got), sandbox, sandboxMark),
		calls: calls,
	}
}

func TestGoldenRestoreTaskMatchesShellVersion(t *testing.T) {
	t.Parallel()

	for _, tc := range loadGoldenRestoreCases(t) {
		t.Run(tc.Name, func(t *testing.T) {
			t.Parallel()

			caseDir := filepath.Join(goldenRestoreTaskDir, tc.Name)
			got := runGoldenRestore(t, tc)

			if want := readGoldenExit(t, caseDir); got.exit != want {
				t.Errorf("%s: 終了コード = %d, want %d", tc.Description, got.exit, want)
			}
			compareGoldenDaily(t, caseDir, got.daily, tc.Description)

			if tc.KnownDifference != "" {
				// 既知の差異がある case では呼び出し列を比べない。差異そのものは
				// この後の TestGoldenRestoreTaskFixesScreenSessionResume が固定する。
				return
			}
			wantCalls := readGoldenLines(t, filepath.Join(caseDir, "zellij.log"))
			if !reflect.DeepEqual(got.calls, wantCalls) {
				t.Errorf("%s: zellij の呼び出しが一致しない\n--- got ---\n%s\n--- want ---\n%s",
					tc.Description, strings.Join(got.calls, "\n"), strings.Join(wantCalls, "\n"))
			}
		})
	}
}

// TestGoldenRestoreTaskFixesScreenSessionResume は、スクリーン検出が合成した
// セッション ID で再開してしまう現行のバグを Go 版が直していることを固定する。
//
// fixture(Shell 版の実行結果)には
// `codex resume screen-cx_screen-123456789` が残っている。存在しない ID なので
// codex は起動時に失敗する。Go 版は resume に使わず新規セッションで起動する
// (evidence §5-1)。daily への restored の付き方は同じなので、ゴールデンでは
// 呼び出し列だけを外し、その差をここで確かめる。
func TestGoldenRestoreTaskFixesScreenSessionResume(t *testing.T) {
	t.Parallel()

	const name = "screen-session-id"
	var target goldenRestoreCase
	for _, tc := range loadGoldenRestoreCases(t) {
		if tc.Name == name {
			target = tc
		}
	}
	if target.Name == "" {
		t.Fatalf("cases.json に %s がない", name)
	}

	// Shell 版は合成 ID をそのまま渡していた(fixture が証拠)。
	shell := strings.Join(readGoldenLines(t, filepath.Join(goldenRestoreTaskDir, name, "zellij.log")), "\n")
	if !strings.Contains(shell, "codex resume screen-") {
		t.Fatalf("fixture が現行のバグを写していない:\n%s", shell)
	}

	got := runGoldenRestore(t, target)
	joined := strings.Join(got.calls, "\n")
	if strings.Contains(joined, "screen-") {
		t.Errorf("合成セッション ID を渡してしまっている:\n%s", joined)
	}
	if !strings.Contains(joined, "TASK_AGENT=codex codex\n") &&
		!strings.HasSuffix(got.calls[0], "TASK_AGENT=codex codex") {
		t.Errorf("新規セッションで起動していない:\n%s", joined)
	}
}

// readGoldenExit は exit.txt を読む。
func readGoldenExit(t *testing.T, caseDir string) int {
	t.Helper()

	b, err := os.ReadFile(filepath.Join(caseDir, "exit.txt"))
	if err != nil {
		t.Fatalf("exit.txt が読めない(scripts/gen-golden-restore-task.sh で生成する): %v", err)
	}
	code, err := strconv.Atoi(strings.TrimSpace(string(b)))
	if err != nil {
		t.Fatalf("exit.txt を数値として読めない: %v", err)
	}
	return code
}

// readGoldenLines はファイルを空行を除いた行の並びとして読む。
func readGoldenLines(t *testing.T, path string) []string {
	t.Helper()

	b, err := os.ReadFile(path) //nolint:gosec // testdata 配下の固定パス
	if err != nil {
		t.Fatalf("%s が読めない(scripts/gen-golden-restore-task.sh で生成する): %v", path, err)
	}
	lines := []string{}
	for _, line := range strings.Split(string(b), "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		lines = append(lines, line)
	}
	return lines
}

// compareGoldenDaily は daily ログを行ごとの JSON 等価で比べる。
//
// バイト列で比べないのは、Go 版が触っていない行を読んだままの表記で残すのに
// 対し、Shell 版は jq -c でファイル全体を出し直すためである(evidence §5-2)。
// 行の並びと、各行のキー・値が一致することを見る。
func compareGoldenDaily(t *testing.T, caseDir, got, description string) {
	t.Helper()

	gotLines := splitJSONLines(got)
	wantLines := splitJSONLines(readGoldenFile(t, filepath.Join(caseDir, "daily.jsonl")))
	if len(gotLines) != len(wantLines) {
		t.Fatalf("%s: daily の行数 = %d, want %d\n--- got ---\n%s", description,
			len(gotLines), len(wantLines), got)
	}
	for i := range gotLines {
		gotValue, err := decodeJSONLine(gotLines[i])
		if err != nil {
			t.Fatalf("%s: daily %d 行目を JSON として読めない: %v", description, i+1, err)
		}
		wantValue, err := decodeJSONLine(wantLines[i])
		if err != nil {
			t.Fatalf("%s: fixture %d 行目を JSON として読めない: %v", description, i+1, err)
		}
		if !reflect.DeepEqual(gotValue, wantValue) {
			t.Errorf("%s: daily %d 行目が一致しない\n--- got ---\n%s\n--- want ---\n%s",
				description, i+1, gotLines[i], wantLines[i])
		}
	}
}

// readGoldenFile はファイルの中身をそのまま返す。
func readGoldenFile(t *testing.T, path string) string {
	t.Helper()

	b, err := os.ReadFile(path) //nolint:gosec // testdata 配下の固定パス
	if err != nil {
		t.Fatalf("%s が読めない(scripts/gen-golden-restore-task.sh で生成する): %v", path, err)
	}
	return string(b)
}

// splitJSONLines は空行を除いた行の並びを返す。
func splitJSONLines(content string) []string {
	lines := []string{}
	for _, line := range strings.Split(content, "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		lines = append(lines, line)
	}
	return lines
}

// decodeJSONLine は 1 行を JSON の値として読む。
func decodeJSONLine(line string) (any, error) {
	var value any
	if err := json.Unmarshal([]byte(line), &value); err != nil {
		return nil, fmt.Errorf("JSON として読めません: %w", err)
	}
	return value, nil
}
