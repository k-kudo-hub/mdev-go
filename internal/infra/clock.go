package infra

import "time"

// SystemClock は実時間を返す app.Clock の実装である。
//
// domain は time.Now() を呼ばない(ADR-0002)ため、実時間の取得はこの
// adapter だけが行い、テストは固定時刻の実装に差し替える。
type SystemClock struct{}

// Now は現在時刻を返す。
func (SystemClock) Now() time.Time { return time.Now() }

// Sleep は d だけ待つ(app.Sleeper の実装)。
//
// タスク作成のポーリング間隔とレイアウトの落ち着き待ちに使う。
// domain と app は time.Sleep を直接呼ばないため、テストは待たない実装へ
// 差し替えられる。
func (SystemClock) Sleep(d time.Duration) { time.Sleep(d) }
