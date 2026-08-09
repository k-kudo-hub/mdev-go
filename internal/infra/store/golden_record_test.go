package store_test

import (
	"encoding/json"
	"io"
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

// record-output のゴールデンテスト。
//
// testdata/golden-record/cases.json が入力(タブ名・環境変数・設定・pending・
// transcript)を定義し、testdata/golden-record/<case>/expected/daily/ には現行
// Shell 版に同じ入力を与えて追記させた実ファイルが入っている。fixture の作り方は
// scripts/gen-golden-record.sh を参照(このテストは fixture を読むだけで、
// Shell 版には依存しない)。
//
// 比較は JSON としての等価で行う。数値は値として比べるため、表記の差
// (0.18 と 1.8e-01 など)は許容し、値は完全に一致する必要がある。

const goldenRecordDir = "testdata/golden-record"

// transcriptDirPlaceholder は pending の transcript_path に書いてある差し込み位置。
// 実行のたびに変わる絶対パスを fixture に残さないための仕組みで、
// scripts/gen-golden-record.sh と同じ文字列を使う。
const transcriptDirPlaceholder = "{{TRANSCRIPT_DIR}}"

// goldenRecordCase は 1 件の入力定義。cases.json の 1 要素に対応する。
type goldenRecordCase struct {
	Name        string            `json:"name"`
	Description string            `json:"description"`
	Tab         string            `json:"tab"`
	Env         map[string]string `json:"env"`
	// Config / ConfigDefault は CONDUCTOR_HOME に置く設定ファイルの中身。
	Config        string `json:"config"`
	ConfigDefault string `json:"config_default"`
	// Pending のキーは <セッション名>/<セッション ID>.json。
	Pending map[string]string `json:"pending"`
	// Transcripts のキーは transcript ディレクトリからの相対パス。
	Transcripts map[string]string `json:"transcripts"`
	// ExistingDaily は実行前から daily ファイルに入っている内容。
	ExistingDaily string `json:"existing_daily"`
}

func loadGoldenRecordCases(t *testing.T) []goldenRecordCase {
	t.Helper()

	b, err := os.ReadFile(filepath.Join(goldenRecordDir, "cases.json"))
	if err != nil {
		t.Fatalf("cases.json が読めない: %v", err)
	}
	var cases []goldenRecordCase
	if err := json.Unmarshal(b, &cases); err != nil {
		t.Fatalf("cases.json の解釈に失敗: %v", err)
	}
	if len(cases) == 0 {
		t.Fatal("cases.json が空")
	}
	return cases
}

func TestGoldenRecordCompatibilityWithShellVersion(t *testing.T) {
	t.Parallel()

	for _, tc := range loadGoldenRecordCases(t) {
		t.Run(tc.Name, func(t *testing.T) {
			t.Parallel()

			expectedDaily := filepath.Join(goldenRecordDir, tc.Name, "expected", "daily")
			if _, err := os.Stat(expectedDaily); err != nil {
				t.Fatalf("fixture が無い(scripts/gen-golden-record.sh で生成する): %v", err)
			}

			root := t.TempDir()
			pendingRoot := filepath.Join(root, "pending")
			conductorHome := filepath.Join(root, "conductor")
			transcriptsDir := filepath.Join(root, "transcripts")
			dailyRoot := store.DailyRoot(conductorHome)

			for name, content := range map[string]string{
				"config.json":         tc.Config,
				"config.default.json": tc.ConfigDefault,
			} {
				if content == "" {
					continue
				}
				writeFixtureFile(t, filepath.Join(conductorHome, name), content)
			}
			for rel, content := range tc.Transcripts {
				writeFixtureFile(t, filepath.Join(transcriptsDir, filepath.FromSlash(rel)), content)
			}
			for rel, content := range tc.Pending {
				resolved := strings.ReplaceAll(content, transcriptDirPlaceholder, transcriptsDir)
				writeFixtureFile(t, filepath.Join(pendingRoot, filepath.FromSlash(rel)), resolved)
			}

			now := recordClockFromFixture(t, expectedDaily)
			session := domain.SessionName(tc.Env["ZELLIJ_SESSION_NAME"])
			if tc.ExistingDaily != "" {
				path := filepath.Join(dailyRoot, session, now.Format(domain.DailyFileDateLayout)+".jsonl")
				writeFixtureFile(t, path, tc.ExistingDaily)
			}

			usecase := &app.RecordOutput{
				Pending:    store.NewPendingStore(pendingRoot),
				Transcript: store.NewTranscriptStore(),
				Daily:      store.NewDailyStore(dailyRoot, io.Discard),
				Pricing:    store.NewPricingStore(conductorHome),
				Clock:      fixedClock{now: now},
			}
			env := app.RecordEnv{ZellijSession: tc.Env["ZELLIJ_SESSION_NAME"]}
			if err := usecase.Execute(tc.Tab, env); err != nil {
				t.Fatalf("Execute() = %v", err)
			}

			compareDailyTree(t, expectedDaily, dailyRoot, transcriptsDir)
		})
	}
}

// recordClockFromFixture は fixture が記録している完了時刻を復元する。
//
// Shell 版は completed_at に `%Y-%m-%dT%H:%M:%S%z` を書く。最後に追記された行の
// 値を使う(実行前から置いてある行は今回の実行時刻ではないため)。
// 出力が無い case は時刻が結果に影響しないので任意の値でよい。
func recordClockFromFixture(t *testing.T, expectedDaily string) time.Time {
	t.Helper()

	for _, path := range dailyFilesUnder(t, expectedDaily) {
		lines := readJSONLines(t, path)
		if len(lines) == 0 {
			continue
		}
		last, ok := lines[len(lines)-1]["completed_at"].(string)
		if !ok {
			continue
		}
		parsed, err := time.Parse(domain.DailyCompletedAtLayout, last)
		if err != nil {
			t.Fatalf("completed_at %q の解釈に失敗: %v", last, err)
		}
		return parsed
	}
	return time.Unix(0, 0).UTC()
}

// compareDailyTree は expected と actual の daily ファイル群を突き合わせる。
//
// actual に含まれる transcript ディレクトリの実パスはプレースホルダへ戻してから
// 比較する。実行のたびに変わる絶対パスを fixture に持たせないためである。
func compareDailyTree(t *testing.T, expectedRoot, actualRoot, transcriptsDir string) {
	t.Helper()

	expected := readDailyTree(t, expectedRoot, "")
	actual := readDailyTree(t, actualRoot, transcriptsDir)

	for rel, want := range expected {
		got, ok := actual[rel]
		if !ok {
			t.Errorf("%s が生成されていない", rel)
			continue
		}
		if len(got) != len(want) {
			t.Errorf("%s の行数 = %d, want %d", rel, len(got), len(want))
			continue
		}
		for i := range want {
			if !reflect.DeepEqual(got[i], want[i]) {
				t.Errorf("%s の %d 行目が違う\n  got:  %v\n  want: %v", rel, i+1, got[i], want[i])
			}
		}
	}
	for rel := range actual {
		if _, ok := expected[rel]; !ok {
			t.Errorf("Shell 版が作らないファイルが生成された: %s", rel)
		}
	}
}

// readDailyTree は root 配下の .jsonl を相対パスをキーにして読む。
// transcriptsDir が空でなければ、その実パスをプレースホルダへ置き換える。
func readDailyTree(t *testing.T, root, transcriptsDir string) map[string][]map[string]any {
	t.Helper()

	tree := map[string][]map[string]any{}
	for _, path := range dailyFilesUnder(t, root) {
		rel, err := filepath.Rel(root, path)
		if err != nil {
			t.Fatalf("Rel(%s, %s) = %v", root, path, err)
		}
		lines := readJSONLines(t, path)
		if transcriptsDir != "" {
			lines = replaceTranscriptDir(t, lines, transcriptsDir)
		}
		tree[filepath.ToSlash(rel)] = lines
	}
	return tree
}

// replaceTranscriptDir は transcript_path の実パスをプレースホルダへ戻す。
func replaceTranscriptDir(t *testing.T, lines []map[string]any, transcriptsDir string) []map[string]any {
	t.Helper()

	for _, line := range lines {
		path, ok := line["transcript_path"].(string)
		if !ok {
			continue
		}
		line["transcript_path"] = strings.ReplaceAll(path, transcriptsDir, transcriptDirPlaceholder)
	}
	return lines
}

// readJSONLines は JSON Lines の各行を読む。
func readJSONLines(t *testing.T, path string) []map[string]any {
	t.Helper()

	b, err := os.ReadFile(path) //nolint:gosec // testdata と一時ディレクトリの列挙結果
	if err != nil {
		t.Fatalf("ReadFile(%s) = %v", path, err)
	}
	raw := strings.TrimSuffix(string(b), "\n")
	if raw == "" {
		return nil
	}

	lines := strings.Split(raw, "\n")
	records := make([]map[string]any, 0, len(lines))
	for _, line := range lines {
		var fields map[string]any
		if err := json.Unmarshal([]byte(line), &fields); err != nil {
			t.Fatalf("%s の行が JSON として読めない(%q): %v", path, line, err)
		}
		records = append(records, fields)
	}
	return records
}

// dailyFilesUnder は root 配下の .jsonl ファイルを相対パスの昇順で返す。
func dailyFilesUnder(t *testing.T, root string) []string {
	t.Helper()

	var paths []string
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !strings.HasSuffix(d.Name(), ".jsonl") {
			return nil
		}
		paths = append(paths, path)
		return nil
	})
	if err != nil && !os.IsNotExist(err) {
		t.Fatalf("WalkDir(%s) = %v", root, err)
	}
	sort.Strings(paths)
	return paths
}
