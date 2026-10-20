package tracking

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	appcache "github.com/wsyangzy/Vane/internal/platform/cache"
)

const archivableRequestStatusSQL = `COALESCE(status, '') NOT IN ('pending', 'forwarding', 'streaming', 'processing', 'retry', 'suspended')`

type historyArchiveFile struct {
	month string
	path  string
}

type sqliteColumn struct {
	name     string
	typeName string
}

func (ut *UsageTracker) reconcileHistoryArchives() error {
	if ut == nil || ut.config == nil || ut.config.ArchiveDirectory == "" || ut.config.DatabasePath == ":memory:" {
		return nil
	}
	if err := os.MkdirAll(ut.config.ArchiveDirectory, 0755); err != nil {
		return fmt.Errorf("创建历史归档目录失败: %w", err)
	}

	ut.historyMu.Lock()
	defer ut.historyMu.Unlock()
	ut.writeMu.Lock()
	defer ut.writeMu.Unlock()
	// 启动对账会删除归档中的重复记录，结束时（defer 逆序，仍持有写锁）清空归档聚合
	defer ut.invalidateArchiveTotalsLocked()

	files, err := ut.historyArchiveFilesLocked()
	if err != nil {
		return err
	}
	for _, file := range files {
		requestCount := 0
		err := ut.withAttachedHistoryArchive(context.Background(), file.path, func(conn *sql.Conn) error {
			if err := purgeAttachedPrivacyPlaintext(context.Background(), conn); err != nil {
				return err
			}
			tx, err := conn.BeginTx(context.Background(), nil)
			if err != nil {
				return err
			}
			defer tx.Rollback()

			if _, err := tx.ExecContext(context.Background(), `
				DELETE FROM history_archive.request_logs
				WHERE request_id IN (SELECT request_id FROM main.request_logs)
				  AND COALESCE(environment, 'release') = ?
			`, ut.environment); err != nil {
				return fmt.Errorf("清理活动库重复记录失败: %w", err)
			}
			if err := rebuildAttachedUsageSummary(context.Background(), tx); err != nil {
				return err
			}
			if err := tx.Commit(); err != nil {
				return err
			}
			return conn.QueryRowContext(context.Background(), `SELECT COUNT(*) FROM history_archive.request_logs`).Scan(&requestCount)
		})
		if err != nil {
			return fmt.Errorf("恢复归档 %s 失败: %w", file.month, err)
		}
		if requestCount == 0 {
			if err := removeSQLiteFiles(file.path); err != nil {
				return fmt.Errorf("删除空归档 %s 失败: %w", file.month, err)
			}
		}
	}
	return nil
}

func (ut *UsageTracker) archiveClosedMonths(ctx context.Context) error {
	if ut == nil || ut.config == nil || ut.config.ArchiveDirectory == "" || ut.config.DatabasePath == ":memory:" {
		return nil
	}

	archived := false
	for {
		month, moved, err := ut.archiveOldestClosedMonth(ctx)
		if err != nil {
			return err
		}
		if !moved {
			break
		}
		archived = true
		slog.Info("请求历史月度归档完成", "month", month)
	}
	if archived {
		if err := ut.checkpointActiveDatabase(ctx); err != nil {
			return fmt.Errorf("月度归档后执行 WAL checkpoint 失败: %w", err)
		}
	}
	return nil
}

