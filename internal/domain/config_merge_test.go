package domain_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/k-kudo-hub/mdev-go/internal/domain"
)

// config.json マージのゴールデンテスト。
//
// testdata/golden-config-merge/cases.json が入力を定義し、<case>/expected.json
// には現行 install.sh の jq 式に同じ入力を与えた出力が入っている。fixture の
// 作り方は scripts/gen-golden-config-merge.sh を参照(このテストは fixture を
// 読むだけで、Shell 版には依存しない)。
//
// 比較は **JSON としての等価** で行う。整形とキーの並びは意図的に違う
// (現行版は jq が全体を整形し直すが、こちらは足りないキーを挿し込むだけ)。
// 中身が 1 か所でも違えば落ちる。
const goldenConfigMergeDir = "testdata/golden-config-merge"

type goldenConfigMergeCase struct {
	Name        string `json:"name"`
	Description string `json:"description"`
}

func TestGoldenConfigMergeMatchesShellVersion(t *testing.T) {
	t.Parallel()

	defaults := readDefaultConfig(t)

	b, err := os.ReadFile(filepath.Join(goldenConfigMergeDir, "cases.json"))
	if err != nil {
		t.Fatalf("cases.json が読めない: %v", err)
	}
	var cases []goldenConfigMergeCase
	if err := json.Unmarshal(b, &cases); err != nil {
		t.Fatalf("cases.json の解釈に失敗: %v", err)
	}
	if len(cases) == 0 {
		t.Fatal("cases.json が空")
	}

	for _, tc := range cases {
		t.Run(tc.Name, func(t *testing.T) {
			t.Parallel()

			dir := filepath.Join(goldenConfigMergeDir, tc.Name)
			config := readFixture(t, filepath.Join(dir, "config.json"))
			want := readFixture(t, filepath.Join(dir, "expected.json"))

			got, _, err := domain.MergeAgentDefaults(config, defaults)
			if err != nil {
				t.Fatalf("MergeAgentDefaults = %v", err)
			}
			if !reflect.DeepEqual(decodeAny(t, got), decodeAny(t, want)) {
				t.Errorf("現行版と中身が違う\n--- got ---\n%s\n--- want ---\n%s", got, want)
			}
		})
	}
}

// readDefaultConfig は fixture に写してある既定の設定を読む。
//
// 埋め込み(assets)から読まないのは、domain がそこへ依存できないためである
// (ADR-0002 の依存方向。depguard が機械的に禁じている)。fixture の中身は
// scripts/gen-golden-config-merge.sh が assets/config.default.json から写す。
func readDefaultConfig(t *testing.T) []byte {
	t.Helper()
	return readFixture(t, filepath.Join(goldenConfigMergeDir, "config.default.json"))
}

// readFixture は fixture のファイルを読む。
func readFixture(t *testing.T, path string) []byte {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("%s が読めない(scripts/gen-golden-config-merge.sh で生成する): %v", path, err)
	}
	return b
}

// decodeAny は JSON を比較できる形へ読む。
func decodeAny(t *testing.T, data []byte) any {
	t.Helper()
	var v any
	if err := json.Unmarshal(data, &v); err != nil {
		t.Fatalf("JSON として読めない: %v\n%s", err, data)
	}
	return v
}

// TestMergeAgentDefaultsIsIdempotent は 2 回目が 1 バイトも書き換えないことを
// 確かめる。
//
// install は繰り返し実行される。1 回目で補ったものを 2 回目がまた触ると、
// 利用者の設定が install のたびに変わり続ける。
func TestMergeAgentDefaultsIsIdempotent(t *testing.T) {
	t.Parallel()

	defaults := readDefaultConfig(t)
	config := []byte(`{
  "agents": {
    "codex": {"command": "codex"}
  }
}
`)

	once, additions, err := domain.MergeAgentDefaults(config, defaults)
	if err != nil {
		t.Fatalf("1 回目 = %v", err)
	}
	if len(additions) == 0 {
		t.Fatal("1 回目で何も補っていない")
	}

	twice, again, err := domain.MergeAgentDefaults(once, defaults)
	if err != nil {
		t.Fatalf("2 回目 = %v", err)
	}
	if len(again) != 0 {
		t.Errorf("2 回目が補った: %v", again)
	}
	if string(twice) != string(once) {
		t.Errorf("2 回目が書き換えた\n--- 2 回目 ---\n%s\n--- 1 回目 ---\n%s", twice, once)
	}
}

