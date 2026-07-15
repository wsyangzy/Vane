package main

import (
	"context"
	"database/sql"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	"ai-relay-forwarder/internal/tracking"
	"ai-relay-forwarder/internal/utils"
)

// migrateLegacyDatabaseIfNeeded 将旧的环境专用数据库迁移到统一的 usage.db。
func (a *App) migrateLegacyDatabaseIfNeeded() {
	newPath := a.getEffectiveUsageDBPath()
	if newPath == "" {
		return
	}

	defaultNew := filepath.Join(utils.GetDataDir(), "usage.db")
	if filepath.Clean(newPath) != filepath.Clean(defaultNew) {
		return
	}

	legacyCandidates := []string{
		filepath.Join(utils.GetDataDir(), "usage-dev.db"),
		filepath.Join(utils.GetDataDir(), "usage-v2.db"),
	}

	// 兼容旧版本/调试环境可能使用的相对路径（例如安装目录/data/*.db 或运行目录/data/*.db）
	if exe, err := os.Executable(); err == nil {
		exeDir := filepath.Dir(exe)
		legacyCandidates = append(legacyCandidates,
			filepath.Join(exeDir, "data", "usage-dev.db"),
			filepath.Join(exeDir, "data", "usage-v2.db"),
		)
	}
	if cwd, err := os.Getwd(); err == nil {
		legacyCandidates = append(legacyCandidates,
			filepath.Join(cwd, "data", "usage-dev.db"),
			filepath.Join(cwd, "data", "usage-v2.db"),
		)
	}

	existingLegacy := make([]string, 0, len(legacyCandidates))
	seen := make(map[string]struct{}, len(legacyCandidates))
	for _, p := range legacyCandidates {
		if p == "" {
			continue
		}
		cp := filepath.Clean(p)
		if cp == filepath.Clean(defaultNew) {
			continue
		}
		if _, ok := seen[cp]; ok {
			continue
		}
		if _, err := os.Stat(cp); err != nil {
			continue
		}
		seen[cp] = struct{}{}
		existingLegacy = append(existingLegacy, cp)
	}
	if len(existingLegacy) == 0 {
		return
	}

	if _, err := os.Stat(defaultNew); err != nil {
		bestLegacy := existingLegacy[0]
		bestReq := int64(-1)
		bestCh := int64(-1)
		for _, p := range existingLegacy {
			reqCount, _ := countTableRows(p, "request_logs")
			chCount, _ := countTableRows(p, "channels")
			if reqCount > bestReq || (reqCount == bestReq && chCount > bestCh) {
				bestLegacy = p
				bestReq = reqCount
				bestCh = chCount
			}
		}

		if err := copyFile(bestLegacy, defaultNew); err != nil {
			a.logger.Warn("⚠️ 旧数据迁移失败，将使用新的空数据库（可在关闭旧版本后重启再迁移）",
				"old", bestLegacy, "new", defaultNew, "error", err)
			a.emitNotification("warning", "旧数据迁移失败", "无法复制旧数据库（可能被旧版本占用），将使用新的空数据库；关闭旧版本后重启可再次尝试迁移。")
			return
		}

		// 拷贝后确保 schema 完整（补表/补列/迁移），并从其他来源合并缺失数据。
		if err := ensureSchemaForDB(defaultNew, a.config.Timezone); err != nil {
			a.logger.Warn("⚠️ 旧数据迁移后补齐数据库 Schema 失败", "new", defaultNew, "error", err)
		}
		for _, p := range existingLegacy {
			if filepath.Clean(p) == filepath.Clean(bestLegacy) {
				continue
			}
			if err := importLegacyTables(defaultNew, p); err != nil {
				a.logger.Warn("⚠️ 合并旧数据库数据失败", "old", p, "new", defaultNew, "error", err)
			}
		}

		a.logger.Info("✅ 旧数据迁移完成", "old", bestLegacy, "new", defaultNew)
		a.emitNotification("success", "旧数据已迁移", "已将旧的环境专用数据库迁移到统一的 usage.db。")
		return
	}

	if err := ensureSchemaForDB(defaultNew, a.config.Timezone); err != nil {
		a.logger.Warn("⚠️ 导入旧数据前补齐数据库 Schema 失败", "new", defaultNew, "error", err)
	}

	newReqCount, _ := countTableRows(defaultNew, "request_logs")
	newChCount, _ := countTableRows(defaultNew, "channels")
	newEpCount, _ := countTableRows(defaultNew, "endpoints")

	needImport := newReqCount == 0 || newChCount == 0 || newEpCount == 0
	if !needImport {
		return
	}

	importedAny := false
	for _, p := range existingLegacy {
		oldReqCount, _ := countTableRows(p, "request_logs")
		oldChCount, _ := countTableRows(p, "channels")
		oldEpCount, _ := countTableRows(p, "endpoints")
		if (newReqCount == 0 && oldReqCount > 0) || (newChCount == 0 && oldChCount > 0) || (newEpCount == 0 && oldEpCount > 0) {
			if err := importLegacyTables(defaultNew, p); err != nil {
				a.logger.Warn("⚠️ 旧数据导入失败", "old", p, "new", defaultNew, "error", err)
				continue
			}
			importedAny = true
		}
	}
	if importedAny {
		a.logger.Info("✅ 已导入旧数据", "new", defaultNew)
		a.emitNotification("success", "旧数据已导入", "已从旧数据库合并导入历史数据到当前数据库（不会覆盖现有配置）。")
	}
}

