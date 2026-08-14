package domain_test

import (
	"strings"
	"testing"

	"github.com/k-kudo-hub/mdev-go/internal/domain"
)

// TestDecideSelfUpdate は自バイナリを更新するかどうかの判断を固定する。
func TestDecideSelfUpdate(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		current string
		latest  string
		want    domain.SelfUpdateDecision
	}{
		{
			// 版が焼き込まれていないビルドは手元で組んだものである。
			// ここで配布物へ置き換えると、検証中の変更を黙って消す。
			name: "開発中のビルドは何もしない", current: "dev", latest: "v0.11.0",
			want: domain.SelfUpdateSkipDev,
		},
		{name: "空の版も開発中扱い", current: "", latest: "v0.11.0", want: domain.SelfUpdateSkipDev},
		{name: "新しい版がある", current: "v0.10.0", latest: "v0.11.0", want: domain.SelfUpdateNeeded},
		{name: "既に最新", current: "v0.11.0", latest: "v0.11.0", want: domain.SelfUpdateUpToDate},
		{name: "手元のほうが新しい", current: "v0.12.0", latest: "v0.11.0", want: domain.SelfUpdateUpToDate},
		// 比較できないなら動かない。
		{name: "配布元の版が読めない", current: "v0.10.0", latest: "", want: domain.SelfUpdateUpToDate},
		{name: "配布元の版が壊れている", current: "v0.10.0", latest: "latest", want: domain.SelfUpdateUpToDate},
		{
			// **git describe の形は手元のビルドである。** 以前は「読めない版は
			// v0.0.0」の正規化に任せていたため、常に更新対象になり、検証中の
			// ローカルビルドが黙って配布物へ置き換わっていた。
			name: "make build のビルドは対象外", current: "v0.10.1-1-gabc1234", latest: "v0.11.0",
			want: domain.SelfUpdateSkipDev,
		},
		{
			name: "未コミットの変更を含むビルドも対象外", current: "v0.10.1-1-gabc1234-dirty", latest: "v0.11.0",
			want: domain.SelfUpdateSkipDev,
		},
		{
			name: "タグの無いリポジトリのビルドも対象外", current: "abc1234", latest: "v0.11.0",
			want: domain.SelfUpdateSkipDev,
		},
		{
			name: "プレリリースも対象外", current: "v0.11.0-rc1", latest: "v0.12.0",
			want: domain.SelfUpdateSkipDev,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := domain.DecideSelfUpdate(tt.current, tt.latest); got != tt.want {
				t.Errorf("DecideSelfUpdate(%q, %q) = %v, want %v", tt.current, tt.latest, got, tt.want)
			}
		})
	}
}

// TestMdevAssetName は取得するバイナリの名前を固定する。
// tag.yml が添付する名前とずれると、更新が必ず失敗する。
func TestMdevAssetName(t *testing.T) {
	t.Parallel()

	tests := []struct {
		goos, goarch string
		want         string
		ok           bool
	}{
		{goos: "darwin", goarch: "arm64", want: "mdev_darwin_arm64", ok: true},
		{goos: "darwin", goarch: "amd64", want: "mdev_darwin_amd64", ok: true},
		// 配布しているのは darwin だけである(ADR-0004 D2)。
		{goos: "linux", goarch: "amd64", ok: false},
		{goos: "darwin", goarch: "386", ok: false},
	}
	for _, tt := range tests {
		t.Run(tt.goos+"/"+tt.goarch, func(t *testing.T) {
			t.Parallel()
			got, ok := domain.MdevAssetName(tt.goos, tt.goarch)
			if ok != tt.ok {
				t.Fatalf("MdevAssetName ok = %v, want %v", ok, tt.ok)
			}
			if ok && got != tt.want {
				t.Errorf("MdevAssetName = %q, want %q", got, tt.want)
			}
		})
	}
}

