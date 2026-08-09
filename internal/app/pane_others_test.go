package app_test

import (
	"reflect"
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

	journal := &paneJournal{}
	daily := &fakeDailyReader{lines: [][]byte{
		[]byte(`{"tab":"alpha","session":"s1","completed_at":"2026-08-09T10:00:00+0900","summary":{"total_turns":3,"total_tool_calls":5,"total_cost_usd":0.42}}`),
	}}
	pane := &app.DonePane{Daily: daily, Shell: &fakeShellRunner{journal: journal}, Clock: paneToday}

	view := pane.Refresh()

	// 当日の日付でファイルを探す。
	if want := []string{"2026-08-09"}; !reflect.DeepEqual(daily.dates, want) {
		t.Errorf("読んだ日付 = %v, want %v", daily.dates, want)
	}
	if view.Count != 1 || len(view.Rows) != 1 || view.Rows[0].Tab != "alpha" {
		t.Errorf("集計結果が想定と違う: %+v", view)
	}
}

func TestDonePaneRestore(t *testing.T) {
	t.Parallel()

	journal := &paneJournal{}
	shell := &fakeShellRunner{journal: journal}
	pane := &app.DonePane{Daily: &fakeDailyReader{}, Shell: shell, Clock: paneToday}

	// restore-task.sh には表示行の 3 つ組をそのまま渡す。終了コードは見ない。
	pane.Restore(domain.DoneRow{
		Tab: "alpha", Session: "s1", CompletedAt: "2026-08-09T10:00:00+0900",
	})

	want := []string{"alpha s1 2026-08-09T10:00:00+0900"}
	if !reflect.DeepEqual(shell.restoredTasks, want) {
		t.Errorf("restore-task の引数 = %v, want %v", shell.restoredTasks, want)
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

	items, err := pane.Refresh(app.PaneEnv{ZellijSession: "s1"})
	if err != nil {
		t.Fatalf("Refresh() = %v", err)
	}

	// Waiting だけが残る。zellij のタブ一覧は参照しない。
	if len(items) != 1 || items[0].Tab != "review" {
		t.Errorf("抽出結果 = %+v, want review 1 件", items)
	}
}

func TestWaitingPaneRefreshOutsideZellij(t *testing.T) {
	t.Parallel()

	pane := &app.WaitingPane{Pending: &fakePendingLister{
		views: map[string][]domain.PendingView{
			domain.DefaultSessionName: {{Name: "a.json", Tab: "x", Event: "Waiting"}},
		},
	}}

	items, err := pane.Refresh(app.PaneEnv{})
	if err != nil {
		t.Fatalf("Refresh() = %v", err)
	}
	if len(items) != 1 {
		t.Errorf("セッション名が unknown に落ちていない: %+v", items)
	}
}

func TestNewsPaneRefresh(t *testing.T) {
	t.Parallel()

	news := &fakeNewsReader{data: []byte(
		`{"items":[{"title":"A","url":"https://a","description":"d"}]}`)}
	pane := &app.NewsPane{News: news, Shell: &fakeShellRunner{}, Opener: &fakeURLOpener{}, Clock: paneToday}

	date, items := pane.Refresh()

	if date != "2026-08-09" {
		t.Errorf("日付 = %q, want 2026-08-09", date)
	}
	if want := []string{"2026-08-09"}; !reflect.DeepEqual(news.dates, want) {
		t.Errorf("読んだ日付 = %v, want %v", news.dates, want)
	}
	if len(items) != 1 || items[0].Title != "A" {
		t.Errorf("items = %+v, want A 1 件", items)
	}
}

func TestNewsPaneReload(t *testing.T) {
	t.Parallel()

	shell := &fakeShellRunner{journal: &paneJournal{}}
	pane := &app.NewsPane{News: &fakeNewsReader{}, Shell: shell, Opener: &fakeURLOpener{}, Clock: paneToday}

	pane.Reload()

	if shell.fetchNewsCalls != 1 {
		t.Errorf("fetch-news の呼び出し = %d, want 1", shell.fetchNewsCalls)
	}
}

func TestNewsPaneOpen(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		item domain.NewsItem
		want []string
	}{
		{
			name: "URL があれば開く",
			item: domain.NewsItem{Title: "A", URL: "https://a"},
			want: []string{"https://a"},
		},
		{
			// jq -r が "null" を返した場合(url キーが無い)は開かない。
			name: "URL が null なら開かない",
			item: domain.NewsItem{Title: "A", URL: "null"},
			want: nil,
		},
		{
			name: "URL が空なら開かない",
			item: domain.NewsItem{Title: "A", URL: ""},
			want: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			opener := &fakeURLOpener{}
			pane := &app.NewsPane{
				News: &fakeNewsReader{}, Shell: &fakeShellRunner{},
				Opener: opener, Clock: paneToday,
			}
			pane.Open(tt.item)

			if !reflect.DeepEqual(opener.opened, tt.want) {
				t.Errorf("開いた URL = %v, want %v", opener.opened, tt.want)
			}
		})
	}
}
