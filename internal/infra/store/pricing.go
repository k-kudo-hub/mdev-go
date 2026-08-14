package store

import (
	"encoding/json"
	"os"
	"path/filepath"

	"github.com/k-kudo-hub/mdev-go/assets"
	"github.com/k-kudo-hub/mdev-go/internal/domain"
)

// PricingStore は設定ファイルから単価表を読む app.PricingLoader の実装である。
type PricingStore struct {
	conductorHome string
}

// NewPricingStore は conductorHome 配下の設定を読む PricingStore を返す。
func NewPricingStore(conductorHome string) PricingStore {
	return PricingStore{conductorHome: conductorHome}
}

// Load は単価表を読む。読めなければ空の単価表を返す(LoadPricing を参照)。
func (s PricingStore) Load() domain.Pricing {
	return LoadPricing(s.conductorHome)
}

// LoadPricing は設定から pricing セクションだけを読む。
//
// LoadConfig(ファイル単位のフォールバック・壊れた JSON はエラー)とは規則が違う。
// 現行 record-output.sh:24-32 は pricing というキー単位で
// config.json → config.default.json の順に探し、どちらも読めなければ空の単価表で
// 処理を続ける(エラー終了しない)。料金の内訳が欠けることより、作業の完了記録
// そのものが失われることのほうが重いためである。この規則をそのまま写している。
//
// 壊れた config.json は「pricing が取れなかった」として扱われ、
// config.default.json へフォールバックする。
// どちらのファイルからも取れなかった場合は、実行ファイルに埋め込まれた
// config.default.json を最後の手として見る。単価表が空だと料金が 0 円として
// 記録され、後から見て「無料だった」のか「単価が無かった」のか区別できない。
func LoadPricing(conductorHome string) domain.Pricing {
	for _, name := range []string{configFileName, defaultConfigFileName} {
		if pricing, found := readPricing(filepath.Join(conductorHome, name)); found {
			return pricing
		}
	}
	if embedded, ok := assets.Read(defaultConfigFileName); ok {
		if pricing, found := parsePricing(embedded); found {
			return pricing
		}
	}
	return emptyPricing()
}

// readPricing は 1 つの設定ファイルから pricing を読む。
//
// found=false は「次のファイルを見るべき」を意味する。ファイルが無い、JSON が
// 壊れている、pricing キーが無い、pricing が null か false の場合である
// (現行版の `jq -c '.pricing // empty'` が空文字を返す条件と同じ)。
//
// pricing はあるが単価表として解釈できない場合(配列など)は found=true で空の
// 単価表を返す。現行版もそこでフォールバックを打ち切るためである。
func readPricing(path string) (domain.Pricing, bool) {
	b, err := os.ReadFile(path) //nolint:gosec // CONDUCTOR_HOME 配下の固定ファイル名
	if err != nil {
		return domain.Pricing{}, false
	}
	return parsePricing(b)
}

// parsePricing は設定の中身から pricing を読む。判定の規則は readPricing と
// 同じで、埋め込みと実ファイルで扱いを変えないために切り出してある。
func parsePricing(b []byte) (domain.Pricing, bool) {
	var config struct {
		Pricing json.RawMessage `json:"pricing"`
	}
	if err := json.Unmarshal(b, &config); err != nil {
		return domain.Pricing{}, false
	}
	if !hasPricingValue(config.Pricing) {
		return domain.Pricing{}, false
	}

	var pricing domain.Pricing
	if err := json.Unmarshal(config.Pricing, &pricing); err != nil {
		return emptyPricing(), true
	}
	return pricing, true
}

// hasPricingValue は pricing キーに値があるかを返す。
//
// 現行版の `.pricing // empty` は jq の `//` を使っており、判定は
// domain.JSONTruthy(偽は null と false だけ。空オブジェクト `{}` は真)に委ねる。
func hasPricingValue(raw json.RawMessage) bool {
	return domain.JSONTruthy(raw)
}

// emptyPricing は単価が 1 件も無い状態を返す(現行版の `PRICING_JSON="{}"`)。
func emptyPricing() domain.Pricing {
	return domain.Pricing{Models: map[string]domain.ModelPricing{}}
}
