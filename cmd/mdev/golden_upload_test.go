package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/k-kudo-hub/mdev-go/internal/domain"
)

// upload-log のゴールデンテスト。
//
// testdata/golden-upload/cases.json が入力を定義し、<case>/expected には
// 現行 Shell 版の upload-log.sh(filter_secrets / build_markdown)へ同じ
// 入力を与えて生成させた出力が入っている。fixture の作り方は
// scripts/gen-golden-upload.sh を参照(このテストは fixture を読むだけで、
// Shell 版には依存しない)。
//
// 比較は **バイト単位** で行う。秘密のマスクは 1 文字ずれれば漏れになり、
// markdown はログリポジトリへそのまま残るものなので、等価では足りない。
//
// build_markdown の case は 11 フィールドすべてが非空のものだけである。
// 現行版は空フィールドで値がずれるバグを持ち、Go 版はそれを直しているため、
// ずれる入力では一致しない(gen-golden-upload.sh の説明と
// internal/domain/upload_log_test.go の該当ケースを参照)。

const goldenUploadDir = "testdata/golden-upload"

// 生成側が扱う入力の種類。
const (
	goldenUploadFilterSecrets = "filter_secrets"
	goldenUploadBuildMarkdown = "build_markdown"
)

// goldenUploadCase は 1 件の入力定義。cases.json の 1 要素に対応する。
type goldenUploadCase struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	// Kind は filter_secrets か build_markdown。
	Kind string `json:"kind"`
	// Input は filter_secrets へ渡す文字列。
	Input string `json:"input"`
	// Record / Summary は build_markdown へ渡す 2 引数。
	Record  string `json:"record"`
	Summary string `json:"summary"`
}

func loadGoldenUploadCases(t *testing.T) []goldenUploadCase {
	t.Helper()

	b, err := os.ReadFile(filepath.Join(goldenUploadDir, "cases.json"))
	if err != nil {
		t.Fatalf("cases.json が読めない: %v", err)
	}
	var cases []goldenUploadCase
	if err := json.Unmarshal(b, &cases); err != nil {
		t.Fatalf("cases.json の解釈に失敗: %v", err)
	}
	if len(cases) == 0 {
		t.Fatal("cases.json が空")
	}
	return cases
}

func TestGoldenUploadMatchesShellVersion(t *testing.T) {
	t.Parallel()

	for _, tc := range loadGoldenUploadCases(t) {
		t.Run(tc.Name, func(t *testing.T) {
			t.Parallel()

			wantRaw, err := os.ReadFile(filepath.Join(goldenUploadDir, tc.Name, "expected"))
			if err != nil {
				t.Fatalf("expected が読めない(scripts/gen-golden-upload.sh で生成する): %v", err)
			}
			want := string(wantRaw)

			var got string
			switch tc.Kind {
			case goldenUploadFilterSecrets:
				got = domain.FilterSecrets(tc.Input)
			case goldenUploadBuildMarkdown:
				got = domain.BuildMarkdown([]byte(tc.Record), tc.Summary)
			default:
				t.Fatalf("未知の kind: %q", tc.Kind)
			}

			if got != want {
				t.Errorf("%s: Shell 版の出力と一致しない\n--- got ---\n%q\n--- want ---\n%q",
					tc.Description, got, want)
			}
		})
	}
}

// TestGoldenUploadCoversBothKinds は fixture が両方の関数を覆っている
// ことを確かめる。片方の case を消したまま気づかない状態を防ぐ。
func TestGoldenUploadCoversBothKinds(t *testing.T) {
	t.Parallel()

	kinds := map[string]int{}
	for _, tc := range loadGoldenUploadCases(t) {
		kinds[tc.Kind]++
	}
	for _, kind := range []string{goldenUploadFilterSecrets, goldenUploadBuildMarkdown} {
		if kinds[kind] == 0 {
			t.Errorf("kind %q の case がありません", kind)
		}
	}
}

// TestGoldenUploadFixturesMaskSecrets は fixture そのものに素の秘密が
// 残っていないことを確かめる。
//
// マスクが効いていない fixture を取り込んでしまうと、テストは通るのに
// 実際には漏れる状態を固定してしまう。
func TestGoldenUploadFixturesMaskSecrets(t *testing.T) {
	t.Parallel()

	// cases.json の入力に出てくる、伏せられていなければならない断片。
	leaks := []string{
		"sk-ant-api03-abcDEF",
		"ghp_abcdef",
		"AKIAIOSFODNN7EXAMPLE",
		"xoxb-EXAMPLESLACK",
		"github_pat_11ABCDEFG0",
		"aBcDeF1234567890",
		"secretkeymaterial",
		"k7QmShortTailLine24charsZ",
		"MIIBstraykeymaterial",
		"RAWMESSAGEMARKER",
	}
	for _, tc := range loadGoldenUploadCases(t) {
		wantRaw, err := os.ReadFile(filepath.Join(goldenUploadDir, tc.Name, "expected"))
		if err != nil {
			t.Fatalf("expected が読めない: %v", err)
		}
		for _, leak := range leaks {
			if strings.Contains(string(wantRaw), leak) {
				t.Errorf("%s の fixture に %q が残っています", tc.Name, leak)
			}
		}
	}
}
