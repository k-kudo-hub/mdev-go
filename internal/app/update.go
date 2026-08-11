package app

import (
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
	repoURL := c.State.RepoURL()
	if repoURL == "" {
		return ""
	}

	today := c.Clock.Now().Format(domain.DailyFileDateLayout)
	latest := c.cachedTag(today, force)
	if latest == "" {
		tag, ok := c.Remote.LatestTag(repoURL)
		if !ok {
			return ""
		}
		// 書き込みに失敗しても案内は出す(次回また引き直すだけ)。
		_ = c.State.WriteUpdateCache(today, tag)
		latest = tag
	}

	current := domain.NormalizeVersion(c.State.InstalledVersion())
	if !domain.VersionGreater(latest, current) {
		return ""
	}
	return domain.RenderUpdateNotice(latest, current)
}

// cachedTag は今日ぶんのキャッシュがあればそのタグを返す。
func (c *UpdateChecker) cachedTag(today string, force bool) string {
	if force {
		return ""
	}
	date, tag := c.State.ReadUpdateCache()
	if date != today {
		return ""
	}
	return tag
}
