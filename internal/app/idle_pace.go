package app

import (
	"time"

	"github.com/k-kudo-hub/mdev-go/internal/domain"
)

// 未アタッチ時の減速の刻み(domain の値をそのまま出す)。
//
// tui は app にしか依存できない(ADR-0002)ため、境界に出す名前は app が持つ。
const (
	// AttachCheckInterval は「誰か開いているか」を確かめる間隔であり、
	// attach し直してから通常の速さへ戻るまでの最大の待ち時間でもある。
	AttachCheckInterval = domain.AttachCheckInterval
)

// IdlePace は「誰も開いていないなら遅く回す」ための状態である。
//
// 中身は domain.IdlePace で、判断はすべてそちらが持つ。ここは tui から
// 使えるようにするための入れ物である(HookCommandChange と同じ流儀)。
// ゼロ値は「まだ一度も確かめていない」状態で、通常の速さで回る。
type IdlePace struct {
	inner domain.IdlePace
}

// ShouldCheck は今 attach の確認を始めてよいかを返す。
func (p IdlePace) ShouldCheck(now time.Time) bool { return p.inner.ShouldCheck(now) }

// MarkChecked は確認を始めたことを記録する(結果は待たない)。
func (p IdlePace) MarkChecked(now time.Time) IdlePace {
	p.inner = p.inner.MarkChecked(now)
	return p
}

// Observe は確認の結果を取り込む。
func (p IdlePace) Observe(attached bool) IdlePace {
	p.inner = p.inner.Observe(attached)
	return p
}

// Detached は誰も開いていないと分かっているかを返す。
func (p IdlePace) Detached() bool { return p.inner.Detached() }

// Interval は次に張る合図までの間隔を返す。
func (p IdlePace) Interval(normal time.Duration) time.Duration { return p.inner.Interval(normal) }
