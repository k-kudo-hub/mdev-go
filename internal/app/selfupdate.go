package app

import (
	"fmt"
	"io"

	"github.com/k-kudo-hub/mdev-go/internal/domain"
)

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

	switch domain.DecideSelfUpdate(u.Version, latest) {
	case domain.SelfUpdateSkipDev:
		write(out, domain.RenderSelfUpdateSkipped(u.Version))
		return SelfUpdateResult{}, nil
	case domain.SelfUpdateUpToDate:
		return SelfUpdateResult{}, nil
	case domain.SelfUpdateNeeded:
	}

	write(out, domain.RenderSelfUpdateStarting(u.Version, latest))
	plan := domain.BuildSelfUpdatePlan(domain.MdevRepoSlug, u.Version, latest, assetName)
	path, err := u.Replacer.Replace(plan)
	if err != nil {
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
