package domain_test

import (
	"reflect"
	"testing"

	"github.com/k-kudo-hub/mdev-go/internal/domain"
)

func TestParseTabNames(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		output string
		want   []string
	}{
		{
			name:   "見出し行を捨てて 3 列目を拾う",
			output: "ID POS NAME\n1 x alpha\n2 x beta\n",
			want:   []string{"alpha", "beta"},
		},
		{
			// 現行は awk '{print $3}' なので、スペースを含むタブ名は
			// 3 列目の断片しか取れない。pending の .tab と一致しなくなり
			// Dashboard に出ない既知バグで、そのまま再現する。
			name:   "スペースを含むタブ名は断片になる",
			output: "ID POS NAME\n1 x my tab\n",
			want:   []string{"my"},
		},
		{
			name:   "3 列に満たない行は落ちる",
			output: "ID POS NAME\n1 x\n2 x beta\n",
			want:   []string{"beta"},
		},
		{
			name:   "空白は連続していても 1 つの区切りとして扱う",
			output: "ID POS NAME\n1    x    alpha\n",
			want:   []string{"alpha"},
		},
		{name: "見出しだけなら空", output: "ID POS NAME\n", want: []string{}},
		{name: "出力が空なら空", output: "", want: []string{}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := domain.ParseTabNames(tt.output); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("ParseTabNames(%q) = %v, want %v", tt.output, got, tt.want)
			}
		})
	}
}

func TestResolveTabID(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		output string
		tab    string
		want   string
	}{
		{
			name:   "先頭 2 列を落とした残りが一致する行の 1 列目",
			output: "ID POS NAME\n1 x alpha\n2 x beta\n",
			tab:    "beta",
			want:   "2",
		},
		{
			// 一覧の取得(awk $3)と違い、id 解決はスペース入りのタブ名に
			// 対応している。現行版の非対称をそのまま持ち込む。
			name:   "スペースを含むタブ名も解決できる",
			output: "ID POS NAME\n1 x my tab\n",
			tab:    "my tab",
			want:   "1",
		},
		{
			name:   "一致しなければ空",
			output: "ID POS NAME\n1 x alpha\n",
			tab:    "ghost",
			want:   "",
		},
		{
			name:   "見出し行は対象外",
			output: "ID POS NAME\n1 x alpha\n",
			tab:    "NAME",
			want:   "",
		},
		{
			// 現行版はコマンド置換の結果が改行で連結された "1\n3" になり、
			// close-tab がその id を解釈できずタブが閉じ残る(pending と
			// レジストリだけが消える)。Go 版は先頭の一致だけを返して
			// 少なくとも 1 つは閉じられるようにする(意図的な差異)。
			name:   "複数一致は先頭の id だけを返す",
			output: "ID POS NAME\n1 x alpha\n2 x beta\n3 x alpha\n",
			tab:    "alpha",
			want:   "1",
		},
		{name: "出力が空なら空", output: "", tab: "alpha", want: ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := domain.ResolveTabID(tt.output, tt.tab); got != tt.want {
				t.Errorf("ResolveTabID(%q, %q) = %q, want %q", tt.output, tt.tab, got, tt.want)
			}
		})
	}
}

func TestScreenTabSlug(t *testing.T) {
	t.Parallel()

	// 期待値は実際の `_screen_tab_slug` を実行して測ったもの。
	//   safe = printf '%s' "$1" | tr -c 'A-Za-z0-9_.-' '_'
	//   hash = printf '%s' "$1" | cksum | awk '{print $1}'
	//
	// cksum は POSIX CRC で、Go の crc32.IEEE とは別物である(evidence 参照)。
	// tr の方はロケール依存で、UTF-8 ロケール(実行環境の LC_CTYPE=UTF-8)では
	// 1 文字が _ 1 つになる。LC_ALL=C だと 1 バイトが _ 1 つになるため
	// 日本語タブ名で結果が変わる。実行環境に合わせて文字単位で実装している。
	tests := []struct {
		name string
		tab  string
		want string
	}{
		{name: "英数字とハイフンはそのまま", tab: "my-task", want: "my-task-805046993"},
		{name: "1 文字", tab: "a", want: "a-1220704766"},
		{name: "空文字", tab: "", want: "-4294967295"},
		{
			// UTF-8 ロケールの tr は 1 文字を _ 1 つに置き換える。
			name: "日本語は文字数ぶんの _ になる",
			tab:  "あいう",
			want: "___-2085384042",
		},
		{name: "スペースは _ になる", tab: "hello world", want: "hello_world-1135714720"},
		{name: "日本語とハイフンの混在", tab: "タスク-01", want: "___-01-268066415"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := domain.ScreenTabSlug(tt.tab); got != tt.want {
				t.Errorf("ScreenTabSlug(%q) = %q, want %q", tt.tab, got, tt.want)
			}
		})
	}
}
