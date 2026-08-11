package domain_test

import (
	"encoding/json"
	"reflect"
	"testing"

	"github.com/k-kudo-hub/mdev-go/internal/domain"
)

func TestConfigAgentDetection(t *testing.T) {
	t.Parallel()

	const configJSON = `{
	  "agents": {
	    "claude": {"command": "claude", "detection": "hooks"},
	    "codex":  {"command": "codex",  "detection": "screen"},
	    "nodetect": {"command": "x"},
	    "empty": {"detection": ""},
	    "nulled": {"detection": null}
	  }
	}`

	var cfg domain.Config
	if err := json.Unmarshal([]byte(configJSON), &cfg); err != nil {
		t.Fatalf("設定の解釈に失敗: %v", err)
	}

	tests := []struct {
		name  string
		agent string
		want  string
	}{
		{name: "screen 方式", agent: "codex", want: "screen"},
		{name: "hooks 方式", agent: "claude", want: "hooks"},
		// 明示されていないものはすべて hooks に落ちる。screen 検出の対象を
		// 取りこぼす方向ではなく、余計に走査しない方向へ倒す現行の既定である。
		{name: "detection が無いエージェント", agent: "nodetect", want: "hooks"},
		{name: "detection が空文字", agent: "empty", want: "hooks"},
		{name: "detection が null", agent: "nulled", want: "hooks"},
		{name: "設定に無いエージェント", agent: "unknown", want: "hooks"},
		{name: "エージェント名が空", agent: "", want: "hooks"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := cfg.AgentDetection(tt.agent); got != tt.want {
				t.Errorf("AgentDetection(%q) = %q, want %q", tt.agent, got, tt.want)
			}
		})
	}
}

