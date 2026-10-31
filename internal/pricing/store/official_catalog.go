package store

import (
	"context"
	"fmt"
)

// A dated catalog is applied once, atomically. User prices and disabled tiers
// stay under user control after this import; restarting cannot recreate them.
func (s *SQLiteModelPricingStore) BackfillOfficialPrices(ctx context.Context, version string, records []*ModelPricingRecord) (int, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS pricing_catalog_versions (version TEXT PRIMARY KEY, applied_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP)`); err != nil {
		return 0, err
	}
	var applied int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM pricing_catalog_versions WHERE version = ?`, version).Scan(&applied); err != nil {
		return 0, err
	}
	if applied > 0 {
		return 0, nil
	}
	st := s.WithTx(tx)
	changed := 0
	for _, row := range records {
		existing, err := st.GetTier(ctx, row.Provider, row.ModelName, row.ServiceTier)
		if err != nil {
			return 0, err
		}
		if existing == nil {
			copy := *row
			if _, err := st.Create(ctx, &copy); err != nil {
				return 0, err
			}
			changed++
			continue
		}
		updated := false
		for _, pair := range []struct {
			target   *float64
			official float64
		}{
			{&existing.CacheWritePrice30m, row.CacheWritePrice30m},
			{&existing.CacheWritePrice12h, row.CacheWritePrice12h},
			{&existing.LongContextCacheWritePrice12h, row.LongContextCacheWritePrice12h},
		} {
			if *pair.target == 0 && pair.official > 0 {
				*pair.target = pair.official
				updated = true
			}
		}
		if updated {
			if err := st.Update(ctx, existing); err != nil {
				return 0, err
			}
			changed++
		}
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO pricing_catalog_versions(version) VALUES (?)`, version); err != nil {
		return 0, err
	}
	if err := tx.Commit(); err != nil {
		return 0, fmt.Errorf("补齐官方价格失败: %w", err)
	}
	return changed, nil
}
