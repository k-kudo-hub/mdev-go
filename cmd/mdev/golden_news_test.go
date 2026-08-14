package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/k-kudo-hub/mdev-go/internal/domain"
)

// news 取得のゴールデンテスト。
//
// testdata/golden-news/feed.xml を現行 Shell 版の fetch-news.sh に食わせて
// 生成した expected.json と、Go 版が同じ入力から組み立てた JSON を
// **バイト単位で** 突き合わせる。fixture の作り方は
// scripts/gen-golden-news.sh を参照(このテストは fixture を読むだけで、
// Shell 版には依存しない)。
//
// バイト単位で見るのは、この JSON が News ペインの表示そのものになるためで
// ある。空白の入り方や切り詰めの位置がずれれば、利用者の画面がずれる。
const goldenNewsDir = "testdata/golden-news"

func TestGoldenNewsMatchesShellVersion(t *testing.T) {
	t.Parallel()

	feed, err := os.ReadFile(filepath.Join(goldenNewsDir, "feed.xml"))
	if err != nil {
		t.Fatalf("feed.xml が読めない: %v", err)
	}
	wantRaw, err := os.ReadFile(filepath.Join(goldenNewsDir, "expected.json"))
	if err != nil {
		t.Fatalf("expected.json が読めない(scripts/gen-golden-news.sh で生成する): %v", err)
	}

	got := string(domain.BuildNewsFile(domain.ParseRSSItems(feed)))
	if got != string(wantRaw) {
		t.Errorf("Shell 版の出力と一致しない\n--- got ---\n%s\n--- want ---\n%s", got, wantRaw)
	}
}
