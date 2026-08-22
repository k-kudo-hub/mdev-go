package tui_test

import (
	"testing"

	"github.com/k-kudo-hub/mdev-go/internal/app"
	"github.com/k-kudo-hub/mdev-go/internal/tui"
)

// 全ペインは alt screen で描画する。
//
// 通常バッファで描くと、古いフレームが zellij のスクロールバックへ蓄積し、
// ペインのリサイズ時に zellij がそれを新しい幅で折り返し直して画面上へ
// 再出現させる(重複ヘッダー・行の断片)。alt screen はスクロールバックを
// 持たず、リサイズで全面クリア+再描画されるため、画面がクリーンに保たれる。
//
// Bubble Tea v2 の alt screen は Program のオプションではなく、View() が
// 返す tea.View のフィールドで毎フレーム指定する。1 つでも false の
// フレームが混ざると通常バッファへ戻ってしまうため、全モデルで確かめる。
func TestPaneViewsUseAltScreen(t *testing.T) {
	t.Parallel()

	models := []paneModel{
		{"task-create", newTaskCreate(defaultTaskCreateStub())},
		{"task-control", tui.NewTaskControlModel(&stubTaskControl{text: "操作バー"}, testEnv, "my-task")},
	}
	models = append(models, paneModels()...)

	for _, tt := range models {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if !tt.model.View().AltScreen {
				t.Error("View() が AltScreen を有効にしていない")
			}
		})
	}
}

// 取得中の News と待ち受け中の TaskCreate のように、別の分岐から返る
// フレームも alt screen のままであること。1 フレームでも false が混ざると
// その瞬間に通常バッファへ切り替わり、崩れの原因が再発する。
func TestPaneAlternateViewsUseAltScreen(t *testing.T) {
	t.Parallel()

	// News: r キーで取得中の画面(FetchingText)へ切り替わる。
	news := tui.NewNewsModel(&stubNews{
		snapshot: app.NewsSnapshot{Text: "ニュース画面", FetchingText: "取得中", Count: 1},
	})
	model, _ := news.Update(key('r'))
	if !model.View().AltScreen {
		t.Error("取得中の News の View() が AltScreen を有効にしていない")
	}
}
