package domain

import "time"

// 未アタッチ時の減速の刻み。
const (
	// AttachCheckInterval は「誰か開いているか」を確かめる間隔である。
	//
	// 確認は zellij の CLI を 1 回叩くので、ポーリングのたびに行うと
	// 減速の目的(サーバの負荷を下げる)を損なう。
	AttachCheckInterval = 30 * time.Second
	// IdlePollInterval は誰も開いていないと分かったときのポーリング間隔である。
	//
	// 閉じたセッションが残っても無害にすることが狙いである。ウィンドウを
	// 閉じても zellij はセッションを残すため、5 つのペインが通常の間隔で
	// 回り続けると、放置されたセッションのぶんだけサーバが劣化していく。
	IdlePollInterval = 60 * time.Second
)

// IdlePace は「誰も開いていないなら遅く回す」ための状態である。
//
// 時刻は呼び出し側から受け取る(domain は time.Now() を呼ばない)。
// ゼロ値は「まだ一度も確かめていない」状態で、通常の速さで回る。
type IdlePace struct {
	// checkedAt は最後に確認を **始めた** 時刻である。
	//
	// 結果を受け取った時刻ではなく始めた時刻を持つのは、確認が返る前に
	// 次のポーリングが来たときに二重に確認を出さないためである。
	checkedAt time.Time
	// checked は一度でも確認を始めたかどうか。
	// ゼロ値の時刻と「本当に古い時刻」を区別するために持つ。
	checked bool
	// detached は誰も開いていないと分かっているかどうか。
	detached bool
}

// ShouldCheck は今 attach の確認を始めてよいかを返す。
//
// まだ一度も確かめていないか、前回の確認から AttachCheckInterval 以上
// 経っていれば真になる。
func (p IdlePace) ShouldCheck(now time.Time) bool {
	if !p.checked {
		return true
	}
	return now.Sub(p.checkedAt) >= AttachCheckInterval
}

// MarkChecked は確認を始めたことを記録する。
//
// 結果を待たずに記録するので、確認が返る前に来たポーリングは
// ShouldCheck が偽になり、確認を重ねて出さない。
func (p IdlePace) MarkChecked(now time.Time) IdlePace {
	p.checkedAt = now
	p.checked = true
	return p
}

// Observe は確認の結果を取り込む。
//
// attached が真になった時点で減速は解ける(次に張る合図から通常の間隔に
// 戻る)。段階的に戻すようなことはしない。
func (p IdlePace) Observe(attached bool) IdlePace {
	p.detached = !attached
	return p
}

// Detached は誰も開いていないと分かっているかを返す。
func (p IdlePace) Detached() bool { return p.detached }

// Interval は次に張る合図までの間隔を返す。
//
// normal はそのペインの通常の間隔である。誰も開いていないと分かっている
// ときだけ IdlePollInterval まで落とす。**通常より速くすることはない**
// (normal のほうが長い設定になっても、そちらを尊重する)。
func (p IdlePace) Interval(normal time.Duration) time.Duration {
	if p.detached && normal < IdlePollInterval {
		return IdlePollInterval
	}
	return normal
}
