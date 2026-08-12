package domain_test

import (
	"testing"
	"time"

	"github.com/k-kudo-hub/mdev-go/internal/domain"
)

var paceBase = time.Date(2026, 8, 11, 10, 0, 0, 0, time.UTC)

// TestIdlePaceShouldCheck は attach の確認を始める間隔を固定する。
//
// 確認は zellij の CLI を 1 回叩くので、ポーリングのたびに行うと減速の
// 目的(サーバの負荷を下げる)を損なう。
func TestIdlePaceShouldCheck(t *testing.T) {
	t.Parallel()

	// **起動直後はいきなり確認しない。** ペインは 5 つ以上同時に立ち上がる
	// ので、揃って list-clients を撃つと、セッションを開いたその瞬間に
	// zellij へ最も負荷をかけることになる。開いた直後は誰かが見ているに
	// 決まっているので、最初の確認は間隔が空いてからでよい。
	var zero domain.IdlePace
	if zero.Started() {
		t.Error("ゼロ値なのに起点が置かれています")
	}
	if zero.ShouldCheck(paceBase) {
		t.Error("起動直後に確認しようとしています")
	}

	checked := zero.MarkChecked(paceBase)
	if !checked.Started() {
		t.Error("起点が置かれていません")
	}
	tests := []struct {
		name  string
		after time.Duration
		want  bool
	}{
		{name: "直後", after: 0, want: false},
		{name: "29 秒後", after: 29 * time.Second, want: false},
		{name: "30 秒ちょうど", after: 30 * time.Second, want: true},
		{name: "31 秒後", after: 31 * time.Second, want: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := checked.ShouldCheck(paceBase.Add(tt.after)); got != tt.want {
				t.Errorf("ShouldCheck(+%v) = %v, want %v", tt.after, got, tt.want)
			}
		})
	}
}

// TestIdlePaceMarkCheckedPreventsOverlap は確認が返る前に来たポーリングが
// 確認を重ねて出さないことを確かめる。
//
// 結果ではなく「始めた時刻」を持つのがこのためである。
func TestIdlePaceMarkCheckedPreventsOverlap(t *testing.T) {
	t.Parallel()

	pace := domain.IdlePace{}.MarkChecked(paceBase)
	// 結果はまだ受け取っていない(Observe を呼んでいない)。
	if pace.ShouldCheck(paceBase.Add(time.Second)) {
		t.Error("確認が返る前に次の確認を始めようとしています")
	}
}

// TestIdlePaceInterval は減速の掛かり方を固定する。
func TestIdlePaceInterval(t *testing.T) {
	t.Parallel()

	const normal = 2 * time.Second
	tests := []struct {
		name string
		pace domain.IdlePace
		want time.Duration
	}{
		{
			// 確かめる前は減速しない。開いているかもしれないため。
			name: "未確認",
			pace: domain.IdlePace{},
			want: normal,
		},
		{
			name: "attach あり",
			pace: domain.IdlePace{}.MarkChecked(paceBase).Observe(true),
			want: normal,
		},
		{
			// 未アタッチ中は読み直しを止め、attach の確認だけを続ける。
			// したがって合図の間隔は確認の間隔と同じになる。
			name: "誰も開いていない",
			pace: domain.IdlePace{}.MarkChecked(paceBase).Observe(false),
			want: domain.AttachCheckInterval,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := tt.pace.Interval(normal); got != tt.want {
				t.Errorf("Interval = %v, want %v", got, tt.want)
			}
		})
	}
}

// TestIdlePaceNeverSpeedsUp は通常の間隔のほうが長い設定で、減速が
// かえって速くしてしまわないことを確かめる。
func TestIdlePaceNeverSpeedsUp(t *testing.T) {
	t.Parallel()

	pace := domain.IdlePace{}.MarkChecked(paceBase).Observe(false)
	const slow = 5 * time.Minute
	if got := pace.Interval(slow); got != slow {
		t.Errorf("Interval(%v) = %v, want %v", slow, got, slow)
	}
}

// TestIdlePaceRecoversImmediately は attach を確認したその時点で減速が
// 解けることを確かめる(段階的に戻さない)。
func TestIdlePaceRecoversImmediately(t *testing.T) {
	t.Parallel()

	const normal = 2 * time.Second
	pace := domain.IdlePace{}.MarkChecked(paceBase).Observe(false)
	if !pace.Detached() {
		t.Fatal("未アタッチと判定されていません")
	}

	back := pace.MarkChecked(paceBase.Add(domain.AttachCheckInterval)).Observe(true)
	if back.Detached() {
		t.Error("attach したのに未アタッチのままです")
	}
	if got := back.Interval(normal); got != normal {
		t.Errorf("Interval = %v, want %v(即座に通常へ戻る)", got, normal)
	}
}

// TestIdlePaceConstants は刻みの値を固定する。
// 変えると「閉じたセッションが無害になる」度合いと、attach 復帰の速さが変わる。
func TestIdlePaceConstants(t *testing.T) {
	t.Parallel()

	if domain.AttachCheckInterval != 30*time.Second {
		t.Errorf("AttachCheckInterval = %v, want 30s", domain.AttachCheckInterval)
	}
}
