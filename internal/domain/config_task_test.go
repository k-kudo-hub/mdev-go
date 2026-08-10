package domain_test

import (
	"encoding/json"
	"reflect"
	"testing"

	"github.com/k-kudo-hub/mdev-go/internal/domain"
)

// タスク作成が使う設定キー(search_dirs / search_depth / skip_task_name_input /
// task_types / agent / agents)の読み取りを固定する。
//
// 期待値の根拠は claude-conductor v0.7.4 の task-lib.sh(agent_command /
// agent_resume_args / agent_names / apply_layout)と task-create-loop.sh
// (skip_name_input_enabled / Step 1・2)の実装である。

func TestConfigTaskKeys(t *testing.T) {
	t.Parallel()

	// 現行 config.default.json と同じ並び。task_types の順序は表示順に
	// 使うため、記述順のまま保たれなければならない。
	const raw = `{
	  "search_dirs": ["~/projects", "~/works"],
	  "search_depth": 2,
	  "skip_task_name_input": true,
	  "agent": {"command": "legacy-cli", "resume_args": "--continue"},
	  "agents": {
	    "claude": {"command": "claude", "resume_args": "--resume", "detection": "hooks"},
	    "codex":  {"command": "codex",  "resume_args": "resume",   "detection": "screen"}
	  },
	  "task_types": {
	    "dev":    {"description": "Claude Code + LazyVim", "layout": [
	      {"action": "new-pane", "direction": "right", "command": "nvim"},
	      {"action": "resize", "direction": "up", "amount": 3},
	      {"action": "focus-previous-pane"}
	    ]},
	    "review": {"description": "Claude Code only", "layout": []},
	    "k8s":    {"description": "Claude Code + k9s"}
	  }
	}`

	var cfg domain.Config
	if err := json.Unmarshal([]byte(raw), &cfg); err != nil {
		t.Fatalf("設定の解釈に失敗: %v", err)
	}

	if want := []string{"~/projects", "~/works"}; !reflect.DeepEqual(cfg.SearchDirs, want) {
		t.Errorf("SearchDirs = %v, want %v", cfg.SearchDirs, want)
	}
	if cfg.SearchDepth() != 2 {
		t.Errorf("SearchDepth() = %d, want 2", cfg.SearchDepth())
	}
	if !cfg.SkipTaskNameInput {
		t.Error("SkipTaskNameInput = false, want true")
	}

	// task_types は記述順(jq の to_entries と同じ)。
	wantTypes := []string{"dev", "review", "k8s"}
	gotTypes := make([]string, 0, len(cfg.TaskTypes))
	for _, tt := range cfg.TaskTypes {
		gotTypes = append(gotTypes, tt.Name)
	}
	if !reflect.DeepEqual(gotTypes, wantTypes) {
		t.Errorf("TaskTypes の並び = %v, want %v", gotTypes, wantTypes)
	}
	if got := cfg.TaskTypes[0].Description; got != "Claude Code + LazyVim" {
		t.Errorf("dev.description = %q", got)
	}

	// layout は記述順。resize の amount は数値をそのまま持つ。
	wantLayout := []domain.LayoutStep{
		{Action: "new-pane", Direction: "right", Command: "nvim", Amount: 1},
		{Action: "resize", Direction: "up", Amount: 3},
		// direction が無いステップは jq -r が文字列 "null" を返すため、
		// 現行版はそれをそのまま zellij へ渡す(evidence §1-2)。
		{Action: "focus-previous-pane", Direction: "null", Amount: 1},
	}
	if !reflect.DeepEqual(cfg.TaskTypes[0].Layout, wantLayout) {
		t.Errorf("dev.layout = %+v, want %+v", cfg.TaskTypes[0].Layout, wantLayout)
	}
	if len(cfg.TaskTypes[1].Layout) != 0 {
		t.Errorf("review.layout = %+v, want 空", cfg.TaskTypes[1].Layout)
	}
	if len(cfg.TaskTypes[2].Layout) != 0 {
		t.Errorf("layout キーが無い k8s の Layout = %+v, want 空", cfg.TaskTypes[2].Layout)
	}

	// agents の並びは jq の keys_unsorted、つまり記述順である。
	if want := []string{"claude", "codex"}; !reflect.DeepEqual(cfg.AgentNames(), want) {
		t.Errorf("AgentNames() = %v, want %v", cfg.AgentNames(), want)
	}
}

