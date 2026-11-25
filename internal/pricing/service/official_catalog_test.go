package service

import (
	"math"
	"testing"

	"github.com/stretchr/testify/require"
	pricingstore "github.com/wsyangzy/Vane/internal/pricing/store"
)

func TestBundledOpenAIPricesCoverPublishedSpeedsAndCacheWrites(t *testing.T) {
	catalog, err := OpenAIPriceCatalog()
	require.NoError(t, err)
	require.Equal(t, "2026-11-18", catalog.AsOf)
	seen := map[string]bool{}
	flagship := map[string][]float64{}
	gpt62 := map[string]*pricingstore.ModelPricingRecord{}
	for _, row := range catalog.Records {
		key := row.Provider + "/" + row.ModelName + "/" + row.ServiceTier
		require.False(t, seen[key], "duplicate catalog price: %s", key)
		seen[key] = true
		for _, value := range []float64{row.InputPrice, row.OutputPrice, row.CacheReadPrice, row.CacheWritePrice30m, row.CacheWritePrice12h} {
			require.False(t, math.IsNaN(value) || math.IsInf(value, 0))
			require.GreaterOrEqual(t, value, 0.0)
		}
		require.Equal(t, "codex", row.Provider)
		if row.ModelName == "gpt-6.1-sol" {
			flagship[row.ServiceTier] = []float64{row.InputPrice, row.OutputPrice, row.CacheReadPrice, row.CacheWritePrice30m, row.CacheWritePrice12h}
		}
		if row.ModelName == "gpt-6.2-astra" || row.ModelName == "gpt-6.2-sol" {
			gpt62[row.ModelName+"/"+row.ServiceTier] = row
		}
	}
	require.Equal(t, map[string][]float64{
		"": {2, 10, .1, 2.5, 5}, "batch": {1, 5, .05, 1.25, 2.5}, "flex": {1, 5, .05, 1.25, 2.5},
		"fast": {4, 20, .2, 5, 10}, "ultrafast": {12, 60, .6, 15, 30},
	}, flagship)
	for key, expected := range map[string][]float64{
		"gpt-6.2-astra/":          {8, 40, .4, 10, 20, 16, 60, .8, 20, 40},
		"gpt-6.2-sol/":            {2, 10, .1, 2.5, 5, 4, 15, .2, 5, 10},
		"gpt-6.2-astra/batch":     {4, 20, .2, 5, 10, 8, 30, .4, 10, 20},
		"gpt-6.2-sol/batch":       {1, 5, .05, 1.25, 2.5, 2, 7.5, .1, 2.5, 5},
		"gpt-6.2-astra/fast":      {16, 80, .8, 20, 40, 32, 120, 1.6, 40, 80},
		"gpt-6.2-sol/fast":        {4, 20, .2, 5, 10, 8, 30, .4, 10, 20},
		"gpt-6.2-astra/ultrafast": {24, 120, 1.2, 30, 60, 48, 180, 2.4, 60, 120},
		"gpt-6.2-sol/ultrafast":   {6, 30, .3, 7.5, 15, 12, 45, .6, 15, 30},
	} {
		row := gpt62[key]
		require.NotNil(t, row, "missing official price row %s", key)
		require.Equal(t, int64(272000), row.LongContextThreshold, key)
		require.Equal(t, expected, []float64{
			row.InputPrice, row.OutputPrice, row.CacheReadPrice, row.CacheWritePrice30m, row.CacheWritePrice12h,
			row.LongContextInputPrice, row.LongContextOutputPrice, row.LongContextCacheReadPrice,
			row.LongContextCacheWritePrice, row.LongContextCacheWritePrice12h,
		}, key)
	}
	require.False(t, seen["codex/gpt-5/ultrafast"], "do not fabricate unsupported speed prices")
}

func TestBundledClaudeFable52PricesMatchOfficialStandardRates(t *testing.T) {
	catalog, err := ClaudePriceCatalog()
	require.NoError(t, err)
	require.Equal(t, "2026-11-11", catalog.AsOf)
	require.Equal(t, "https://platform.claude.com/docs/en/about-claude/pricing", catalog.Source)
	require.Len(t, catalog.Records, 1)
	row := catalog.Records[0]
	require.Equal(t, "claude", row.Provider)
	require.Equal(t, "claude-fable-5-2", row.ModelName)
	require.Empty(t, row.ServiceTier)
	require.Equal(t, 8.0, row.InputPrice)
	require.Equal(t, 40.0, row.OutputPrice)
	require.Equal(t, 10.0, row.CacheCreationPrice5m)
	require.Equal(t, 16.0, row.CacheCreationPrice1h)
	require.Equal(t, .2, row.CacheReadPrice, "Fable 5.2 cache reads use 0.025x input, not the legacy 0.1x default")
	require.Zero(t, row.LongContextThreshold, "the full 1M context window uses standard pricing")
}

func TestCacheWritePriceValidationAndTrackingMapping(t *testing.T) {
	s := &ModelPricingService{}
	for _, invalid := range []float64{-1, math.NaN(), math.Inf(1)} {
		require.Error(t, s.validateRecord(&pricingstore.ModelPricingRecord{ModelName: "test", CacheWritePrice12h: invalid}))
	}
	record := &pricingstore.ModelPricingRecord{ModelName: "test", Provider: "codex", CacheWritePrice30m: 5, CacheWritePrice12h: 10, LongContextCacheWritePrice12h: 20}
	require.NoError(t, s.validateRecord(record))
	p := s.ToTrackingPricing(record)
	require.True(t, p.CodexCachePricing)
	require.Equal(t, 5.0, p.CacheWritePrice30m)
	require.Equal(t, 10.0, p.CacheWritePrice12h)
	require.Equal(t, 20.0, p.LongContextCacheWritePrice12h)
	record.Provider = "claude"
	require.False(t, s.ToTrackingPricing(record).CodexCachePricing)
}
