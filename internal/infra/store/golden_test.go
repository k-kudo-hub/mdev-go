package store_test

import (
	"encoding/json"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/k-kudo-hub/mdev-go/internal/app"
	"github.com/k-kudo-hub/mdev-go/internal/domain"
	"github.com/k-kudo-hub/mdev-go/internal/infra/store"
)

// ゴールデンテスト。
//
// testdata/golden/cases.json が入力(標準入力・環境変数・実行前のファイル)を
// 定義し、testdata/golden/<case>/expected/ には現行 Shell 版に同じ入力を与えて
// 生成させた実ファイルが入っている。fixture の作り方は scripts/gen-golden.sh を
// 参照(このテストは fixture を読むだけで、Shell 版には依存しない)。
//
// 比較は JSON としての等価(キー集合と値)で行い、キーの並び順は問わない。

const goldenDir = "testdata/golden"

// goldenCase は 1 件の入力定義。cases.json の 1 要素に対応する。
type goldenCase struct {
	Name        string            `json:"name"`
	Description string            `json:"description"`
	Hook        string            `json:"hook"`
	Stdin       string            `json:"stdin"`
	Env         map[string]string `json:"env"`
	// Pre は実行前に置いておくファイル。キーは pending/ または tasks/ から
	// 始まる相対パス。
	Pre map[string]string `json:"pre"`
}

// zellijCallsFile は Shell 版が実行した zellij コマンドの記録。
const zellijCallsFile = "zellij-calls.txt"

func loadGoldenCases(t *testing.T) []goldenCase {
	t.Helper()

	b, err := os.ReadFile(filepath.Join(goldenDir, "cases.json"))
	if err != nil {
		t.Fatalf("cases.json が読めない: %v", err)
	}
	var cases []goldenCase
	if err := json.Unmarshal(b, &cases); err != nil {
		t.Fatalf("cases.json の解釈に失敗: %v", err)
	}
	if len(cases) == 0 {
		t.Fatal("cases.json が空")
	}
	return cases
}

// recordingFocuser は zellij のフォーカス移動を記録する。
type recordingFocuser struct {
	calls []string
}

func (f *recordingFocuser) FocusTab(name string) error {
	f.calls = append(f.calls, "go-to-tab-name "+name)
	return nil
}

// fixedClock は fixture から復元した時刻を返す。
type fixedClock struct {
	now time.Time
}

func (c fixedClock) Now() time.Time { return c.now }

func TestGoldenCompatibilityWithShellVersion(t *testing.T) {
	t.Parallel()

	for _, tc := range loadGoldenCases(t) {
		t.Run(tc.Name, func(t *testing.T) {
			t.Parallel()

			expectedDir := filepath.Join(goldenDir, tc.Name, "expected")
			if _, err := os.Stat(expectedDir); err != nil {
				t.Fatalf("fixture が無い(scripts/gen-golden.sh で生成する): %v", err)
			}

			// 実行前の状態を用意する。
			root := t.TempDir()
			pendingRoot := filepath.Join(root, "pending")
			tasksRoot := filepath.Join(root, "tasks")
			for rel, content := range tc.Pre {
				writeFixtureFile(t, filepath.Join(root, filepath.FromSlash(rel)), content)
			}

			focuser := &recordingFocuser{}
			handler := &app.HookHandler{
				Pending:  store.NewPendingStore(pendingRoot),
				Registry: store.NewRegistryStore(tasksRoot),
				Focuser:  focuser,
				Clock:    fixedClock{now: clockFromFixture(t, expectedDir)},
			}

			env := app.HookEnv{
				ZellijSession: tc.Env["ZELLIJ_SESSION_NAME"],
				TaskTabName:   tc.Env["TASK_TAB_NAME"],
				TaskType:      tc.Env["TASK_TYPE"],
				TaskAgent:     tc.Env["TASK_AGENT"],
			}
			runGoldenHook(t, handler, tc, env)

			compareJSONTree(t, filepath.Join(expectedDir, "pending"), pendingRoot)
			compareJSONTree(t, filepath.Join(expectedDir, "tasks"), tasksRoot)
			compareZellijCalls(t, expectedDir, focuser.calls)
		})
	}
}

// runGoldenHook は case が指定する hook を実行する。
func runGoldenHook(t *testing.T, handler *app.HookHandler, tc goldenCase, env app.HookEnv) {
	t.Helper()

	raw := []byte(tc.Stdin)
	var err error
	switch tc.Hook {
	case "notify":
		err = handler.HandleNotify(raw, env)
	case "post-tool":
		err = handler.HandlePostTool(raw, env)
	case "resolve":
		err = handler.HandleResolve(raw, env)
	default:
		t.Fatalf("未知の hook: %q", tc.Hook)
	}
	if err != nil {
		t.Fatalf("%s hook = %v", tc.Hook, err)
	}
}

// writeFixtureFile は親ディレクトリを作って content を書き出す。
func writeFixtureFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("MkdirAll(%s) = %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("WriteFile(%s) = %v", path, err)
	}
}

