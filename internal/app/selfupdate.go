package app

import (
	"errors"
	"fmt"
	"io"

	"github.com/k-kudo-hub/mdev-go/internal/domain"
)

// DevVersion はビルド時に版を焼き込まなかった場合の値である。
// cli は app にしか依存できない(ADR-0002)ため、境界に出す名前は app が持つ。
const DevVersion = domain.DevVersion

// ErrSelfUpdateNotStarted は **置き換えに踏み切る前** に失敗したことを表す。
//
// 取得できなかった・照合が合わなかった、といった失敗がこれに当たる。
// この時点では実行ファイルに一切触れていないので、呼び出し側は警告を出して
// 先(conductor 資産の更新)へ進んでよい。自分を新しくできないことと、
// 資産を新しくできることは別の話である。
var ErrSelfUpdateNotStarted = errors.New("自バイナリの置き換えは開始していません")

// SelfUpdater は mdev 自身のバイナリを新しくする。
//
// conductor の資産(scripts / layouts / hooks)の更新とは別物である。
// あちらは tarball を取って install.sh を走らせるもので、こちらは
// 実行ファイルそのものを差し替える。
type SelfUpdater struct {
	// Version は今動いているバイナリの版(ビルド時に焼き込んだもの)。
	Version string
	// Remote は配布元の最新タグを引く。
	Remote RemoteTagLister
	// Replacer は取得したバイナリで自分自身を置き換える。
	Replacer SelfReplacer
}

// SelfUpdateResult は自バイナリの更新の結果である。
type SelfUpdateResult struct {
	// Replaced は置き換えたかどうか。
	//
	// 真のときは **今動いているプロセスが古いまま** である。呼び出し側は
	// 続きの処理へ進まず、利用者へ実行し直しを案内する。
	Replaced bool
	// Latest は置き換えた先の版。
	Latest string
}

// Run は必要なら自バイナリを置き換える。
//
// 何もしなかった場合(既に最新・開発中のビルド・環境が配布対象外・配布元へ
// 到達できない)は Replaced=false で戻り、**error は返さない**。自分の更新に
// 失敗しても、続けて行う conductor 資産の更新まで巻き添えにする理由は無い。
// 置き換えに踏み切った後の失敗だけを error にする(素性の分からない
// バイナリを置いた可能性を黙って流さないため)。
//
// 進み具合は out へ書く。
func (u *SelfUpdater) Run(out io.Writer) (SelfUpdateResult, error) {
	// **配布元へ問い合わせる前に**、そもそも更新してよいビルドかを見る。
	// 手元で組んだバイナリのために ls-remote を撃つ意味は無い。
	if domain.IsDevBuild(u.Version) {
		write(out, domain.RenderSelfUpdateSkipped(u.Version))
		return SelfUpdateResult{}, nil
	}

	assetName, supported := domain.CurrentAssetName()
	if !supported {
		// 配布しているのは darwin の 2 種類だけである。
		return SelfUpdateResult{}, nil
	}

	latest, ok := u.Remote.LatestTag(mdevRepoURL())
	if !ok {
		// 配布元へ届かないだけなので黙って飛ばす。
		return SelfUpdateResult{}, nil
	}
	if domain.DecideSelfUpdate(u.Version, latest) != domain.SelfUpdateNeeded {
		return SelfUpdateResult{}, nil
	}

	write(out, domain.RenderSelfUpdateStarting(u.Version, latest))
	plan := domain.BuildSelfUpdatePlan(domain.MdevRepoSlug, u.Version, latest, assetName)
	path, err := u.Replacer.Replace(plan)
	if err != nil {
		// **踏み切る前の失敗は止めない。** 取得できなかっただけで実行ファイルは
		// 無傷なので、続けて conductor の資産を更新してよい。踏み切った後
		// (置き換えの最中)の失敗だけを error にする。
		if errors.Is(err, ErrSelfUpdateNotStarted) {
			write(out, domain.RenderSelfUpdateUnavailable(err))
			return SelfUpdateResult{}, nil
		}
		return SelfUpdateResult{}, fmt.Errorf("mdev 自身の更新に失敗しました: %w", err)
	}
	write(out, domain.RenderSelfUpdateReplaced(latest, path))
	return SelfUpdateResult{Replaced: true, Latest: latest}, nil
}

// mdevRepoURL は mdev 自身の配布元の URL を返す。
//
// 6-3 で REPO_URL の一本化を行う際に、conductor 側の更新元と合わせて
// 整理する予定である(ADR-0004)。
func mdevRepoURL() string {
	return "https://github.com/" + domain.MdevRepoSlug + ".git"
}
