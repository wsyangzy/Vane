package store

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestOfficialCatalogBackfillPreservesCustomPricesAndRunsOnce(t *testing.T) {
	s := newModelPricingTestStore(t)
	ctx := context.Background()
	custom, err := s.Create(ctx, &ModelPricingRecord{Provider: "codex", ModelName: "test", InputPrice: 7, OutputPrice: 8, CacheWritePrice30m: 9, Description: "custom", IsDefault: true})
	require.NoError(t, err)
	rows := []*ModelPricingRecord{
		{Provider: "codex", ModelName: "test", InputPrice: 2, OutputPrice: 10, CacheWritePrice30m: 2.5, CacheWritePrice12h: 5},
		{Provider: "codex", ModelName: "test", ServiceTier: "ultrafast", InputPrice: 12, OutputPrice: 60, CacheWritePrice30m: 15, CacheWritePrice12h: 30, LongContextCacheWritePrice12h: 60},
	}
	changed, err := s.BackfillOfficialPrices(ctx, "catalog-test", rows)
	require.NoError(t, err)
	require.Equal(t, 2, changed)
	got, err := s.GetByID(ctx, custom.ID)
	require.NoError(t, err)
	require.Equal(t, 7.0, got.InputPrice)
	require.Equal(t, 8.0, got.OutputPrice)
	require.Equal(t, 9.0, got.CacheWritePrice30m)
	require.Equal(t, 5.0, got.CacheWritePrice12h)
	require.Equal(t, "custom", got.Description)
	require.True(t, got.IsDefault)
	ultra, err := s.GetTier(ctx, "codex", "test", "ultrafast")
	require.NoError(t, err)
	require.Equal(t, 60.0, ultra.LongContextCacheWritePrice12h)
	got.CacheWritePrice12h = 0
	require.NoError(t, s.Update(ctx, got))
	require.NoError(t, s.DeleteTier(ctx, "codex", "test", "ultrafast"))
	changed, err = s.BackfillOfficialPrices(ctx, "catalog-test", rows)
	require.NoError(t, err)
	require.Zero(t, changed)
	ultra, err = s.GetTier(ctx, "codex", "test", "ultrafast")
	require.NoError(t, err)
	require.Nil(t, ultra, "restart must respect a user's deleted tier")
	got, err = s.GetByID(ctx, custom.ID)
	require.NoError(t, err)
	require.Zero(t, got.CacheWritePrice12h, "restart must respect an intentionally cleared price")
}

func TestCacheWritePriceBatchRoundTrip(t *testing.T) {
	s := newModelPricingTestStore(t)
	ctx := context.Background()
	row := &ModelPricingRecord{Provider: "codex", ModelName: "cache-model", InputPrice: 2, OutputPrice: 10, CacheWritePrice30m: 2.5, CacheWritePrice12h: 5, LongContextCacheWritePrice12h: 10}
	require.NoError(t, s.BatchCreate(ctx, []*ModelPricingRecord{row}))
	got, err := s.GetTier(ctx, "codex", "cache-model", "")
	require.NoError(t, err)
	require.Equal(t, 2.5, got.CacheWritePrice30m)
	require.Equal(t, 5.0, got.CacheWritePrice12h)
	require.Equal(t, 10.0, got.LongContextCacheWritePrice12h)
	row.CacheWritePrice30m, row.CacheWritePrice12h, row.LongContextCacheWritePrice12h = 3, 6, 12
	require.NoError(t, s.BatchUpsert(ctx, []*ModelPricingRecord{row}))
	got, err = s.GetTier(ctx, "codex", "cache-model", "")
	require.NoError(t, err)
	require.Equal(t, 3.0, got.CacheWritePrice30m)
	require.Equal(t, 6.0, got.CacheWritePrice12h)
	require.Equal(t, 12.0, got.LongContextCacheWritePrice12h)
}

func TestOfficialCatalogBackfillRollsBackPartialImport(t *testing.T) {
	s := newModelPricingTestStore(t)
	ctx := context.Background()
	_, err := s.db.Exec(`CREATE TRIGGER reject_bad_price BEFORE INSERT ON model_pricing WHEN NEW.model_name = 'bad' BEGIN SELECT RAISE(ABORT, 'reject test row'); END`)
	require.NoError(t, err)
	rows := []*ModelPricingRecord{{Provider: "codex", ModelName: "good", InputPrice: 1, OutputPrice: 2}, {Provider: "codex", ModelName: "bad", InputPrice: 1, OutputPrice: 2}}
	_, err = s.BackfillOfficialPrices(ctx, "rollback-test", rows)
	require.Error(t, err)
	good, err := s.GetTier(ctx, "codex", "good", "")
	require.NoError(t, err)
	require.Nil(t, good)
	_, err = s.db.Exec(`DROP TRIGGER reject_bad_price`)
	require.NoError(t, err)
	changed, err := s.BackfillOfficialPrices(ctx, "rollback-test", rows)
	require.NoError(t, err)
	require.Equal(t, 2, changed, "failed import must not mark the catalog applied")
}
