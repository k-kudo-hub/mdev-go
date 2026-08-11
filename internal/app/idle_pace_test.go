package app_test

import (
	"testing"
	"time"

	"github.com/k-kudo-hub/mdev-go/internal/app"
)

var idleBase = time.Date(2026, 8, 11, 10, 0, 0, 0, time.UTC)

// TestIdlePaceBoundary は境界の型が domain の判断をそのまま通すことを
// 確かめる。tui はこちらしか触れないので、ここが素通しでないと減速が効かない。
func TestIdlePaceBoundary(t *testing.T) {
	t.Parallel()

	const normal = 2 * time.Second
	var pace app.IdlePace

	if !pace.ShouldCheck(idleBase) {
		t.Error("未確認なのに確認しません")
	}
	if pace.Detached() {
		t.Error("確認前から未アタッチ扱いです")
	}
	if got := pace.Interval(normal); got != normal {
		t.Errorf("確認前の間隔 = %v, want %v", got, normal)
	}

	pace = pace.MarkChecked(idleBase)
	if pace.ShouldCheck(idleBase.Add(time.Second)) {
		t.Error("確認の直後にまた確認しようとしています")
	}
	if !pace.ShouldCheck(idleBase.Add(app.AttachCheckInterval)) {
		t.Error("確認の間隔を過ぎても確認しません")
	}

	pace = pace.Observe(false)
	if !pace.Detached() {
		t.Error("未アタッチと判定されていません")
	}
	if got := pace.Interval(normal); got != app.AttachCheckInterval {
		t.Errorf("未アタッチの間隔 = %v, want %v", got, app.AttachCheckInterval)
	}

	pace = pace.Observe(true)
	if got := pace.Interval(normal); got != normal {
		t.Errorf("attach 復帰後の間隔 = %v, want %v", got, normal)
	}
}
