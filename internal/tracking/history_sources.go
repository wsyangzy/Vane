package tracking

import (
	"context"
	"database/sql"
	"fmt"
	"time"
)

type historyQuerySource struct {
	label string
	db    *sql.DB
}

func (ut *UsageTracker) forEachHistorySource(
	ctx context.Context,
	startTime, endTime *time.Time,
	query func(historyQuerySource) error,
) error {
	if ut == nil || ut.readDB == nil {
		return fmt.Errorf("read database not initialized")
	}

	ut.historyMu.RLock()
	defer ut.historyMu.RUnlock()
	if err := query(historyQuerySource{label: "活动数据库", db: ut.readDB}); err != nil {
		return fmt.Errorf("活动数据库查询失败: %w", err)
	}
	files, err := ut.intersectingArchiveFilesLocked(startTime, endTime)
	if err != nil {
		return err
	}
	for _, file := range files {
		if err := ut.queryHistoryArchiveFile(ctx, file, query); err != nil {
			return err
		}
	}
	return nil
}

// intersectingArchiveFilesLocked 返回与时间范围相交的月度归档文件（新月份在前），调用方须持有 historyMu。
func (ut *UsageTracker) intersectingArchiveFilesLocked(startTime, endTime *time.Time) ([]historyArchiveFile, error) {
	if ut.config == nil || ut.config.ArchiveDirectory == "" {
		return nil, nil
	}
	files, err := ut.historyArchiveFilesLocked()
	if err != nil {
		return nil, err
	}
	result := make([]historyArchiveFile, 0, len(files))
	for i := len(files) - 1; i >= 0; i-- {
		if ut.archiveMonthIntersects(files[i].month, startTime, endTime) {
			result = append(result, files[i])
		}
	}
	return result, nil
}

func (ut *UsageTracker) queryHistoryArchiveFile(ctx context.Context, file historyArchiveFile, query func(historyQuerySource) error) error {
	db, err := sql.Open("sqlite", file.path+"?mode=ro&_busy_timeout=5000")
	if err != nil {
		return fmt.Errorf("打开归档 %s 失败: %w", file.month, err)
	}
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	if err := db.PingContext(ctx); err != nil {
		_ = db.Close()
		return fmt.Errorf("打开归档 %s 失败: %w", file.month, err)
	}
	var tables int
	if err := db.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM sqlite_master
		WHERE type = 'table' AND name IN ('request_logs', 'usage_summary')
	`).Scan(&tables); err != nil || tables != 2 {
		_ = db.Close()
		if err == nil {
			err = fmt.Errorf("缺少请求或汇总表")
		}
		return fmt.Errorf("归档 %s 损坏: %w", file.month, err)
	}
	queryErr := query(historyQuerySource{label: file.month, db: db})
	closeErr := db.Close()
	if queryErr != nil {
		return fmt.Errorf("归档 %s 查询失败: %w", file.month, queryErr)
	}
	if closeErr != nil {
		return fmt.Errorf("关闭归档 %s 失败: %w", file.month, closeErr)
	}
	return nil
}

// archiveMonthBounds 返回归档月份的 [起, 止) 区间。
func (ut *UsageTracker) archiveMonthBounds(month string) (time.Time, time.Time, error) {
	location := ut.location
	if location == nil {
		location = time.Local
	}
	monthStart, err := time.ParseInLocation("2006-01", month, location)
	if err != nil {
		return time.Time{}, time.Time{}, err
	}
	return monthStart, monthStart.AddDate(0, 1, 0), nil
}

func (ut *UsageTracker) archiveMonthIntersects(month string, startTime, endTime *time.Time) bool {
	monthStart, monthEnd, err := ut.archiveMonthBounds(month)
	if err != nil {
		return false
	}
	if startTime != nil && !monthEnd.After(*startTime) {
		return false
	}
	if endTime != nil && monthStart.After(*endTime) {
		return false
	}
	return true
}

func (ut *UsageTracker) hasHistoryArchives() (bool, error) {
	if ut == nil || ut.config == nil || ut.config.ArchiveDirectory == "" {
		return false, nil
	}
	ut.historyMu.RLock()
	defer ut.historyMu.RUnlock()
	files, err := ut.historyArchiveFilesLocked()
	return len(files) > 0, err
}
