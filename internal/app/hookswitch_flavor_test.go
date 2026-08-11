package app_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/k-kudo-hub/mdev-go/internal/domain"
)

// FLAVOR ファイル(Go 版を使う印)の書き手としての HookSwitcher のテスト。
//
// この印は conductor の install.sh が読む。印が無いと install.sh も
// `mdev update` も layouts と hooks を無条件に上書きし、Go 版へ寄せた設定が
// 再実行のたびに黙って巻き戻る。書く場所を hooks の切り替えに置いているのは、
// それが「Go 版を採用する」という意思表示そのものだからである。

// TestSwitchWritesFlavor は切り替えの成功で印が書かれることを確かめる。
//
// hooks に変更が無い場合も書く。印は意思表示であって hooks の差分ではなく、
// 「切り替え済みだが印だけ失われている」状態(install.sh が hooks を
// 戻した直後など)から回復できなければならない。
func TestSwitchWritesFlavor(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		content string
	}{
		{name: "hooks を切り替えた", content: settingsBefore},
		{name: "既に切り替え済みで変更が無い", content: settingsAfter},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			flavor := newFakeFlavorStore()
			got, err := newSwitcherWithFlavor(newFakeSettingsStore(tt.content), flavor).Switch(false)
			if err != nil {
				t.Fatalf("Switch() = %v", err)
			}
			if !flavor.exists {
				t.Fatal("印が書かれていません")
			}
			if flavor.written != domain.FlavorGo {
				t.Errorf("印の中身 = %q, want %q", flavor.written, domain.FlavorGo)
			}
			if got.FlavorPath != flavor.Path() {
				t.Errorf("FlavorPath = %q, want %q", got.FlavorPath, flavor.Path())
			}
		})
	}
}

// TestSwitchWritesFlavorEveryTime は繰り返し実行しても印が書き直されることを
// 確かめる(冪等)。
func TestSwitchWritesFlavorEveryTime(t *testing.T) {
	t.Parallel()

	flavor := newFakeFlavorStore()
	switcher := newSwitcherWithFlavor(newFakeSettingsStore(settingsBefore), flavor)

	for i := range 3 {
		if _, err := switcher.Switch(false); err != nil {
			t.Fatalf("%d 回目の Switch() = %v", i+1, err)
		}
	}
	if want := []string{"WriteFlavor", "WriteFlavor", "WriteFlavor"}; !equalStrings(flavor.calls, want) {
		t.Errorf("印への操作 = %v, want %v", flavor.calls, want)
	}
	if flavor.written != domain.FlavorGo {
		t.Errorf("印の中身 = %q, want %q", flavor.written, domain.FlavorGo)
	}
}

// TestSwitchDryRunDoesNotWriteFlavor は dry-run が印にも触れないことを
// 確かめる。書き込みを行わないという約束は settings.json だけの話ではない。
func TestSwitchDryRunDoesNotWriteFlavor(t *testing.T) {
	t.Parallel()

	flavor := newFakeFlavorStore()
	got, err := newSwitcherWithFlavor(newFakeSettingsStore(settingsBefore), flavor).Switch(true)
	if err != nil {
		t.Fatalf("Switch() = %v", err)
	}
	if len(flavor.calls) != 0 {
		t.Errorf("dry-run で印を触りました: %v", flavor.calls)
	}
	if got.FlavorPath != "" {
		t.Errorf("FlavorPath = %q, want 空", got.FlavorPath)
	}
}

// TestSwitchFailsWhenFlavorCannotBeWritten は印を書けないときに error に
// なることを確かめる。
//
// hooks 自体は切り替わっているので「成功」に見えるが、印が無いままでは
// 次の install.sh / mdev update で設定が Shell 版へ戻る。この機構が防ごうと
// している事故そのものなので、黙って成功にはしない。install.sh は
// `mdev hooks switch >/dev/null 2>&1` と標準エラーを捨てて呼ぶため、
// 警告として出しても利用者には届かず、終了コードだけが観測される。
func TestSwitchFailsWhenFlavorCannotBeWritten(t *testing.T) {
	t.Parallel()

	settings := newFakeSettingsStore(settingsBefore)
	flavor := newFakeFlavorStore()
	flavor.writeErr = errors.New("書けない")

	got, err := newSwitcherWithFlavor(settings, flavor).Switch(false)
	if err == nil {
		t.Fatal("印を書けないのに error になりませんでした")
	}
	// 何が起きたかを利用者が判断できる文言であること。
	for _, want := range []string{"hooks は切り替えました", "印を書けませんでした"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error = %v, want %q を含む", err, want)
		}
	}
	// hooks の切り替えそのものは済んでいる。ここを巻き戻すと、
	// 印が書けないだけで settings.json が中途半端になる。
	if settings.content != settingsAfter {
		t.Errorf("settings.json = %s, want 切り替え済み", settings.content)
	}
	if len(got.Changes) == 0 {
		t.Error("切り替えた内容が結果に残っていません")
	}
}

