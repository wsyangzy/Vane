package app

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"math"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
	pricingservice "github.com/wsyangzy/Vane/internal/pricing/service"
	pricingstore "github.com/wsyangzy/Vane/internal/pricing/store"
	"github.com/wsyangzy/Vane/internal/tracking"
	"github.com/wsyangzy/Vane/internal/tracking/storage"
)

func TestFreshPricingStartupSeedsOfficialRatesAndRespectsLaterEdits(t *testing.T) {
	adapter, err := storage.NewSQLiteAdapter(storage.DatabaseConfig{DatabasePath: filepath.Join(t.TempDir(), "fresh.db")})
	require.NoError(t, err)
	require.NoError(t, adapter.Open())
	defer adapter.Close()
	require.NoError(t, adapter.InitSchema())
	a := &App{storeDB: adapter.GetWriteDB(), logger: slog.New(slog.NewTextHandler(io.Discard, nil))}
	a.setupModelPricingStore()
	ctx := context.Background()
	fable, err := a.modelPricingStore.GetTier(ctx, "claude", "claude-fable-5-2", "")
	require.NoError(t, err)
	require.NotNil(t, fable)
	require.Equal(t, 8.0, fable.InputPrice)
	require.Equal(t, 40.0, fable.OutputPrice)
	require.Equal(t, 10.0, fable.CacheCreationPrice5m)
	require.Equal(t, 16.0, fable.CacheCreationPrice1h)
	require.Equal(t, .2, fable.CacheReadPrice)
	fable.CacheReadPrice = .5
	require.NoError(t, a.modelPricingStore.Update(ctx, fable))

	o3, err := a.modelPricingStore.GetTier(ctx, "codex", "o3", "")
	require.NoError(t, err)
	require.NotNil(t, o3)
	require.Equal(t, .5, o3.CacheReadPrice, "legacy seed must not replace the official cached-input price with zero")
	fast, err := a.modelPricingStore.GetTier(ctx, "codex", "gpt-6.1-sol", "fast")
	require.NoError(t, err)
	require.Equal(t, 4.0, fast.InputPrice)
	astra, err := a.modelPricingStore.GetTier(ctx, "codex", "gpt-6.2-astra", "")
	require.NoError(t, err)
	require.NotNil(t, astra)
	require.Equal(t, 8.0, astra.InputPrice)
	sol, err := a.modelPricingStore.GetTier(ctx, "codex", "gpt-6.2-sol", "")
	require.NoError(t, err)
	require.NotNil(t, sol)
	require.Equal(t, 2.0, sol.InputPrice)
	fast.InputPrice = 7
	require.NoError(t, a.modelPricingStore.Update(ctx, fast))
	require.NoError(t, a.modelPricingStore.DeleteTier(ctx, "codex", "gpt-6.1-sol", "ultrafast"))
	a.setupModelPricingStore()
	fast, err = a.modelPricingStore.GetTier(ctx, "codex", "gpt-6.1-sol", "fast")
	require.NoError(t, err)
	require.Equal(t, 7.0, fast.InputPrice)
	ultra, err := a.modelPricingStore.GetTier(ctx, "codex", "gpt-6.1-sol", "ultrafast")
	require.NoError(t, err)
	require.Nil(t, ultra)
	fable, err = a.modelPricingStore.GetTier(ctx, "claude", "claude-fable-5-2", "")
	require.NoError(t, err)
	require.NotNil(t, fable)
	require.Equal(t, .5, fable.CacheReadPrice, "restart must preserve a user's edited Claude price")
}