func (ut *UsageTracker) archiveOldestClosedMonth(ctx context.Context) (string, bool, error) {
	ut.historyMu.Lock()
	defer ut.historyMu.Unlock()
	ut.writeMu.Lock()
	defer ut.writeMu.Unlock()

	conn, err := ut.writeDB.Conn(ctx)
	if err != nil {
		return "", false, err
	}
	defer conn.Close()

	currentMonth := ut.now().Format("2006-01")
	var month string
	err = conn.QueryRowContext(ctx, fmt.Sprintf(`
		SELECT substr(start_time, 1, 7)
		FROM request_logs
		WHERE substr(start_time, 1, 7) < ?
		  AND COALESCE(environment, 'release') = ?
		  AND %s
		GROUP BY substr(start_time, 1, 7)
		ORDER BY substr(start_time, 1, 7) ASC
		LIMIT 1
	`, archivableRequestStatusSQL), currentMonth, ut.environment).Scan(&month)
	if err == sql.ErrNoRows {
		return "", false, nil
	}
	if err != nil {
		return "", false, fmt.Errorf("查找最早可归档月份失败: %w", err)
	}
	if _, err := time.Parse("2006-01", month); err != nil {
		return "", false, fmt.Errorf("数据库返回了无效归档月份 %q", month)
	}

	archivePath := filepath.Join(ut.config.ArchiveDirectory, month+".db")
	if err := os.MkdirAll(filepath.Dir(archivePath), 0755); err != nil {
		return "", false, fmt.Errorf("创建归档目录失败: %w", err)
	}

	if err := ut.attachHistoryArchive(ctx, conn, archivePath); err != nil {
		return "", false, fmt.Errorf("打开归档 %s 失败: %w", month, err)
	}
	defer func() { _, _ = conn.ExecContext(context.Background(), `DETACH DATABASE history_archive`) }()
	if err := ensureAttachedHistorySchema(ctx, conn); err != nil {
		return "", false, err
	}

	columns, err := sqliteTableColumns(ctx, conn, "main", "request_logs")
	if err != nil {
		return "", false, err
	}
	columnList := quotedColumnList(columns)

	tx, err := conn.BeginTx(ctx, nil)
	if err != nil {
		return "", false, err
	}
	defer tx.Rollback()
	copyQuery := fmt.Sprintf(`
		INSERT OR IGNORE INTO history_archive.request_logs (%s)
		SELECT %s FROM main.request_logs
		WHERE substr(start_time, 1, 7) = ?
		  AND COALESCE(environment, 'release') = ?
		  AND %s
	`, columnList, columnList, archivableRequestStatusSQL)
	if _, err := tx.ExecContext(ctx, copyQuery, month, ut.environment); err != nil {
		return "", false, fmt.Errorf("写入月度归档失败: %w", err)
	}
	if err := rebuildAttachedUsageSummary(ctx, tx); err != nil {
		return "", false, err
	}
	if err := tx.Commit(); err != nil {
		return "", false, fmt.Errorf("提交月度归档失败: %w", err)
	}

	var missing int
	verifyQuery := fmt.Sprintf(`
		SELECT COUNT(*)
		FROM main.request_logs AS active
		WHERE substr(active.start_time, 1, 7) = ?
		  AND COALESCE(active.environment, 'release') = ?
		  AND %s
		  AND NOT EXISTS (
			SELECT 1 FROM history_archive.request_logs AS archived
			WHERE archived.request_id = active.request_id
			  AND COALESCE(archived.environment, 'release') = ?
		  )
	`, archivableRequestStatusSQL)
	if err := conn.QueryRowContext(ctx, verifyQuery, month, ut.environment, ut.environment).Scan(&missing); err != nil {
		return "", false, fmt.Errorf("校验月度归档失败: %w", err)
	}
	if missing != 0 {
		return "", false, fmt.Errorf("归档 %s 校验失败，仍有 %d 条记录未写入", month, missing)
	}

	for {
		deleteQuery := fmt.Sprintf(`
			DELETE FROM main.request_logs WHERE id IN (
				SELECT id FROM main.request_logs
				WHERE substr(start_time, 1, 7) = ?
				  AND COALESCE(environment, 'release') = ?
				  AND %s
				LIMIT 1000
			)
		`, archivableRequestStatusSQL)
		result, err := conn.ExecContext(ctx, deleteQuery, month, ut.environment)
		if err != nil {
			return "", false, fmt.Errorf("清理已归档活动记录失败: %w", err)
		}
		deleted, err := result.RowsAffected()
		if err != nil {
			return "", false, err
		}
		if deleted == 0 {
			break
		}
	}
	if _, err := conn.ExecContext(ctx,
		`DELETE FROM main.usage_summary WHERE substr(date, 1, 7) = ? AND COALESCE(environment, 'release') = ?`,
		month, ut.environment); err != nil {
		return "", false, fmt.Errorf("清理已归档汇总失败: %w", err)
	}

	ut.resyncRequestCounter()
	// 记录从活动库搬到归档文件：总量不变，但归档聚合与按数据源拆分的结果都已过期
	ut.invalidateArchiveTotalsLocked()
	ut.notifyUsageDataChanged(appcache.Change{All: true})
	return month, true, nil
}

