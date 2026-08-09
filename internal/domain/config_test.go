package domain_test

import (
	"encoding/json"
	"reflect"
	"testing"

	"github.com/k-kudo-hub/mdev-go/internal/domain"
)

func TestConfigUnmarshalPricing(t *testing.T) {
	t.Parallel()

	// 現行 config.default.json と同じ形。モデル表の中に
	// スカラーの fast_multiplier が同居している。
	raw := []byte(`{
	  "pricing": {
	    "claude-opus-4-7": {
	      "input": 5.0, "output": 25.0,
	      "cache_write_5m": 6.25, "cache_write_1h": 10.0, "cache_hit": 0.5
	    },
	    "gpt-5.6-sol": {
	      "input": 5.0, "output": 30.0, "cache_write": 6.25, "cache_hit": 0.5
	    },
	    "fast_multiplier": 6
	  }
	}`)

	var cfg domain.Config
	if err := json.Unmarshal(raw, &cfg); err != nil {
		t.Fatalf("Unmarshal() = %v", err)
	}

	// claude 系は cache_write を、gpt 系は cache_write_5m/1h を持たないため、
	// それぞれの欠落が Missing に記録される(cost 式が使わないキーなら無害)。
	wantModels := map[string]domain.ModelPricing{
		"claude-opus-4-7": {Input: 5.0, Output: 25.0, CacheWrite5m: 6.25, CacheWrite1h: 10.0, CacheHit: 0.5,
			Missing: map[string]bool{domain.PricingKeyCacheWrite: true}},
		"gpt-5.6-sol": {Input: 5.0, Output: 30.0, CacheWrite: 6.25, CacheHit: 0.5,
			Missing: map[string]bool{domain.PricingKeyCacheWrite5m: true, domain.PricingKeyCacheWrite1h: true}},
	}
	if !reflect.DeepEqual(cfg.Pricing.Models, wantModels) {
		t.Errorf("Models = %+v, want %+v", cfg.Pricing.Models, wantModels)
	}
	if cfg.Pricing.FastMultiplier != 6 || !cfg.Pricing.HasFastMultiplier {
		t.Errorf("FastMultiplier = %v (has=%v), want 6 (has=true)",
			cfg.Pricing.FastMultiplier, cfg.Pricing.HasFastMultiplier)
	}
}

// missingExcept は指定キー以外のすべての単価キーを欠落として持つ集合を返す。
func missingExcept(present ...string) map[string]bool {
	keys := []string{
		domain.PricingKeyInput, domain.PricingKeyOutput, domain.PricingKeyCacheWrite5m,
		domain.PricingKeyCacheWrite1h, domain.PricingKeyCacheWrite, domain.PricingKeyCacheHit,
	}
	missing := map[string]bool{}
	for _, key := range keys {
		missing[key] = true
	}
	for _, key := range present {
		delete(missing, key)
	}
	return missing
}

func TestConfigUnmarshalPricingEdgeCases(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name               string
		raw                string
		wantModels         map[string]domain.ModelPricing
		wantFastMultiplier float64
		wantHasMultiplier  bool
	}{
		{
			name:               "pricing が空オブジェクト",
			raw:                `{"pricing":{}}`,
			wantModels:         map[string]domain.ModelPricing{},
			wantFastMultiplier: 0,
		},
		{
			// 既定値の適用は利用側の責務なので、ここでは 0 のままにする。
			name:               "fast_multiplier が無ければ 0",
			raw:                `{"pricing":{"m":{"input":1}}}`,
			wantModels:         map[string]domain.ModelPricing{"m": {Input: 1, Missing: missingExcept(domain.PricingKeyInput)}},
			wantFastMultiplier: 0,
		},
		{
			// jq では文字列エントリも truthy なので選ばれ、`$p.input` の
			// インデックスエラーで Parse failed に落ちる。全キー欠落として保持する。
			name: "オブジェクトでない値は全キー欠落のエントリになる",
			raw:  `{"pricing":{"m":{"input":1},"junk":"文字列"}}`,
			wantModels: map[string]domain.ModelPricing{
				"m":    {Input: 1, Missing: missingExcept(domain.PricingKeyInput)},
				"junk": {Missing: missingExcept()},
			},
			wantFastMultiplier: 0,
		},
		{
			// jq の `//` は null / false を偽とするため、エントリごと読み飛ばす
			// (モデルはフォールバックへ落ちる)。
			name:               "null や false のエントリは読み飛ばす",
			raw:                `{"pricing":{"m":null,"n":false}}`,
			wantModels:         map[string]domain.ModelPricing{},
			wantFastMultiplier: 0,
		},
		{
			// jq の `null // 6` は 6 を返すため、null は未設定と同じ扱いにする。
			name:               "fast_multiplier の null は未設定と同じ",
			raw:                `{"pricing":{"fast_multiplier":null}}`,
			wantModels:         map[string]domain.ModelPricing{},
			wantFastMultiplier: 0,
			wantHasMultiplier:  false,
		},
		{
			name:               "fast_multiplier が数値でなければ 0 のまま",
			raw:                `{"pricing":{"fast_multiplier":"six"}}`,
			wantModels:         map[string]domain.ModelPricing{},
			wantFastMultiplier: 0,
		},
		{
			// jq の `//` は 0 を真として扱うため、明示的な 0 は既定値 6 に
			// 置き換わらない。未設定との区別を HasFastMultiplier が持つ。
			name:               "fast_multiplier の明示的な 0 は未設定と区別する",
			raw:                `{"pricing":{"fast_multiplier":0}}`,
			wantModels:         map[string]domain.ModelPricing{},
			wantFastMultiplier: 0,
			wantHasMultiplier:  true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			var cfg domain.Config
			if err := json.Unmarshal([]byte(tt.raw), &cfg); err != nil {
				t.Fatalf("Unmarshal() = %v", err)
			}
			if !reflect.DeepEqual(cfg.Pricing.Models, tt.wantModels) {
				t.Errorf("Models = %+v, want %+v", cfg.Pricing.Models, tt.wantModels)
			}
			if cfg.Pricing.FastMultiplier != tt.wantFastMultiplier {
				t.Errorf("FastMultiplier = %v, want %v", cfg.Pricing.FastMultiplier, tt.wantFastMultiplier)
			}
			if cfg.Pricing.HasFastMultiplier != tt.wantHasMultiplier {
				t.Errorf("HasFastMultiplier = %v, want %v", cfg.Pricing.HasFastMultiplier, tt.wantHasMultiplier)
			}
		})
	}
}

func TestConfigUnmarshalRejectsBrokenPricing(t *testing.T) {
	t.Parallel()

	if err := json.Unmarshal([]byte(`{"pricing":[1,2]}`), new(domain.Config)); err == nil {
		t.Error("Unmarshal() = nil, want エラー")
	}
}

func TestConfigIgnoresUnknownKeys(t *testing.T) {
	t.Parallel()

	// まだ解釈しないキー(search_dirs など)があっても読み取りは成功する。
	var cfg domain.Config
	raw := []byte(`{"search_dirs":["~/projects"],"skip_task_name_input":false,"pricing":{"m":{"input":2}}}`)
	if err := json.Unmarshal(raw, &cfg); err != nil {
		t.Fatalf("Unmarshal() = %v", err)
	}
	if got := cfg.Pricing.Models["m"].Input; got != 2 {
		t.Errorf("Models[m].Input = %v, want 2", got)
	}
}
