package store

import (
	"context"
	"database/sql"
	"strings"
	"time"
)

const sqliteBusyMaxAttempts = 6

func execWithSQLiteBusyRetry(ctx context.Context, execFn func() (sql.Result, error)) (sql.Result, error) {
	backoff := 10 * time.Millisecond
	for attempt := 0; ; attempt++ {
		result, err := execFn()
		if err == nil || !isSQLiteBusyError(err) || attempt+1 >= sqliteBusyMaxAttempts {
			return result, err
		}

		timer := time.NewTimer(backoff)
		if ctx == nil {
			<-timer.C
		} else {
			select {
			case <-ctx.Done():
				timer.Stop()
				return nil, err
			case <-timer.C:
			}
		}
		if backoff < 160*time.Millisecond {
			backoff *= 2
		}
	}
}

func isSQLiteBusyError(err error) bool {
	if err == nil {
		return false
	}
	message := strings.ToLower(err.Error())
	return strings.Contains(message, "sqlite_busy") || strings.Contains(message, "database is locked")
}
