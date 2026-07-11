package store

import (
	"context"
	"database/sql"
	"errors"
	"testing"
)

func TestExecWithSQLiteBusyRetryRetriesTransientLock(t *testing.T) {
	attempts := 0
	_, err := execWithSQLiteBusyRetry(context.Background(), func() (sql.Result, error) {
		attempts++
		if attempts < 3 {
			return nil, errors.New("database is locked (5) (SQLITE_BUSY)")
		}
		return nil, nil
	})

	if err != nil {
		t.Fatalf("execWithSQLiteBusyRetry returned error: %v", err)
	}
	if attempts != 3 {
		t.Fatalf("attempts = %d, want 3", attempts)
	}
}

func TestExecWithSQLiteBusyRetryDoesNotRetryOtherErrors(t *testing.T) {
	attempts := 0
	wantErr := errors.New("invalid endpoint")
	_, err := execWithSQLiteBusyRetry(context.Background(), func() (sql.Result, error) {
		attempts++
		return nil, wantErr
	})

	if !errors.Is(err, wantErr) {
		t.Fatalf("error = %v, want %v", err, wantErr)
	}
	if attempts != 1 {
		t.Fatalf("attempts = %d, want 1", attempts)
	}
}
