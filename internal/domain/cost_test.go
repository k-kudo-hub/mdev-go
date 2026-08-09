package domain_test

import (
	"encoding/json"
	"math"
	"reflect"
	"testing"

	"github.com/k-kudo-hub/mdev-go/internal/domain"
)

// defaultPricing は claude-conductor の config.default.json の pricing と同じ内容。
func defaultPricing(t *testing.T) domain.Pricing {
	t.Helper()

	raw := `{
	  "claude-opus-4-6":  {"input": 5.0, "output": 25.0, "cache_write_5m": 6.25, "cache_write_1h": 10.0, "cache_hit": 0.5},
	  "claude-sonnet-4-6":{"input": 3.0, "output": 15.0, "cache_write_5m": 3.75, "cache_write_1h": 6.0,  "cache_hit": 0.3},
	  "claude-haiku-4-5": {"input": 1.0, "output": 5.0,  "cache_write_5m": 1.25, "cache_write_1h": 2.0,  "cache_hit": 0.1},
	  "gpt-5.6-sol":      {"input": 5.0, "output": 30.0, "cache_write": 6.25, "cache_hit": 0.5},
	  "fast_multiplier": 6
	}`
	var pricing domain.Pricing
	if err := json.Unmarshal([]byte(raw), &pricing); err != nil {
		t.Fatalf("Unmarshal() = %v", err)
	}
	return pricing
}

// parsePricing はテスト内の JSON リテラルから Pricing を作る。
func parsePricing(t *testing.T, raw string) domain.Pricing {
	t.Helper()

	var pricing domain.Pricing
	if err := json.Unmarshal([]byte(raw), &pricing); err != nil {
		t.Fatalf("Unmarshal(%s) = %v", raw, err)
	}
	return pricing
}

func TestPricingForClaudeFallsBackInThreeSteps(t *testing.T) {
	t.Parallel()

	// defaultPricing の claude エントリは cache_write キーを持たないため、
	// パース結果には Missing が付く(claude の cost 式は cache_write を使わない)。
	missingCacheWrite := map[string]bool{domain.PricingKeyCacheWrite: true}
	sonnet := domain.ModelPricing{Input: 3, Output: 15, CacheWrite5m: 3.75, CacheWrite1h: 6, CacheHit: 0.3, Missing: missingCacheWrite}
	opus := domain.ModelPricing{Input: 5, Output: 25, CacheWrite5m: 6.25, CacheWrite1h: 10, CacheHit: 0.5, Missing: missingCacheWrite}

	tests := []struct {
		name    string
		pricing domain.Pricing
		model   string
		want    domain.ModelPricing
	}{
		{
			name:    "モデル表にあればそれを使う",
			pricing: defaultPricing(t),
			model:   "claude-opus-4-6",
			want:    opus,
		},
		{
			name:    "未知のモデルは claude-sonnet-4-6 へフォールバック",
			pricing: defaultPricing(t),
			model:   "claude-opus-9-9",
			want:    sonnet,
		},
		{
			// `$pricing[$model] // $pricing["claude-sonnet-4-6"] // {既定}` の 3 段目。
			name:    "sonnet も無ければハードコードの既定単価",
			pricing: parsePricing(t, `{"claude-haiku-4-5":{"input":1,"output":5}}`),
			model:   "claude-opus-4-6",
			want:    domain.DefaultClaudePricing,
		},
		{
			name:    "pricing が空でもハードコードの既定単価",
			pricing: parsePricing(t, `{}`),
			model:   "claude-opus-4-6",
			want:    domain.DefaultClaudePricing,
		},
		{
			// transcript にモデル名が無いときは "unknown" が渡ってくる。
			name:    "unknown も sonnet へフォールバック",
			pricing: defaultPricing(t),
			model:   domain.UnknownModel,
			want:    sonnet,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := tt.pricing.ForClaude(tt.model); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("ForClaude(%q) = %+v, want %+v", tt.model, got, tt.want)
			}
		})
	}
}

func TestDefaultClaudePricingMatchesShell(t *testing.T) {
	t.Parallel()

	// record-output.sh:162 のハードコード既定値。
	want := domain.ModelPricing{Input: 3, Output: 15, CacheWrite5m: 3.75, CacheWrite1h: 6, CacheHit: 0.3}
	if !reflect.DeepEqual(domain.DefaultClaudePricing, want) {
		t.Errorf("DefaultClaudePricing = %+v, want %+v", domain.DefaultClaudePricing, want)
	}
}