func TestGPT62PricesBackfillAfterEarlierCatalogAndRespectLaterEdits(t *testing.T) {
	adapter, err := storage.NewSQLiteAdapter(storage.DatabaseConfig{DatabasePath: filepath.Join(t.TempDir(), "upgrade.db")})
	require.NoError(t, err)
	require.NoError(t, adapter.Open())
	defer adapter.Close()
	require.NoError(t, adapter.InitSchema())
	ctx := context.Background()
	st := pricingstore.NewSQLiteModelPricingStore(adapter.GetWriteDB())
	catalog, err := pricingservice.OpenAIPriceCatalog()
	require.NoError(t, err)
	var earlierRows []*pricingstore.ModelPricingRecord
	for _, row := range catalog.Records {
		if row.ModelName != "gpt-6.2-astra" && row.ModelName != "gpt-6.2-sol" {
			earlierRows = append(earlierRows, row)
		}
	}
	_, err = st.BackfillOfficialPrices(ctx, "openai_prices_2026-10-30", earlierRows)
	require.NoError(t, err)
	a := &App{storeDB: adapter.GetWriteDB(), logger: slog.New(slog.NewTextHandler(io.Discard, nil))}
	a.setupModelPricingStore()
	for _, model := range []string{"gpt-6.2-astra", "gpt-6.2-sol"} {
		for _, tier := range []string{"", "batch", "fast", "ultrafast"} {
			got, err := st.GetTier(ctx, "codex", model, tier)
			require.NoError(t, err)
			require.NotNil(t, got, "%s/%s must be added after the earlier catalog import", model, tier)
		}
	}
	sol, err := st.GetTier(ctx, "codex", "gpt-6.2-sol", "")
	require.NoError(t, err)
	sol.InputPrice = 7
	require.NoError(t, st.Update(ctx, sol))
	require.NoError(t, st.DeleteTier(ctx, "codex", "gpt-6.2-astra", "ultrafast"))
	a.setupModelPricingStore()
	sol, err = st.GetTier(ctx, "codex", "gpt-6.2-sol", "")
	require.NoError(t, err)
	require.Equal(t, 7.0, sol.InputPrice)
	ultra, err := st.GetTier(ctx, "codex", "gpt-6.2-astra", "ultrafast")
	require.NoError(t, err)
	require.Nil(t, ultra, "restart must preserve a user's disabled tier")
}

func TestClaudePricesBackfillAfterEarlierCatalogsPreservesExistingPrices(t *testing.T) {
	for _, custom := range []bool{false, true} {
		name := "missing model"
		if custom {
			name = "custom model"
		}
		t.Run(name, func(t *testing.T) {
			adapter, err := storage.NewSQLiteAdapter(storage.DatabaseConfig{DatabasePath: filepath.Join(t.TempDir(), "upgrade.db")})
			require.NoError(t, err)
			require.NoError(t, adapter.Open())
			defer adapter.Close()
			require.NoError(t, adapter.InitSchema())
			ctx := context.Background()
			st := pricingstore.NewSQLiteModelPricingStore(adapter.GetWriteDB())
			svc := pricingservice.NewModelPricingService(st)
			_, err = svc.BackfillOfficialPrices(ctx)
			require.NoError(t, err)
			_, err = svc.BackfillClaudeCompatiblePrices(ctx)
			require.NoError(t, err)
			if custom {
				_, err = st.Create(ctx, &pricingstore.ModelPricingRecord{
					Provider: "claude", ModelName: "claude-fable-5-2", Description: "custom",
					InputPrice: 3, OutputPrice: 15, CacheCreationPrice5m: 4, CacheCreationPrice1h: 6, CacheReadPrice: .3,
				})
				require.NoError(t, err)
			}
			a := &App{storeDB: adapter.GetWriteDB(), logger: slog.New(slog.NewTextHandler(io.Discard, nil))}
			a.setupModelPricingStore()
			got, err := st.GetTier(ctx, "claude", "claude-fable-5-2", "")
			require.NoError(t, err)
			require.NotNil(t, got, "an earlier catalog import must not block the new Claude catalog")
			if custom {
				require.Equal(t, 3.0, got.InputPrice)
				require.Equal(t, 15.0, got.OutputPrice)
				require.Equal(t, 4.0, got.CacheCreationPrice5m)
				require.Equal(t, 6.0, got.CacheCreationPrice1h)
				require.Equal(t, .3, got.CacheReadPrice)
				require.Equal(t, "custom", got.Description)
			} else {
				require.Equal(t, 8.0, got.InputPrice)
				require.Equal(t, .2, got.CacheReadPrice)
			}
		})
	}
}

