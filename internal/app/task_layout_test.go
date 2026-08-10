package app_test

import (
	"encoding/json"
	"reflect"
	"testing"
	"time"

	"github.com/k-kudo-hub/mdev-go/internal/app"
	"github.com/k-kudo-hub/mdev-go/internal/domain"
)

// apply_layout(task-lib.sh:255-318)の移植を固定する。
// 期待値の根拠は test.sh 16(dev / k8s のレイアウトが撃つコマンド)。

// newLayoutFixture はレイアウト適用だけを試す TaskCreator を組み立てる。
func newLayoutFixture(t *testing.T, configJSON string) (*app.TaskCreator, *paneJournal, *fakeStopwatch) {
	t.Helper()

	var config domain.Config
	if err := json.Unmarshal([]byte(configJSON), &config); err != nil {
		t.Fatalf("設定の解釈に失敗: %v", err)
	}
	journal := &paneJournal{}
	clock := newStopwatch()
	return &app.TaskCreator{
		Tabs:        &fakeTabActor{journal: journal, clock: clock},
		ScreenState: &fakeScreenStateRemover{journal: journal},
		Config:      &fakeConfigLoader{config: config},
		Clock:       clock,
		Sleeper:     clock,
		Launcher:    fakeLauncher{},
	}, journal, clock
}

func TestApplyLayoutDev(t *testing.T) {
	t.Parallel()

	creator, journal, clock := newLayoutFixture(t, `{"task_types": {"dev": {"layout": [
	  {"action": "new-pane", "direction": "right", "command": "nvim"},
	  {"action": "new-pane", "direction": "down", "command": "lazygit"},
	  {"action": "move-focus", "direction": "left"}
	]}}}`)

	config, _ := creator.Config.Load()
	creator.ApplyLayout(config, "/tmp/proj", "dev", time.Minute)

	want := []string{
		"new-pane right /tmp/proj -- nvim",
		"new-pane down /tmp/proj -- lazygit",
		"move-focus left",
	}
	if !reflect.DeepEqual(journal.entries, want) {
		t.Errorf("撃ったコマンド = %v, want %v", journal.entries, want)
	}
	// レイアウトの前に 0.3 秒待つ(現行版の sleep 0.3)。
	if got := clock.slept; len(got) != 1 || got[0] != 300*time.Millisecond {
		t.Errorf("待ち = %v, want [300ms]", got)
	}
}

func TestApplyLayoutK8s(t *testing.T) {
	t.Parallel()

	creator, journal, _ := newLayoutFixture(t, `{"task_types": {"k8s": {"layout": [
	  {"action": "new-pane", "direction": "right", "command": "k9s"},
	  {"action": "new-pane", "direction": "down", "command": "nvim"},
	  {"action": "move-focus", "direction": "left"},
	  {"action": "new-pane", "direction": "down"},
	  {"action": "move-focus", "direction": "up"}
	]}}}`)

	config, _ := creator.Config.Load()
	creator.ApplyLayout(config, "/tmp/proj", "k8s", time.Minute)

	want := []string{
		"new-pane right /tmp/proj -- k9s",
		"new-pane down /tmp/proj -- nvim",
		"move-focus left",
		// command 省略時は素のシェルが起動する(`--` を付けない)。
		"new-pane down /tmp/proj",
		"move-focus up",
	}
	if !reflect.DeepEqual(journal.entries, want) {
		t.Errorf("撃ったコマンド = %v, want %v", journal.entries, want)
	}
}

func TestApplyLayoutResizeRepeatsAmountTimes(t *testing.T) {
	t.Parallel()

	creator, journal, _ := newLayoutFixture(t, `{"task_types": {"t": {"layout": [
	  {"action": "resize", "direction": "up", "amount": 3},
	  {"action": "focus-previous-pane"},
	  {"action": "resize", "direction": "down"}
	]}}}`)

	config, _ := creator.Config.Load()
	creator.ApplyLayout(config, "/tmp", "t", time.Minute)

	want := []string{
		"resize up", "resize up", "resize up",
		"focus-previous-pane",
		// amount 省略は 1 回。
		"resize down",
	}
	if !reflect.DeepEqual(journal.entries, want) {
		t.Errorf("撃ったコマンド = %v, want %v", journal.entries, want)
	}
}

