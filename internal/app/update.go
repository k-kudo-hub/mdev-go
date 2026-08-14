package app

import (
	"sync"

	"github.com/k-kudo-hub/mdev-go/internal/domain"
)

// UpdateStateStore は CONDUCTOR_HOME 直下の小さな状態ファイルを読み書きする。
//
// いずれも install.sh / 更新確認が置くもので、無い・読めない場合は空を返す。
// 更新確認は失敗しても黙って諦める処理なので、読み取りの失敗は
// 「値が無い」と区別しない。
type UpdateStateStore interface {
	// RepoURL は更新元リポジトリ(REPO_URL)を返す。
	RepoURL() string
	// InstalledVersion はインストール済みの版(VERSION)を返す。
	InstalledVersion() string
	// ReadUpdateCache は .update-check の 1 行を (日付, タグ) として返す。
	ReadUpdateCache() (date, tag string)
	// WriteUpdateCache は .update-check を書き換える。
	WriteUpdateCache(date, tag string) error
	// ReadMdevUpdateCache は mdev 本体ぶんの確認結果を返す。
	//
	// conductor 資産とは版が別々に進むのでキャッシュを分ける。同じ
	// ファイルに 2 つ書くと現行の 1 行 2 列の形が壊れ、古い mdev や
	// install.sh が読めなくなる。
	ReadMdevUpdateCache() (date, tag string)
	// WriteMdevUpdateCache は mdev 本体ぶんの確認結果を書き換える。
	WriteMdevUpdateCache(date, tag string) error
}

// RemoteTagLister はリモートの最新 semver タグを引く。
//
// ネットワークに出る処理なので、到達できない・タグが無い場合は ok=false を
// 返す(error にしない)。更新確認はセッションの起動を止めてはならず、
// 呼び出し側はどの失敗でも同じく黙って諦める。
type RemoteTagLister interface {
	LatestTag(url string) (tag string, ok bool)
}

// UpdateChecker は起動時の更新確認である(現行 check-update.sh 相当)。
type UpdateChecker struct {
	Config ConfigLoader
	State  UpdateStateStore
	Remote RemoteTagLister
	Clock  Clock
	// MdevVersion は今動いている mdev の版(ビルド時に焼き込んだもの)。
	// 空や "dev" のときは mdev 本体の案内を出さない。
	MdevVersion string
}

// Check は更新の案内を返す。案内するものが無ければ空文字を返す。
//
// **どの失敗経路でも空文字を返し、error は返さない。** これはセッションの
// 起動前に走る処理で、設定の不備やネットワークの不調で起動が止まっては
// ならないためである(現行版もすべての経路で黙って exit 0 する)。
//
// force が真ならその日のキャッシュを無視して引き直す。1 日 1 回に絞るのは、
// セッションを開くたびにリモートへ出ると起動が目に見えて遅くなるためである。
func (c *UpdateChecker) Check(force bool) string {
	config, _ := c.Config.Load()
	if config.UpdateCheck.Disabled {
		return ""
	}
	// **更新元が分からなければ何も確かめない。** ここで外へ出ないことは
	// 現行版からの約束である(セッションの起動前に走るため)。mdev 本体の
	// 確認も同じ扱いにする。設定していない利用者に、こちらの都合で
	// ネットワークを使わせない。
	repoURL := c.State.RepoURL()
	if repoURL == "" {
		return ""
	}

	today := c.Clock.Now().Format(domain.DailyFileDateLayout)
	conductorTag, mdevTag := c.latestTags(today, force, repoURL)

	// conductor の資産と mdev 本体は別々に版が進む。どちらが古いのかが
	// 分からないと、`mdev update` で何が変わるのかが読めない。
	return c.conductorNotice(today, conductorTag) + c.mdevNotice(today, mdevTag)
}

// latestTags は必要な最新タグを引く。
//
// **2 つの問い合わせを同時に出す。** どちらも ls-remote で、届かない相手には
// 数秒待つ。順に撃つとその待ちが 2 回ぶん積み上がり、セッションの起動が
// そのぶん遅れる。キャッシュが使える側は撃たない。
//
// 引けなかった場合は空文字を返し、その側の案内は出ない。
func (c *UpdateChecker) latestTags(today string, force bool, repoURL string) (string, string) {
	conductor := c.cachedTag(c.State.ReadUpdateCache, today, force)
	mdev := ""
	if c.mdevSupported() {
		mdev = c.cachedTag(c.State.ReadMdevUpdateCache, today, force)
	}

	var wg sync.WaitGroup
	if conductor == "" {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if tag, ok := c.Remote.LatestTag(repoURL); ok {
				conductor = tag
			}
		}()
	}
	// mdev 本体は配布物のある環境でだけ確かめる。
	if mdev == "" && c.mdevSupported() {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if tag, ok := c.Remote.LatestTag(mdevRepoURL()); ok {
				mdev = tag
			}
		}()
	}
	wg.Wait()
	return conductor, mdev
}

// mdevSupported は mdev 本体の更新を扱える状況かを返す。
//
// 版を焼き込んでいないビルドでは何もしない。手元で組んだものを「古い」と
// 言っても意味が無く、自己更新も行わない。配布物の無い環境も同じである。
func (c *UpdateChecker) mdevSupported() bool {
	if domain.IsDevBuild(c.MdevVersion) {
		return false
	}
	_, supported := domain.CurrentAssetName()
	return supported
}

// conductorNotice は conductor 資産の新しい版があれば案内を返す。
// 引き直したタグはここで書き戻す(キャッシュの書き込みは 1 か所に集める)。
func (c *UpdateChecker) conductorNotice(today, latest string) string {
	if latest == "" {
		return ""
	}
	if date, tag := c.State.ReadUpdateCache(); date != today || tag != latest {
		// 書き込みに失敗しても案内は出す(次回また引き直すだけ)。
		_ = c.State.WriteUpdateCache(today, latest)
	}

	current := domain.NormalizeVersion(c.State.InstalledVersion())
	if !domain.VersionGreater(latest, current) {
		return ""
	}
	return domain.RenderUpdateNotice(latest, current)
}

// mdevNotice は mdev 本体の新しい版があれば案内を返す。
func (c *UpdateChecker) mdevNotice(today, latest string) string {
	if latest == "" || !c.mdevSupported() {
		return ""
	}
	if date, tag := c.State.ReadMdevUpdateCache(); date != today || tag != latest {
		_ = c.State.WriteMdevUpdateCache(today, latest)
	}

	if domain.DecideSelfUpdate(c.MdevVersion, latest) != domain.SelfUpdateNeeded {
		return ""
	}
	return domain.RenderMdevUpdateNotice(latest, c.MdevVersion)
}

// cachedTag は今日ぶんのキャッシュがあればそのタグを返す。
//
// read には conductor 用と mdev 用のどちらかを渡す。両者は別のファイルを
// 読むだけで、判断の仕方は同じである。
func (c *UpdateChecker) cachedTag(read func() (string, string), today string, force bool) string {
	if force {
		return ""
	}
	date, tag := read()
	if date != today {
		return ""
	}
	return tag
}
