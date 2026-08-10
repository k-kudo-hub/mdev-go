package infra_test

import (
	"testing"
	"time"

	"github.com/k-kudo-hub/mdev-go/internal/app"
	"github.com/k-kudo-hub/mdev-go/internal/infra"
)

var (
	_ app.Clock   = infra.SystemClock{}
	_ app.Sleeper = infra.SystemClock{}
)

func TestSystemClockReturnsCurrentTime(t *testing.T) {
	t.Parallel()

	before := time.Now()
	got := infra.SystemClock{}.Now()
	after := time.Now()

	if got.Before(before) || got.After(after) {
		t.Errorf("Now() = %v, want %v 以上 %v 以下", got, before, after)
	}
}

func TestSystemClockSleep(t *testing.T) {
	t.Parallel()

	// 実際に待つことだけを確かめる(精度は要らない)。タスク作成の
	// ポーリング間隔がここを通る。
	start := time.Now()
	infra.SystemClock{}.Sleep(time.Millisecond)
	if elapsed := time.Since(start); elapsed <= 0 {
		t.Errorf("経過 = %v, want 正の値", elapsed)
	}
}
