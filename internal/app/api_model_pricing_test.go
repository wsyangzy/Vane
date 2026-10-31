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
	pricingstore "github.com/wsyangzy/Vane/internal/pricing/store"
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
	o3, err := a.modelPricingStore.GetTier(ctx, "codex", "o3", "")
	require.NoError(t, err)
	require.NotNil(t, o3)
	require.Equal(t, .5, o3.CacheReadPrice, "legacy seed must not replace the official cached-input price with zero")
	fast, err := a.modelPricingStore.GetTier(ctx, "codex", "gpt-6.1-sol", "fast")
	require.NoError(t, err)
	require.Equal(t, 4.0, fast.InputPrice)
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