func copyFile(src, dst string) (err error) {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer func() {
		if closeErr := in.Close(); closeErr != nil && err == nil {
			err = fmt.Errorf("close source file: %w", closeErr)
		}
	}()

	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0644)
	if err != nil {
		return err
	}
	defer func() {
		if closeErr := out.Close(); closeErr != nil && err == nil {
			err = fmt.Errorf("close destination file: %w", closeErr)
		}
	}()

	if _, err = io.Copy(out, in); err != nil {
		return err
	}
	if err = out.Sync(); err != nil {
		return err
	}
	return nil
}

func countTableRows(dbPath, table string) (count int64, err error) {
	if dbPath == "" || table == "" {
		return 0, nil
	}
	dsn := dbPath + "?_busy_timeout=2000"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return 0, err
	}
	defer func() {
		if closeErr := db.Close(); closeErr != nil && err == nil {
			err = fmt.Errorf("close row count database: %w", closeErr)
		}
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	// table 名仅允许字母数字下划线，避免拼接 SQL 注入
	for _, r := range table {
		if !(r == '_' || (r >= '0' && r <= '9') || (r >= 'A' && r <= 'Z') || (r >= 'a' && r <= 'z')) {
			return 0, fmt.Errorf("invalid table name: %s", table)
		}
	}

	row := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM "+table)
	if err := row.Scan(&count); err != nil {
		// 例如表不存在：当作 0
		return 0, nil
	}
	return count, nil
}

func ensureSchemaForDB(dbPath, timezone string) (err error) {
	if dbPath == "" {
		return nil
	}
	adapter, err := tracking.NewDatabaseAdapter(tracking.DatabaseConfig{
		Type:         "sqlite",
		DatabasePath: dbPath,
		Timezone:     timezone,
	})
	if err != nil {
		return err
	}
	if err := adapter.Open(); err != nil {
		return err
	}
	defer func() {
		if closeErr := adapter.Close(); closeErr != nil && err == nil {
			err = fmt.Errorf("close database adapter: %w", closeErr)
		}
	}()
	return adapter.InitSchema()
}