func TestConfigSearchDepthDefaultsToOne(t *testing.T) {
	t.Parallel()

	// search_depth が無い設定。現行 Shell は `fd --max-depth null` を撃って
	// 候補ゼロになるが、Go は既定 1 に落とす(意図的な挙動差。evidence §4-4)。
	var cfg domain.Config
	if err := json.Unmarshal([]byte(`{}`), &cfg); err != nil {
		t.Fatalf("設定の解釈に失敗: %v", err)
	}
	if cfg.SearchDepth() != 1 {
		t.Errorf("SearchDepth() = %d, want 1", cfg.SearchDepth())
	}
	if cfg.SkipTaskNameInput {
		t.Error("SkipTaskNameInput は既定で false であること")
	}
	if len(cfg.AgentNames()) != 0 {
		t.Errorf("AgentNames() = %v, want 空", cfg.AgentNames())
	}
}

func TestConfigAgentCommand(t *testing.T) {
	t.Parallel()

	const raw = `{
	  "agent": {"command": "legacy-cli", "resume_args": "--continue"},
	  "agents": {
	    "claude": {"command": "claude", "resume_args": "--resume"},
	    "codex":  {"command": "codex",  "resume_args": "resume"},
	    "wrapped": {"command": "fdev exec wrapper -- claude"},
	    "blank":  {"command": "", "resume_args": ""}
	  }
	}`

	var cfg domain.Config
	if err := json.Unmarshal([]byte(raw), &cfg); err != nil {
		t.Fatalf("設定の解釈に失敗: %v", err)
	}

	tests := []struct {
		name       string
		agent      string
		wantCmd    []string
		wantResume []string
	}{
		{"名前付き agent は .agents から解決する", "codex",
			[]string{"codex"}, []string{"resume"}},
		{"名前なしは旧来の .agent 経路", "",
			[]string{"legacy-cli"}, []string{"--continue"}},
		{"未知の名前はその名前自身がコマンドになる", "somecli",
			[]string{"somecli"}, []string{"--resume"}},
		{"複数語のコマンドは空白で分割する", "wrapped",
			[]string{"fdev", "exec", "wrapper", "--", "claude"}, []string{"--resume"}},
		{"空文字は未設定と同じ扱い", "blank",
			[]string{"blank"}, []string{"--resume"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := cfg.AgentCommand(tc.agent); !reflect.DeepEqual(got, tc.wantCmd) {
				t.Errorf("AgentCommand(%q) = %v, want %v", tc.agent, got, tc.wantCmd)
			}
			if got := cfg.AgentResumeArgs(tc.agent); !reflect.DeepEqual(got, tc.wantResume) {
				t.Errorf("AgentResumeArgs(%q) = %v, want %v", tc.agent, got, tc.wantResume)
			}
		})
	}
}

func TestConfigAgentCommandDefaults(t *testing.T) {
	t.Parallel()

	// .agent も .agents も無い設定。名前なしの既定は "claude"。
	var cfg domain.Config
	if err := json.Unmarshal([]byte(`{}`), &cfg); err != nil {
		t.Fatalf("設定の解釈に失敗: %v", err)
	}
	if got := cfg.AgentCommand(""); !reflect.DeepEqual(got, []string{"claude"}) {
		t.Errorf("AgentCommand(\"\") = %v, want [claude]", got)
	}
	if got := cfg.AgentResumeArgs(""); !reflect.DeepEqual(got, []string{"--resume"}) {
		t.Errorf("AgentResumeArgs(\"\") = %v, want [--resume]", got)
	}
}