func (ut *UsageTracker) cleanupHistoryArchivesBefore(ctx context.Context, cutoff time.Time) error {
	files, err := ut.historyArchiveFilesLocked()
	if err != nil {
		return err
	}
	cutoffBound := ut.formatStartTimeQueryBound(cutoff)

	ut.writeMu.Lock()
	defer ut.writeMu.Unlock()
	for _, file := range files {
		requestCount := 0
		err := ut.withAttachedHistoryArchive(ctx, file.path, func(conn *sql.Conn) error {
			tx, err := conn.BeginTx(ctx, nil)
			if err != nil {
				return err
			}
			defer tx.Rollback()
			if _, err := tx.ExecContext(ctx,
				`DELETE FROM history_archive.request_logs WHERE start_time < ? AND COALESCE(environment, 'release') = ?`,
				cutoffBound, ut.environment); err != nil {
				return err
			}
			if err := rebuildAttachedUsageSummary(ctx, tx); err != nil {
				return err
			}
			if err := tx.Commit(); err != nil {
				return err
			}
			return conn.QueryRowContext(ctx, `SELECT COUNT(*) FROM history_archive.request_logs`).Scan(&requestCount)
		})
		if err != nil {
			return fmt.Errorf("清理归档 %s 失败: %w", file.month, err)
		}
		if requestCount == 0 {
			if err := removeSQLiteFiles(file.path); err != nil {
				return fmt.Errorf("删除空归档 %s 失败: %w", file.month, err)
			}
		}
	}
	return nil
}

func (ut *UsageTracker) checkpointActiveDatabase(ctx context.Context) error {
	ut.writeMu.Lock()
	defer ut.writeMu.Unlock()
	if _, err := ut.writeDB.ExecContext(ctx, `PRAGMA wal_checkpoint(TRUNCATE)`); err != nil {
		if _, passiveErr := ut.writeDB.ExecContext(ctx, `PRAGMA wal_checkpoint(PASSIVE)`); passiveErr != nil {
			return passiveErr
		}
	}
	return nil
}

func (ut *UsageTracker) resyncRequestCounter() {
	if ut.dbCounter == nil || ut.readDB == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := ut.dbCounter.Init(ctx, ut.readDB, ut.environment); err != nil {
		return
	}
}

func (ut *UsageTracker) historyArchiveFilesLocked() ([]historyArchiveFile, error) {
	entries, err := os.ReadDir(ut.config.ArchiveDirectory)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("读取历史归档目录失败: %w", err)
	}
	files := make([]historyArchiveFile, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".db" {
			continue
		}
		month := strings.TrimSuffix(entry.Name(), ".db")
		if _, err := time.Parse("2006-01", month); err != nil {
			continue
		}
		files = append(files, historyArchiveFile{month: month, path: filepath.Join(ut.config.ArchiveDirectory, entry.Name())})
	}
	sort.Slice(files, func(i, j int) bool { return files[i].month < files[j].month })
	return files, nil
}

func (ut *UsageTracker) withAttachedHistoryArchive(ctx context.Context, path string, fn func(*sql.Conn) error) error {
	conn, err := ut.writeDB.Conn(ctx)
	if err != nil {
		return err
	}
	defer conn.Close()
	if err := ut.attachHistoryArchive(ctx, conn, path); err != nil {
		return err
	}
	if err := ensureAttachedHistorySchema(ctx, conn); err != nil {
		_, _ = conn.ExecContext(context.Background(), `DETACH DATABASE history_archive`)
		return err
	}
	fnErr := fn(conn)
	_, detachErr := conn.ExecContext(context.Background(), `DETACH DATABASE history_archive`)
	if fnErr != nil {
		return fnErr
	}
	return detachErr
}

