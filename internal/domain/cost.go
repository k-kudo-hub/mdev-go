package domain

import "math"

// FallbackClaudeModel は単価表に無いモデルの代わりに引くモデル名
// (現行 record-output.sh:162 の `$pricing["claude-sonnet-4-6"]`)。
const FallbackClaudeModel = "claude-sonnet-4-6"

// DefaultFastMultiplier は設定に fast_multiplier が無いときの倍率
// (現行版の `$pricing.fast_multiplier // 6`)。
const DefaultFastMultiplier = 6

// FastSpeed は倍率が掛かる speed の値(現行版の `if $speed == "fast"`)。
const FastSpeed = "fast"

// tokensPerPriceUnit は単価の分母。単価は 100 万トークンあたりの USD である。
const tokensPerPriceUnit = 1000000

// DefaultClaudePricing は設定にも FallbackClaudeModel にも単価が無いときに使う
// ハードコードの既定単価である(現行 record-output.sh:162 と同じ値)。
//
// claude は必ず何らかの単価にたどり着くため cost が null になることはない。
// codex は価格が分からなければ null にする(ForCodex を参照)。
var DefaultClaudePricing = ModelPricing{
	Input:        3,
	Output:       15,
	CacheWrite5m: 3.75,
	CacheWrite1h: 6,
	CacheHit:     0.3,
}

// ForClaude は claude のモデルに使う単価を 3 段のフォールバックで決める。
// モデル表 → FallbackClaudeModel → DefaultClaudePricing の順である。
func (p Pricing) ForClaude(model string) ModelPricing {
	if pricing, ok := p.Models[model]; ok {
		return pricing
	}
	if pricing, ok := p.Models[FallbackClaudeModel]; ok {
		return pricing
	}
	return DefaultClaudePricing
}

// ForCodex は codex のモデルに使う単価を返す。
//
// claude と違いフォールバックしない。codex のモデルに claude の単価を当てても
// 意味のある金額にならないため、現行版は単価が無ければ cost を null にする。
func (p Pricing) ForCodex(model string) (ModelPricing, bool) {
	pricing, ok := p.Models[model]
	return pricing, ok
}

// SpeedMultiplier は speed に応じて単価へ掛ける倍率を返す。
//
// FastSpeed 以外は常に 1 である。FastSpeed のときは設定の fast_multiplier を
// 使い、設定に無ければ DefaultFastMultiplier を使う。設定に 0 が書いてあれば
// 0 が使われる(jq の `//` は 0 を真として扱うため。evidence の 2 節)。
func (p Pricing) SpeedMultiplier(speed string) float64 {
	if speed != FastSpeed {
		return 1
	}
	if !p.HasFastMultiplier {
		return DefaultFastMultiplier
	}
	return p.FastMultiplier
}

// RoundCost は金額を小数第 6 位へ丸める。
//
// 現行版の `($cost * 1000000 | round | . / 1000000)` に対応する。jq の round は
// 絶対値の大きい方向へ丸めるため math.Round と一致する(乱数 20 万件で実測。
// evidence の 1 節)。
func RoundCost(cost float64) float64 {
	return math.Round(cost*tokensPerPriceUnit) / tokensPerPriceUnit
}

// claudeRequiredPricingKeys は claude の cost 式が素の参照で使うキーである。
// jq(record-output.sh:164-170)ではこのどれかが null だと掛け算がエラーになり、
// レコード全体が Parse failed に落ちる。
var claudeRequiredPricingKeys = []string{
	PricingKeyInput, PricingKeyOutput, PricingKeyCacheWrite5m,
	PricingKeyCacheWrite1h, PricingKeyCacheHit,
}

// ClaudeCost は claude のセッションの利用料金(USD)を返す。
//
// 項ごとに「トークン数 × 単価 × 倍率 ÷ 100 万」を出してから足す。現行版
// (record-output.sh:164-170)と演算の順序を揃えてあり、浮動小数点の丸め誤差の
// 出方まで一致する。
//
// 選ばれた単価エントリに cost 式が使うキーが欠けている場合は ok=false を返す。
// 現行版では `$in * $p.input` が「number and null cannot be multiplied」で落ち、
// レコード全体が summary 無しの Parse failed になる(呼び出し側が再現する)。
func ClaudeCost(transcript ClaudeTranscript, pricing Pricing) (float64, bool) {
	model := pricing.ForClaude(transcript.Model)
	if model.MissingAny(claudeRequiredPricingKeys...) {
		return 0, false
	}
	multiplier := pricing.SpeedMultiplier(transcript.Speed)

	cost := float64(transcript.TotalInputTokens)*model.Input*multiplier/tokensPerPriceUnit +
		float64(transcript.TotalOutputTokens)*model.Output*multiplier/tokensPerPriceUnit +
		float64(transcript.CacheWrite5mTokens)*model.CacheWrite5m*multiplier/tokensPerPriceUnit +
		float64(transcript.CacheWrite1hTokens)*model.CacheWrite1h*multiplier/tokensPerPriceUnit +
		float64(transcript.CacheReadTokens)*model.CacheHit*multiplier/tokensPerPriceUnit

	return RoundCost(cost), true
}
