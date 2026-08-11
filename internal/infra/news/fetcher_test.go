package news

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// rssFeed は test.sh「26. fetch-news.sh」のモックが返す RSS の一部である。
const rssFeed = `<rss><channel><title>TC</title>
<item><title><![CDATA[GPT-5 Released]]></title><link>https://e/gpt5</link><description><![CDATA[desc one]]></description></item>
<item><title><![CDATA[Claude 4.6]]></title><link>https://e/claude</link><description><![CDATA[desc two]]></description></item>
</channel></rss>`

// newTestFetcher は body を返すサーバを立て、そこを見る Fetcher を返す。
func newTestFetcher(t *testing.T, status int, body string) (*Fetcher, string) {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(server.Close)

	home := t.TempDir()
	f := NewFetcher(home)
	f.url = server.URL
	return f, filepath.Join(home, newsDirName)
}

// TestFetchNewsWritesFile は取得できたニュースが保存されることを確かめる。
func TestFetchNewsWritesFile(t *testing.T) {
	f, root := newTestFetcher(t, http.StatusOK, rssFeed)

	f.FetchNews("2026-08-09")

	got, err := os.ReadFile(filepath.Join(root, "2026-08-09.json"))
	if err != nil {
		t.Fatalf("ニュースファイルがありません: %v", err)
	}
	want := `{
  "items": [
    {
      "title": "GPT-5 Released",
      "url": "https://e/gpt5",
      "description": "desc one"
    },
    {
      "title": "Claude 4.6",
      "url": "https://e/claude",
      "description": "desc two"
    }
  ]
}
`
	if string(got) != want {
		t.Errorf("ニュースファイル =\n%s\nwant\n%s", got, want)
	}
}

// TestFetchNewsWritesEmptyItems は RSS でない応答でも「空だと分かる」ファイルを
// 書くことを確かめる。フィードの形が変わったときに古い内容が残り続けるより、
// 空だと分かるほうがよい(現行版も空の items を書く)。
func TestFetchNewsWritesEmptyItems(t *testing.T) {
	f, root := newTestFetcher(t, http.StatusOK, "not valid xml at all")

	f.FetchNews("2026-08-09")

	got, err := os.ReadFile(filepath.Join(root, "2026-08-09.json"))
	if err != nil {
		t.Fatalf("ニュースファイルがありません: %v", err)
	}
	if !strings.Contains(string(got), `"items": []`) {
		t.Errorf("空の items になっていません: %s", got)
	}
}

// TestFetchNewsKeepsPreviousOnFailure は取得に失敗しても前の内容を壊さない
// ことを確かめる。ニュースは無くても作業は進むので、黙って何もしない。
func TestFetchNewsKeepsPreviousOnFailure(t *testing.T) {
	tests := []struct {
		name   string
		status int
		body   string
	}{
		{name: "サーバエラー", status: http.StatusInternalServerError, body: rssFeed},
		{name: "応答が空", status: http.StatusOK, body: ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f, root := newTestFetcher(t, tt.status, tt.body)
			if err := os.MkdirAll(root, dirPerm); err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(root, "2026-08-09.json")
			if err := os.WriteFile(path, []byte("PREVIOUS"), filePerm); err != nil {
				t.Fatal(err)
			}

			f.FetchNews("2026-08-09")

			got, err := os.ReadFile(path)
			if err != nil || string(got) != "PREVIOUS" {
				t.Errorf("前の内容が壊れました: %q (%v)", got, err)
			}
		})
	}
}

// TestFetchNewsIsSilentWhenUnreachable は到達できない相手でも何も起きない
// ことを確かめる(画面を止めない)。
func TestFetchNewsIsSilentWhenUnreachable(t *testing.T) {
	home := t.TempDir()
	f := NewFetcher(home)
	// 立てたサーバをすぐ閉じて、確実に接続できない URL を作る。
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	f.url = server.URL
	server.Close()

	f.FetchNews("2026-08-09")

	if _, err := os.Stat(filepath.Join(home, newsDirName, "2026-08-09.json")); !os.IsNotExist(err) {
		t.Errorf("失敗したのにファイルができています: %v", err)
	}
}

// TestFetchNewsRemovesExpired は保存期間を過ぎたファイルが消えることを
// 確かめる(現行版の `find ... -mtime +7 -delete`)。
func TestFetchNewsRemovesExpired(t *testing.T) {
	f, root := newTestFetcher(t, http.StatusOK, rssFeed)
	now := time.Date(2026, 8, 9, 12, 0, 0, 0, time.UTC)
	f.now = func() time.Time { return now }

	if err := os.MkdirAll(root, dirPerm); err != nil {
		t.Fatal(err)
	}
	files := map[string]time.Time{
		"2026-01-01.json": now.AddDate(0, 0, -30), // 期限切れ
		"2026-08-05.json": now.AddDate(0, 0, -4),  // 期限内
		"keep.txt":        now.AddDate(0, 0, -30), // json ではないので触らない
	}
	for name, mtime := range files {
		path := filepath.Join(root, name)
		if err := os.WriteFile(path, []byte("{}"), filePerm); err != nil {
			t.Fatal(err)
		}
		if err := os.Chtimes(path, mtime, mtime); err != nil {
			t.Fatal(err)
		}
	}

	f.FetchNews("2026-08-09")

	for name, wantExists := range map[string]bool{
		"2026-01-01.json": false,
		"2026-08-05.json": true,
		"keep.txt":        true,
		"2026-08-09.json": true,
	} {
		_, err := os.Stat(filepath.Join(root, name))
		if exists := err == nil; exists != wantExists {
			t.Errorf("%s の存在 = %v, want %v", name, exists, wantExists)
		}
	}
}
