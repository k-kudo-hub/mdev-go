package domain_test

import (
	"reflect"
	"testing"

	"github.com/k-kudo-hub/mdev-go/internal/domain"
)

func TestDefaultTaskName(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		dir      string
		taskType string
		want     string
	}{
		// test.sh 32 の期待値をそのまま移す。
		{"ディレクトリ名とタイプを繋ぐ", "/home/user/myapp", "dev", "myapp-dev"},
		{"末尾スラッシュがあっても basename が取れる", "/home/user/api-server/", "k8s", "api-server-k8s"},
		{"末尾スラッシュが複数でも同じ", "/home/user/api-server///", "dev", "api-server-dev"},
		{"相対パスでも末尾の要素を使う", "projects/tool", "docs", "tool-docs"},
		{"要素が 1 つだけならそれ自身", "myapp", "dev", "myapp-dev"},
		{"ルートは basename と同じく / になる", "/", "dev", "/-dev"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := domain.DefaultTaskName(tc.dir, tc.taskType); got != tc.want {
				t.Errorf("DefaultTaskName(%q, %q) = %q, want %q", tc.dir, tc.taskType, got, tc.want)
			}
		})
	}
}

func TestResolveTaskName(t *testing.T) {
	t.Parallel()

	// test.sh 34。空入力(Enter のみ)は既定名、入力があればそれを採る。
	if got := domain.ResolveTaskName("myapp-dev", ""); got != "myapp-dev" {
		t.Errorf("空入力 = %q, want myapp-dev", got)
	}
	if got := domain.ResolveTaskName("myapp-dev", "custom-name"); got != "custom-name" {
		t.Errorf("手入力 = %q, want custom-name", got)
	}
	// 空白だけの入力は「入力あり」として扱う(現行版の `[[ -z ]]` は
	// 空白を空とは見ない)。
	if got := domain.ResolveTaskName("myapp-dev", " "); got != " " {
		t.Errorf("空白入力 = %q, want %q", got, " ")
	}
}

func TestFilterCandidates(t *testing.T) {
	t.Parallel()

	items := []string{
		"/Users/me/projects/claude-conductor",
		"/Users/me/projects/mdev-go",
		"/Users/me/works/api-server",
		"/Users/me/works/Admin-Console",
	}

	tests := []struct {
		name  string
		query string
		want  []string
	}{
		{
			name:  "空のクエリは全件をそのまま返す",
			query: "",
			want:  items,
		},
		{
			name:  "部分列で絞り込む(連続していなくてよい)",
			query: "mdvg",
			want:  []string{"/Users/me/projects/mdev-go"},
		},
		{
			name:  "連続した部分文字列も当然一致する",
			query: "works",
			want:  []string{"/Users/me/works/api-server", "/Users/me/works/Admin-Console"},
		},
		{
			name:  "大文字小文字は区別しない",
			query: "admin",
			want:  []string{"/Users/me/works/Admin-Console"},
		},
		{
			name:  "元の並びは保たれる",
			query: "e",
			want:  items,
		},
		{
			name:  "一致しなければ空",
			query: "zzz",
			want:  []string{},
		},
		{
			name:  "順序が違う文字列は一致しない(部分列なので)",
			query: "og-vedm",
			want:  []string{},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := domain.FilterCandidates(items, tc.query)
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("FilterCandidates(%q) = %v, want %v", tc.query, got, tc.want)
			}
		})
	}
}

func TestFilterCandidatesMultibyte(t *testing.T) {
	t.Parallel()

	items := []string{"/Users/me/projects/日本語プロジェクト", "/Users/me/projects/other"}
	// 部分列の判定はルーン単位で行う(バイト単位だと多バイト文字の途中で
	// 一致してしまう)。
	if got, want := domain.FilterCandidates(items, "日本"), items[:1]; !reflect.DeepEqual(got, want) {
		t.Errorf("FilterCandidates(日本) = %v, want %v", got, want)
	}
	if got := domain.FilterCandidates(items, "語日"); len(got) != 0 {
		t.Errorf("順序違いは一致しないこと: %v", got)
	}
}

func TestExpandHome(t *testing.T) {
	t.Parallel()

	// 現行版の `"${d/#\~/$HOME}"` は**先頭の ~ 1 つだけ**を置き換える。
	tests := []struct {
		in   string
		want string
	}{
		{"~/projects", "/home/u/projects"},
		{"~", "/home/u"},
		{"/abs/~/x", "/abs/~/x"},
		{"no-tilde", "no-tilde"},
		{"~~/x", "/home/u~/x"},
		{"", ""},
	}
	for _, tc := range tests {
		t.Run(tc.in, func(t *testing.T) {
			t.Parallel()
			if got := domain.ExpandHome(tc.in, "/home/u"); got != tc.want {
				t.Errorf("ExpandHome(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

func TestFilterCandidateIndexes(t *testing.T) {
	t.Parallel()

	// 位置で返すのは、選択 UI が「元の何番目か」を要るためである。
	// 値だけを返して突き合わせで引き直すと、同じ文字列が 2 つある一覧で
	// 最初の 1 つに寄ってしまう(ディレクトリ名は重複しうる)。
	items := []string{"/a/dup", "/b/other", "/b/dup"}

	if got, want := domain.FilterCandidateIndexes(items, "dup"), []int{0, 2}; !reflect.DeepEqual(got, want) {
		t.Errorf("FilterCandidateIndexes(dup) = %v, want %v", got, want)
	}
	if got, want := domain.FilterCandidateIndexes(items, ""), []int{0, 1, 2}; !reflect.DeepEqual(got, want) {
		t.Errorf("FilterCandidateIndexes(空) = %v, want %v", got, want)
	}
	if got := domain.FilterCandidateIndexes(items, "zzz"); len(got) != 0 {
		t.Errorf("FilterCandidateIndexes(zzz) = %v, want 空", got)
	}
}
