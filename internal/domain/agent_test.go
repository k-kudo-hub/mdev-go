package domain_test

import (
	"encoding/json"
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
