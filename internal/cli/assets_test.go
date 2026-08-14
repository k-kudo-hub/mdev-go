package cli

import (
	"errors"
	"strings"
	"testing"
)

// fakeAssetService は資産の解決の代役である。
type fakeAssetService struct {
	names []string
	body  string
	err   error
	// asked は Read に渡された名前。
	asked []string
}

func (s *fakeAssetService) Names() []string { return s.names }

func (s *fakeAssetService) Read(name string) ([]byte, error) {
	s.asked = append(s.asked, name)
	if s.err != nil {
		return nil, s.err
	}
	return []byte(s.body), nil
}

// TestAssetsCommandWritesContent は資産の中身をそのまま出すことを確かめる。
//
// 6-3 のインストーラがここから取り出して置くため、余計な装飾を付けると
// 置いたファイルが壊れる。
func TestAssetsCommandWritesContent(t *testing.T) {
	t.Parallel()

	svc := &fakeAssetService{body: "layout {\n    tab\n}\n"}
	code, stdout, stderr := runCLIWithOut(t, Deps{Assets: svc}, "assets", "layouts/dev.kdl")

	if code != exitOK {
		t.Errorf("終了コード = %d, want %d (stderr=%q)", code, exitOK, stderr)
	}
	if stdout != svc.body {
		t.Errorf("標準出力 = %q, want %q", stdout, svc.body)
	}
	if len(svc.asked) != 1 || svc.asked[0] != "layouts/dev.kdl" {
		t.Errorf("求めた名前 = %q", svc.asked)
	}
}

// TestAssetsCommandListsNames は引数が無いときに一覧を出すことを確かめる。
func TestAssetsCommandListsNames(t *testing.T) {
	t.Parallel()

	svc := &fakeAssetService{names: []string{"config.default.json", "layouts/dev.kdl"}}
	code, stdout, _ := runCLIWithOut(t, Deps{Assets: svc}, "assets")

	if code != exitOK {
		t.Errorf("終了コード = %d, want %d", code, exitOK)
	}
	if want := "config.default.json\nlayouts/dev.kdl\n"; stdout != want {
		t.Errorf("標準出力 = %q, want %q", stdout, want)
	}
	if len(svc.asked) != 0 {
		t.Errorf("一覧なのに中身を読んだ: %q", svc.asked)
	}
}

// TestAssetsCommandReportsUnknownName は覚えの無い名前の報告を確かめる。
func TestAssetsCommandReportsUnknownName(t *testing.T) {
	t.Parallel()

	svc := &fakeAssetService{err: errors.New("そのような資産はありません: nope")}
	code, stdout, stderr := runCLIWithOut(t, Deps{Assets: svc}, "assets", "nope")

	if code != exitError {
		t.Errorf("終了コード = %d, want %d", code, exitError)
	}
	if stdout != "" {
		t.Errorf("標準出力 = %q, want 空", stdout)
	}
	if !strings.Contains(stderr, "nope") {
		t.Errorf("標準エラー = %q", stderr)
	}
}

// TestAssetsCommandRejectsExtraArguments は引数が多いときに弾くことを確かめる。
func TestAssetsCommandRejectsExtraArguments(t *testing.T) {
	t.Parallel()

	svc := &fakeAssetService{}
	code, _, _ := runCLIWithOut(t, Deps{Assets: svc}, "assets", "a", "b")

	if code != exitError {
		t.Errorf("終了コード = %d, want %d", code, exitError)
	}
	if len(svc.asked) != 0 {
		t.Errorf("弾くはずが読みに行った: %q", svc.asked)
	}
}
