package store_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/k-kudo-hub/mdev-go/internal/infra/store"
)

// writeConfig は conductorHome に設定ファイルを置く。
func writeConfig(t *testing.T, conductorHome, name, content string) {
	t.Helper()
	if err := os.MkdirAll(conductorHome, 0o755); err != nil {
		t.Fatalf("MkdirAll() = %v", err)
	}
	if err := os.WriteFile(filepath.Join(conductorHome, name), []byte(content), 0o600); err != nil {
		t.Fatalf("WriteFile(%s) = %v", name, err)
	}
}

func TestConductorHome(t *testing.T) {
	t.Parallel()

	// 現行版の `${CONDUCTOR_HOME:-$HOME/.claude-conductor}`。
	tests := []struct {
		name     string
		envValue string
		want     string
	}{
		{name: "CONDUCTOR_HOME を優先", envValue: "/opt/conductor", want: "/opt/conductor"},
		{name: "空ならホーム直下", envValue: "", want: "/Users/x/.claude-conductor"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := store.ConductorHome("/Users/x", tt.envValue); got != tt.want {
				t.Errorf("ConductorHome() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestConfigPathPrefersUserConfig(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		files    []string
		wantFile string
	}{
		{name: "両方あれば config.json", files: []string{"config.json", "config.default.json"}, wantFile: "config.json"},
		{name: "config.json だけ", files: []string{"config.json"}, wantFile: "config.json"},
		{name: "config.default.json だけ", files: []string{"config.default.json"}, wantFile: "config.default.json"},
		{name: "どちらも無ければ config.default.json", files: nil, wantFile: "config.default.json"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			home := t.TempDir()
			for _, name := range tt.files {
				writeConfig(t, home, name, `{}`)
			}
			want := filepath.Join(home, tt.wantFile)
			if got := store.ConfigPath(home); got != want {
				t.Errorf("ConfigPath() = %q, want %q", got, want)
			}
		})
	}
}

func TestLoadConfigFallsBackPerFile(t *testing.T) {
	t.Parallel()

	// キー単位のマージは行わない。config.json を置くと
	// config.default.json の内容は一切参照されない。
	home := t.TempDir()
	writeConfig(t, home, "config.default.json", `{"pricing":{"only-in-default":{"input":9},"fast_multiplier":6}}`)
	writeConfig(t, home, "config.json", `{"pricing":{"only-in-user":{"input":1}}}`)

	cfg, err := store.LoadConfig(home)
	if err != nil {
		t.Fatalf("LoadConfig() = %v", err)
	}
	if _, ok := cfg.Pricing.Models["only-in-user"]; !ok {
		t.Errorf("config.json の内容が読まれていない: %+v", cfg.Pricing.Models)
	}
	if _, ok := cfg.Pricing.Models["only-in-default"]; ok {
		t.Errorf("config.default.json の内容がマージされている: %+v", cfg.Pricing.Models)
	}
	if cfg.Pricing.FastMultiplier != 0 {
		t.Errorf("FastMultiplier = %v, want 0(既定ファイルからマージされない)", cfg.Pricing.FastMultiplier)
	}
}

func TestLoadConfigUsesDefaultWhenUserConfigAbsent(t *testing.T) {
	t.Parallel()

	home := t.TempDir()
	writeConfig(t, home, "config.default.json", `{"pricing":{"m":{"input":3}}}`)

	cfg, err := store.LoadConfig(home)
	if err != nil {
		t.Fatalf("LoadConfig() = %v", err)
	}
	if got := cfg.Pricing.Models["m"].Input; got != 3 {
		t.Errorf("Models[m].Input = %v, want 3", got)
	}
}

// TestLoadConfigWithoutAnyFile は設置物が無いときに埋め込みへ落ちることを
// 確かめる。
//
// 設置の前や、設置物が欠けた状態でも既定の設定で動けるようにするための
// 経路である。ここが空の設定に落ちると、単価表もエージェントの指定も無い
// まま動いてしまい、料金が 0 円として記録される。
func TestLoadConfigWithoutAnyFile(t *testing.T) {
	t.Parallel()

	cfg, err := store.LoadConfig(t.TempDir())
	if err != nil {
		t.Fatalf("LoadConfig() = %v", err)
	}
	if len(cfg.Pricing.Models) == 0 {
		t.Error("埋め込みの単価表が読めていない")
	}
	if got := cfg.AgentCommand(""); len(got) == 0 {
		t.Error("埋め込みからエージェントのコマンドが読めていない")
	}
}

func TestLoadConfigRejectsBrokenJSON(t *testing.T) {
	t.Parallel()

	home := t.TempDir()
	writeConfig(t, home, "config.json", `{"pricing":`)

	if _, err := store.LoadConfig(home); err == nil {
		t.Error("LoadConfig() = nil, want エラー")
	}
}

func TestLoadConfigReportsReadFailure(t *testing.T) {
	t.Parallel()

	home := t.TempDir()
	writeConfig(t, home, "config.json", `{}`)
	path := filepath.Join(home, "config.json")
	if err := os.Chmod(path, 0o000); err != nil {
		t.Fatalf("Chmod() = %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(path, 0o600) })

	if _, err := store.LoadConfig(home); err == nil {
		t.Error("LoadConfig() = nil, want エラー")
	}
}

func TestLoadConfigReadsShippedDefault(t *testing.T) {
	t.Parallel()

	// 現行リポジトリの config.default.json をそのまま読めることを確認する。
	home := t.TempDir()
	b, err := os.ReadFile(filepath.Join("testdata", "config.default.json"))
	if err != nil {
		t.Fatalf("ReadFile() = %v", err)
	}
	writeConfig(t, home, "config.default.json", string(b))

	cfg, err := store.LoadConfig(home)
	if err != nil {
		t.Fatalf("LoadConfig() = %v", err)
	}

	if cfg.Pricing.FastMultiplier != 6 {
		t.Errorf("FastMultiplier = %v, want 6", cfg.Pricing.FastMultiplier)
	}
	if _, ok := cfg.Pricing.Models["fast_multiplier"]; ok {
		t.Error("fast_multiplier がモデルとして登録されている")
	}

	opus, ok := cfg.Pricing.Models["claude-opus-4-7"]
	if !ok {
		t.Fatalf("claude-opus-4-7 が読めていない: %+v", cfg.Pricing.Models)
	}
	if opus.Input != 5.0 || opus.Output != 25.0 || opus.CacheWrite5m != 6.25 ||
		opus.CacheWrite1h != 10.0 || opus.CacheHit != 0.5 {
		t.Errorf("claude-opus-4-7 = %+v", opus)
	}

	// gpt 系は cache_write_5m / 1h ではなく cache_write を持つ。
	gpt, ok := cfg.Pricing.Models["gpt-5.6-sol"]
	if !ok {
		t.Fatalf("gpt-5.6-sol が読めていない: %+v", cfg.Pricing.Models)
	}
	if gpt.CacheWrite != 6.25 || gpt.CacheWrite5m != 0 {
		t.Errorf("gpt-5.6-sol = %+v", gpt)
	}
}
