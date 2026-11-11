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

//go:embed claude-pricing.json
var claudePrices []byte

//go:embed claude-compatible-pricing.json
var claudeCompatiblePrices []byte

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

func ClaudePriceCatalog() (OfficialPriceCatalog, error) {
	var catalog OfficialPriceCatalog
	if err := json.Unmarshal(claudePrices, &catalog); err != nil {
		return catalog, err
	}
	if catalog.AsOf == "" || len(catalog.Records) == 0 {
		return catalog, fmt.Errorf("Claude 官方价格目录为空")
	}
	for _, row := range catalog.Records {
		row.DisplayName = row.ModelName
		row.Description = "Claude 官方价格 · " + catalog.AsOf
	}
	return catalog, nil
}

func (s *ModelPricingService) BackfillClaudePrices(ctx context.Context) (int, error) {
	catalog, err := ClaudePriceCatalog()
	if err != nil {
		return 0, err
	}
	return s.store.BackfillOfficialPrices(ctx, "claude_prices_"+catalog.AsOf, catalog.Records)
}

// ClaudeCompatiblePriceCatalog contains models commonly reached through the
// Claude/Anthropic protocol. Their prices use USD per 1M tokens and the
// provider remains "claude" so the existing billing path can account for them.
func ClaudeCompatiblePriceCatalog() (OfficialPriceCatalog, error) {
	var catalog OfficialPriceCatalog
	if err := json.Unmarshal(claudeCompatiblePrices, &catalog); err != nil {
		return catalog, err
	}
	if catalog.AsOf == "" || len(catalog.Records) == 0 {
		return catalog, fmt.Errorf("Claude 兼容模型价格目录为空")
	}
	for _, row := range catalog.Records {
		row.DisplayName = row.ModelName
		row.Description = "Claude 接入模型官方价格 · " + catalog.AsOf
	}
	return catalog, nil
}

func (s *ModelPricingService) BackfillClaudeCompatiblePrices(ctx context.Context) (int, error) {
	catalog, err := ClaudeCompatiblePriceCatalog()
	if err != nil {
		return 0, err
	}
	return s.store.BackfillOfficialPrices(ctx, "claude_compatible_prices_"+catalog.AsOf, catalog.Records)
}
