package store_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/k-kudo-hub/mdev-go/internal/app"
	"github.com/k-kudo-hub/mdev-go/internal/infra/codex"
	"github.com/k-kudo-hub/mdev-go/internal/infra/store"
)

// codex notify のゴールデンテスト。
//
// testdata/golden-codex/cases.json が入力(payload・環境変数・実行前から
// 置いてある pending・会話ログ)を定義し、
// testdata/golden-codex/<case>/expected/ には現行 Shell 版
// (codex-notify.sh)に同じ入力を与えて書かせた実ファイルが入っている。
// fixture の作り方は scripts/gen-golden-codex.sh を参照(このテストは
// fixture を読むだけで、Shell 版には依存しない)。
//
// 比較はファイルの集合・JSON としての内容・**キーの並び順**で行う。
// 体裁(現行版は jq で整形して書き、こちらは 1 行で書く)は比較しない。
//
// 並び順まで見るのは、pending を人が直接覗くことがあるためである。省略される
// キー(transcript_path / dir / task_type)の位置がずれると、Shell 版が書いた
// ファイルと Go 版が書いたファイルが並んだときに読み比べにくくなる。値の
// 一致だけなら並び順は関係ないが、揃えられるものを揃えない理由も無い。
const goldenCodexDir = "testdata/golden-codex"

// codexHomePlaceholder は会話ログのパスに書いてある差し込み位置。
// 実行のたびに変わる絶対パスを fixture に残さないための仕組みで、
// scripts/gen-golden-codex.sh と同じ文字列を使う。
const codexHomePlaceholder = "{{CODEX_HOME}}"

// goldenCodexCase は 1 件の入力定義。cases.json の 1 要素に対応する。
type goldenCodexCase struct {
	Name        string            `json:"name"`
	Description string            `json:"description"`
	Payload     json.RawMessage   `json:"payload"`
	Env         map[string]string `json:"env"`
	// Pending のキーは <セッション名>/<スレッド ID>.json。
	Pending map[string]string `json:"pending"`
	// Rollouts のキーは CODEX_HOME/sessions からの相対パス。
	Rollouts map[string]string `json:"rollouts"`
}

func loadGoldenCodexCases(t *testing.T) []goldenCodexCase {
	t.Helper()

	b, err := os.ReadFile(filepath.Join(goldenCodexDir, "cases.json"))
	if err != nil {
		t.Fatalf("cases.json が読めない: %v", err)
	}
	var cases []goldenCodexCase
	if err := json.Unmarshal(b, &cases); err != nil {
		t.Fatalf("cases.json の解釈に失敗: %v", err)
	}
	if len(cases) == 0 {
		t.Fatal("cases.json が空")
	}
	return cases
}

func TestGoldenCodexNotifyCompatibilityWithShellVersion(t *testing.T) {
	t.Parallel()

	for _, tc := range loadGoldenCodexCases(t) {
		t.Run(tc.Name, func(t *testing.T) {
			t.Parallel()

			expected := filepath.Join(goldenCodexDir, tc.Name, "expected")
			if _, err := os.Stat(filepath.Dir(expected)); err != nil {
				t.Fatalf("fixture が無い(scripts/gen-golden-codex.sh で生成する): %v", err)
			}

			root := t.TempDir()
			pendingRoot := filepath.Join(root, "pending")
			conductorHome := filepath.Join(root, "conductor")
			codexHome := filepath.Join(root, "codex")

			for rel, content := range tc.Pending {
				writeFixtureFile(t, filepath.Join(pendingRoot, filepath.FromSlash(rel)), content)
			}
			for rel, content := range tc.Rollouts {
				writeFixtureFile(t,
					filepath.Join(codexHome, "sessions", filepath.FromSlash(rel)), content)
			}

			locator := codex.NewLocator(codexHome, "")
			notifier := &app.CodexNotifier{
				Pending:    store.NewPendingStore(pendingRoot),
				Registry:   store.NewRegistryStore(store.RegistryRoot(conductorHome)),
				Transcript: locator,
				Clock:      codexClockFromFixture(t, expected),
			}
			env := app.HookEnv{
				ZellijSession: tc.Env["ZELLIJ_SESSION_NAME"],
				TaskTabName:   tc.Env["TASK_TAB_NAME"],
				TaskType:      tc.Env["TASK_TYPE"],
				TaskAgent:     tc.Env["TASK_AGENT"],
			}

			if err := notifier.Notify(tc.Payload, env); err != nil {
				t.Fatalf("Notify = %v", err)
			}

			compareCodexTree(t, filepath.Join(expected, "pending"), pendingRoot, codexHome)
			compareCodexTree(t,
				filepath.Join(expected, "registry"), store.RegistryRoot(conductorHome), codexHome)
		})
	}
}