func TestApplyLayoutSkipsUnknownActions(t *testing.T) {
	t.Parallel()

	// 現行版の case 文はどの枝にも当たらない action を黙って読み飛ばす。
	creator, journal, clock := newLayoutFixture(t, `{"task_types": {"t": {"layout": [
	  {"action": "explode", "direction": "up"},
	  {"action": "move-focus", "direction": "left"}
	]}}}`)

	config, _ := creator.Config.Load()
	creator.ApplyLayout(config, "/tmp", "t", time.Minute)

	if want := []string{"move-focus left"}; !reflect.DeepEqual(journal.entries, want) {
		t.Errorf("撃ったコマンド = %v, want %v", journal.entries, want)
	}
	if len(clock.slept) != 1 {
		t.Errorf("ステップが 1 つでもあれば sleep する: %v", clock.slept)
	}
}

func TestApplyLayoutWithoutStepsDoesNothing(t *testing.T) {
	t.Parallel()

	for _, name := range []string{"review", "unknown"} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			creator, journal, clock := newLayoutFixture(t,
				`{"task_types": {"review": {"layout": []}}}`)

			config, _ := creator.Config.Load()
			creator.ApplyLayout(config, "/tmp", name, time.Minute)

			if len(journal.entries) != 0 {
				t.Errorf("何も撃たないこと: %v", journal.entries)
			}
			// ステップが無いときは sleep もしない(現行版は steps が空なら即 return)。
			if len(clock.slept) != 0 {
				t.Errorf("sleep しないこと: %v", clock.slept)
			}
		})
	}
}

func TestApplyLayoutStopsWhenBudgetRunsOut(t *testing.T) {
	t.Parallel()

	// 1 回の呼び出しが 1 秒かかるサーバで、予算 2 秒。
	// sleep 0.3 のぶんも予算を食う。
	creator, journal, _ := newLayoutFixture(t, `{"task_types": {"t": {"layout": [
	  {"action": "resize", "direction": "up", "amount": 10}
	]}}}`)
	actor, ok := creator.Tabs.(*fakeTabActor)
	if !ok {
		t.Fatal("fake の型が違う")
	}
	actor.spend = time.Second

	config, _ := creator.Config.Load()
	warning := creator.ApplyLayout(config, "/tmp", "t", 2*time.Second)

	if journal.count("resize") >= 10 {
		t.Errorf("予算切れで打ち切られていない: %d 回", journal.count("resize"))
	}
	if journal.count("resize") == 0 {
		t.Error("1 回も撃たれていない")
	}
	if warning == "" {
		t.Error("予算切れの警告が返っていない")
	}
}

func TestApplyLayoutWithoutBudgetRunsEverything(t *testing.T) {
	t.Parallel()

	// 予算 0 は「無制限」である(現行版は第 3 引数を省略できる)。
	creator, journal, _ := newLayoutFixture(t, `{"task_types": {"t": {"layout": [
	  {"action": "resize", "direction": "up", "amount": 5}
	]}}}`)
	actor, ok := creator.Tabs.(*fakeTabActor)
	if !ok {
		t.Fatal("fake の型が違う")
	}
	actor.spend = time.Minute

	config, _ := creator.Config.Load()
	if warning := creator.ApplyLayout(config, "/tmp", "t", 0); warning != "" {
		t.Errorf("警告 = %q, want 空", warning)
	}
	if got := journal.count("resize"); got != 5 {
		t.Errorf("resize = %d 回, want 5", got)
	}
}

func TestApplyLayoutCapsEachCallByTheRemainingBudget(t *testing.T) {
	t.Parallel()

	// 1 回ごとの上限は「残り予算」と「zellij の上限」の小さいほうになる
	// (現行 _zj_budget_cap)。残り 2 秒なら 2 秒で諦める。
	creator, _, _ := newLayoutFixture(t, `{"task_types": {"t": {"layout": [
	  {"action": "move-focus", "direction": "left"}
	]}}}`)
	actor, ok := creator.Tabs.(*fakeTabActor)
	if !ok {
		t.Fatal("fake の型が違う")
	}

	config, _ := creator.Config.Load()
	creator.ApplyLayout(config, "/tmp", "t", 2*time.Second)

	// sleep 0.3 を引いた 1.7 秒。
	if want := 1700 * time.Millisecond; actor.caps[0] != want {
		t.Errorf("渡した上限 = %v, want %v", actor.caps[0], want)
	}
}
