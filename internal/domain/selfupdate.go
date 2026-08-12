package domain

import (
	"runtime"
	"strings"
)

// MdevRepoSlug は mdev 自身の配布元である。
//
// 6-3 で REPO_URL の一本化を行う際に、conductor 側の更新元と合わせて
// 整理する予定である(ADR-0004)。それまでは自己更新の取得先として
// ここに置く。
const MdevRepoSlug = "k-kudo-hub/mdev-go"

// DevVersion はビルド時に版を焼き込まなかった場合の値である。
// cli 側にも同じ値の定数があるが、domain は他層へ依存できないため持つ。
const DevVersion = "dev"

// ChecksumsAssetName はリリースに添付される SHA-256 の一覧である。
const ChecksumsAssetName = "checksums.txt"

// SelfUpdatePlan は自バイナリの更新で何をするかである。
type SelfUpdatePlan struct {
	// Current は今動いているバイナリの版。
	Current string
	// Latest は配布元の最新版。
	Latest string
	// AssetName は取得するバイナリの名前。
	AssetName string
	// AssetURL は取得するバイナリの URL。
	AssetURL string
	// ChecksumsURL は SHA-256 の一覧の URL。
	ChecksumsURL string
}

// SelfUpdateDecision は自バイナリを更新するかどうかの判断である。
type SelfUpdateDecision int

const (
	// SelfUpdateSkipDev は開発中のビルドなので何もしないことを表す。
	SelfUpdateSkipDev SelfUpdateDecision = iota
	// SelfUpdateUpToDate は既に最新であることを表す。
	SelfUpdateUpToDate
	// SelfUpdateNeeded は新しい版があることを表す。
	SelfUpdateNeeded
)

// DecideSelfUpdate は自バイナリを更新すべきかを返す。
//
// **開発中のビルド("dev")では何もしない。** 版が焼き込まれていないビルドは
// 手元で組んだものであり、比較する土台が無い。ここで配布物を取ってきて
// 置き換えると、検証中の変更を含むバイナリを黙って消すことになる。
//
// 配布元の版が読めない場合も何もしない(比較できないため)。
func DecideSelfUpdate(current, latest string) SelfUpdateDecision {
	if strings.TrimSpace(current) == "" || strings.TrimSpace(current) == DevVersion {
		return SelfUpdateSkipDev
	}
	if _, ok := ParseVersion(latest); !ok {
		return SelfUpdateUpToDate
	}
	if VersionGreater(latest, NormalizeVersion(current)) {
		return SelfUpdateNeeded
	}
	return SelfUpdateUpToDate
}

// MdevAssetName は今動いている環境向けのバイナリの名前を返す。
//
// リリースに添付されるのは darwin の 2 種類だけである(ADR-0004 D2)。
// それ以外の環境では ok=false を返し、呼び出し側は自己更新を行わない。
func MdevAssetName(goos, goarch string) (string, bool) {
	if goos != "darwin" {
		return "", false
	}
	if goarch != "arm64" && goarch != "amd64" {
		return "", false
	}
	return "mdev_" + goos + "_" + goarch, true
}

// CurrentAssetName は今のプロセスが動いている環境向けの名前を返す。
func CurrentAssetName() (string, bool) {
	return MdevAssetName(runtime.GOOS, runtime.GOARCH)
}

// BuildSelfUpdatePlan は自バイナリの更新の段取りを組み立てる。
//
// 取得先は GitHub Release の固定の並びである。
//
//	https://github.com/<slug>/releases/download/<タグ>/<名前>
func BuildSelfUpdatePlan(slug, current, latest, assetName string) SelfUpdatePlan {
	base := "https://github.com/" + slug + "/releases/download/" + latest + "/"
	return SelfUpdatePlan{
		Current:      current,
		Latest:       latest,
		AssetName:    assetName,
		AssetURL:     base + assetName,
		ChecksumsURL: base + ChecksumsAssetName,
	}
}

// FindChecksum は checksums.txt から名前に対応する SHA-256 を取り出す。
//
// 中身は `shasum -a 256` の出力で、1 行が `<16 進の値>  <名前>` である。
// 見つからない場合は ok=false を返す。**呼び出し側は必ず中止すること。**
// 照合できないバイナリを実行ファイルとして置くわけにはいかない。
func FindChecksum(contents, name string) (string, bool) {
	for _, line := range strings.Split(contents, "\n") {
		fields := strings.Fields(line)
		if len(fields) != 2 || fields[1] != name {
			continue
		}
		if len(fields[0]) != sha256HexLength {
			continue
		}
		return strings.ToLower(fields[0]), true
	}
	return "", false
}

// sha256HexLength は SHA-256 を 16 進で書いたときの長さである。
const sha256HexLength = 64

// 自バイナリの更新で出す文言。

// RenderSelfUpdateSkipped は開発中のビルドで自己更新を飛ばすときの断りを返す。
//
// 黙って飛ばさないのは、`mdev update` を叩いた利用者が「更新された」と
// 思い込まないようにするためである。手元のビルドを使っていること自体は
// 異常ではないので、警告の色で 1 行だけ出す。
func RenderSelfUpdateSkipped(current string) string {
	return ansiYellow + "警告: mdev 自身は更新しません(版が焼き込まれていないビルドです: " +
		current + ")" + ansiReset + "\n"
}

// RenderSelfUpdateStarting は自バイナリの更新に取りかかるときの 1 行を返す。
func RenderSelfUpdateStarting(current, latest string) string {
	return "mdev 自身を " + current + " -> " + latest + " に更新します...\n"
}

// RenderSelfUpdateReplaced は自バイナリを置き換えたときの案内を返す。
//
// **ここで処理を終える。** 今動いているプロセスは置き換える前の中身のまま
// なので、続けて conductor の資産を更新すると古い実装で行うことになる。
// 実行し直してもらえば、すべてを新しい mdev が行う。
func RenderSelfUpdateReplaced(latest, path string) string {
	return ansiGreen + ansiBold + "✅ mdev 自身を " + latest + " に更新しました。" + ansiReset + "\n" +
		"   " + path + "\n" +
		"\n" +
		"   続けて conductor の資産を更新します。" +
		"もう一度 `mdev update` を実行してください。\n"
}
