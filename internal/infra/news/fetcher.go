// Package news は AI 関連ニュースの取得と保存を担当する。
// internal/app が定義する port の実装(adapter)である(ADR-0002)。
package news

import (
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/k-kudo-hub/mdev-go/internal/app"
	"github.com/k-kudo-hub/mdev-go/internal/domain"
	"github.com/k-kudo-hub/mdev-go/internal/infra/fsutil"
)

// newsDirName は CONDUCTOR_HOME 直下のニュース置き場。
const newsDirName = "news"

// newsSuffix はニュースファイルの拡張子。
const newsSuffix = ".json"

// FeedURL は取得元のフィードである(現行 fetch-news.sh と同じ)。
const FeedURL = "https://techcrunch.com/category/artificial-intelligence/feed/"

// fetchTimeout は 1 回の取得を諦めるまでの時間(現行版の `curl --max-time 5`)。
//
// ニュースは無くても困らない一方、取得は利用者のキー入力(r)を待たせる
// 同期処理なので、短く切る。
const fetchTimeout = 5 * time.Second

// retentionDays は古いニュースファイルを残す日数(現行版の
// `find ... -mtime +7 -delete`)。
//
// find の -mtime は経過時間を 86400 秒で割って **切り捨てた**日数を見るため、
// `+7` は「その値が 7 より大きい」= 経過 8 日以上を意味する。7 日ちょうどを
// 過ぎただけのファイルはまだ消えない。expiredBefore がこの丸めを再現する。
const retentionDays = 7

// Fetcher はフィードを取得してニュースファイルへ保存する。
type Fetcher struct {
	root   string
	url    string
	client *http.Client
	// now は保存期間の判定に使う「今」。テストで差し替える。
	now func() time.Time
}

var _ app.NewsFetcher = (*Fetcher)(nil)

// NewFetcher は conductorHome/news へ保存する Fetcher を返す。
func NewFetcher(conductorHome string) *Fetcher {
	return &Fetcher{
		root:   filepath.Join(conductorHome, newsDirName),
		url:    FeedURL,
		client: &http.Client{Timeout: fetchTimeout},
		now:    time.Now,
	}
}

// FetchNews は date のニュースを取り直す。
//
// **失敗しても何も返さない。** ニュースは無くても作業は進むため、取得の
// 失敗で画面が止まったり警告が出たりしないほうがよい(現行版もすべての
// 失敗経路で黙って exit 0 する)。取れなかった日は前の内容がそのまま残る。
//
// 取得できた場合は、項目が 0 件でもファイルを書く。フィードの形が変わって
// 何も取れなくなったとき、古い内容が残り続けるより空だと分かるほうがよい
// (現行版も awk が空の items を出して jq の検証を通す)。
func (f *Fetcher) FetchNews(date string) {
	body, ok := f.fetch()
	if !ok {
		return
	}
	if err := f.save(date, domain.BuildNewsFile(domain.ParseRSSItems(body))); err != nil {
		return
	}
	f.removeExpired()
}

// HasNews は date のニュースが既にあるかを返す。
//
// 中身は見ない。現行版も `[ -f "$NEWS_FILE" ]` としか見ておらず、壊れた
// ファイルが残っていても取り直さない。取り直したいときは --force がある。
func (f *Fetcher) HasNews(date string) bool {
	info, err := os.Stat(filepath.Join(f.root, date+newsSuffix))
	return err == nil && !info.IsDir()
}

// fetch はフィードを取得して本文を返す。失敗と空の応答は ok=false になる。
func (f *Fetcher) fetch() ([]byte, bool) {
	resp, err := f.client.Get(f.url) //nolint:noctx // Client.Timeout で上限を持つ
	if err != nil {
		return nil, false
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return nil, false
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil || len(body) == 0 {
		return nil, false
	}
	return body, true
}

// save はニュースファイルを書く。
//
// 書き込みは fsutil に任せる。News ペインはポーリングで読んでいるため
// 書きかけの内容を読ませてはならず、その置き換え方は store と同じでよい。
// 一時ファイル名を固定にしないのもここで効く(同時に走った 2 つの取得が
// 同じ名前を奪い合うと、壊れた内容が残りうる)。
func (f *Fetcher) save(date string, data []byte) error {
	return fsutil.WriteFile(filepath.Join(f.root, date+newsSuffix), data)
}

// removeExpired は保持期間を過ぎたニュースファイルを消す。
// 消せなかったものは黙って残す(次回また試す)。
func (f *Fetcher) removeExpired() {
	entries, err := os.ReadDir(f.root)
	if err != nil {
		return
	}
	deadline := expiredBefore(f.now())
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), newsSuffix) {
			continue
		}
		info, err := entry.Info()
		if err != nil || info.ModTime().After(deadline) {
			continue
		}
		_ = os.Remove(filepath.Join(f.root, entry.Name()))
	}
}

// expiredBefore は「この時刻以前に更新されたものは消してよい」境界を返す。
//
// find -mtime +7 は経過時間を切り捨てた日数が 7 より大きいもの、つまり
// **経過 8 日以上**を消す。境界を now-7 日にすると現行版より丸 1 日早く
// 消してしまい、7 日前のニュースが読めなくなる。
func expiredBefore(now time.Time) time.Time {
	return now.Add(-time.Duration(retentionDays+1) * 24 * time.Hour)
}
