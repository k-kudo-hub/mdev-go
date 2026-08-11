package domain

import (
	"regexp"
	"strconv"
	"strings"
)

// ZeroVersion は版が分からないときに使う値。
//
// 現行 update-lib.sh は VERSION ファイルが無いときだけ v0.0.0 へ落とし、
// 中身が空や semver でない場合はそのまま算術へ渡してエラーになる
// (比較そのものが失敗し、更新の判定が壊れる)。Go 版は解釈できない値を
// すべてここへ落とす(意図的な修正)。
const ZeroVersion = "v0.0.0"

// semverPattern は採用する版の書き方である。
// 現行 uc_latest_tag の `grep -E '^v[0-9]+\.[0-9]+\.[0-9]+$'` に対応する。
var semverPattern = regexp.MustCompile(`^v?([0-9]+)\.([0-9]+)\.([0-9]+)$`)

// tagRefPrefix は ls-remote が返す参照名の接頭辞。
const tagRefPrefix = "refs/tags/"

// Version は major.minor.patch の 3 つ組である。
type Version struct {
	Major, Minor, Patch int
}

// ParseVersion は "v1.2.3" / "1.2.3" を読む。
//
// 解釈できない場合は ok=false と 0.0.0 を返す。桁の先頭に 0 が付いていても
// 10 進として読む(現行版が `10#` を付けているのと同じで、8 進と誤解しない)。
func ParseVersion(s string) (Version, bool) {
	match := semverPattern.FindStringSubmatch(strings.TrimSpace(s))
	if match == nil {
		return Version{}, false
	}
	// 正規表現が数字だけを通しているので Atoi は桁あふれでしか失敗しない。
	major, err1 := strconv.Atoi(match[1])
	minor, err2 := strconv.Atoi(match[2])
	patch, err3 := strconv.Atoi(match[3])
	if err1 != nil || err2 != nil || err3 != nil {
		return Version{}, false
	}
	return Version{Major: major, Minor: minor, Patch: patch}, true
}

// NormalizeVersion は解釈できない版を ZeroVersion に揃える。
// VERSION ファイルが空・欠落・壊れている場合の受け皿である。
func NormalizeVersion(s string) string {
	if _, ok := ParseVersion(s); !ok {
		return ZeroVersion
	}
	return strings.TrimSpace(s)
}

// VersionGreater は a が b より新しいかを返す(現行 uc_version_gt)。
//
// 比較は major → minor → patch の順の数値比較で、辞書順ではない
// (v1.2.10 は v1.2.9 より新しい)。解釈できない側は 0.0.0 として扱う。
func VersionGreater(a, b string) bool {
	left, _ := ParseVersion(a)
	right, _ := ParseVersion(b)
	if left.Major != right.Major {
		return left.Major > right.Major
	}
	if left.Minor != right.Minor {
		return left.Minor > right.Minor
	}
	return left.Patch > right.Patch
}

// RepoSlug は git の URL から "owner/repo" を取り出す(現行 uc_repo_slug)。
//
// SSH(git@github.com:owner/repo.git)・HTTPS・ssh:// のどれでも読めるよう、
// 末尾の .git を落としてコロンをスラッシュに変えてから、後ろ 2 つの区切りを
// 見る。ホスト名を解釈しないので GitHub 以外の URL でも同じように働く。
//
// 区切りが足りない(パスを持たない)文字列は ok=false になる。owner と repo が
// 同じ名前(torvalds/torvalds)は正当なので弾かない。
func RepoSlug(url string) (string, bool) {
	if url == "" {
		return "", false
	}
	url = strings.TrimSuffix(url, ".git")
	url = strings.ReplaceAll(url, ":", "/")

	repo := url
	rest := url
	if i := strings.LastIndex(url, "/"); i >= 0 {
		repo = url[i+1:]
		rest = url[:i]
	}
	owner := rest
	if i := strings.LastIndex(rest, "/"); i >= 0 {
		owner = rest[i+1:]
	}
	// owner == rest は「区切りが 1 つも無かった」ことを意味する。
	if owner == "" || repo == "" || owner == rest {
		return "", false
	}
	return owner + "/" + repo, true
}

// LatestSemverTag は `git ls-remote --tags` の出力から最大の semver タグを返す。
//
// 出力の 2 列目が参照名で、`refs/tags/` を外した名前が
// `v<数>.<数>.<数>` のものだけを見る。`^{}`(注釈付きタグの実体)や
// プレリリースは形が合わないので自然に外れる。
//
// 比較は数値なので、v1.2.10 が v1.2.9 より後になる。1 つも無ければ
// ok=false を返し、呼び出し側は「確認できなかった」として黙って諦める。
func LatestSemverTag(output string) (string, bool) {
	var latest Version
	found := false
	for _, line := range strings.Split(output, "\n") {
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		name := strings.Replace(fields[1], tagRefPrefix, "", 1)
		version, ok := ParseVersion(name)
		if !ok || !strings.HasPrefix(name, "v") {
			continue
		}
		if !found || greater(version, latest) {
			latest, found = version, true
		}
	}
	if !found {
		return "", false
	}
	return "v" + strconv.Itoa(latest.Major) + "." +
		strconv.Itoa(latest.Minor) + "." + strconv.Itoa(latest.Patch), true
}

// greater は Version 同士の大小を返す。
func greater(a, b Version) bool {
	if a.Major != b.Major {
		return a.Major > b.Major
	}
	if a.Minor != b.Minor {
		return a.Minor > b.Minor
	}
	return a.Patch > b.Patch
}

// RenderUpdateNotice は新しい版があるときの案内を組み立てる。
//
// 文言と字下げは現行 check-update.sh:49-52 の 4 行そのままである。
// 前後の空行は、zellij が画面を取る前に利用者が読めるようにするための
// 間である。
func RenderUpdateNotice(latest, current string) string {
	return "\n" +
		"  📦 新しいバージョン " + latest + " があります（現在: " + current + "）。\n" +
		"     'mdev update' で更新できます。\n" +
		"\n"
}

// TarballURL は GitHub のソース tarball の URL を返す。
// 現行 update.sh の
// `https://github.com/$SLUG/archive/refs/tags/$LATEST.tar.gz` に対応する。
func TarballURL(slug, tag string) string {
	return "https://github.com/" + slug + "/archive/refs/tags/" + tag + ".tar.gz"
}

// 更新コマンドが出す文言。現行 update.sh の日本語と装飾をそのまま写す。

// RenderUpdateChecking は最新版の確認を始めるときの 1 行を返す。
func RenderUpdateChecking() string {
	return ansiBold + "最新バージョンを確認しています..." + ansiReset + "\n"
}

// RenderUpdateUpToDate は既に最新だったときの 1 行を返す。
func RenderUpdateUpToDate(current string) string {
	return "既に最新です（" + current + "）。\n"
}

// RenderUpdateStarting は更新に取りかかるときの 1 行を返す。
func RenderUpdateStarting(current, latest string) string {
	return current + " -> " + latest + " に更新します...\n"
}

// RenderUpdateDone は更新が終わったときの 2 行を返す。
func RenderUpdateDone(latest string) string {
	return "\n" + ansiGreen + ansiBold + "✅ " + latest + " に更新しました。" + ansiReset + "\n"
}