// compareCodexTree は fixture のファイル集合と中身を実行結果と突き合わせる。
//
// want が無い(現行版が何も書かなかった)case では、こちらも何も書いていない
// ことを確かめる。書きすぎの検出がこのテストの主眼の 1 つで、たとえば
// Waiting を上書きしてしまう回帰はここに出る。
func compareCodexTree(t *testing.T, wantDir, gotDir, codexHome string) {
	t.Helper()

	want := readCodexTree(t, wantDir)
	got := readCodexTree(t, gotDir)

	if len(want) != len(got) {
		t.Errorf("ファイル数が違う: got %v, want %v", codexTreeKeys(got), codexTreeKeys(want))
	}
	for rel, wantBody := range want {
		gotBody, ok := got[rel]
		if !ok {
			t.Errorf("%s が書かれていない", rel)
			continue
		}
		// fixture 側のプレースホルダを、この実行の CODEX_HOME へ戻して比べる。
		wantBody = strings.ReplaceAll(wantBody, codexHomePlaceholder, codexHome)

		if !reflect.DeepEqual(decodeJSON(t, gotBody), decodeJSON(t, wantBody)) {
			t.Errorf("%s の内容が違う\n  got:  %s\n  want: %s", rel, gotBody, wantBody)
		}
		if got, want := jsonKeyOrder(t, gotBody), jsonKeyOrder(t, wantBody); !reflect.DeepEqual(got, want) {
			t.Errorf("%s のキーの並びが違う\n  got:  %v\n  want: %v", rel, got, want)
		}
	}
	for rel := range got {
		if _, ok := want[rel]; !ok {
			t.Errorf("現行版が書かない %s を書いた:\n%s", rel, got[rel])
		}
	}
}

// readCodexTree は dir 配下のファイルを相対パスと中身の対応で返す。
// dir が無ければ空を返す。
func readCodexTree(t *testing.T, dir string) map[string]string {
	t.Helper()

	tree := map[string]string{}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return tree
	}
	for _, entry := range entries {
		path := filepath.Join(dir, entry.Name())
		if entry.IsDir() {
			for rel, body := range readCodexTree(t, path) {
				tree[filepath.Join(entry.Name(), rel)] = body
			}
			continue
		}
		b, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("%s が読めない: %v", path, err)
		}
		tree[entry.Name()] = string(b)
	}
	return tree
}

// codexTreeKeys は失敗時の表示用に相対パスを並べて返す。
func codexTreeKeys(tree map[string]string) []string {
	keys := make([]string, 0, len(tree))
	for k := range tree {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// codexClockFromFixture は fixture に書かれた時刻を返す時計を組み立てる。
//
// 現行版は実行時の date を書くため、Go 側は fixture から時刻を復元して
// 同じ値になるようにする。レジストリの updated_at は日付ごと入っているので
// これを第一に使い、無ければ pending の time(時分秒のみ)を使う。
func codexClockFromFixture(t *testing.T, expected string) app.Clock {
	t.Helper()

	if at, ok := codexTimeFromRegistry(t, filepath.Join(expected, "registry")); ok {
		return fixedClock{now: at}
	}
	if at, ok := codexTimeFromPending(t, filepath.Join(expected, "pending")); ok {
		return fixedClock{now: at}
	}
	// 何も書かれない case では時刻が観測されない。
	return fixedClock{now: time.Time{}}
}

// codexTimeFromRegistry はレジストリの updated_at から時刻を読む。
func codexTimeFromRegistry(t *testing.T, dir string) (time.Time, bool) {
	t.Helper()

	for _, body := range readCodexTree(t, dir) {
		var entry struct {
			UpdatedAt string `json:"updated_at"`
		}
		if err := json.Unmarshal([]byte(body), &entry); err != nil || entry.UpdatedAt == "" {
			continue
		}
		at, err := time.Parse("2006-01-02T15:04:05-0700", entry.UpdatedAt)
		if err != nil {
			t.Fatalf("updated_at を解釈できない: %v", err)
		}
		return at, true
	}
	return time.Time{}, false
}

// codexTimeFromPending は pending の time から時刻を読む。
//
// 日付は入っていないが、pending が持つのは時分秒だけなので、比較には十分で
// ある(日付の入るレジストリが書かれない case でのみ使う)。
func codexTimeFromPending(t *testing.T, dir string) (time.Time, bool) {
	t.Helper()

	for _, body := range readCodexTree(t, dir) {
		var pending struct {
			Time string `json:"time"`
		}
		if err := json.Unmarshal([]byte(body), &pending); err != nil || pending.Time == "" {
			continue
		}
		at, err := time.Parse("15:04:05", pending.Time)
		if err != nil {
			t.Fatalf("time を解釈できない: %v", err)
		}
		return at, true
	}
	return time.Time{}, false
}

// decodeJSON は body を JSON として読む。読めなければテストを止める。
func decodeJSON(t *testing.T, body string) map[string]any {
	t.Helper()

	var fields map[string]any
	if err := json.Unmarshal([]byte(body), &fields); err != nil {
		t.Fatalf("JSON として読めない: %v\n%s", err, body)
	}
	return fields
}

// jsonKeyOrder は最上位オブジェクトのキーを書かれている順に返す。
func jsonKeyOrder(t *testing.T, body string) []string {
	t.Helper()

	dec := json.NewDecoder(strings.NewReader(body))
	if tok, err := dec.Token(); err != nil || tok != json.Delim('{') {
		t.Fatalf("オブジェクトで始まっていない: %v\n%s", err, body)
	}

	var keys []string
	for dec.More() {
		tok, err := dec.Token()
		if err != nil {
			t.Fatalf("キーを読めない: %v\n%s", err, body)
		}
		key, ok := tok.(string)
		if !ok {
			t.Fatalf("キーが文字列でない: %v\n%s", tok, body)
		}
		keys = append(keys, key)
		// 値は読み飛ばす(入れ子は無いが、あっても 1 回で読み切れる)。
		var discard json.RawMessage
		if err := dec.Decode(&discard); err != nil {
			t.Fatalf("値を読めない: %v\n%s", err, body)
		}
	}
	return keys
}