// TestMergeAgentDefaultsKeepsFormatting は補う必要が無いときに入力を 1 バイトも
// 変えないことを確かめる。
//
// 現行版は jq が全体を整形し直すため、触っていない箇所の見た目も変わる。
// こちらは足りないキーを挿し込むだけなので、変えるものが無ければ何も変わらない。
func TestMergeAgentDefaultsKeepsFormatting(t *testing.T) {
	t.Parallel()

	defaults := readDefaultConfig(t)
	// 独特な字下げと並び。jq に通すとこれがすべて崩れる。
	config := []byte("{\n\t\"search_dirs\":[\"~/projects\"],\n\t\"agents\":{}\n}\n")

	got, additions, err := domain.MergeAgentDefaults(config, defaults)
	if err != nil {
		t.Fatalf("MergeAgentDefaults = %v", err)
	}
	if len(additions) != 0 {
		t.Errorf("補うものは無いはず: %v", additions)
	}
	if string(got) != string(config) {
		t.Errorf("入力を書き換えた\n--- got ---\n%q\n--- want ---\n%q", got, config)
	}
}

// TestMergeAgentDefaultsRejectsBrokenJSON は壊れた設定を弾くことを確かめる。
//
// 現行版も jq が落ちてマージを飛ばし、既存の設定をそのまま残したうえで
// 警告を出す。呼び出し側が同じ扱いをできるよう、ここでは error を返す。
func TestMergeAgentDefaultsRejectsBrokenJSON(t *testing.T) {
	t.Parallel()

	defaults := readDefaultConfig(t)
	if _, _, err := domain.MergeAgentDefaults([]byte(`{"agents":`), defaults); err == nil {
		t.Error("エラーを返すはず")
	}
}

// TestMergeAgentDefaultsReportsAdditions は補った内容を返すことを確かめる。
// 何が足されたのかが画面に出ないと、設定が黙って増えたように見える。
func TestMergeAgentDefaultsReportsAdditions(t *testing.T) {
	t.Parallel()

	defaults := readDefaultConfig(t)
	config := []byte(`{"agents":{"codex":{"command":"codex","detection":"screen","patterns":{"blocked":[]}}}}`)

	_, additions, err := domain.MergeAgentDefaults(config, defaults)
	if err != nil {
		t.Fatalf("MergeAgentDefaults = %v", err)
	}
	got := domain.RenderAgentDefaultAdditions(additions)
	if want := "codex.patterns.neutral, codex.patterns.working"; got != want {
		t.Errorf("補った内容 = %q, want %q", got, want)
	}
}

// TestMergeAgentDefaultsRejectsNonObjectRoot はトップレベルがオブジェクトで
// ない config.json を弾くことを確かめる。
//
// null や配列でも JSON としては妥当なので、json.Valid だけでは通ってしまう。
// そこへキーを挿し込む位置は無い。
func TestMergeAgentDefaultsRejectsNonObjectRoot(t *testing.T) {
	t.Parallel()

	defaults := readDefaultConfig(t)
	for _, config := range []string{"null", "[]", "[1,2]", `"文字列"`, "42"} {
		t.Run(config, func(t *testing.T) {
			t.Parallel()
			if _, _, err := domain.MergeAgentDefaults([]byte(config), defaults); err == nil {
				t.Errorf("MergeAgentDefaults(%q) = nil, want エラー", config)
			}
		})
	}
}