// TestBuildSelfUpdatePlan は取得先の URL を固定する。
func TestBuildSelfUpdatePlan(t *testing.T) {
	t.Parallel()

	got := domain.BuildSelfUpdatePlan("o/r", "v0.10.0", "v0.11.0", "mdev_darwin_arm64")
	if want := "https://github.com/o/r/releases/download/v0.11.0/mdev_darwin_arm64"; got.AssetURL != want {
		t.Errorf("AssetURL = %q, want %q", got.AssetURL, want)
	}
	if want := "https://github.com/o/r/releases/download/v0.11.0/checksums.txt"; got.ChecksumsURL != want {
		t.Errorf("ChecksumsURL = %q, want %q", got.ChecksumsURL, want)
	}
}

// TestFindChecksum は checksums.txt の読み取りを固定する。
//
// 期待値は `shasum -a 256` の実出力の形である(値 2 スペース 名前)。
func TestFindChecksum(t *testing.T) {
	t.Parallel()

	const contents = "fb58a9e41f857791dfb3e2bfcf45e5f893531934c06865e13d51f6df4aaf1516  mdev_darwin_amd64\n" +
		"bdbb817e79a5e5d9a63360bcf66a9c6f620fbac8e4507e271030ea39eace7211  mdev_darwin_arm64\n"

	got, ok := domain.FindChecksum(contents, "mdev_darwin_arm64")
	if !ok {
		t.Fatal("見つかりませんでした")
	}
	if want := "bdbb817e79a5e5d9a63360bcf66a9c6f620fbac8e4507e271030ea39eace7211"; got != want {
		t.Errorf("FindChecksum = %q, want %q", got, want)
	}

	// 照合できないものは必ず ok=false にする。呼び出し側が中止できないと、
	// 素性の分からないバイナリを実行ファイルとして置くことになる。
	for _, name := range []string{"mdev_darwin_386", "", "checksums.txt"} {
		if _, ok := domain.FindChecksum(contents, name); ok {
			t.Errorf("%q が見つかってしまいました", name)
		}
	}
}

// TestFindChecksumRejectsMalformed は形の違う行を拾わないことを確かめる。
func TestFindChecksumRejectsMalformed(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		contents string
	}{
		{name: "値が短い", contents: "abc  mdev_darwin_arm64\n"},
		{name: "列が多い", contents: strings.Repeat("a", 64) + "  mdev_darwin_arm64  extra\n"},
		{name: "名前だけ", contents: "mdev_darwin_arm64\n"},
		{name: "空", contents: ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if _, ok := domain.FindChecksum(tt.contents, "mdev_darwin_arm64"); ok {
				t.Error("壊れた行を拾いました")
			}
		})
	}
}

// TestIsDevBuild は自己更新の対象にしてよいビルドの見分け方を固定する。
//
// **リリースのバイナリだけが厳密な semver タグを名乗る。** ここを緩めると、
// 手元で組んだバイナリが配布物で上書きされ、検証中の変更が黙って消える。
func TestIsDevBuild(t *testing.T) {
	t.Parallel()

	tests := []struct {
		version string
		want    bool
	}{
		// リリースのバイナリ(自己更新の対象)。
		{version: "v0.11.0", want: false},
		{version: "v1.2.3", want: false},
		{version: "0.11.0", want: false},
		// 手元のビルド(対象外)。
		{version: "dev", want: true},
		{version: "", want: true},
		{version: "v0.10.1-1-gabc1234", want: true},
		{version: "v0.10.1-1-gabc1234-dirty", want: true},
		{version: "abc1234", want: true},
		{version: "v0.11.0-rc1", want: true},
		{version: "v0.11", want: true},
		{version: "latest", want: true},
	}
	for _, tt := range tests {
		t.Run(tt.version, func(t *testing.T) {
			t.Parallel()
			if got := domain.IsDevBuild(tt.version); got != tt.want {
				t.Errorf("IsDevBuild(%q) = %v, want %v", tt.version, got, tt.want)
			}
		})
	}
}
