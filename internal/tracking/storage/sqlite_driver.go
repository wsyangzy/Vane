package storage

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"fmt"

	"modernc.org/sqlite"
)

const sqliteDriverName = "relay-sqlite"

func init() {
	sql.Register(sqliteDriverName, &guardedSQLiteDriver{Driver: &sqlite.Driver{}})
}

// Keep modernc's context-aware execution and session checks when wrapping it.
type sqliteDriverConnection interface {
	driver.Conn
	driver.ConnBeginTx
	driver.ConnPrepareContext
	driver.ExecerContext
	driver.QueryerContext
	driver.Pinger
	driver.SessionResetter
	driver.Validator
}

type guardedSQLiteDriver struct {
	driver.Driver
}

func (d *guardedSQLiteDriver) Open(dsn string) (driver.Conn, error) {
	conn, err := d.Driver.Open(dsn)
	if err != nil {
		return nil, err
	}
	sqliteConn, ok := conn.(sqliteDriverConnection)
	if !ok {
		_ = conn.Close()
		return nil, fmt.Errorf("SQLite driver is missing required connection interfaces")
	}
	return &guardedSQLiteConn{sqliteDriverConnection: sqliteConn}, nil
}

type guardedSQLiteConn struct {
	sqliteDriverConnection
	invalid bool // database/sql serializes access to each driver connection.
}

func (c *guardedSQLiteConn) Begin() (driver.Tx, error) {
	return c.BeginTx(context.Background(), driver.TxOptions{})
}

func (c *guardedSQLiteConn) BeginTx(ctx context.Context, opts driver.TxOptions) (driver.Tx, error) {
	tx, err := c.sqliteDriverConnection.BeginTx(ctx, opts)
	if err != nil {
		c.invalid = true
		return nil, err
	}
	return &guardedSQLiteTx{Tx: tx, conn: c}, nil
}

func (c *guardedSQLiteConn) IsValid() bool {
	return !c.invalid && c.sqliteDriverConnection.IsValid()
}

func (c *guardedSQLiteConn) ResetSession(ctx context.Context) error {
	if c.invalid {
		return driver.ErrBadConn
	}
	return c.sqliteDriverConnection.ResetSession(ctx)
}

type guardedSQLiteTx struct {
	driver.Tx
	conn *guardedSQLiteConn
}

func (t *guardedSQLiteTx) Commit() error {
	err := t.Tx.Commit()
	if err != nil {
		// sql.Tx is already finished after a failed Commit, so a later
		// sql.Tx.Rollback cannot release SQLite's still-open transaction.
		// Roll back the driver transaction before returning the connection,
		// and discard it even if cleanup fails. This also covers pinned Conn.
		t.conn.invalid = true
		_ = t.Tx.Rollback()
	}
	// Preserve the original error; ErrBadConn could cause an unsafe replay.
	return err
}

func (t *guardedSQLiteTx) Rollback() error {
	err := t.Tx.Rollback()
	if err != nil {
		t.conn.invalid = true
	}
	return err
}
