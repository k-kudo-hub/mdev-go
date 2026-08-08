package zellij

import (
	"errors"
	"reflect"
	"testing"
)

func TestFocuserRunsGoToTabName(t *testing.T) {
	t.Parallel()

	var got []string
	f := &Focuser{run: func(name string, args ...string) error {
		got = append([]string{name}, args...)
		return nil
	}}

	if err := f.FocusTab("Main"); err != nil {
		t.Fatalf("FocusTab() = %v", err)
	}

	want := []string{"zellij", "action", "go-to-tab-name", "Main"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("実行されたコマンド = %v, want %v", got, want)
	}
}

func TestFocuserIgnoresCommandFailure(t *testing.T) {
	t.Parallel()

	// zellij の外での実行や、既に閉じたタブへの移動は失敗するが、
	// hook としては正常な経過なので error にしない(現行版と同じ)。
	f := &Focuser{run: func(string, ...string) error { return errors.New("zellij: command not found") }}

	if err := f.FocusTab("Main"); err != nil {
		t.Errorf("FocusTab() = %v, want nil", err)
	}
}

func TestNewFocuserUsesRealCommand(t *testing.T) {
	t.Parallel()

	// 既定の実行関数が実プロセスを起動することを、確実に存在する
	// コマンドと存在しないコマンドで確認する。
	if err := runCommand("true"); err != nil {
		t.Errorf("runCommand(true) = %v, want nil", err)
	}
	if err := runCommand("mdev-no-such-command-for-test"); err == nil {
		t.Error("存在しないコマンドで nil が返った")
	}

	// zellij が入っていない環境でも FocusTab は成功扱いになる。
	if err := NewFocuser().FocusTab("Main"); err != nil {
		t.Errorf("FocusTab() = %v, want nil", err)
	}
}
