package domain

import "encoding/json"

// Config は conductor の設定ファイル(config.json / config.default.json)を表す。
//
// フェーズごとに必要になったキーを追加していく。現時点で解釈するのは pricing
// だけで、未知のキーは読み飛ばされる(書き戻しは行わないため失われない)。
type Config struct {
	Pricing Pricing `json:"pricing"`
}

// ModelPricing は 1 モデルあたりの 100 万トークンあたり単価(USD)である。
//
// claude 系は cache_write_5m / cache_write_1h に分かれ、gpt 系は cache_write に
// まとまる。現行の config.default.json が両方の形を含むため、どちらも受ける。
type ModelPricing struct {
	Input        float64 `json:"input"`
	Output       float64 `json:"output"`
	CacheWrite5m float64 `json:"cache_write_5m"`
	CacheWrite1h float64 `json:"cache_write_1h"`
	CacheWrite   float64 `json:"cache_write"`
	CacheHit     float64 `json:"cache_hit"`
}

// Pricing は設定の pricing セクションである。
//
// 現行の config.default.json ではモデル名をキーとする単価表の中に、
// スカラーの fast_multiplier が同じ階層で混ざっている。この形をそのまま
// 受けるため、モデル表と倍率を分けて保持する。
type Pricing struct {
	// Models はモデル名から単価への対応。
	Models map[string]ModelPricing
	// FastMultiplier は速い応答(speed が "fast")のときに単価へ掛ける倍率。
	// 設定に無い場合は 0 になるため、既定値の適用は利用側(SpeedMultiplier)で行う。
	FastMultiplier float64
	// HasFastMultiplier は設定に fast_multiplier が数値として書いてあったかを表す。
	//
	// 現行版の `$pricing.fast_multiplier // 6` は jq の `//` を使っており、jq では
	// 0 が真なので「明示的な 0」と「未設定」で結果が変わる(前者は 0、後者は 6)。
	// FastMultiplier だけではこの 2 つを区別できないため別に持つ。
	HasFastMultiplier bool
}

// UnmarshalJSON は pricing セクションを Models と FastMultiplier に振り分ける。
// モデルの値として解釈できないキーは読み飛ばす。
func (p *Pricing) UnmarshalJSON(data []byte) error {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}

	p.Models = make(map[string]ModelPricing, len(raw))
	p.FastMultiplier = 0
	p.HasFastMultiplier = false
	for key, value := range raw {
		if key == pricingFastMultiplierKey {
			var multiplier float64
			if err := json.Unmarshal(value, &multiplier); err == nil {
				p.FastMultiplier = multiplier
				p.HasFastMultiplier = true
			}
			continue
		}
		var model ModelPricing
		if err := json.Unmarshal(value, &model); err != nil {
			continue
		}
		p.Models[key] = model
	}
	return nil
}

// pricingFastMultiplierKey はモデル名ではなく倍率を表す予約キー。
const pricingFastMultiplierKey = "fast_multiplier"