func TestConfigHasScreenDetectionAgent(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		configJSON string
		want       bool
	}{
		{
			name:       "screen 方式のエージェントがある",
			configJSON: `{"agents": {"claude": {"detection": "hooks"}, "codex": {"detection": "screen"}}}`,
			want:       true,
		},
		{
			name:       "hooks 方式だけ",
			configJSON: `{"agents": {"claude": {"detection": "hooks"}}}`,
			want:       false,
		},
		{
			name:       "detection が書かれていない",
			configJSON: `{"agents": {"somecli": {"command": "x"}}}`,
			want:       false,
		},
		{
			name:       "agents が空",
			configJSON: `{"agents": {}}`,
			want:       false,
		},
		{
			name:       "agents が無い",
			configJSON: `{}`,
			want:       false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			var cfg domain.Config
			if err := json.Unmarshal([]byte(tt.configJSON), &cfg); err != nil {
				t.Fatalf("設定の解釈に失敗: %v", err)
			}
			if got := cfg.HasScreenDetectionAgent(); got != tt.want {
				t.Errorf("HasScreenDetectionAgent() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestConfigHasScreenDetectionAgentOnZeroValue(t *testing.T) {
	t.Parallel()

	// 設定が読めなかった場合はゼロ値の Config から false が返る。
	var cfg domain.Config
	if cfg.HasScreenDetectionAgent() {
		t.Error("HasScreenDetectionAgent() = true, want false")
	}
}

func TestConfigAgentDetectionOnZeroValue(t *testing.T) {
	t.Parallel()

	// 設定が読めなかった場合もゼロ値の Config から hooks が返る。
	var cfg domain.Config
	if got := cfg.AgentDetection("codex"); got != domain.DetectionHooks {
		t.Errorf("AgentDetection() = %q, want %q", got, domain.DetectionHooks)
	}
}

// TestConfigAgentPatterns は `.agents.<name>.patterns` の読み取りを固定する。
//
// 現行 task-lib.sh の agent_patterns は
// `jq -r '.agents[$a].patterns[$s] // [] | .[]' 2>/dev/null` なので、
// エージェント名が空・未設定・patterns が無い場合はいずれも空になる。
func TestConfigAgentPatterns(t *testing.T) {
	t.Parallel()

	const configJSON = `{
	  "agents": {
	    "codex": {
	      "detection": "screen",
	      "patterns": {
	        "neutral": [],
	        "blocked": ["^ *Press enter", "^ *Would you"],
	        "working": ["to interrupt"]
	      }
	    },
	    "claude": {"detection": "hooks"}
	  }
	}`

	var cfg domain.Config
	if err := json.Unmarshal([]byte(configJSON), &cfg); err != nil {
		t.Fatalf("設定の解釈に失敗: %v", err)
	}

	tests := []struct {
		name  string
		agent string
		want  domain.ScreenPatterns
	}{
		{
			name:  "設定されたパターンを記述順で返す",
			agent: "codex",
			want: domain.ScreenPatterns{
				Neutral: []string{},
				Blocked: []string{"^ *Press enter", "^ *Would you"},
				Working: []string{"to interrupt"},
			},
		},
		{name: "patterns を持たないエージェントは空", agent: "claude"},
		{name: "未知のエージェントは空", agent: "unknown"},
		{name: "エージェント名が空なら空", agent: ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got := cfg.AgentPatterns(tt.agent); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("AgentPatterns(%q) = %+v, want %+v", tt.agent, got, tt.want)
			}
		})
	}
}

// TestConfigAgentPatternsIgnoreBrokenShapes は壊れた patterns が
// **そのキーだけ**空になり、他のキーとエージェントを巻き込まないことを固定する。
//
// jq はキーごとに読むため、1 つの型違いが他へ波及しない(Config の
// per-entry 許容の流儀。evidence §1-6)。
func TestConfigAgentPatternsIgnoreBrokenShapes(t *testing.T) {
	t.Parallel()

	const configJSON = `{
	  "agents": {
	    "broken-key":  {"patterns": {"blocked": "not-an-array", "working": ["ok"]}},
	    "broken-all":  {"patterns": "not-an-object", "command": "still-here"},
	    "nulled":      {"patterns": null},
	    "good":        {"patterns": {"neutral": ["n"]}}
	  }
	}`

	var cfg domain.Config
	if err := json.Unmarshal([]byte(configJSON), &cfg); err != nil {
		t.Fatalf("設定の解釈に失敗: %v", err)
	}

	if got := cfg.AgentPatterns("broken-key"); !reflect.DeepEqual(got,
		domain.ScreenPatterns{Working: []string{"ok"}}) {
		t.Errorf("壊れたキーが他のキーを巻き込んだ: %+v", got)
	}
	if got := cfg.AgentPatterns("broken-all"); !reflect.DeepEqual(got, domain.ScreenPatterns{}) {
		t.Errorf("patterns がオブジェクトでない場合は空になるべき: %+v", got)
	}
	if got := cfg.AgentCommand("broken-all"); !reflect.DeepEqual(got, []string{"still-here"}) {
		t.Errorf("壊れた patterns が command を巻き込んだ: %q", got)
	}
	if got := cfg.AgentPatterns("nulled"); !reflect.DeepEqual(got, domain.ScreenPatterns{}) {
		t.Errorf("null の patterns は空になるべき: %+v", got)
	}
	if got := cfg.AgentPatterns("good"); !reflect.DeepEqual(got,
		domain.ScreenPatterns{Neutral: []string{"n"}}) {
		t.Errorf("壊れたエントリが健全なエントリを巻き込んだ: %+v", got)
	}
}

// TestConfigAgentPatternsKeepNonStringElements は配列の要素が文字列でない
// 場合の扱いを固定する。
//
// jq -r は数値や真偽値をそのままの表記で 1 行ずつ出すため、パターン文字列と
// して扱われる。Go 側も同じ表記へ落とす(実用上は正規表現として無意味だが、
// 落とし方が違うと「1 つの型違いで配列全体が消える」という差になる)。
func TestConfigAgentPatternsKeepNonStringElements(t *testing.T) {
	t.Parallel()

	const configJSON = `{"agents": {"a": {"patterns": {"blocked": ["ok", 12, true, null]}}}}`

	var cfg domain.Config
	if err := json.Unmarshal([]byte(configJSON), &cfg); err != nil {
		t.Fatalf("設定の解釈に失敗: %v", err)
	}
	want := []string{"ok", "12", "true", "null"}
	if got := cfg.AgentPatterns("a").Blocked; !reflect.DeepEqual(got, want) {
		t.Errorf("Blocked = %q, want %q", got, want)
	}
}
