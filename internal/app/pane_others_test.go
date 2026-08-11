package app_test

import (
	"errors"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/k-kudo-hub/mdev-go/internal/app"
	"github.com/k-kudo-hub/mdev-go/internal/domain"
)

// paneClock はペインの「今日」を固定するための時計。
type paneClock struct{ now time.Time }

func (c paneClock) Now() time.Time { return c.now }

var paneToday = paneClock{now: time.Date(2026, 8, 9, 15, 4, 5, 0, time.UTC)}

func TestDonePaneRefresh(t *testing.T) {
	t.Parallel()

	daily := &fakeDailyReader{lines: [][]byte{
		[]byte(`{"tab":"alpha","session":"s1","completed_at":"2026-08-09T10:00:00+0900","summary":{"total_turns":3,"total_tool_calls":5,"total_cost_usd":0.42}}`),
	}}
	pane := &app.DonePane{Daily: daily, Clock: paneToday}

	snapshot := pane.Refresh()

	// 当日の日付でファイルを探す。
	if want := []string{"2026-08-09"}; !reflect.DeepEqual(daily.dates, want) {
		t.Errorf("読んだ日付 = %v, want %v", daily.dates, want)
	}
	if snapshot.Count != 1 {
		t.Fatalf("件数 = %d, want 1", snapshot.Count)
	}
	if !strings.Contains(snapshot.Text, "alpha") || !strings.Contains(snapshot.Text, "Done Tasks") {
		t.Errorf("描画結果が想定と違う: %q", snapshot.Text)
	}
}

func TestDonePaneRestore(t *testing.T) {
	t.Parallel()

	restorer := &fakeTaskRestoreRunner{}
	daily := &fakeDailyReader{lines: [][]byte{
		[]byte(`{"tab":"alpha","session":"s1","completed_at":"2026-08-09T10:00:00+0900","summary":{"total_turns":1,"total_tool_calls":1,"total_cost_usd":0.1}}`),
	}}
	pane := &app.DonePane{Daily: daily, Restorer: restorer, Clock: paneToday}

	// 復元には表示行の 3 つ組をそのまま渡す。失敗は見ない(エントリが Done に
	// 残ることで利用者が気づく)。
	if _, err := pane.Restore(dashboardEnv, pane.Refresh(), 1); err != nil {
		t.Fatalf("Restore() = %v", err)
	}

	want := []string{"s1 alpha s1 2026-08-09T10:00:00+0900"}
	if !reflect.DeepEqual(restorer.calls, want) {
		t.Errorf("復元の引数 = %v, want %v", restorer.calls, want)
	}
}

func TestWaitingPaneRefresh(t *testing.T) {
	t.Parallel()

	pane := &app.WaitingPane{Pending: &fakePendingLister{
		views: map[string][]domain.PendingView{"s1": {
			{Name: "a.json", Tab: "alpha", Event: "Notification"},
			{Name: "b.json", Tab: "review", Event: "Waiting"},
		}},
	}}

	text, err := pane.Refresh(app.PaneEnv{ZellijSession: "s1"})
	if err != nil {
		t.Fatalf("Refresh() = %v", err)
	}

	// Waiting だけが残る。zellij のタブ一覧は参照しない。
	if !strings.Contains(text, "review") {
		t.Errorf("Waiting が出ていない: %q", text)
	}
	if strings.Contains(text, "alpha") {
		t.Errorf("Waiting 以外が出ている: %q", text)
	}
	if !strings.Contains(text, "Waiting: 1") {
		t.Errorf("件数が 1 になっていない: %q", text)
	}
}

func TestWaitingPaneRefreshOutsideZellij(t *testing.T) {
	t.Parallel()

	pane := &app.WaitingPane{Pending: &fakePendingLister{
		views: map[string][]domain.PendingView{
			domain.DefaultSessionName: {{Name: "a.json", Tab: "x", Event: "Waiting"}},
		},
	}}

	text, err := pane.Refresh(app.PaneEnv{})
	if err != nil {
		t.Fatalf("Refresh() = %v", err)
	}
	if !strings.Contains(text, "Waiting: 1") {
		t.Errorf("セッション名が unknown に落ちていない: %q", text)
	}
}

func TestNewsPaneRefresh(t *testing.T) {
	t.Parallel()

	news := &fakeNewsReader{data: []byte(
		`{"items":[{"title":"A","url":"https://a","description":"d"}]}`)}
	pane := &app.NewsPane{News: news, Fetcher: &fakeNewsFetcher{}, Opener: &fakeURLOpener{}, Clock: paneToday}

	snapshot := pane.Refresh()

	if want := []string{"2026-08-09"}; !reflect.DeepEqual(news.dates, want) {
		t.Errorf("読んだ日付 = %v, want %v", news.dates, want)
	}
	if !strings.Contains(snapshot.Text, "[2026-08-09]") || !strings.Contains(snapshot.Text, "A") {
		t.Errorf("描画結果が想定と違う: %q", snapshot.Text)
	}
	if !strings.Contains(snapshot.FetchingText, "Fetching news...") {
		t.Errorf("取得中の画面が用意されていない: %q", snapshot.FetchingText)
	}
	if snapshot.Count != 1 {
		t.Errorf("件数 = %d, want 1", snapshot.Count)
	}
}

