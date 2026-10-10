package tracking

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"modernc.org/sqlite"
)

func newSQLiteConnectionTestAdapter(t *testing.T) *SQLiteAdapter {
	t.Helper()
	adapter, err := NewSQLiteAdapter(DatabaseConfig{DatabasePath: filepath.Join(t.TempDir(), "usage.db")})
	require.NoError(t, err)
	require.NoError(t, adapter.Open())
	t.Cleanup(func() { require.NoError(t, adapter.Close()) })
	return adapter
}

func TestSQLiteAdapter_ConnectionPragmasSurvivePoolGrowthAndReconnect(t *testing.T) {
	adapter := newSQLiteConnectionTestAdapter(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	for _, pool := range []struct {
		name string
		db   *sql.DB
		size int
	}{
		{"write", adapter.GetWriteDB(), 1},
		{"read", adapter.GetReadDB(), 4},
	} {
		t.Run(pool.name, func(t *testing.T) {
			for generation := 0; generation < 2; generation++ {
				conns := make([]*sql.Conn, 0, pool.size)
				for i := 0; i < pool.size; i++ {
					conn, err := pool.db.Conn(ctx)
					require.NoError(t, err)
					conns = append(conns, conn)
					t.Cleanup(func() { _ = conn.Close() })

					var journalMode string
					require.NoError(t, conn.QueryRowContext(ctx, "PRAGMA journal_mode").Scan(&journalMode))
					require.Equal(t, "wal", journalMode)
					var busyTimeout, synchronous, cacheSize int
					require.NoError(t, conn.QueryRowContext(ctx, "PRAGMA busy_timeout").Scan(&busyTimeout))
					require.Equal(t, 5000, busyTimeout)
					require.NoError(t, conn.QueryRowContext(ctx, "PRAGMA synchronous").Scan(&synchronous))
					require.Equal(t, 1, synchronous)
					require.NoError(t, conn.QueryRowContext(ctx, "PRAGMA cache_size").Scan(&cacheSize))
					require.Equal(t, 10000, cacheSize)
				}
				for _, conn := range conns {
					require.NoError(t, conn.Close())
				}
				pool.db.SetMaxIdleConns(0)
				pool.db.SetMaxIdleConns(pool.size)
			}
		})
	}
}

func TestSQLiteAdapter_ReaderDoesNotBlockWriteCommit(t *testing.T) {
	adapter := newSQLiteConnectionTestAdapter(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, err := adapter.GetWriteDB().ExecContext(ctx, "CREATE TABLE probe (value INTEGER); INSERT INTO probe VALUES (0)")
	require.NoError(t, err)

	reader, err := adapter.GetReadDB().BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	require.NoError(t, err)
	defer reader.Rollback()
	var value int
	require.NoError(t, reader.QueryRowContext(ctx, "SELECT value FROM probe").Scan(&value))

	writer, err := adapter.GetWriteDB().BeginTx(ctx, nil)
	require.NoError(t, err)
	defer writer.Rollback()
	_, err = writer.ExecContext(ctx, "UPDATE probe SET value = 1")
	require.NoError(t, err)
	require.NoError(t, writer.Commit(), "a request-list reader must not block a configuration write")

	require.NoError(t, reader.QueryRowContext(ctx, "SELECT value FROM probe").Scan(&value))
	require.Zero(t, value, "the existing reader retains its snapshot")
	require.NoError(t, reader.Rollback())
	require.NoError(t, adapter.GetReadDB().QueryRowContext(ctx, "SELECT value FROM probe").Scan(&value))
	require.Equal(t, 1, value)
}

// Use the application's actual driver with a deliberately contended rollback
// journal. WAL lowers contention, but a failed transaction must still be safe.
func TestSQLiteAdapter_FailedCommitDoesNotPoisonSharedConnection(t *testing.T) {
	adapter := newSQLiteConnectionTestAdapter(t)
	connector := sqliteTestConnector{
		driver: adapter.GetWriteDB().Driver(),
		dsn:    filepath.Join(t.TempDir(), "contended.db"),
	}
	writerDB := sql.OpenDB(connector)
	readerDB := sql.OpenDB(connector)
	writerDB.SetMaxOpenConns(1)
	readerDB.SetMaxOpenConns(1)
	defer writerDB.Close()
	defer readerDB.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	_, err := writerDB.ExecContext(ctx, "CREATE TABLE probe (value INTEGER); INSERT INTO probe VALUES (0)")
	require.NoError(t, err)
	reader, err := readerDB.BeginTx(ctx, nil)
	require.NoError(t, err)
	defer reader.Rollback()
	var value int
	require.NoError(t, reader.QueryRowContext(ctx, "SELECT value FROM probe").Scan(&value))

	writer, err := writerDB.BeginTx(ctx, nil)
	require.NoError(t, err)
	_, err = writer.ExecContext(ctx, "UPDATE probe SET value = 1")
	require.NoError(t, err)
	err = writer.Commit()
	var sqliteErr *sqlite.Error
	require.True(t, errors.As(err, &sqliteErr), "preserve the driver's original error: %v", err)
	require.Equal(t, 5, sqliteErr.Code(), "force SQLITE_BUSY at commit")
	require.ErrorIs(t, writer.Rollback(), sql.ErrTxDone)
	require.NoError(t, reader.Rollback())

	require.NoError(t, readerDB.QueryRowContext(ctx, "SELECT value FROM probe").Scan(&value), "failed commit must release read locks")
	require.Zero(t, value, "failed transaction must not leak its changes")
	next, err := writerDB.BeginTx(ctx, nil)
	require.NoError(t, err, "the next endpoint or tracking transaction must start normally")
	defer next.Rollback()
	_, err = next.ExecContext(ctx, "UPDATE probe SET value = 2")
	require.NoError(t, err)
	require.NoError(t, next.Commit())
	require.NoError(t, readerDB.QueryRowContext(ctx, "SELECT value FROM probe").Scan(&value))
	require.Equal(t, 2, value)
}

type sqliteTestConnector struct {
	driver driver.Driver
	dsn    string
}

func (c sqliteTestConnector) Connect(context.Context) (driver.Conn, error) {
	return c.driver.Open(c.dsn)
}

func (c sqliteTestConnector) Driver() driver.Driver { return c.driver }
