package app

import (
	"errors"
	"fmt"
	"io"

	"github.com/k-kudo-hub/mdev-go/internal/domain"
)

// TarballURLEnv は取得元の tarball を差し替える環境変数である。
// 現行 update.sh と同じく、テストが file:// を指すために使う。
const TarballURLEnv = "CONDUCTOR_TARBALL_URL"

// ReleaseInstaller は指定した版のソースを取ってきて install を実行する。
//
// 取得・展開・install の実行をまとめて 1 つの操作にしているのは、途中の
// 生成物(一時ディレクトリ)が呼び出し側から見えても使い道が無く、後始末の
// 責任だけが増えるためである。
type ReleaseInstaller interface {
	// Install は tarballURL からソースを取り、その中の install.sh を実行する。
	//
	// version と repoURL は install.sh へ環境変数として渡す。tarball には
	// .git が入っていないため、install.sh が自分で版と更新元を知る手段が
	// これしかない。
	Install(tarballURL, version, repoURL string) error
}

// Updater は `mdev update` の本体である(現行 update.sh 相当)。
type Updater struct {
	State     UpdateStateStore
	Remote    RemoteTagLister
	Installer ReleaseInstaller
	// Self は mdev 自身のバイナリを新しくする。
	//
	// nil のときは自バイナリの更新を行わない(conductor 資産の更新だけを
	// 行う従来の動きになる)。
	Self *SelfUpdater
	// Getenv は環境変数を読む(tarball の差し替え用)。
	Getenv func(string) string
}

// Update は最新版へ更新する。進み具合は out へ書く。
//
// 更新確認(UpdateChecker)と違い、こちらは利用者が明示的に叩くコマンドなので
// 失敗は必ず error として返す。黙って何もしないと、更新したつもりで古いまま
// 使い続けることになる。
//
// 既に最新の場合は「既に最新です」と出して error は返さない。
func (u *Updater) Update(out io.Writer) error {
	// **先に自分自身を新しくする。** 古い mdev で conductor の資産を
	// 入れ直しても、次に mdev を動かした瞬間にまた古い実装が動く。
	// 置き換えたらそこで終える(SelfUpdateResult.Replaced のコメントを参照)。
	if u.Self != nil {
		result, err := u.Self.Run(out)
		if err != nil {
			return err
		}
		if result.Replaced {
			return nil
		}
	}

	repoURL := u.State.RepoURL()
	if repoURL == "" {
		return errors.New("更新元リポジトリが不明です。リポジトリで install.sh を再実行してください。")
	}
	slug, ok := domain.RepoSlug(repoURL)
	if !ok {
		return fmt.Errorf("リポジトリURLを解釈できません: %s", repoURL)
	}

	write(out, domain.RenderUpdateChecking())
	latest, ok := u.Remote.LatestTag(repoURL)
	if !ok {
		return errors.New("最新バージョンの取得に失敗しました。")
	}

	current := domain.NormalizeVersion(u.State.InstalledVersion())
	if !domain.VersionGreater(latest, current) {
		write(out, domain.RenderUpdateUpToDate(current))
		return nil
	}
	write(out, domain.RenderUpdateStarting(current, latest))

	if err := u.Installer.Install(u.tarballURL(slug, latest), latest, repoURL); err != nil {
		return err
	}
	write(out, domain.RenderUpdateDone(latest))
	return nil
}

// tarballURL は取得元の URL を決める。環境変数があればそちらを優先する。
func (u *Updater) tarballURL(slug, tag string) string {
	if u.Getenv != nil {
		if override := u.Getenv(TarballURLEnv); override != "" {
			return override
		}
	}
	return domain.TarballURL(slug, tag)
}

// write は出力先へ書く。書けない状況で追加の報告先は無いため失敗は無視する。
func write(out io.Writer, s string) {
	_, _ = io.WriteString(out, s)
}
