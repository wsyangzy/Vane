package service

import (
	"math"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestClaudeCompatiblePriceCatalogUsesClaudeProviderAndValidPrices(t *testing.T) {
	catalog, err := ClaudeCompatiblePriceCatalog()
	require.NoError(t, err)
	require.Equal(t, "2026-11-11", catalog.AsOf)
	require.GreaterOrEqual(t, len(catalog.Records), 12)

	seen := make(map[string]bool, len(catalog.Records))
	for _, row := range catalog.Records {
		require.Equal(t, "claude", row.Provider, row.ModelName)
		key := row.Provider + "/" + row.ModelName + "/" + row.ServiceTier
		require.False(t, seen[key], "duplicate catalog row: %s", key)
		seen[key] = true
		for _, price := range []float64{row.InputPrice, row.OutputPrice, row.CacheCreationPrice5m, row.CacheCreationPrice1h, row.CacheReadPrice} {
			require.True(t, price >= 0 && !math.IsNaN(price) && !math.IsInf(price, 0), row.ModelName)
		}
	}
	for _, model := range []string{"deepseek-v4.1-flash", "glm-5.3", "kimi-k3.1-pro", "qwen3.8-max"} {
		require.True(t, seen["claude/"+model+"/"], model)
	}
}
