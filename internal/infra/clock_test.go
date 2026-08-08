package infra_test

import (
	"testing"
	"time"

	"github.com/k-kudo-hub/mdev-go/internal/app"
	"github.com/k-kudo-hub/mdev-go/internal/infra"
)

var _ app.Clock = infra.SystemClock{}

func TestSystemClockReturnsCurrentTime(t *testing.T) {
	t.Parallel()

	before := time.Now()
	got := infra.SystemClock{}.Now()
	after := time.Now()

	if got.Before(before) || got.After(after) {
		t.Errorf("Now() = %v, want %v 以上 %v 以下", got, before, after)
	}
}
