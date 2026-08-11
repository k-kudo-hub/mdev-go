package git

import (
	"os"

	"github.com/k-kudo-hub/mdev-go/internal/app"
	"github.com/k-kudo-hub/mdev-go/internal/domain"
)

// 更新確認がリモートへ出るときの待ち時間の上限。
//
// この処理はセッションを開く前に走るため、到達できないリモートで
// 起動が固まってはならない。経路ごとに別の仕組みで抑える必要がある。
const (
	// httpLowSpeedLimit / httpLowSpeedTime は HTTPS の転送を打ち切る条件
	// (毎秒 1000 バイトを 5 秒下回ったら中止)。
	httpLowSpeedLimit = "1000"
	httpLowSpeedTime  = "5"
	// sshCommand は SSH の接続待ちを 5 秒で切り、鍵の入力を求めさせない。
	sshCommand = "ssh -o ConnectTimeout=5 -o BatchMode=yes"
)

// RemoteTags はリモートのタグ一覧を引く。
type RemoteTags struct {
	// run は git を実行して標準出力を返す。テストで差し替える。
	run func(env []string, args ...string) (string, error)
}

var _ app.RemoteTagLister = (*RemoteTags)(nil)

// NewRemoteTags は RemoteTags を返す。
func NewRemoteTags() *RemoteTags {
	return &RemoteTags{run: runGitWithEnv}
}

// LatestTag はリモートの最大 semver タグを返す。
//
// 到達できない・タグが無い・解釈できない、のいずれでも ok=false を返す
// (error にしない)。呼び出し側はどの失敗でも黙って諦めるため、区別する
// 意味がない。
func (t *RemoteTags) LatestTag(url string) (string, bool) {
	if url == "" {
		return "", false
	}
	env := append(os.Environ(),
		// 認証を求められたときに端末で止まらないようにする。
		"GIT_TERMINAL_PROMPT=0",
		"GIT_SSH_COMMAND="+sshCommand,
	)
	out, err := t.run(env,
		"-c", "http.lowSpeedLimit="+httpLowSpeedLimit,
		"-c", "http.lowSpeedTime="+httpLowSpeedTime,
		"ls-remote", "--tags", url,
	)
	if err != nil {
		return "", false
	}
	return domain.LatestSemverTag(out)
}