func TestPricingForCodexDoesNotFallBack(t *testing.T) {
	t.Parallel()

	pricing := defaultPricing(t)

	got, ok := pricing.ForCodex("gpt-5.6-sol")
	if !ok {
		t.Fatal("ForCodex(gpt-5.6-sol) ok = false, want true")
	}
	want := domain.ModelPricing{Input: 5, Output: 30, CacheWrite: 6.25, CacheHit: 0.5,
		Missing: map[string]bool{domain.PricingKeyCacheWrite5m: true, domain.PricingKeyCacheWrite1h: true}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("ForCodex() = %+v, want %+v", got, want)
	}

	// claude と違い sonnet や既定値へは落ちない(価格未知は cost null になる)。
	if _, ok := pricing.ForCodex("gpt-unknown-model"); ok {
		t.Error("ForCodex(gpt-unknown-model) ok = true, want false")
	}
	if _, ok := pricing.ForCodex(domain.UnknownModel); ok {
		t.Error("ForCodex(unknown) ok = true, want false")
	}
}

func TestPricingSpeedMultiplier(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		pricing domain.Pricing
		speed   string
		want    float64
	}{
		{name: "standard は常に 1", pricing: defaultPricing(t), speed: "standard", want: 1},
		{name: "設定に無い speed も 1", pricing: defaultPricing(t), speed: "turbo", want: 1},
		{name: "fast は設定の倍率", pricing: defaultPricing(t), speed: "fast", want: 6},
		{
			name:    "fast_multiplier が無ければ既定の 6",
			pricing: parsePricing(t, `{"m":{"input":1}}`),
			speed:   "fast",
			want:    6,
		},
		{
			// jq の `//` は 0 を真とするため、明示的な 0 は 0 のまま使われる。
			name:    "fast_multiplier が 0 なら 0",
			pricing: parsePricing(t, `{"fast_multiplier":0}`),
			speed:   "fast",
			want:    0,
		},
		{
			name:    "fast_multiplier が 0 でも standard は 1",
			pricing: parsePricing(t, `{"fast_multiplier":0}`),
			speed:   "standard",
			want:    1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := tt.pricing.SpeedMultiplier(tt.speed); got != tt.want {
				t.Errorf("SpeedMultiplier(%q) = %v, want %v", tt.speed, got, tt.want)
			}
		})
	}
}

func TestRoundCost(t *testing.T) {
	t.Parallel()

	// jq の `(. * 1000000 | round | . / 1000000)` と同じ丸め
	// (絶対値の大きい方向へ。実測は evidence の 1 節)。
	tests := []struct {
		in   float64
		want float64
	}{
		{in: 0, want: 0},
		{in: 0.0000005, want: 0.000001},
		{in: 0.0000004, want: 0},
		{in: 0.0000015, want: 0.000002},
		{in: 0.082, want: 0.082},
		{in: 1.2345675, want: 1.234568},
		{in: 9.5, want: 9.5},
	}
	for _, tt := range tests {
		if got := domain.RoundCost(tt.in); got != tt.want {
			t.Errorf("RoundCost(%v) = %v, want %v", tt.in, got, tt.want)
		}
	}
}

