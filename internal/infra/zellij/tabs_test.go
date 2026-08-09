package zellij

import (
	"errors"
	"reflect"
	"testing"
)

func TestTabControllerListTabs(t *testing.T) {
	t.Parallel()

	var got []string
	c := &TabController{
		output: func(name string, args ...string) string {
			got = append([]string{name}, args...)
			return "ID POS NAME\n1 x alpha\n"
		},
	}

	if out := c.ListTabs(); out != "ID POS NAME\n1 x alpha\n" {
		t.Errorf("ListTabs() = %q", out)
	}
	want := []string{"zellij", "action", "list-tabs"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("実行コマンド = %v, want %v", got, want)
	}
}

func TestTabControllerListTabsReturnsEmptyOnFailure(t *testing.T) {
	t.Parallel()

	// zellij の外で動いた場合など。タブが 1 つも無い扱いになる。
	c := &TabController{output: func(string, ...string) string { return "" }}
	if out := c.ListTabs(); out != "" {
		t.Errorf("ListTabs() = %q, want 空", out)
	}
}

func TestTabControllerCloseTabByID(t *testing.T) {
	t.Parallel()

	var got []string
	c := &TabController{
		run: func(name string, args ...string) error {
			got = append([]string{name}, args...)
			return nil
		},
	}
	c.CloseTabByID("7")

	want := []string{"zellij", "action", "close-tab-by-id", "7"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("実行コマンド = %v, want %v", got, want)
	}
}

func TestTabControllerCloseTabByIDIgnoresFailure(t *testing.T) {
	t.Parallel()

	// 既に閉じられている場合など。削除フローとしては進んでよい。
	c := &TabController{run: func(string, ...string) error { return errors.New("失敗") }}
	c.CloseTabByID("7")
}

func TestNewTabControllerIsWired(t *testing.T) {
	t.Parallel()

	c := NewTabController()
	if c.output == nil || c.run == nil {
		t.Error("実コマンドの実行関数が設定されていない")
	}
}
