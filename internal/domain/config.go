package domain

import "encoding/json"

// Config は conductor の設定ファイル(config.json / config.default.json)を表す。
//
// フェーズごとに必要になったキーを追加していく。未知のキーは読み飛ばされる
// (書き戻しは行わないため失われない)。
type Config struct {
	Pricing Pricing
	// Agents は名前付きエージェントの設定(`.agents`)である。
	// ダッシュボードがジャンプ時に pending を消すかどうかの判断に使う。
	Agents map[string]AgentConfig
	// Agent は旧来の単一エージェント設定(`.agent`)である。
	// 名前付きエージェントを選ばなかったタスクの起動コマンドになる。
	Agent AgentSpec
	// SearchDirs はタスク作成のディレクトリ候補を掘る起点(`search_dirs`)。
	// 先頭の `~` だけがホームへ展開される。
	SearchDirs []string
	// SkipTaskNameInput はタスク名の入力を省いて既定名で作るかどうか。
	SkipTaskNameInput bool
	// TaskTypes は選択肢に並ぶタスク種別を**記述順**で保持する。
	TaskTypes []TaskType

	// searchDepth は設定に書かれていた探索の深さ。0 は未設定を表し、
	// 参照は SearchDepth() を通す(既定 1 の適用があるため)。
	searchDepth int
	// agentNames は .agents のキーを記述順で保持する。参照は AgentNames()。
	agentNames []string
}

// configTagged は Config のうち標準のタグ解釈で足りる部分である。
// Config が UnmarshalJSON を持つため、再帰を避ける入れ物として使う。
type configTagged struct {
	Pricing Pricing `json:"pricing"`
}

// UnmarshalJSON は設定を読む。
//
// pricing はタグ付きの構造体で読み、それ以外(記述順を保つ必要があるもの、
// jq のフォールバック規則を写すもの)は unmarshalTaskKeys が別途読む。
// 型の合わないキーはいずれも既定へ落とすため、ここで error を返すのは
// JSON として壊れている場合だけである。
func (c *Config) UnmarshalJSON(data []byte) error {
	var base configTagged
	if err := json.Unmarshal(data, &base); err != nil {
		return err
	}
	*c = Config{Pricing: base.Pricing}
	c.unmarshalTaskKeys(data)
	return nil
}

// 単価表のキー名。ModelPricing.Missing と cost 計算の必須キー判定で使う。
const (
	PricingKeyInput        = "input"
	PricingKeyOutput       = "output"
	PricingKeyCacheWrite5m = "cache_write_5m"
	PricingKeyCacheWrite1h = "cache_write_1h"
	PricingKeyCacheWrite   = "cache_write"
	PricingKeyCacheHit     = "cache_hit"
)

// pricingKeys は単価表の全キー。
var pricingKeys = []string{
	PricingKeyInput, PricingKeyOutput, PricingKeyCacheWrite5m,
	PricingKeyCacheWrite1h, PricingKeyCacheWrite, PricingKeyCacheHit,
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

	// Missing は JSON に数値として存在しなかったキーの集合である。
	//
	// jq では欠けたキーの参照が null になり、cost の式で素の掛け算に使われると
	// エラー(Parse failed 落ち)、`// 0` 付きなら 0 になる。どちらへ転ぶかは
	// 使う側の式で決まるため、ここでは欠落の事実だけを持つ。コードから
	// リテラルで組んだ値(テスト・DefaultClaudePricing)は nil = 欠落なし。
	Missing map[string]bool `json:"-"`
}

// MissingAny は keys のうち 1 つでも Missing に含まれるかを返す。
func (m ModelPricing) MissingAny(keys ...string) bool {
	for _, key := range keys {
		if m.Missing[key] {
			return true
		}
	}
	return false
}

// UnmarshalJSON は単価エントリを jq の参照セマンティクスに合わせて読む。
//
// オブジェクトでない値(数値・文字列・配列)は jq では `$p.input` の時点で
// インデックスエラーになるため、全キー欠落として表す。オブジェクトの場合、
// 数値でないキー(欠落・null・型違い)を Missing に記録する。error は返さない。
func (m *ModelPricing) UnmarshalJSON(data []byte) error {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		missing := make(map[string]bool, len(pricingKeys))
		for _, key := range pricingKeys {
			missing[key] = true
		}
		*m = ModelPricing{Missing: missing}
		return nil
	}

	// 全キーが揃っている通常のエントリでは Missing を nil に保つ。
	// リテラルで組んだ値(Missing nil)と reflect.DeepEqual で等しくなる。
	*m = ModelPricing{}
	missing := map[string]bool{}
	for _, key := range pricingKeys {
		var value float64
		if rawValue, ok := raw[key]; !ok || json.Unmarshal(rawValue, &value) != nil {
			missing[key] = true
			continue
		}
		switch key {
		case PricingKeyInput:
			m.Input = value
		case PricingKeyOutput:
			m.Output = value
		case PricingKeyCacheWrite5m:
			m.CacheWrite5m = value
		case PricingKeyCacheWrite1h:
			m.CacheWrite1h = value
		case PricingKeyCacheWrite:
			m.CacheWrite = value
		case PricingKeyCacheHit:
			m.CacheHit = value
		}
	}
	if len(missing) > 0 {
		m.Missing = missing
	}
	return nil
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
//
// jq の `//` は null と false だけを偽とするため、値が null / false のエントリは
// 「無いもの」として読み飛ばす(モデルはフォールバックへ、fast_multiplier は
// 既定値 6 へ落ちる)。それ以外の値は ModelPricing.UnmarshalJSON が jq の参照
// セマンティクスで受け止める。
func (p *Pricing) UnmarshalJSON(data []byte) error {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}

	p.Models = make(map[string]ModelPricing, len(raw))
	p.FastMultiplier = 0
	p.HasFastMultiplier = false
	for key, value := range raw {
		if !JSONTruthy(value) {
			continue
		}
		if key == pricingFastMultiplierKey {
			var multiplier float64
			if err := json.Unmarshal(value, &multiplier); err == nil {
				p.FastMultiplier = multiplier
				p.HasFastMultiplier = true
			}
			continue
		}
		var model ModelPricing
		// ModelPricing.UnmarshalJSON は error を返さない。
		_ = json.Unmarshal(value, &model)
		p.Models[key] = model
	}
	return nil
}

// pricingFastMultiplierKey はモデル名ではなく倍率を表す予約キー。
const pricingFastMultiplierKey = "fast_multiplier"