func TestClaudeCost(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		pricing    domain.Pricing
		transcript domain.ClaudeTranscript
		want       float64
	}{
		{
			// test.sh セクション 26a の期待値。
			// 150*5 + 800*25 + 3000*6.25 + 3000*10 + 25000*0.5 = 82000(/1e6)
			name:    "キャッシュ込みの opus",
			pricing: defaultPricing(t),
			transcript: domain.ClaudeTranscript{
				Model: "claude-opus-4-6", Speed: "standard",
				TotalInputTokens: 150, TotalOutputTokens: 800,
				CacheReadTokens: 25000, CacheWrite5mTokens: 3000, CacheWrite1hTokens: 3000,
			},
			want: 0.082,
		},
		{
			// test.sh セクション 26b。fast は 6 倍。
			name:    "fast モードは倍率が掛かる",
			pricing: defaultPricing(t),
			transcript: domain.ClaudeTranscript{
				Model: "claude-opus-4-6", Speed: "fast",
				TotalInputTokens: 1000, TotalOutputTokens: 1000,
			},
			want: 0.18,
		},
		{
			// test.sh セクション 26c。
			name:    "sonnet の単価",
			pricing: defaultPricing(t),
			transcript: domain.ClaudeTranscript{
				Model: "claude-sonnet-4-6", Speed: "standard",
				TotalInputTokens: 1000, TotalOutputTokens: 1000,
			},
			want: 0.018,
		},
		{
			// 未知モデル → sonnet フォールバックなので上と同額になる。
			name:    "未知モデルは sonnet と同額",
			pricing: defaultPricing(t),
			transcript: domain.ClaudeTranscript{
				Model: "claude-opus-9-9", Speed: "standard",
				TotalInputTokens: 1000, TotalOutputTokens: 1000,
			},
			want: 0.018,
		},
		{
			// pricing が空 → ハードコード既定値(sonnet と同じ単価)。
			name:    "pricing が空でも既定単価で計算する",
			pricing: parsePricing(t, `{}`),
			transcript: domain.ClaudeTranscript{
				Model: "claude-opus-4-6", Speed: "standard",
				TotalInputTokens: 1000, TotalOutputTokens: 1000,
			},
			want: 0.018,
		},
		{
			name:       "トークンが無ければ 0",
			pricing:    defaultPricing(t),
			transcript: domain.ClaudeTranscript{Model: domain.UnknownModel, Speed: "standard"},
			want:       0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, ok := domain.ClaudeCost(tt.transcript, tt.pricing)
			if !ok {
				t.Fatal("ClaudeCost() ok = false, want true")
			}
			if got != tt.want {
				t.Errorf("ClaudeCost() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestClaudeCostFailsOnIncompletePricingEntry(t *testing.T) {
	t.Parallel()

	// 選ばれたエントリに cost 式の必須キーが欠けていると、現行版は
	// `number and null cannot be multiplied` で落ち Parse failed になる(実測)。
	transcript := domain.ClaudeTranscript{
		Model: "claude-opus-4-6", Speed: "standard",
		TotalInputTokens: 1000, TotalOutputTokens: 1000,
	}

	// モデル自体のエントリが部分的。
	if _, ok := domain.ClaudeCost(transcript, parsePricing(t, `{"claude-opus-4-6":{"output":25}}`)); ok {
		t.Error("ClaudeCost(部分エントリ) ok = true, want false")
	}
	// フォールバック先の sonnet が部分的でも同じく落ちる。
	if _, ok := domain.ClaudeCost(transcript, parsePricing(t, `{"claude-sonnet-4-6":{"input":3}}`)); ok {
		t.Error("ClaudeCost(部分 sonnet フォールバック) ok = true, want false")
	}
	// オブジェクトでない単価は jq では `$p.input` のインデックスエラーになる。
	if _, ok := domain.ClaudeCost(transcript, parsePricing(t, `{"claude-opus-4-6":5}`)); ok {
		t.Error("ClaudeCost(非オブジェクト) ok = true, want false")
	}
}

func TestPricingSkipsNullAndFalseEntries(t *testing.T) {
	t.Parallel()

	// jq の `//` は null / false を偽とするため、null のエントリは選ばれず
	// フォールバックが進む(実測: `{"m":null}["m"] // 既定` は既定を返す)。
	transcript := domain.ClaudeTranscript{
		Model: "claude-opus-4-6", Speed: "standard",
		TotalInputTokens: 1000, TotalOutputTokens: 1000,
	}
	got, ok := domain.ClaudeCost(transcript, parsePricing(t, `{"claude-opus-4-6":null}`))
	if !ok {
		t.Fatal("ClaudeCost() ok = false, want true")
	}
	// null エントリ → sonnet も無し → ハードコード既定(sonnet 相当)で計算される。
	if got != 0.018 {
		t.Errorf("ClaudeCost() = %v, want 0.018(既定単価へのフォールバック)", got)
	}

	if _, found := parsePricing(t, `{"m":false}`).ForCodex("m"); found {
		t.Error("ForCodex(false エントリ) found = true, want false")
	}
}

func TestSpeedMultiplierTreatsNullAsAbsent(t *testing.T) {
	t.Parallel()

	// jq の `null // 6` は 6 を返す(実測)。null の fast_multiplier は未設定と同じ。
	pricing := parsePricing(t, `{"fast_multiplier":null}`)
	if got := pricing.SpeedMultiplier(domain.FastSpeed); got != domain.DefaultFastMultiplier {
		t.Errorf("SpeedMultiplier(fast) = %v, want %v", got, domain.DefaultFastMultiplier)
	}
	if pricing.HasFastMultiplier {
		t.Error("HasFastMultiplier = true, want false")
	}
}

func TestClaudeCostUsesSameArithmeticOrderAsShell(t *testing.T) {
	t.Parallel()

	// 現行版は項ごとに `(トークン * 単価 * 倍率 / 1000000)` を出してから足す。
	// 掛け算・割り算の順序が違うと最終ビットがずれ得るため、式の形も揃える。
	pricing := defaultPricing(t)
	tr := domain.ClaudeTranscript{
		Model: "claude-opus-4-6", Speed: "fast",
		TotalInputTokens: 123457, TotalOutputTokens: 98765,
		CacheReadTokens: 456789, CacheWrite5mTokens: 13579, CacheWrite1hTokens: 24680,
	}
	want := domain.RoundCost(
		float64(123457)*5.0*6/1000000 +
			float64(98765)*25.0*6/1000000 +
			float64(13579)*6.25*6/1000000 +
			float64(24680)*10.0*6/1000000 +
			float64(456789)*0.5*6/1000000)

	got, ok := domain.ClaudeCost(tr, pricing)
	if !ok {
		t.Fatal("ClaudeCost() ok = false, want true")
	}
	if got != want {
		t.Errorf("ClaudeCost() = %v, want %v (差 %v)", got, want, math.Abs(got-want))
	}
}
