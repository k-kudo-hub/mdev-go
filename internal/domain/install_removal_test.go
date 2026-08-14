package domain_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/k-kudo-hub/mdev-go/internal/domain"
)

// TestCheckRemovableRejectsDangerousPaths は消してはいけない場所を弾くことを
// 確かめる。
//
// CONDUCTOR_HOME は環境変数で外から与えられるため、空・相対パス・`/`・
// ホームそのもの、といった値がそのまま届きうる。os.RemoveAll はそれらを
// 黙って受け取るので、判断はこちらが持つ。
func TestCheckRemovableRejectsDangerousPaths(t *testing.T) {
	t.Parallel()

	const home = "/Users/dev"
	// 痕跡はどこにでもある、という最悪の状況で試す。それでも弾けなければ
	// 「痕跡があること」以外の条件が効いていない。
	always := func(string) bool { return true }

	tests := []struct {
		name string
		path string
		want string
	}{
		{name: "空", path: "", want: "場所が空です"},
		{name: "ルート", path: "/", want: "ルートディレクトリ"},
		{name: "ルート(末尾スラッシュの重複)", path: "//", want: "ルートディレクトリ"},
		{name: "ルート(相対の混ざった表記)", path: "/tmp/..", want: "ルートディレクトリ"},
		{name: "ホームそのもの", path: home, want: "ホームディレクトリそのもの"},
		{name: "ホームそのもの(末尾スラッシュ)", path: home + "/", want: "ホームディレクトリそのもの"},
		{name: "ホームそのもの(相対の混ざった表記)", path: home + "/x/..", want: "ホームディレクトリそのもの"},
		{name: "相対パス", path: ".claude-conductor", want: "絶対パスではありません"},
		{name: "相対パス(上へ登る)", path: "../..", want: "絶対パスではありません"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			err := domain.CheckRemovable(tt.path, home, always)
			if !errors.Is(err, domain.ErrUnsafeRemoval) {
				t.Fatalf("CheckRemovable = %v, want %v", err, domain.ErrUnsafeRemoval)
			}
			if !strings.Contains(err.Error(), tt.want) {
				t.Errorf("説明 = %q, want %q を含む", err, tt.want)
			}
		})
	}
}

// TestCheckRemovableRequiresInstallTrace は設置の痕跡が無い場所を弾くことを
// 確かめる。
//
// これが本命の防御である。他の条件を抜けても、利用者が CONDUCTOR_HOME を
// 書類ディレクトリなどへ向けていれば中身ごと消える。mdev が置いたものが
// 1 つも無い場所は、mdev が消してよい場所ではない。
func TestCheckRemovableRequiresInstallTrace(t *testing.T) {
	t.Parallel()

	const home = "/Users/dev"
	const target = "/Users/dev/Documents"

	err := domain.CheckRemovable(target, home, func(string) bool { return false })
	if !errors.Is(err, domain.ErrUnsafeRemoval) {
		t.Fatalf("CheckRemovable = %v, want %v", err, domain.ErrUnsafeRemoval)
	}
	if !strings.Contains(err.Error(), "痕跡") {
		t.Errorf("説明 = %q", err)
	}
}

// TestCheckRemovableAcceptsInstalledHome は設置済みの場所を通すことを確かめる。
func TestCheckRemovableAcceptsInstalledHome(t *testing.T) {
	t.Parallel()

	const home = "/Users/dev"
	const target = "/Users/dev/.claude-conductor"

	tests := []struct {
		name  string
		trace string
	}{
		{name: "bin/mdev がある", trace: target + "/bin/mdev"},
		{name: "VERSION がある", trace: target + "/VERSION"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			exists := func(p string) bool { return p == tt.trace }
			if err := domain.CheckRemovable(target, home, exists); err != nil {
				t.Errorf("CheckRemovable = %v, want nil", err)
			}
		})
	}
}

// TestCheckRemovableWithoutHome はホームが分からない場合を確かめる。
// 他の条件は変わらず効く。
func TestCheckRemovableWithoutHome(t *testing.T) {
	t.Parallel()

	always := func(string) bool { return true }
	if err := domain.CheckRemovable("/w/conductor", "", always); err != nil {
		t.Errorf("CheckRemovable = %v, want nil", err)
	}
	if err := domain.CheckRemovable("/", "", always); !errors.Is(err, domain.ErrUnsafeRemoval) {
		t.Errorf("CheckRemovable = %v, want %v", err, domain.ErrUnsafeRemoval)
	}
}