func importLegacyTables(dstDBPath, legacyDBPath string) (err error) {
	if dstDBPath == "" || legacyDBPath == "" {
		return nil
	}

	dsn := dstDBPath + "?_journal_mode=WAL&_synchronous=NORMAL&_foreign_keys=1&_busy_timeout=10000"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return err
	}
	defer func() {
		if closeErr := db.Close(); closeErr != nil {
			if err == nil {
				err = fmt.Errorf("close legacy import database: %w", closeErr)
			} else {
				slog.Debug("Close legacy import database failed", "legacy", legacyDBPath, "error", closeErr)
			}
		}
	}()
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	legacyEscaped := strings.ReplaceAll(legacyDBPath, "'", "''")
	if _, err := db.ExecContext(ctx, "ATTACH DATABASE '"+legacyEscaped+"' AS legacy"); err != nil {
		return fmt.Errorf("attach legacy database failed: %w", err)
	}
	defer func() {
		if _, detachErr := db.ExecContext(context.Background(), "DETACH DATABASE legacy"); detachErr != nil {
			if err == nil {
				err = fmt.Errorf("detach legacy database failed: %w", detachErr)
			} else {
				slog.Debug("DETACH legacy database failed", "legacy", legacyDBPath, "error", detachErr)
			}
		}
	}()

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	committed := false
	defer func() {
		if !committed {
			if rbErr := tx.Rollback(); rbErr != nil && rbErr != sql.ErrTxDone {
				if err == nil {
					err = fmt.Errorf("rollback legacy import transaction failed: %w", rbErr)
				} else {
					slog.Debug("Rollback legacy import transaction failed", "legacy", legacyDBPath, "error", rbErr)
				}
			}
		}
	}()

	// 导入历史数据（INSERT OR IGNORE），不覆盖新库中已存在的记录
	for _, table := range []string{"request_logs", "usage_summary", "endpoints", "channels", "model_pricing", "settings", "codex_providers"} {
		cols, err := commonTableColumns(ctx, tx, table)
		if err != nil {
			continue
		}
		cols = filterOut(cols, "id")
		if len(cols) == 0 {
			continue
		}

		colList := make([]string, 0, len(cols))
		for _, c := range cols {
			colList = append(colList, `"`+c+`"`)
		}
		colCSV := strings.Join(colList, ", ")

		sqlStmt := fmt.Sprintf(
			"INSERT OR IGNORE INTO %s (%s) SELECT %s FROM legacy.%s",
			table, colCSV, colCSV, table,
		)
		if _, err := tx.ExecContext(ctx, sqlStmt); err != nil {
			continue
		}
	}

	if err = tx.Commit(); err != nil {
		return err
	}
	committed = true
	return nil
}

func commonTableColumns(ctx context.Context, q interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
}, table string) ([]string, error) {
	mainCols, err := tableColumns(ctx, q, "main", table)
	if err != nil {
		return nil, err
	}
	legacyCols, err := tableColumns(ctx, q, "legacy", table)
	if err != nil {
		return nil, err
	}

	legacySet := make(map[string]struct{}, len(legacyCols))
	for _, c := range legacyCols {
		legacySet[c] = struct{}{}
	}
	common := make([]string, 0, len(mainCols))
	for _, c := range mainCols {
		if _, ok := legacySet[c]; ok {
			common = append(common, c)
		}
	}
	return common, nil
}

func tableColumns(ctx context.Context, q interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
}, schema, table string) (cols []string, err error) {
	rows, err := q.QueryContext(ctx, "PRAGMA "+schema+".table_info("+table+")")
	if err != nil {
		return nil, err
	}
	defer func() {
		if closeErr := rows.Close(); closeErr != nil && err == nil {
			err = fmt.Errorf("close table columns rows: %w", closeErr)
		}
	}()

	for rows.Next() {
		var cid int
		var name string
		var typ string
		var notnull int
		var dflt sql.NullString
		var pk int
		if err := rows.Scan(&cid, &name, &typ, &notnull, &dflt, &pk); err != nil {
			continue
		}
		if name != "" {
			cols = append(cols, name)
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return cols, nil
}

func filterOut(cols []string, disallow string) []string {
	if disallow == "" {
		return cols
	}
	out := make([]string, 0, len(cols))
	for _, c := range cols {
		if c == disallow {
			continue
		}
		out = append(out, c)
	}
	return out
}
