package app

import (
	"io"

	"github.com/k-kudo-hub/mdev-go/internal/domain"
)

// Updater は `mdev update` の本体である。
//
// # ADR D4-2 の完成形
//
// 更新は 2 段で終わる。
//
//  1. 新しいバイナリを取得して自分自身を置き換える(SelfUpdater)
//  2. **新しいバイナリの** `mdev install` が設定を貼り直す
//
// 2 段目を今のプロセスで続けないのが要点である。今動いているのは置き換える
// **前**の中身なので、そのまま設定を貼ると古い実装で貼ることになる。置き換え
// たらそこで終え、実行し直しを案内する。次の `mdev update` は新しいバイナリが
// 受け、自分は最新なので 1 段目を飛ばして install だけを行う。
//
// # 旧フローの廃止
//
// 以前は conductor の tarball を取ってきて中の install.sh を bash で走らせて
// いた。REPO_URL が mdev-go を指すようになった時点でその tarball には
// install.sh も scripts/ も無く、フローとして成立しない(ADR D8 の移行)。
// 資産は既にバイナリへ埋め込んであるので、貼り直しは自分の install で足りる。
type Updater struct {
	State UpdateStateStore
	// Self は mdev 自身のバイナリを新しくする。
	//
	// nil のときは自バイナリの更新を行わない(設定の貼り直しだけを行う)。
	Self *SelfUpdater
	// Install は設定を貼り直すユースケースである。
	Install InstallRunner
}

// InstallRunner は設置と移行を行う。実体は Installer。
type InstallRunner interface {
	Install(out io.Writer) error
}

// Update は最新版へ更新する。進み具合は out へ書く。
//
// 更新確認(UpdateChecker)と違い、こちらは利用者が明示的に叩くコマンドなので
// 失敗は必ず error として返す。黙って何もしないと、更新したつもりで古いまま
// 使い続けることになる。
func (u *Updater) Update(out io.Writer) error {
	if u.Self != nil {
		result, err := u.Self.Run(out)
		if err != nil {
			return err
		}
		if result.Replaced {
			// 置き換えた。ここから先は新しいバイナリの仕事である。
			return nil
		}
	}

	// 自分は最新。設定を貼り直す(冪等なので、変わっていなければ何も書かない)。
	write(out, domain.RenderUpdateApplying())
	return u.Install.Install(out)
}

// write は出力先へ書く。書けない状況で追加の報告先は無いため失敗は無視する。
func write(out io.Writer, s string) {
	_, _ = io.WriteString(out, s)
}
