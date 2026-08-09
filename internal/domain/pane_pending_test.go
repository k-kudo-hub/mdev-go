package domain_test

import (
	"reflect"
	"testing"

	"github.com/k-kudo-hub/mdev-go/internal/domain"
)

// tabsOf は表示順の確認用にタブ名だけを取り出す。
func tabsOf(views []domain.PendingView) []string {
	out := make([]string, 0, len(views))
	for _, v := range views {
		out = append(out, v.Tab)
	}
	return out
}

func TestParsePendingView(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		data string
		want domain.PendingView
	}{
		{
			name: "全フィールドが揃っている",
			data: `{"tab":"alpha","event":"Stop","message":"done","time":"10:00:00","agent":"codex"}`,
			want: domain.PendingView{
				Name: "p.json", Tab: "alpha", Event: "Stop",
				Message: "done", Time: "10:00:00", Agent: "codex",
			},
		},
		{
			// jq -r は欠けたキーを null として読み、-r でも文字列 "null" を出す。
			name: "欠けたキーは null という文字列になる",
			data: `{"tab":"alpha"}`,
			want: domain.PendingView{
				Name: "p.json", Tab: "alpha", Event: "null",
				Message: "null", Time: "null", Agent: "null",
			},
		},
		{
			// 壊れた JSON では jq が失敗し、コマンド置換の結果は空文字になる。
			// "null" ではなく空文字である点が欠けたキーとの違いで、
			// タブ名の一致判定から必ず外れる(= 表示されない)。
			name: "壊れた JSON は全フィールドが空文字",
			data: `{broken`,
			want: domain.PendingView{Name: "p.json"},
		},
		{
			name: "空ファイルも全フィールドが空文字",
			data: ``,
			want: domain.PendingView{Name: "p.json"},
		},
		{
			// jq -r はスカラーをそのままの表現で出す(実測で確認)。
			//   jq -r .tab     <- 123   => 123
			//   jq -r .event   <- true  => true
			//   jq -r .message <- null  => null
			//   jq -r .time    <- 1.5   => 1.5
			name: "文字列以外のスカラーはそのままの表現になる",
			data: `{"tab":123,"event":true,"message":null,"time":1.5,"agent":"claude"}`,
			want: domain.PendingView{
				Name: "p.json", Tab: "123", Event: "true",
				Message: "null", Time: "1.5", Agent: "claude",
			},
		},
		{
			// トップレベルがオブジェクトでないと jq の `.tab` がエラーになる。
			name: "トップレベルが配列なら全フィールドが空文字",
			data: `["a"]`,
			want: domain.PendingView{Name: "p.json"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := domain.ParsePendingView("p.json", []byte(tt.data))
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("ParsePendingView() = %+v, want %+v", got, tt.want)
			}
		})
	}
}

func TestDashboardItems(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		tabOrder []string
		entries  []domain.PendingView
		want     []string
	}{
		{
			// 外側がタブ順、内側が glob(ファイル名)順。
			name:     "タブ順が優先され同じタブ内はファイル名順",
			tabOrder: []string{"beta", "alpha"},
			entries: []domain.PendingView{
				{Name: "a.json", Tab: "alpha", Event: "Stop"},
				{Name: "b.json", Tab: "beta", Event: "Stop"},
				{Name: "c.json", Tab: "alpha", Event: "Notification"},
			},
			want: []string{"beta", "alpha", "alpha"},
		},
		{
			name:     "Waiting は除外される",
			tabOrder: []string{"alpha", "beta"},
			entries: []domain.PendingView{
				{Name: "a.json", Tab: "alpha", Event: "Notification"},
				{Name: "b.json", Tab: "beta", Event: "Waiting"},
			},
			want: []string{"alpha"},
		},
		{
			name:     "タブ一覧に無い pending は表示されない",
			tabOrder: []string{"alpha"},
			entries: []domain.PendingView{
				{Name: "a.json", Tab: "alpha", Event: "Stop"},
				{Name: "b.json", Tab: "ghost", Event: "Stop"},
			},
			want: []string{"alpha"},
		},
		{
			name:     "壊れた JSON(タブ名が空)は表示されない",
			tabOrder: []string{"alpha"},
			entries: []domain.PendingView{
				{Name: "a.json"},
				{Name: "b.json", Tab: "alpha", Event: "Stop"},
			},
			want: []string{"alpha"},
		},
		{
			// 現行版は同名タブを畳まないため、同じ pending が 2 回並ぶ。
			name:     "タブ一覧に同名が 2 度出れば 2 度並ぶ",
			tabOrder: []string{"alpha", "alpha"},
			entries: []domain.PendingView{
				{Name: "a.json", Tab: "alpha", Event: "Stop"},
			},
			want: []string{"alpha", "alpha"},
		},
		{
			name:     "タブが 1 つも無ければ空",
			tabOrder: nil,
			entries: []domain.PendingView{
				{Name: "a.json", Tab: "alpha", Event: "Stop"},
			},
			want: []string{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := tabsOf(domain.DashboardItems(tt.tabOrder, tt.entries))
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("DashboardItems() タブ順 = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestWaitingItems(t *testing.T) {
	t.Parallel()

	// Waiting ペインはタブ順を見ず、ファイル名順のまま event == Waiting だけを拾う。
	entries := []domain.PendingView{
		{Name: "a.json", Tab: "alpha", Event: "Notification"},
		{Name: "b.json", Tab: "beta", Event: "Waiting"},
		{Name: "c.json", Tab: "ghost", Event: "Waiting"},
		{Name: "d.json"},
	}
	want := []string{"beta", "ghost"}
	if got := tabsOf(domain.WaitingItems(entries)); !reflect.DeepEqual(got, want) {
		t.Errorf("WaitingItems() タブ順 = %v, want %v", got, want)
	}

	if got := domain.WaitingItems(nil); len(got) != 0 {
		t.Errorf("WaitingItems(nil) = %v, want 空", got)
	}
}

func TestTruncateBytes(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		in    string
		limit int
		want  string
	}{
		{name: "上限以下はそのまま", in: "hello", limit: 60, want: "hello"},
		{name: "ちょうど上限", in: "abcde", limit: 5, want: "abcde"},
		{name: "バイト数で切る", in: "abcdef", limit: 5, want: "abcde"},
		{
			// head -c はバイト単位なので、マルチバイト文字の途中で切れる。
			// 実測: printf 'あいう' | head -c 4 | xxd -> e3 81 82 e3
			// 現行の表示を再現するため Go 側でも同じバイト列を返す。
			name:  "マルチバイト文字の途中で切れる",
			in:    "あいう",
			limit: 4,
			want:  "あ\xe3",
		},
		{name: "空文字", in: "", limit: 60, want: ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := domain.TruncateBytes(tt.in, tt.limit); got != tt.want {
				t.Errorf("TruncateBytes(%q, %d) = %q, want %q", tt.in, tt.limit, got, tt.want)
			}
		})
	}
}
