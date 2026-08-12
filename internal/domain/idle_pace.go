package domain

import "time"

// 未アタッチ時の減速の刻み。
const (
	// AttachCheckInterval は「誰か開いているか」を確かめる間隔である。
	//
	// 誰も開いていないと分かっている間は読み直しを完全に止め、この間隔で
	// 確認だけを続ける。したがってこの値は 2 つの意味を持つ。
	//
	//   - 放置されたセッションが立てる負荷(30 秒に 1 回の list-clients だけ)
	//   - **attach し直してから通常の速さへ戻るまでの最大の待ち時間**
	//
	// 確認そのものは zellij の CLI を 1 回叩くので、ポーリングのたびに
	// 行うと減速の目的(サーバの負荷を下げる)を損なう。
	AttachCheckInterval = 30 * time.Second
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

// Started は一度でも確認の起点を置いたかを返す。
//
// ゼロ値は「まだ起点が無い」状態で、呼び出し側は最初の機会に MarkChecked で
// 起点を置く。**起動直後にいきなり確認しない**ためである。ペインは 5 つ以上
// 同時に立ち上がるので、揃って list-clients を撃つと、セッションを開いた
// その瞬間に zellij へ最も負荷をかけることになる。開いた直後は誰かが見て
// いるに決まっているので、最初の確認は AttachCheckInterval の後でよい。
func (p IdlePace) Started() bool { return p.checked }

// ShouldCheck は今 attach の確認を始めてよいかを返す。
//
// 起点からの経過が AttachCheckInterval 以上のときだけ真になる。起点が
// 置かれていない間は偽である(Started を参照)。
func (p IdlePace) ShouldCheck(now time.Time) bool {
	if !p.checked {
		return false
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
// normal はそのペインの通常の間隔である。誰も開いていないと分かっている間は
// 読み直しを止めて attach の確認だけを続けるため、確認の間隔で回す。
// **通常より速くすることはない**(normal のほうが長い設定になっても、
// そちらを尊重する)。
func (p IdlePace) Interval(normal time.Duration) time.Duration {
	if p.detached && normal < AttachCheckInterval {
		return AttachCheckInterval
	}
	return normal
}
