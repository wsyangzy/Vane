package storage

import "context"

// Keep the old Claude columns intact. Only the newly introduced Codex 30m
// column inherits the relay's previous generic cache-write price, once.
func (s *SQLiteAdapter) ensureCodexCacheWritePrices(ctx context.Context) error {
	exists, err := s.tableExists(ctx, "model_pricing")
	if err != nil || !exists {
		return err
	}
	for _, name := range []string{"cache_write_price_30m", "cache_write_price_12h", "long_context_cache_write_price_12h"} {
		exists, err := s.columnExists(ctx, "model_pricing", name)
		if err != nil {
			return err
		}
		if !exists {
			if _, err := s.GetWriteDB().ExecContext(ctx, "ALTER TABLE model_pricing ADD COLUMN "+name+" REAL NOT NULL DEFAULT 0"); err != nil {
				return err
			}
		}
	}
	const version = "20261030_codex_cache_write_30m"
	done, err := s.isSchemaVersionApplied(ctx, version)
	if err != nil || done {
		return err
	}
	tx, err := s.GetWriteDB().BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	_, err = tx.ExecContext(ctx, `
		UPDATE model_pricing SET cache_write_price_30m = cache_creation_price_5m
		WHERE provider = 'codex' AND cache_write_price_30m = 0;
		INSERT INTO schema_versions(version, description) VALUES ('20261030_codex_cache_write_30m', 'Codex 30m cache write price');`)
	if err != nil {
		return err
	}
	return tx.Commit()
}