func TestConfigAgentCommandSplitsOnlyFirstLine(t *testing.T) {
	t.Parallel()

	// 現行版は `read -r -a arr <<< "$(agent_command ...)"` で語分割する。
	// read は 1 行しか読まないため、改行以降は捨てられる(evidence §1-1)。
	const raw = `{"agents": {"multi": {"command": "first line\nsecond line"}}}`

	var cfg domain.Config
	if err := json.Unmarshal([]byte(raw), &cfg); err != nil {
		t.Fatalf("設定の解釈に失敗: %v", err)
	}
	if got, want := cfg.AgentCommand("multi"), []string{"first", "line"}; !reflect.DeepEqual(got, want) {
		t.Errorf("AgentCommand(multi) = %v, want %v", got, want)
	}
}

func TestConfigLayoutAmountFallback(t *testing.T) {
	t.Parallel()

	// amount は `.amount // 1` で読まれる(missing / null / false は 1)。
	// 数値に解釈できない値は bash の算術で 0 回になる(evidence §1-4,5)。
	const raw = `{"task_types": {"t": {"layout": [
	  {"action": "resize", "direction": "up"},
	  {"action": "resize", "direction": "up", "amount": null},
	  {"action": "resize", "direction": "up", "amount": false},
	  {"action": "resize", "direction": "up", "amount": "4"},
	  {"action": "resize", "direction": "up", "amount": "abc"},
	  {"action": "resize", "direction": "up", "amount": 0}
	]}}}`

	var cfg domain.Config
	if err := json.Unmarshal([]byte(raw), &cfg); err != nil {
		t.Fatalf("設定の解釈に失敗: %v", err)
	}
	want := []int{1, 1, 1, 4, 0, 0}
	got := make([]int, 0, len(want))
	for _, step := range cfg.TaskTypes[0].Layout {
		got = append(got, step.Amount)
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("amount = %v, want %v", got, want)
	}
}

func TestConfigTaskTypeLookup(t *testing.T) {
	t.Parallel()

	const raw = `{"task_types": {"dev": {"description": "d", "layout": [
	  {"action": "move-focus", "direction": "left"}
	]}}}`

	var cfg domain.Config
	if err := json.Unmarshal([]byte(raw), &cfg); err != nil {
		t.Fatalf("設定の解釈に失敗: %v", err)
	}
	if got := cfg.Layout("dev"); len(got) != 1 || got[0].Action != "move-focus" {
		t.Errorf("Layout(dev) = %+v", got)
	}
	// 未知の型は空のレイアウト(現行版も jq が何も出さず apply_layout が即 return)。
	if got := cfg.Layout("nope"); len(got) != 0 {
		t.Errorf("Layout(nope) = %+v, want 空", got)
	}
}

func TestConfigTaskKeysIgnoreBrokenShapes(t *testing.T) {
	t.Parallel()

	// 型が違うキーは「無かった」ものとして既定へ落とす。現行版も
	// `jq ... 2>/dev/null` と `// 既定値` で同じところへ落ちる。
	const raw = `{
	  "search_dirs": "not-an-array",
	  "search_depth": "not-a-number",
	  "skip_task_name_input": "true",
	  "agents": [],
	  "task_types": 3
	}`

	var cfg domain.Config
	if err := json.Unmarshal([]byte(raw), &cfg); err != nil {
		t.Fatalf("設定の解釈に失敗: %v", err)
	}
	if len(cfg.SearchDirs) != 0 {
		t.Errorf("SearchDirs = %v, want 空", cfg.SearchDirs)
	}
	if cfg.SearchDepth() != 1 {
		t.Errorf("SearchDepth() = %d, want 1", cfg.SearchDepth())
	}
	// jq の比較は文字列 "true" と真偽値 true を区別するため、文字列は false。
	if cfg.SkipTaskNameInput {
		t.Error("SkipTaskNameInput は文字列 \"true\" では立たない")
	}
	if len(cfg.TaskTypes) != 0 || len(cfg.AgentNames()) != 0 {
		t.Errorf("TaskTypes = %+v / AgentNames = %v, want どちらも空", cfg.TaskTypes, cfg.AgentNames())
	}
}
