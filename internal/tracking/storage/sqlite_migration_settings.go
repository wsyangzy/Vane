package storage

import (
	"context"
	"fmt"

	_ "modernc.org/sqlite"
)

func (s *SQLiteAdapter) ensureSettingsProviderScope(ctx context.Context) error {
	exists, err := s.tableExists(ctx, "settings")
	if err != nil || !exists {
		return err
	}
	hasProvider, err := s.columnExists(ctx, "settings", "provider")
	if err != nil || hasProvider {
		return err
	}

	tx, err := s.GetWriteDB().BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin settings provider migration: %w", err)
	}
	defer tx.Rollback()

	statements := []string{
		`DROP INDEX IF EXISTS idx_settings_category`,
		`DROP INDEX IF EXISTS idx_settings_category_key`,
		`DROP TRIGGER IF EXISTS update_settings_timestamp`,
		`ALTER TABLE settings RENAME TO settings_legacy_provider_scope`,
		`CREATE TABLE settings (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			provider TEXT NOT NULL CHECK (provider IN ('global', 'claude', 'codex')),
			category TEXT NOT NULL,
			key TEXT NOT NULL,
			value TEXT NOT NULL,
			value_type TEXT DEFAULT 'string',
			label TEXT,
			description TEXT,
			display_order INTEGER DEFAULT 0,
			requires_restart INTEGER DEFAULT 0,
			created_at DATETIME DEFAULT (strftime('%Y-%m-%d %H:%M:%f', 'now', 'localtime') || '+08:00'),
			updated_at DATETIME DEFAULT (strftime('%Y-%m-%d %H:%M:%f', 'now', 'localtime') || '+08:00'),
			UNIQUE(provider, category, key)
		)`,
		`INSERT INTO settings (
			id, provider, category, key, value, value_type, label, description,
			display_order, requires_restart, created_at, updated_at
		) SELECT
			id, CASE WHEN category = 'failover' THEN 'claude' ELSE 'global' END,
			category, key, value, value_type, label, description,
			display_order, requires_restart, created_at, updated_at
		FROM settings_legacy_provider_scope`,
	}
	for _, statement := range statements {
		if _, err := tx.ExecContext(ctx, statement); err != nil {
			return fmt.Errorf("migrate settings provider scope: %w", err)
		}
	}

	var oldCount, newCount int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM settings_legacy_provider_scope`).Scan(&oldCount); err != nil {
		return fmt.Errorf("count legacy settings: %w", err)
	}
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM settings`).Scan(&newCount); err != nil {
		return fmt.Errorf("count migrated settings: %w", err)
	}
	if oldCount != newCount {
		return fmt.Errorf("settings provider migration row mismatch: old=%d new=%d", oldCount, newCount)
	}

	for _, statement := range []string{
		`DROP TABLE settings_legacy_provider_scope`,
		`CREATE INDEX idx_settings_provider_category ON settings(provider, category)`,
		`CREATE INDEX idx_settings_provider_category_key ON settings(provider, category, key)`,
		`CREATE TRIGGER update_settings_timestamp
			AFTER UPDATE ON settings
			FOR EACH ROW
			WHEN NEW.updated_at = OLD.updated_at
			BEGIN
				UPDATE settings SET updated_at = strftime('%Y-%m-%d %H:%M:%f', 'now', 'localtime') || '+08:00' WHERE id = NEW.id;
			END`,
	} {
		if _, err := tx.ExecContext(ctx, statement); err != nil {
			return fmt.Errorf("finalize settings provider migration: %w", err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit settings provider migration: %w", err)
	}
	return nil
}
