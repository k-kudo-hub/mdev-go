package domain_test

import (
	"strings"
	"testing"

	"github.com/k-kudo-hub/mdev-go/internal/domain"
)

// TestMaskURLCredentials は画面へ出す前に資格情報を伏せることを確かめる。
//
// upload.repo に認証情報付きの URL を書いている利用者では、push の失敗
// メッセージにそれがそのまま載り、画面とスクロールバックに残る。
func TestMaskURLCredentials(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		in   string
		want string
	}{
		{
			name: "利用者名と秘密",
			in:   "https://x-access-token:ghp_0123456789abcdef0123@github.com/o/r.git",
			want: "https://***@github.com/o/r.git",
		},
		{
			name: "利用者名だけ",
			in:   "ssh://git@github.com/o/r.git",
			want: "ssh://***@github.com/o/r.git",
		},
		{
			name: "空の秘密",
			in:   "https://user:@example.com/x",
			want: "https://***@example.com/x",
		},
		{
			name: "単体のトークン",
			in:   "認証に失敗しました: ghp_0123456789abcdefghij",
			want: "認証に失敗しました: ***",
		},
		{
			name: "GitLab のトークン",
			in:   "token=glpat-0123456789abcdefghij",
			want: "token=***",
		},
		{
			name: "資格情報の無い URL は触らない",
			in:   "https://github.com/o/r.git を見てください",
			want: "https://github.com/o/r.git を見てください",
		},
		{
			name: "メールアドレスは URL ではないので触らない",
			in:   "連絡先: dev@example.com",
			want: "連絡先: dev@example.com",
		},
		{
			name: "説明の文はそのまま残す",
			in:   "ログリポジトリへのpushに失敗しました",
			want: "ログリポジトリへのpushに失敗しました",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := domain.MaskURLCredentials(tt.in); got != tt.want {
				t.Errorf("MaskURLCredentials(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

// TestMaskURLCredentialsKeepsDiagnosis は伏せすぎないことを確かめる。
//
// 何が失敗したのかが読めなくなると、画面へ理由を出す意味が無くなる。
func TestMaskURLCredentialsKeepsDiagnosis(t *testing.T) {
	t.Parallel()

	const in = "ログリポジトリへのpushに失敗しました: " +
		"https://x-access-token:ghp_0123456789abcdef0123@github.com/o/r.git: 認証エラー"
	got := domain.MaskURLCredentials(in)

	for _, want := range []string{"ログリポジトリへのpushに失敗しました", "github.com/o/r.git", "認証エラー"} {
		if !strings.Contains(got, want) {
			t.Errorf("%q が失われた: %q", want, got)
		}
	}
	if strings.Contains(got, "ghp_") {
		t.Errorf("秘密が残っている: %q", got)
	}
}

// TestMaskURLCredentialsDoesNotChangeFilterSecrets は 2 つの関数が別物で
// あることを確かめる。
//
// FilterSecrets の出力は golden テストでバイト単位に固定されており、表示の
// ために伏せる範囲を足すとその一致が崩れる。
func TestMaskURLCredentialsDoesNotChangeFilterSecrets(t *testing.T) {
	t.Parallel()

	const in = "ssh://git@github.com/o/r.git"
	// FilterSecrets は行単位の処理なので末尾に改行が付く(現行版の awk と
	// 同じ)。伏せているかどうかだけを見る。
	if got := strings.TrimRight(domain.FilterSecrets(in), "\n"); got != in {
		t.Errorf("FilterSecrets が表示用のマスクを行っている: %q", got)
	}
	if got := domain.MaskURLCredentials(in); got == in {
		t.Errorf("MaskURLCredentials が伏せていない: %q", got)
	}
}
