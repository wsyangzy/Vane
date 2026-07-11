package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"math"
	"path/filepath"
	"testing"

	_ "modernc.org/sqlite"
)

func TestSQLiteModelPricingStore_LongContextRoundTrip(t *testing.T) {
	db, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "model-pricing.db"))
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	_, err = db.Exec(`
CREATE TABLE model_pricing (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    model_name TEXT UNIQUE NOT NULL,
    provider TEXT NOT NULL DEFAULT 'claude',
    input_price REAL NOT NULL DEFAULT 3.0,
    output_price REAL NOT NULL DEFAULT 15.0,
    cache_creation_price_5m REAL DEFAULT 3.75,
    cache_creation_price_1h REAL DEFAULT 6.0,
    cache_read_price REAL DEFAULT 0.30,
    long_context_threshold INTEGER NOT NULL DEFAULT 0,
    long_context_input_price REAL NOT NULL DEFAULT 0,
    long_context_cache_read_price REAL NOT NULL DEFAULT 0,
    long_context_cache_write_price REAL NOT NULL DEFAULT 0,
    long_context_output_price REAL NOT NULL DEFAULT 0,
    display_name TEXT,
    description TEXT,
    is_default INTEGER DEFAULT 0,
    created_at DATETIME,
    updated_at DATETIME
);`)
	if err != nil {
		t.Fatalf("create model_pricing: %v", err)
	}

	ctx := context.Background()
	pricingStore := NewSQLiteModelPricingStore(db)
	created, err := pricingStore.Create(ctx, decodeModelPricingRecord(t, `{
        "model_name": "gpt-long",
        "provider": "codex",
        "input_price": 5,
        "output_price": 30,
        "cache_creation_price_5m": 6.25,
        "cache_read_price": 0.5,
        "long_context_threshold": 272000,
        "long_context_input_price": 7.5,
        "long_context_cache_read_price": 0.75,
        "long_context_cache_write_price": 9.375,
        "long_context_output_price": 45,
        "is_default": true
    }`))
	if err != nil {
		t.Fatalf("create pricing: %v", err)
	}

	assertLongContextPricing(t, mustGetPricing(t, pricingStore, ctx, "gpt-long"), 272000, 7.5, 0.75, 9.375, 45)
	byID, err := pricingStore.GetByID(ctx, created.ID)
	if err != nil {
		t.Fatalf("get pricing by id: %v", err)
	}
	assertLongContextPricing(t, byID, 272000, 7.5, 0.75, 9.375, 45)

	all, err := pricingStore.List(ctx)
	if err != nil || len(all) != 1 {
		t.Fatalf("list pricing: len=%d err=%v", len(all), err)
	}
	assertLongContextPricing(t, all[0], 272000, 7.5, 0.75, 9.375, 45)
	byProvider, err := pricingStore.ListByProvider(ctx, "codex")
	if err != nil || len(byProvider) != 1 {
		t.Fatalf("list pricing by provider: len=%d err=%v", len(byProvider), err)
	}
	assertLongContextPricing(t, byProvider[0], 272000, 7.5, 0.75, 9.375, 45)
	defaultPricing, err := pricingStore.GetDefault(ctx)
	if err != nil {
		t.Fatalf("get default pricing: %v", err)
	}
	assertLongContextPricing(t, defaultPricing, 272000, 7.5, 0.75, 9.375, 45)
	providerDefault, err := pricingStore.GetDefaultByProvider(ctx, "codex")
	if err != nil {
		t.Fatalf("get provider default pricing: %v", err)
	}
	assertLongContextPricing(t, providerDefault, 272000, 7.5, 0.75, 9.375, 45)

	updated := decodeModelPricingRecord(t, `{
        "model_name": "gpt-long",
        "provider": "codex",
        "input_price": 5,
        "output_price": 30,
        "long_context_threshold": 300000,
        "long_context_input_price": 8,
        "long_context_cache_read_price": 0.8,
        "long_context_cache_write_price": 10,
        "long_context_output_price": 48,
        "is_default": true
    }`)
	if err := pricingStore.Update(ctx, updated); err != nil {
		t.Fatalf("update pricing: %v", err)
	}
	assertLongContextPricing(t, mustGetPricing(t, pricingStore, ctx, "gpt-long"), 300000, 8, 0.8, 10, 48)

	batchRecord := decodeModelPricingRecord(t, `{
        "model_name": "gpt-batch",
        "provider": "codex",
        "input_price": 2,
        "output_price": 8,
        "long_context_threshold": 128000,
        "long_context_input_price": 3,
        "long_context_cache_read_price": 0.3,
        "long_context_cache_write_price": 3.75,
        "long_context_output_price": 12
    }`)
	if err := pricingStore.BatchCreate(ctx, []*ModelPricingRecord{batchRecord}); err != nil {
		t.Fatalf("batch create pricing: %v", err)
	}
	assertLongContextPricing(t, mustGetPricing(t, pricingStore, ctx, "gpt-batch"), 128000, 3, 0.3, 3.75, 12)

	batchUpsert := decodeModelPricingRecord(t, `{
        "model_name": "gpt-batch",
        "provider": "codex",
        "input_price": 2,
        "output_price": 8,
        "long_context_threshold": 256000,
        "long_context_input_price": 4,
        "long_context_cache_read_price": 0.4,
        "long_context_cache_write_price": 5,
        "long_context_output_price": 16
    }`)
	if err := pricingStore.BatchUpsert(ctx, []*ModelPricingRecord{batchUpsert}); err != nil {
		t.Fatalf("batch upsert pricing: %v", err)
	}
	assertLongContextPricing(t, mustGetPricing(t, pricingStore, ctx, "gpt-batch"), 256000, 4, 0.4, 5, 16)
}

func decodeModelPricingRecord(t *testing.T, payload string) *ModelPricingRecord {
	t.Helper()
	var record ModelPricingRecord
	if err := json.Unmarshal([]byte(payload), &record); err != nil {
		t.Fatalf("decode model pricing: %v", err)
	}
	return &record
}

func mustGetPricing(t *testing.T, pricingStore *SQLiteModelPricingStore, ctx context.Context, modelName string) *ModelPricingRecord {
	t.Helper()
	record, err := pricingStore.Get(ctx, modelName)
	if err != nil {
		t.Fatalf("get pricing %s: %v", modelName, err)
	}
	if record == nil {
		t.Fatalf("pricing %s not found", modelName)
	}
	return record
}

func assertLongContextPricing(t *testing.T, record *ModelPricingRecord, threshold int64, input, cacheRead, cacheWrite, output float64) {
	t.Helper()
	encoded, err := json.Marshal(record)
	if err != nil {
		t.Fatalf("encode model pricing: %v", err)
	}
	var values map[string]any
	if err := json.Unmarshal(encoded, &values); err != nil {
		t.Fatalf("decode model pricing map: %v", err)
	}

	assertJSONNumber(t, values, "long_context_threshold", float64(threshold))
	assertJSONNumber(t, values, "long_context_input_price", input)
	assertJSONNumber(t, values, "long_context_cache_read_price", cacheRead)
	assertJSONNumber(t, values, "long_context_cache_write_price", cacheWrite)
	assertJSONNumber(t, values, "long_context_output_price", output)
}

func assertJSONNumber(t *testing.T, values map[string]any, key string, expected float64) {
	t.Helper()
	actual, ok := values[key].(float64)
	if !ok {
		t.Fatalf("expected numeric JSON field %s, got %#v", key, values[key])
	}
	if math.Abs(actual-expected) > 0.000001 {
		t.Fatalf("expected %s=%f, got %f", key, expected, actual)
	}
}
