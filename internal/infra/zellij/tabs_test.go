package zellij

import (
	"errors"
	"reflect"
	"testing"
	"time"
)

func TestTabControllerListTabs(t *testing.T) {
	t.Parallel()

	var got []string
	c := &TabController{
		output: func(_ time.Duration, name string, args ...string) string {
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
	c := &TabController{output: func(time.Duration, string, ...string) string { return "" }}
	if out := c.ListTabs(); out != "" {
		t.Errorf("ListTabs() = %q, want 空", out)
	}
}

func TestTabControllerCloseTabByID(t *testing.T) {
	t.Parallel()

	var got []string
	c := &TabController{
		run: func(_ time.Duration, name string, args ...string) error {
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
	c := &TabController{run: func(time.Duration, string, ...string) error { return errors.New("失敗") }}
	c.CloseTabByID("7")
}

func TestNewTabControllerIsWired(t *testing.T) {
	t.Parallel()

	c := NewTabController()
	if c.output == nil || c.run == nil {
		t.Error("実コマンドの実行関数が設定されていない")
	}
}

func TestCommandOutputCutsOffAtTimeout(t *testing.T) {
	t.Parallel()

	// 返らない zellij は上限で切られ、空文字(= タブが 1 つも無い扱い)になる。
	// 切れないとダッシュボードのポーリングがそこで止まる。
	start := time.Now()
	out := commandOutput(50*time.Millisecond, "sleep", "30")
	elapsed := time.Since(start)

	if out != "" {
		t.Errorf("出力 = %q, want 空", out)
	}
	if elapsed > 10*time.Second {
		t.Errorf("上限で切れていない: %v かかった", elapsed)
	}
}

func TestRunCommandCutsOffAtTimeout(t *testing.T) {
	t.Parallel()

	start := time.Now()
	err := runCommand(50*time.Millisecond, "sleep", "30")
	elapsed := time.Since(start)

	if err == nil {
		t.Fatal("上限を超えたのにエラーが返っていない")
	}
	if elapsed > 10*time.Second {
		t.Errorf("上限で切れていない: %v かかった", elapsed)
	}
}

func TestCommandTimeoutIsTenSeconds(t *testing.T) {
	t.Parallel()

	// ポーリングが毎周期呼ぶ list-tabs の上限。値そのものを固定しておく。
	if commandTimeout != 10*time.Second {
		t.Errorf("commandTimeout = %v, want 10s", commandTimeout)
	}
}

// ---- 起動方法の使い分け ---------------------------------------------------

func TestCommandUsesProcessGroupOnlyWithTimeout(t *testing.T) {
	t.Parallel()

	// 上限つきの呼び出しは proc.Command を通す(プロセスグループごと切るため)。
	// 孫まで止まることの実証は internal/infra/proc のテストが持つ。
	// zellij CLI はすべて上限つきなので、実際に通るのは上の枝だけである。
	tests := []struct {
		name       string
		timeout    time.Duration
		wantPgroup bool
	}{
		{name: "上限つきはプロセスグループを分ける", timeout: commandTimeout, wantPgroup: true},
		{name: "上限なしは分けない", timeout: 0, wantPgroup: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			cmd, cancel := command(tt.timeout, "true")
			defer cancel()

			gotPgroup := cmd.SysProcAttr != nil && cmd.SysProcAttr.Setpgid
			if gotPgroup != tt.wantPgroup {
				t.Errorf("Setpgid = %v, want %v", gotPgroup, tt.wantPgroup)
			}
			if got := cmd.Cancel != nil; got != tt.wantPgroup {
				t.Errorf("Cancel の差し替え = %v, want %v", got, tt.wantPgroup)
			}
		})
	}
}
