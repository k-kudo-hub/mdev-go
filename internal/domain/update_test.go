package domain_test

import (
	"testing"

	"github.com/k-kudo-hub/mdev-go/internal/domain"
)

// TestRepoSlug は test.sh「50. update-lib.sh」の repo_slug のケースを移植する。
func TestRepoSlug(t *testing.T) {
	tests := []struct {
		name string
		url  string
		want string
		ok   bool
	}{
		{name: "SSH url", url: "git@github.com:owner/repo.git", want: "owner/repo", ok: true},
		{name: "HTTPS url(.git 付き)", url: "https://github.com/owner/repo.git", want: "owner/repo", ok: true},
		{name: "HTTPS url(.git 無し)", url: "https://github.com/owner/repo", want: "owner/repo", ok: true},
		{name: "ssh:// url", url: "ssh://git@github.com/owner/repo.git", want: "owner/repo", ok: true},
		// owner と repo が同じ名前は正当なので弾かない。
		{name: "owner == repo", url: "git@github.com:torvalds/torvalds.git", want: "torvalds/torvalds", ok: true},
		// GitHub 以外でもホスト名を解釈しないので同じように読める。
		{name: "GitHub 以外のホスト", url: "https://git.example.com/g/sub/repo.git", want: "sub/repo", ok: true},
		{name: "ローカルパス", url: "/var/log-repo.git", want: "var/log-repo", ok: true},
		{name: "空文字は失敗", url: "", ok: false},
		{name: "区切りの無い文字列は失敗", url: "notaurl", ok: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := domain.RepoSlug(tt.url)
			if ok != tt.ok {
				t.Fatalf("RepoSlug(%q) ok = %v, want %v (got %q)", tt.url, ok, tt.ok, got)
			}
			if ok && got != tt.want {
				t.Errorf("RepoSlug(%q) = %q, want %q", tt.url, got, tt.want)
			}
		})
	}
}

// TestVersionGreater は test.sh「50. version_gt」のケースを移植する。
func TestVersionGreater(t *testing.T) {
	tests := []struct {
		name string
		a, b string
		want bool
	}{
		{name: "patch が新しい", a: "v1.2.4", b: "v1.2.3", want: true},
		{name: "minor は patch より強い", a: "v1.3.0", b: "v1.2.9", want: true},
		{name: "major は minor より強い", a: "v2.0.0", b: "v1.9.9", want: true},
		{name: "辞書順ではなく数値で比べる", a: "v1.2.10", b: "v1.2.9", want: true},
		{name: "v 無しも受ける", a: "1.2.4", b: "1.2.3", want: true},
		{name: "同じなら新しくない", a: "v1.2.3", b: "v1.2.3", want: false},
		{name: "古いほうは新しくない", a: "v1.2.3", b: "v1.2.4", want: false},
		// 先頭に 0 が付いていても 10 進として読む(8 進と誤解しない)。
		{name: "先頭 0 は 10 進", a: "v1.2.08", b: "v1.2.7", want: true},
		// 現行版はここで算術エラーになり比較そのものが壊れる。
		{name: "空の現在版は v0.0.0 扱い", a: "v0.1.0", b: "", want: true},
		{name: "壊れた現在版は v0.0.0 扱い", a: "v0.0.1", b: "not-a-version", want: true},
		{name: "壊れた最新版は新しくない", a: "garbage", b: "v0.0.1", want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := domain.VersionGreater(tt.a, tt.b); got != tt.want {
				t.Errorf("VersionGreater(%q, %q) = %v, want %v", tt.a, tt.b, got, tt.want)
			}
		})
	}
}

// TestNormalizeVersion は VERSION ファイルの中身の正規化を固定する。
func TestNormalizeVersion(t *testing.T) {
	tests := []struct{ in, want string }{
		{"v1.2.3", "v1.2.3"},
		{"1.2.3", "1.2.3"},
		{"v1.2.3\n", "v1.2.3"},
		{"", "v0.0.0"},
		{"   ", "v0.0.0"},
		{"1.2", "v0.0.0"},
		{"v1.2.3-rc1", "v0.0.0"},
	}
	for _, tt := range tests {
		if got := domain.NormalizeVersion(tt.in); got != tt.want {
			t.Errorf("NormalizeVersion(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

// TestLatestSemverTag は ls-remote の出力から最大のタグを選ぶことを確かめる。
func TestLatestSemverTag(t *testing.T) {
	tests := []struct {
		name   string
		output string
		want   string
		ok     bool
	}{
		{
			name: "最大の semver タグを選ぶ",
			output: "abc123\trefs/tags/v0.1.0\n" +
				"def456\trefs/tags/v0.2.0\n",
			want: "v0.2.0",
			ok:   true,
		},
		{
			// 並びは入力順ではなく数値の大小で決まる。
			name: "並び順に依らず最大を選ぶ",
			output: "a\trefs/tags/v1.2.10\n" +
				"b\trefs/tags/v1.2.9\n" +
				"c\trefs/tags/v1.10.0\n",
			want: "v1.10.0",
			ok:   true,
		},
		{
			// 注釈付きタグの実体(^{})とプレリリースは形が合わない。
			name: "semver でない参照は無視する",
			output: "a\trefs/tags/v0.1.0\n" +
				"b\trefs/tags/v0.1.0^{}\n" +
				"c\trefs/tags/v0.9.0-rc1\n" +
				"d\trefs/tags/latest\n" +
				"e\trefs/heads/main\n",
			want: "v0.1.0",
			ok:   true,
		},
		{name: "semver タグが無ければ失敗", output: "a\trefs/heads/main\n", ok: false},
		{name: "空の出力は失敗", output: "", ok: false},
		{name: "列が足りない行は無視する", output: "refs/tags/v1.0.0\n", ok: false},
		{
			// v 無しのタグは現行版の grep が弾く。
			name:   "v の無いタグは採らない",
			output: "a\trefs/tags/1.0.0\n",
			ok:     false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := domain.LatestSemverTag(tt.output)
			if ok != tt.ok {
				t.Fatalf("LatestSemverTag ok = %v, want %v (got %q)", ok, tt.ok, got)
			}
			if ok && got != tt.want {
				t.Errorf("LatestSemverTag = %q, want %q", got, tt.want)
			}
		})
	}
}