func TestSelectedDefaultPricingReachesRuntimeAndChangesWithSelection(t *testing.T) {
	adapter, err := storage.NewSQLiteAdapter(storage.DatabaseConfig{DatabasePath: filepath.Join(t.TempDir(), "default.db")})
	require.NoError(t, err)
	require.NoError(t, adapter.Open())
	defer adapter.Close()
	require.NoError(t, adapter.InitSchema())
	tracker := newAppUsageTestTracker(t)
	defer tracker.Close()
	st := pricingstore.NewSQLiteModelPricingStore(adapter.GetWriteDB())
	a := &App{
		modelPricingService: pricingservice.NewModelPricingService(st), usageTracker: tracker,
		logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
	}
	ctx := context.Background()
	for _, row := range []*pricingstore.ModelPricingRecord{
		{Provider: "claude", ModelName: "claude-sonnet-5-5", IsDefault: true, InputPrice: 2, OutputPrice: 10},
		{Provider: "codex", ModelName: "gpt-5.5", IsDefault: true, InputPrice: 5, OutputPrice: 30},
		{Provider: "claude", ModelName: "replacement", InputPrice: 4, OutputPrice: 20},
	} {
		_, err := st.Create(ctx, row)
		require.NoError(t, err)
	}
	a.syncPricingToTracker(ctx)
	assertCost := func(provider string, want float64) {
		cost, known := tracker.CalculateAttemptCost(provider, "", "", "", "unknown", "", &tracking.TokenUsage{InputTokens: 1000000})
		require.True(t, known)
		require.Equal(t, want, cost)
	}
	assertCost("claude", 2)
	assertCost("codex", 5)
	require.NoError(t, a.SetDefaultModelPricing("replacement"))
	assertCost("claude", 4)
	assertCost("codex", 5)
}

func TestModelPricingWailsDTOCarriesLongContext(t *testing.T) {
	inputValues := decodePricingJSON(t, `{
        "model_name": "gpt-long",
        "provider": "codex",
        "cache_write_price_30m": 2.5,
        "cache_write_price_12h": 5,
        "long_context_cache_write_price_12h": 10,
        "long_context_threshold": 272000,
        "long_context_input_price": 7.5,
        "long_context_cache_read_price": 0.75,
        "long_context_cache_write_price": 9.375,
        "long_context_output_price": 45
    }`, &CreateModelPricingInput{})
	assertPricingJSONNumber(t, inputValues, "cache_write_price_30m", 2.5)
	assertPricingJSONNumber(t, inputValues, "cache_write_price_12h", 5)
	assertPricingJSONNumber(t, inputValues, "long_context_cache_write_price_12h", 10)
	assertPricingJSONNumber(t, inputValues, "long_context_threshold", 272000)
	assertPricingJSONNumber(t, inputValues, "long_context_input_price", 7.5)
	assertPricingJSONNumber(t, inputValues, "long_context_cache_read_price", 0.75)
	assertPricingJSONNumber(t, inputValues, "long_context_cache_write_price", 9.375)
	assertPricingJSONNumber(t, inputValues, "long_context_output_price", 45)

	info := (&App{}).pricingRecordToInfo(&pricingstore.ModelPricingRecord{
		CacheWritePrice30m:            5,
		CacheWritePrice12h:            10,
		LongContextCacheWritePrice12h: 20,
		LongContextThreshold:          300_000,
		LongContextInputPrice:         8,
		LongContextCacheReadPrice:     0.8,
		LongContextCacheWritePrice:    10,
		LongContextOutputPrice:        48,
	})
	infoValues := encodePricingJSON(t, info)
	assertPricingJSONNumber(t, infoValues, "cache_write_price_30m", 5)
	assertPricingJSONNumber(t, infoValues, "cache_write_price_12h", 10)
	assertPricingJSONNumber(t, infoValues, "long_context_cache_write_price_12h", 20)
	assertPricingJSONNumber(t, infoValues, "long_context_threshold", 300000)
	assertPricingJSONNumber(t, infoValues, "long_context_input_price", 8)
	assertPricingJSONNumber(t, infoValues, "long_context_cache_read_price", 0.8)
	assertPricingJSONNumber(t, infoValues, "long_context_cache_write_price", 10)
	assertPricingJSONNumber(t, infoValues, "long_context_output_price", 48)
}

func decodePricingJSON(t *testing.T, payload string, target any) map[string]any {
	t.Helper()
	if err := json.Unmarshal([]byte(payload), target); err != nil {
		t.Fatalf("decode pricing JSON: %v", err)
	}
	return encodePricingJSON(t, target)
}

func encodePricingJSON(t *testing.T, value any) map[string]any {
	t.Helper()
	encoded, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("encode pricing JSON: %v", err)
	}
	var values map[string]any
	if err := json.Unmarshal(encoded, &values); err != nil {
		t.Fatalf("decode pricing JSON map: %v", err)
	}
	return values
}

func assertPricingJSONNumber(t *testing.T, values map[string]any, key string, expected float64) {
	t.Helper()
	actual, ok := values[key].(float64)
	if !ok {
		t.Fatalf("expected numeric JSON field %s, got %#v", key, values[key])
	}
	if math.Abs(actual-expected) > 0.000001 {
		t.Fatalf("expected %s=%f, got %f", key, expected, actual)
	}
}