func TestNewsPaneReload(t *testing.T) {
	t.Parallel()

	fetcher := &fakeNewsFetcher{journal: &paneJournal{}}
	pane := &app.NewsPane{News: &fakeNewsReader{}, Fetcher: fetcher, Opener: &fakeURLOpener{}, Clock: paneToday}

	pane.Reload()

	if fetcher.fetchNewsCalls != 1 {
		t.Errorf("fetch-news の呼び出し = %d, want 1", fetcher.fetchNewsCalls)
	}
	// 取り直す先は「今日」で、表示に使う日付と同じでなければならない。
	// ずれると取得したのに画面が変わらない。
	if want := []string{"2026-08-09"}; !slices.Equal(fetcher.dates, want) {
		t.Errorf("渡された日付 = %v, want %v", fetcher.dates, want)
	}
}

func TestNewsPaneOpen(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		data   string
		number int
		want   []string
	}{
		{
			name:   "URL があれば開く",
			data:   `{"items":[{"title":"A","url":"https://a"}]}`,
			number: 1,
			want:   []string{"https://a"},
		},
		{
			// jq -r が "null" を返す(url キーが無い)場合は開かない。
			name:   "URL が無いなら開かない",
			data:   `{"items":[{"title":"A"}]}`,
			number: 1,
			want:   nil,
		},
		{
			name:   "URL が空なら開かない",
			data:   `{"items":[{"title":"A","url":""}]}`,
			number: 1,
			want:   nil,
		},
		{
			name:   "範囲外の番号は開かない",
			data:   `{"items":[{"title":"A","url":"https://a"}]}`,
			number: 5,
			want:   nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			opener := &fakeURLOpener{}
			pane := &app.NewsPane{
				News: &fakeNewsReader{data: []byte(tt.data)}, Fetcher: &fakeNewsFetcher{},
				Opener: opener, Clock: paneToday,
			}
			pane.Open(pane.Refresh(), tt.number)

			if !reflect.DeepEqual(opener.opened, tt.want) {
				t.Errorf("開いた URL = %v, want %v", opener.opened, tt.want)
			}
		})
	}
}

// TestDonePaneRestoreReportsFailure は復元の失敗と説明をそのまま返すことを
// 固定する。
//
// 現行 Shell 版は終了コードを握り潰しており、失敗しても画面に何も出ない。
// 復元はキーを押した結果として起きるので、無反応だと押し直しを誘って
// 同じ名前のタブが増える(意図的な改善)。
func TestDonePaneRestoreReportsFailure(t *testing.T) {
	t.Parallel()

	daily := &fakeDailyReader{lines: [][]byte{
		[]byte(`{"tab":"alpha","session":"s1","completed_at":"2026-08-09T10:00:00+0900","summary":{"total_turns":1,"total_tool_calls":1,"total_cost_usd":0.1}}`),
	}}
	restorer := &fakeTaskRestoreRunner{warning: "タブだけ復元しました", err: app.ErrRestoreDirMissing}
	pane := &app.DonePane{Daily: daily, Restorer: restorer, Clock: paneToday}

	warning, err := pane.Restore(dashboardEnv, pane.Refresh(), 1)
	if !errors.Is(err, app.ErrRestoreDirMissing) {
		t.Errorf("Restore() = %v, want %v", err, app.ErrRestoreDirMissing)
	}
	if warning != "タブだけ復元しました" {
		t.Errorf("警告 = %q", warning)
	}
}

// TestDonePaneRestoreIgnoresOutOfRange は範囲外の番号で何も呼ばないことを
// 固定する。
func TestDonePaneRestoreIgnoresOutOfRange(t *testing.T) {
	t.Parallel()

	daily := &fakeDailyReader{lines: [][]byte{
		[]byte(`{"tab":"alpha","session":"s1","completed_at":"2026-08-09T10:00:00+0900","summary":{"total_turns":1,"total_tool_calls":1,"total_cost_usd":0.1}}`),
	}}
	restorer := &fakeTaskRestoreRunner{err: app.ErrRestoreDirMissing}
	pane := &app.DonePane{Daily: daily, Restorer: restorer, Clock: paneToday}

	for _, number := range []int{0, 2} {
		warning, err := pane.Restore(dashboardEnv, pane.Refresh(), number)
		if warning != "" || err != nil {
			t.Errorf("番号 %d で復元を呼んでいる: (%q, %v)", number, warning, err)
		}
	}
	if len(restorer.calls) != 0 {
		t.Errorf("復元を呼んだ: %v", restorer.calls)
	}
}