func (ut *UsageTracker) attachHistoryArchive(ctx context.Context, conn *sql.Conn, path string) error {
	_, err := conn.ExecContext(ctx, `ATTACH DATABASE ? AS history_archive`, path)
	return err
}

func ensureAttachedHistorySchema(ctx context.Context, conn *sql.Conn) error {
	for _, table := range []string{"request_logs", "usage_summary"} {
		var sourceSQL string
		if err := conn.QueryRowContext(ctx, `SELECT sql FROM main.sqlite_master WHERE type = 'table' AND name = ?`, table).Scan(&sourceSQL); err != nil {
			return fmt.Errorf("读取活动表 %s 定义失败: %w", table, err)
		}
		columnsStart := strings.Index(sourceSQL, "(")
		if columnsStart < 0 {
			return fmt.Errorf("活动表 %s 定义无效", table)
		}
		createSQL := fmt.Sprintf(`CREATE TABLE IF NOT EXISTS history_archive.%s %s`, quoteIdentifier(table), sourceSQL[columnsStart:])
		if _, err := conn.ExecContext(ctx, createSQL); err != nil {
			return fmt.Errorf("创建归档表 %s 失败: %w", table, err)
		}
		if err := syncAttachedTableColumns(ctx, conn, table); err != nil {
			return err
		}
	}
	statements := []string{
		`CREATE UNIQUE INDEX IF NOT EXISTS history_archive.idx_history_request_id ON request_logs(request_id)`,
		`CREATE INDEX IF NOT EXISTS history_archive.idx_history_request_start ON request_logs(start_time)`,
		`CREATE INDEX IF NOT EXISTS history_archive.idx_history_request_provider ON request_logs(provider)`,
		`CREATE INDEX IF NOT EXISTS history_archive.idx_history_request_environment_start ON request_logs(environment, start_time)`,
		`CREATE INDEX IF NOT EXISTS history_archive.idx_history_summary_date ON usage_summary(date)`,
	}
	for _, statement := range statements {
		if _, err := conn.ExecContext(ctx, statement); err != nil {
			return fmt.Errorf("创建归档索引失败: %w", err)
		}
	}
	return nil
}

// purgeAttachedPrivacyPlaintext 清除历史归档中遗留的 privacy_redaction_matches 明文。
//
// 活动库的同名列由 SQLiteAdapter.purgePrivacyPlaintextMatches 处理，但月度归档是
// 独立文件，归档表结构复制自当时的活动表——停止写入并不会让已归档的明文消失。
// 归档在启动时逐个 ATTACH 做一致性修复，这里顺带完成同一次止血。
func purgeAttachedPrivacyPlaintext(ctx context.Context, conn *sql.Conn) error {
	columns, err := sqliteTableColumns(ctx, conn, "history_archive", "request_logs")
	if err != nil {
		return err
	}
	found := false
	for _, column := range columns {
		if column.name == "privacy_redaction_matches" {
			found = true
			break
		}
	}
	if !found {
		return nil
	}
	if _, err := conn.ExecContext(ctx,
		`UPDATE history_archive.request_logs SET privacy_redaction_matches = NULL
		 WHERE privacy_redaction_matches IS NOT NULL`); err != nil {
		return fmt.Errorf("清除归档隐私明文失败: %w", err)
	}
	if _, err := conn.ExecContext(ctx,
		`ALTER TABLE history_archive.request_logs DROP COLUMN privacy_redaction_matches`); err != nil {
		// 旧版 SQLite 不支持 DROP COLUMN。内容已置空，留列不影响隐私。
		slog.Warn("归档隐私明文列内容已清空，但当前 SQLite 不支持删除该列", "error", err)
	}
	return nil
}

