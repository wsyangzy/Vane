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
	ctx := context.Background()
	pricingStore := newModelPricingTestStore(t)
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

// 档位定位与删除的三条不变量：GetTier 按 (provider, model, tier) 精确命中；
// DeleteTier 只删子行；Delete 级联删该 provider 下全部行且不碰另一个 provider 的同名模型。
// 任一条失效的表现都是静默算错账，不会报错。
func TestSQLiteModelPricingStore_TierScopedCRUD(t *testing.T) {
	ctx := context.Background()
	pricingStore := newModelPricingTestStore(t)

	seed := func(provider, tier string, input float64) *ModelPricingRecord {
		t.Helper()
		record, err := pricingStore.Create(ctx, &ModelPricingRecord{
			ModelName: "shared-name", Provider: provider, ServiceTier: tier,
			InputPrice: input, OutputPrice: input * 4,
		})
		if err != nil {
			t.Fatalf("create %s/%s: %v", provider, tier, err)
		}
		return record
	}
	claudeMain := seed("claude", "", 3)
	seed("codex", "", 5)
	codexFlex := seed("codex", "flex", 2.5)

	mustTier := func(provider, tier string, wantInput float64) *ModelPricingRecord {
		t.Helper()
		got, err := pricingStore.GetTier(ctx, provider, "shared-name", tier)
		if err != nil {
			t.Fatalf("get tier %s/%s: %v", provider, tier, err)
		}
		if got == nil {
			t.Fatalf("tier %s/%s not found", provider, tier)
		}
		if got.InputPrice != wantInput {
			t.Fatalf("tier %s/%s: input=%v, want %v", provider, tier, got.InputPrice, wantInput)
		}
		return got
	}
	mustTier("claude", "", 3)
	mustTier("codex", "", 5)
	mustTier("codex", "flex", 2.5)
	if got, err := pricingStore.GetTier(ctx, "claude", "shared-name", "flex"); err != nil || got != nil {
		t.Fatalf("claude 不该看到 codex 的 flex 档位: got=%#v err=%v", got, err)
	}

	// Update 按 ID 定位，因此把一行的 provider 从 codex 改到 claude 也能命中原行；
	// 若 WHERE 里写死 provider，这里会静默匹配不到、什么都不改。
	codexFlex.Provider = "claude"
	codexFlex.InputPrice = 9
	if err := pricingStore.Update(ctx, codexFlex); err != nil {
		t.Fatalf("update by id: %v", err)
	}
	mustTier("claude", "flex", 9)
	if got, _ := pricingStore.GetTier(ctx, "codex", "shared-name", "flex"); got != nil {
		t.Fatal("改 provider 后原 codex 档位行应消失")
	}

	if err := pricingStore.DeleteTier(ctx, "claude", "shared-name", ""); err == nil {
		t.Fatal("DeleteTier 不该接受空档位——那是整模型删除")
	}
	if err := pricingStore.DeleteTier(ctx, "claude", "shared-name", "flex"); err != nil {
		t.Fatalf("delete tier: %v", err)
	}
	mustTier("claude", "", 3) // 主行不受档位删除影响

	// 整模型删除只作用于指定 provider
	seed("codex", "flex", 2.5)
	if err := pricingStore.Delete(ctx, "codex", "shared-name"); err != nil {
		t.Fatalf("delete model: %v", err)
	}
	remaining, err := pricingStore.List(ctx)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(remaining) != 1 || remaining[0].ID != claudeMain.ID {
		t.Fatalf("codex 整模型删除后应只剩 claude 主行, got %d 行: %#v", len(remaining), remaining)
	}
}

func newModelPricingTestStore(t *testing.T) *SQLiteModelPricingStore {
	t.Helper()
	db, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "model-pricing.db"))
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	if _, err = db.Exec(`
CREATE TABLE model_pricing (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    model_name TEXT NOT NULL,
    provider TEXT NOT NULL DEFAULT 'claude',
    service_tier TEXT NOT NULL DEFAULT '',
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
    updated_at DATETIME,
    UNIQUE(provider, model_name, service_tier)
);`); err != nil {
		t.Fatalf("create model_pricing: %v", err)
	}

	return NewSQLiteModelPricingStore(db)
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
