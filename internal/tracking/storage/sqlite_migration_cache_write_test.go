package storage

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCodexCacheWriteMigrationCopiesLegacyPriceOnlyOnce(t *testing.T) {
	db, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "legacy.db"))
	require.NoError(t, err)
	defer db.Close()
	_, err = db.Exec(`CREATE TABLE model_pricing(provider TEXT, cache_creation_price_5m REAL, cache_creation_price_1h REAL);
		INSERT INTO model_pricing VALUES ('codex', 3.75, 6), ('claude', 3.75, 6);`)
	require.NoError(t, err)
	s := &SQLiteAdapter{writeDB: db, readDB: db}
	ctx := context.Background()
	require.NoError(t, s.ensureSchemaVersionsTable(ctx))
	require.NoError(t, s.ensureCodexCacheWritePrices(ctx))
	var p30, p12, p1h float64
	require.NoError(t, db.QueryRow(`SELECT cache_write_price_30m,cache_write_price_12h,cache_creation_price_1h FROM model_pricing WHERE provider='codex'`).Scan(&p30, &p12, &p1h))
	require.Equal(t, 3.75, p30)
	require.Zero(t, p12, "Claude's legacy 1h is not Codex's 12h")
	require.Equal(t, 6.0, p1h)
	require.NoError(t, db.QueryRow(`SELECT cache_write_price_30m FROM model_pricing WHERE provider='claude'`).Scan(&p30))
	require.Zero(t, p30)
	_, err = db.Exec(`UPDATE model_pricing SET cache_write_price_30m = 0 WHERE provider='codex'`)
	require.NoError(t, err)
	require.NoError(t, s.ensureCodexCacheWritePrices(ctx))
	require.NoError(t, db.QueryRow(`SELECT cache_write_price_30m FROM model_pricing WHERE provider='codex'`).Scan(&p30))
	require.Zero(t, p30, "subsequent startup must respect intentionally cleared prices")
}
