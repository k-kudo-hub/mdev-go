package store_test

import (
	"reflect"
	"testing"

	"github.com/k-kudo-hub/mdev-go/internal/app"
	"github.com/k-kudo-hub/mdev-go/internal/domain"
	"github.com/k-kudo-hub/mdev-go/internal/infra/store"
)

var _ app.PricingLoader = store.PricingStore{}

// emptyPricing は単価表が空の状態(現行版の `PRICING_JSON="{}"`)。
var emptyPricing = domain.Pricing{Models: map[string]domain.ModelPricing{}}

func TestLoadPricingFallsBackPerKey(t *testing.T) {
	t.Parallel()

	// fixture のエントリは input しか持たないため、残りのキーが Missing に載る。
	missingExceptInput := map[string]bool{
		domain.PricingKeyOutput: true, domain.PricingKeyCacheWrite5m: true,
		domain.PricingKeyCacheWrite1h: true, domain.PricingKeyCacheWrite: true,
		domain.PricingKeyCacheHit: true,
	}
	userOnly := domain.Pricing{
		Models:            map[string]domain.ModelPricing{"only-in-user": {Input: 1, Missing: missingExceptInput}},
		FastMultiplier:    0,
		HasFastMultiplier: false,
	}
	defaultOnly := domain.Pricing{
		Models:            map[string]domain.ModelPricing{"only-in-default": {Input: 9, Missing: missingExceptInput}},
		FastMultiplier:    6,
		HasFastMultiplier: true,
	}

	tests := []struct {
		name        string
		userConfig  string // 空文字は config.json を置かないことを表す
		baseConfig  string // 空文字は config.default.json を置かないことを表す
		want        domain.Pricing
		description string
	}{
		{
			name:       "config.json の pricing を使う",
			userConfig: `{"pricing":{"only-in-user":{"input":1}}}`,
			baseConfig: `{"pricing":{"only-in-default":{"input":9},"fast_multiplier":6}}`,
			want:       userOnly,
		},
		{
			name:       "config.json が無ければ config.default.json",
			baseConfig: `{"pricing":{"only-in-default":{"input":9},"fast_multiplier":6}}`,
			want:       defaultOnly,
		},
		{
			// 現行版は `jq -c '.pricing // empty'` が空文字になるため既定へ落ちる。
			// 設定の破損で料金計算だけが止まるより、既定の単価で続けるほうがよい。
			name:       "config.json が壊れていれば config.default.json",
			userConfig: `{"pricing":{`,
			baseConfig: `{"pricing":{"only-in-default":{"input":9},"fast_multiplier":6}}`,
			want:       defaultOnly,
		},
		{
			name:       "config.json に pricing が無ければ config.default.json",
			userConfig: `{"search_dirs":["~/projects"]}`,
			baseConfig: `{"pricing":{"only-in-default":{"input":9},"fast_multiplier":6}}`,
			want:       defaultOnly,
		},
		{
			name:       "pricing が null なら config.default.json",
			userConfig: `{"pricing":null}`,
			baseConfig: `{"pricing":{"only-in-default":{"input":9},"fast_multiplier":6}}`,
			want:       defaultOnly,
		},
		{
			// `{}` は jq で真なのでフォールバックしない。空の単価表として使う。
			name:       "pricing が空オブジェクトならそこで止まる",
			userConfig: `{"pricing":{}}`,
			baseConfig: `{"pricing":{"only-in-default":{"input":9},"fast_multiplier":6}}`,
			want:       emptyPricing,
		},
		// どちらのファイルからも取れない 2 つの場合(壊れている / 無い)は
		// 埋め込みへ落ちる。値が入るため、この表では扱わない
		// (TestLoadPricingFallsBackToEmbedded を参照)。
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			home := t.TempDir()
			if tt.userConfig != "" {
				writeConfig(t, home, "config.json", tt.userConfig)
			}
			if tt.baseConfig != "" {
				writeConfig(t, home, "config.default.json", tt.baseConfig)
			}

			if got := store.LoadPricing(home); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("LoadPricing() = %+v, want %+v", got, tt.want)
			}
		})
	}
}

func TestLoadPricingReadsRealDefaultConfig(t *testing.T) {
	t.Parallel()

	// testdata に置いた実物の config.default.json(claude-conductor 由来)。
	pricing := store.LoadPricing("testdata")

	if got := pricing.Models["claude-opus-4-6"].Input; got != 5.0 {
		t.Errorf("claude-opus-4-6.input = %v, want 5.0", got)
	}
	if got := pricing.Models["gpt-5.6-sol"].CacheWrite; got != 6.25 {
		t.Errorf("gpt-5.6-sol.cache_write = %v, want 6.25", got)
	}
	if !pricing.HasFastMultiplier || pricing.FastMultiplier != 6 {
		t.Errorf("fast_multiplier = %v (has=%v), want 6", pricing.FastMultiplier, pricing.HasFastMultiplier)
	}
}

func TestPricingStoreLoad(t *testing.T) {
	t.Parallel()

	home := t.TempDir()
	writeConfig(t, home, "config.json", `{"pricing":{"m":{"input":2}}}`)

	if got := store.NewPricingStore(home).Load().Models["m"].Input; got != 2 {
		t.Errorf("Load().Models[m].Input = %v, want 2", got)
	}
}

// TestLoadPricingFallsBackToEmbedded は設置物から単価表が取れないときに
// 埋め込みへ落ちることを確かめる。
//
// 単価表が空だと料金が 0 円として記録され、後から見て「無料だった」のか
// 「単価が無かった」のか区別できない。
func TestLoadPricingFallsBackToEmbedded(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		userConfig string
		baseConfig string
	}{
		{name: "どちらも無い"},
		{name: "どちらも壊れている", userConfig: `not json`, baseConfig: `not json either`},
		{
			name:       "どちらにも pricing が無い",
			userConfig: `{"search_dirs":["~/projects"]}`,
			baseConfig: `{"search_dirs":["~/projects"]}`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			home := t.TempDir()
			if tt.userConfig != "" {
				writeConfig(t, home, "config.json", tt.userConfig)
			}
			if tt.baseConfig != "" {
				writeConfig(t, home, "config.default.json", tt.baseConfig)
			}

			got := store.LoadPricing(home)
			if len(got.Models) == 0 {
				t.Error("埋め込みの単価表が読めていない")
			}
			if !got.HasFastMultiplier {
				t.Error("埋め込みの fast_multiplier が読めていない")
			}
		})
	}
}
