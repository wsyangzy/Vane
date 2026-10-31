package service

import (
	"context"
	_ "embed"
	"encoding/json"
	"fmt"

	pricingstore "github.com/wsyangzy/Vane/internal/pricing/store"
)

//go:embed openai-pricing.json
var openAIPrices []byte

type OfficialPriceCatalog struct {
	Source  string                             `json:"source"`
	AsOf    string                             `json:"as_of"`
	Records []*pricingstore.ModelPricingRecord `json:"records"`
}

func OpenAIPriceCatalog() (OfficialPriceCatalog, error) {
	var catalog OfficialPriceCatalog
	err := json.Unmarshal(openAIPrices, &catalog)
	if err != nil {
		return catalog, err
	}
	if catalog.AsOf == "" || len(catalog.Records) == 0 {
		return catalog, fmt.Errorf("官方价格目录为空")
	}
	for _, row := range catalog.Records {
		row.DisplayName = row.ModelName
		row.Description = "OpenAI 官方价格 · " + catalog.AsOf
	}
	return catalog, nil
}

func (s *ModelPricingService) BackfillOfficialPrices(ctx context.Context) (int, error) {
	catalog, err := OpenAIPriceCatalog()
	if err != nil {
		return 0, err
	}
	return s.store.BackfillOfficialPrices(ctx, "openai_prices_"+catalog.AsOf, catalog.Records)
}