// TestRestoreRemovesFlavor は復元の成功で印が消えることを確かめる。
//
// Switch が変更の有無に依らず書くのと対称に、こちらも変更の有無に依らず消す。
func TestRestoreRemovesFlavor(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		content string
	}{
		{name: "hooks を戻した", content: settingsAfter},
		{name: "既にスクリプトを指していて変更が無い", content: settingsBefore},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			flavor := newFakeFlavorStore()
			flavor.exists = true
			flavor.written = domain.FlavorGo

			got, err := newSwitcherWithFlavor(newFakeSettingsStore(tt.content), flavor).Restore(false)
			if err != nil {
				t.Fatalf("Restore() = %v", err)
			}
			if flavor.exists {
				t.Error("印が残っています")
			}
			if got.FlavorPath != flavor.Path() {
				t.Errorf("FlavorPath = %q, want %q", got.FlavorPath, flavor.Path())
			}
		})
	}
}

// TestRestoreRemovesFlavorWhenSettingsMissing は settings.json ごと失われて
// いる復元(バックアップからの書き戻し)でも印が消えることを確かめる。
func TestRestoreRemovesFlavorWhenSettingsMissing(t *testing.T) {
	t.Parallel()

	settings := newFakeSettingsStore(settingsBefore)
	// バックアップを 1 件作ってから settings.json を失った状態にする。
	if _, err := settings.Backup([]byte(settingsBefore)); err != nil {
		t.Fatal(err)
	}
	settings.readErr = notExistErr()

	flavor := newFakeFlavorStore()
	flavor.exists = true

	if _, err := newSwitcherWithFlavor(settings, flavor).Restore(false); err != nil {
		t.Fatalf("Restore() = %v", err)
	}
	if flavor.exists {
		t.Error("印が残っています")
	}
}

// TestRestoreDryRunDoesNotRemoveFlavor は dry-run が印を消さないことを
// 確かめる。
func TestRestoreDryRunDoesNotRemoveFlavor(t *testing.T) {
	t.Parallel()

	flavor := newFakeFlavorStore()
	flavor.exists = true

	got, err := newSwitcherWithFlavor(newFakeSettingsStore(settingsAfter), flavor).Restore(true)
	if err != nil {
		t.Fatalf("Restore() = %v", err)
	}
	if len(flavor.calls) != 0 {
		t.Errorf("dry-run で印を触りました: %v", flavor.calls)
	}
	if !flavor.exists {
		t.Error("dry-run なのに印が消えています")
	}
	if got.FlavorPath != "" {
		t.Errorf("FlavorPath = %q, want 空", got.FlavorPath)
	}
}

// TestRestoreFailsWhenFlavorCannotBeRemoved は印を消せないときに error に
// なることを確かめる。印が残ったままだと、install.sh が次に走ったときに
// `mdev hooks switch` を呼んで Go 版へ切り替え直してしまう。
func TestRestoreFailsWhenFlavorCannotBeRemoved(t *testing.T) {
	t.Parallel()

	settings := newFakeSettingsStore(settingsAfter)
	flavor := newFakeFlavorStore()
	flavor.exists = true
	flavor.removeErr = errors.New("消せない")

	_, err := newSwitcherWithFlavor(settings, flavor).Restore(false)
	if err == nil {
		t.Fatal("印を消せないのに error になりませんでした")
	}
	for _, want := range []string{"hooks は戻しました", "印を消せませんでした"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error = %v, want %q を含む", err, want)
		}
	}
	// hooks を戻す処理そのものは済んでいる。
	if settings.content != settingsBefore {
		t.Errorf("settings.json = %s, want 復元済み", settings.content)
	}
}

// TestSwitchRestoreRoundTripOnFlavor は切り替えと復元を往復しても印の
// 状態が食い違わないことを確かめる。
func TestSwitchRestoreRoundTripOnFlavor(t *testing.T) {
	t.Parallel()

	settings := newFakeSettingsStore(settingsBefore)
	flavor := newFakeFlavorStore()
	switcher := newSwitcherWithFlavor(settings, flavor)

	if _, err := switcher.Switch(false); err != nil {
		t.Fatalf("Switch() = %v", err)
	}
	if !flavor.exists {
		t.Fatal("切り替え後に印がありません")
	}
	if _, err := switcher.Restore(false); err != nil {
		t.Fatalf("Restore() = %v", err)
	}
	if flavor.exists {
		t.Fatal("復元後に印が残っています")
	}
	if _, err := switcher.Switch(false); err != nil {
		t.Fatalf("2 度目の Switch() = %v", err)
	}
	if !flavor.exists || flavor.written != domain.FlavorGo {
		t.Errorf("再切り替え後の印 = %q(exists=%v)", flavor.written, flavor.exists)
	}
}