func syncAttachedTableColumns(ctx context.Context, conn *sql.Conn, table string) error {
	mainColumns, err := sqliteTableColumns(ctx, conn, "main", table)
	if err != nil {
		return err
	}
	archiveColumns, err := sqliteTableColumns(ctx, conn, "history_archive", table)
	if err != nil {
		return err
	}
	existing := make(map[string]struct{}, len(archiveColumns))
	for _, column := range archiveColumns {
		existing[column.name] = struct{}{}
	}
	for _, column := range mainColumns {
		if _, ok := existing[column.name]; ok {
			continue
		}
		statement := fmt.Sprintf(`ALTER TABLE history_archive.%s ADD COLUMN %s %s`, quoteIdentifier(table), quoteIdentifier(column.name), column.typeName)
		if _, err := conn.ExecContext(ctx, statement); err != nil {
			return fmt.Errorf("同步归档字段 %s.%s 失败: %w", table, column.name, err)
		}
	}
	return nil
}

func sqliteTableColumns(ctx context.Context, conn *sql.Conn, schema, table string) ([]sqliteColumn, error) {
	rows, err := conn.QueryContext(ctx, fmt.Sprintf(`PRAGMA %s.table_info(%s)`, schema, quoteIdentifier(table)))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var columns []sqliteColumn
	for rows.Next() {
		var cid, notNull, primaryKey int
		var name, typeName string
		var defaultValue sql.NullString
		if err := rows.Scan(&cid, &name, &typeName, &notNull, &defaultValue, &primaryKey); err != nil {
			return nil, err
		}
		if typeName == "" {
			typeName = "BLOB"
		}
		columns = append(columns, sqliteColumn{name: name, typeName: typeName})
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(columns) == 0 {
		return nil, fmt.Errorf("表 %s.%s 没有可用字段", schema, table)
	}
	return columns, nil
}

func quotedColumnList(columns []sqliteColumn) string {
	names := make([]string, 0, len(columns))
	for _, column := range columns {
		names = append(names, quoteIdentifier(column.name))
	}
	return strings.Join(names, ", ")
}

func quoteIdentifier(value string) string {
	return `"` + strings.ReplaceAll(value, `"`, `""`) + `"`
}

type historyExecer interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
}

func rebuildAttachedUsageSummary(ctx context.Context, execer historyExecer) error {
	if _, err := execer.ExecContext(ctx, `DELETE FROM history_archive.usage_summary`); err != nil {
		return fmt.Errorf("重建归档汇总前清理失败: %w", err)
	}

	query := fmt.Sprintf(`
		INSERT INTO history_archive.usage_summary (
			date, model_name, endpoint_name, group_name, provider, environment,
			request_count, success_count, error_count,
			total_input_tokens, total_output_tokens,
			total_cache_creation_tokens, total_cache_read_tokens,
			total_cost_usd, avg_duration_ms, created_at, updated_at
		)
		SELECT
			substr(start_time, 1, 10),
			COALESCE(model_name, ''), COALESCE(endpoint_name, ''), group_name,
			COALESCE(provider, 'claude'),
			COALESCE(environment, 'release'),
			COUNT(*),
			SUM(CASE WHEN status = 'completed' THEN 1 ELSE 0 END),
			SUM(CASE WHEN status IN ('failed', 'cancelled') THEN 1 ELSE 0 END),
			COALESCE(SUM(input_tokens), 0), COALESCE(SUM(output_tokens), 0),
			COALESCE(SUM(cache_creation_tokens), 0), COALESCE(SUM(cache_read_tokens), 0),
			COALESCE(SUM(total_cost_usd), 0), COALESCE(AVG(duration_ms), 0),
			CURRENT_TIMESTAMP, CURRENT_TIMESTAMP
		FROM history_archive.request_logs
		GROUP BY substr(start_time, 1, 10), model_name, endpoint_name, group_name, provider, environment
	`)
	if _, err := execer.ExecContext(ctx, query); err != nil {
		return fmt.Errorf("重建归档汇总失败: %w", err)
	}
	return nil
}

func removeSQLiteFiles(path string) error {
	for _, candidate := range []string{path, path + "-wal", path + "-shm"} {
		if err := os.Remove(candidate); err != nil && !os.IsNotExist(err) {
			return err
		}
	}
	return nil
}