// clockFromFixture は fixture が記録している時刻を復元する。
//
// Shell 版はレジストリに `%Y-%m-%dT%H:%M:%S%z`、pending に `%H:%M:%S` を書く。
// 前者があればそれを、無ければ後者を使う。どちらも無い場合(出力が無い case)は
// 時刻が結果に影響しないため任意の値でよい。
func clockFromFixture(t *testing.T, expectedDir string) time.Time {
	t.Helper()

	if entry := firstJSONField(t, filepath.Join(expectedDir, "tasks"), "updated_at"); entry != "" {
		parsed, err := time.Parse(domain.RegistryUpdatedAtLayout, entry)
		if err != nil {
			t.Fatalf("updated_at %q の解釈に失敗: %v", entry, err)
		}
		return parsed
	}
	if entry := firstJSONField(t, filepath.Join(expectedDir, "pending"), "time"); entry != "" {
		parsed, err := time.Parse(domain.PendingTimeLayout, entry)
		if err != nil {
			t.Fatalf("time %q の解釈に失敗: %v", entry, err)
		}
		return parsed
	}
	return time.Unix(0, 0).UTC()
}

// firstJSONField は dir 配下の最初の JSON ファイルから key の値を返す。
func firstJSONField(t *testing.T, dir, key string) string {
	t.Helper()

	for _, path := range jsonFilesUnder(t, dir) {
		b, err := os.ReadFile(path) //nolint:gosec // testdata 配下の列挙結果
		if err != nil {
			t.Fatalf("ReadFile(%s) = %v", path, err)
		}
		var fields map[string]any
		if err := json.Unmarshal(b, &fields); err != nil {
			continue
		}
		if value, ok := fields[key].(string); ok {
			return value
		}
	}
	return ""
}

// jsonFilesUnder は dir 配下の .json ファイルを相対パスの昇順で返す。
// dir が無い場合は空を返す。
func jsonFilesUnder(t *testing.T, dir string) []string {
	t.Helper()

	var paths []string
	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !strings.HasSuffix(d.Name(), ".json") {
			return nil
		}
		paths = append(paths, path)
		return nil
	})
	if err != nil && !os.IsNotExist(err) {
		t.Fatalf("WalkDir(%s) = %v", dir, err)
	}
	sort.Strings(paths)
	return paths
}

// compareJSONTree は expected と actual の JSON ファイル群を突き合わせる。
//
// 比較するのはファイルの集合と、各ファイルの JSON としての内容である。
// キーの並び順と空ディレクトリの有無は比較しない(現行版は session_id の
// 検証前に pending ディレクトリを作るため、空ディレクトリだけが残る場合がある)。
func compareJSONTree(t *testing.T, expectedRoot, actualRoot string) {
	t.Helper()

	expected := readJSONTree(t, expectedRoot)
	actual := readJSONTree(t, actualRoot)

	for rel, want := range expected {
		got, ok := actual[rel]
		if !ok {
			t.Errorf("%s が生成されていない", rel)
			continue
		}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("%s の内容が違う\n  got:  %v\n  want: %v", rel, got, want)
		}
	}
	for rel := range actual {
		if _, ok := expected[rel]; !ok {
			t.Errorf("Shell 版が作らないファイルが生成された: %s (%v)", rel, actual[rel])
		}
	}
}

// readJSONTree は root 配下の JSON ファイルを相対パスをキーにして読む。
func readJSONTree(t *testing.T, root string) map[string]map[string]any {
	t.Helper()

	tree := map[string]map[string]any{}
	for _, path := range jsonFilesUnder(t, root) {
		rel, err := filepath.Rel(root, path)
		if err != nil {
			t.Fatalf("Rel(%s, %s) = %v", root, path, err)
		}
		b, err := os.ReadFile(path) //nolint:gosec // 列挙結果のパス
		if err != nil {
			t.Fatalf("ReadFile(%s) = %v", path, err)
		}
		var fields map[string]any
		if err := json.Unmarshal(b, &fields); err != nil {
			// 壊れた fixture(実行前状態としてわざと壊してあるもの)は
			// 生の内容で比較する。
			tree[filepath.ToSlash(rel)] = map[string]any{"__raw__": string(b)}
			continue
		}
		tree[filepath.ToSlash(rel)] = fields
	}
	return tree
}

// compareZellijCalls は zellij に対する副作用が一致するかを見る。
func compareZellijCalls(t *testing.T, expectedDir string, got []string) {
	t.Helper()

	b, err := os.ReadFile(filepath.Join(expectedDir, zellijCallsFile))
	if err != nil {
		t.Fatalf("%s が読めない: %v", zellijCallsFile, err)
	}
	want := splitLines(string(b))

	if !reflect.DeepEqual(splitLines(strings.Join(got, "\n")), want) {
		t.Errorf("zellij の呼び出し = %v, want %v", got, want)
	}
}

// splitLines は空行を除いた行の一覧を返す。
func splitLines(s string) []string {
	lines := []string{}
	for _, line := range strings.Split(s, "\n") {
		if trimmed := strings.TrimSpace(line); trimmed != "" {
			lines = append(lines, trimmed)
		}
	}
	return lines
}
