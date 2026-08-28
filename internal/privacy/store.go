package privacy

import (
	"context"
	"database/sql"
	"fmt"
	"time"
)

// LegacyRule 旧版（v8.5 及以前）隐私规则的只读快照。
//
// 旧规则刻意不自动转换为新模型：转换出错在隐私场景是静默失败——
// 规则看似还在列表里、实际已不匹配，用户以为受保护实则裸奔。
// 保留原样供用户逐条确认重建，确认后才生效。
type LegacyRule struct {
	ID              int64  `json:"id"`
	Name            string `json:"name"`
	Type            string `json:"type"`
	Category        string `json:"category"`
	PatternMasked   string `json:"pattern_masked"`
	ValueLength     int    `json:"value_length"`
	ReplacementMode string `json:"replacement_mode"`
	Enabled         bool   `json:"enabled"`
	Priority        int    `json:"priority"`
}

// StoredRule 带持久化元数据的规则
type StoredRule struct {
	Rule
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

type Store struct {
	db *sql.DB
}

func NewStore(db *sql.DB) *Store {
	return &Store{db: db}
}

const sqlNowExpr = `(strftime('%Y-%m-%d %H:%M:%f', 'now', 'localtime') || '+08:00')`

// EnsureSchema 建立新版隐私表，并完成一次性的破坏性迁移。
//
// 迁移做三件事，均围绕「隐私防护自身不得成为隐私集中池」：
//  1. 旧 privacy_rules 整表改名保留为 privacy_legacy_rules（零拷贝，供用户手工重建）；
//  2. 删除 privacy_synthetic_values / privacy_synthetic_mappings——后者的 raw_value 列
//     直接存放客户真实姓名与号码，是本次重构要根除的明文存储；
//  3. 种子化内置 PII 规则，避免新装用户面对一份空规则表却以为已受保护。
func (s *Store) EnsureSchema(ctx context.Context) error {
	if s == nil || s.db == nil {
		return fmt.Errorf("privacy store not initialized")
	}

	migrated, err := s.migrateLegacyRulesTable(ctx)
	if err != nil {
		return err
	}

	if _, err := s.db.ExecContext(ctx, `
		CREATE TABLE IF NOT EXISTS privacy_rules (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			enabled INTEGER NOT NULL DEFAULT 1,
			name TEXT NOT NULL,
			description TEXT NOT NULL DEFAULT '',
			priority INTEGER NOT NULL DEFAULT 100,
			match_type TEXT NOT NULL,
			pattern TEXT NOT NULL,
			action TEXT NOT NULL DEFAULT 'redact',
			replacement_mode TEXT NOT NULL DEFAULT 'placeholder',
			placeholder TEXT NOT NULL DEFAULT '[已脱敏]',
			pseudonym_kind TEXT NOT NULL DEFAULT '',
			source TEXT NOT NULL DEFAULT 'custom',
			created_at DATETIME DEFAULT `+sqlNowExpr+`,
			updated_at DATETIME DEFAULT `+sqlNowExpr+`
		)
	`); err != nil {
		return fmt.Errorf("create privacy_rules: %w", err)
	}
	if _, err := s.db.ExecContext(ctx,
		`CREATE INDEX IF NOT EXISTS idx_privacy_rules_enabled_priority ON privacy_rules(enabled, priority)`); err != nil {
		return fmt.Errorf("create idx_privacy_rules_enabled_priority: %w", err)
	}

	if _, err := s.db.ExecContext(ctx, `
		CREATE TABLE IF NOT EXISTS privacy_settings (
			id INTEGER PRIMARY KEY CHECK (id = 1),
			mode TEXT NOT NULL DEFAULT 'detect',
			scan_max_bytes INTEGER NOT NULL DEFAULT 4194304,
			over_limit_action TEXT NOT NULL DEFAULT 'scan_prefix',
			on_error TEXT NOT NULL DEFAULT 'fail_open',
			unsupported_path_action TEXT NOT NULL DEFAULT 'warn',
			updated_at DATETIME DEFAULT `+sqlNowExpr+`
		)
	`); err != nil {
		return fmt.Errorf("create privacy_settings: %w", err)
	}
	if _, err := s.db.ExecContext(ctx, `INSERT OR IGNORE INTO privacy_settings (id) VALUES (1)`); err != nil {
		return fmt.Errorf("seed privacy_settings: %w", err)
	}

	if err := s.dropPlaintextSyntheticTables(ctx); err != nil {
		return err
	}
	if err := s.seedBuiltinRules(ctx); err != nil {
		return err
	}

	if migrated {
		// 规则集已整体更换，沿用旧的「有启用规则即脱敏」语义会让全新规则集
		// 未经验证就改写生产流量。统一降到 detect，由用户看过命中统计后再开 redact。
		if _, err := s.db.ExecContext(ctx,
			`UPDATE privacy_settings SET mode = ?, updated_at = `+sqlNowExpr+` WHERE id = 1`, ModeDetect); err != nil {
			return fmt.Errorf("reset mode after legacy migration: %w", err)
		}
	}
	return nil
}

// migrateLegacyRulesTable 旧结构（有 type 列、无 match_type 列）整表改名保留。
// 返回是否发生了迁移。
//
// 一次性迁移代码，有明确终点：只在 v8.5 及更早版本的库上触发一次，
// 之后 privacy_rules 已是新结构，本函数恒走 isNew 分支直接返回。
// 移除条件：确认在网用户不再有 v8.5 及更早版本时，可连同
// ListLegacyRules / DiscardLegacyRules 与前端 LegacyRulesPanel 一并删除。
func (s *Store) migrateLegacyRulesTable(ctx context.Context) (bool, error) {
	exists, err := s.tableExists(ctx, "privacy_rules")
	if err != nil {
		return false, err
	}
	if !exists {
		return false, nil
	}
	isNew, err := s.hasColumn(ctx, "privacy_rules", "match_type")
	if err != nil {
		return false, err
	}
	if isNew {
		return false, nil
	}

	legacyExists, err := s.tableExists(ctx, "privacy_legacy_rules")
	if err != nil {
		return false, err
	}
	if legacyExists {
		// 已经迁移过一次却仍有旧结构表，说明是异常中断的残留，直接丢弃残留
		if _, err := s.db.ExecContext(ctx, `DROP TABLE privacy_rules`); err != nil {
			return false, fmt.Errorf("drop stale legacy privacy_rules: %w", err)
		}
		return true, nil
	}
	if _, err := s.db.ExecContext(ctx, `ALTER TABLE privacy_rules RENAME TO privacy_legacy_rules`); err != nil {
		return false, fmt.Errorf("rename legacy privacy_rules: %w", err)
	}
	return true, nil
}

// dropPlaintextSyntheticTables 删除伪名化映射表。
// privacy_synthetic_mappings.raw_value 存放客户真实值，是明文泄漏面；
// 新的伪名化实现无状态，不需要任何持久化映射。
func (s *Store) dropPlaintextSyntheticTables(ctx context.Context) error {
	for _, table := range []string{"privacy_synthetic_mappings", "privacy_synthetic_values"} {
		if _, err := s.db.ExecContext(ctx, `DROP TABLE IF EXISTS `+table); err != nil {
			return fmt.Errorf("drop %s: %w", table, err)
		}
	}
	return nil
}

// seedBuiltinRules 幂等种子化内置 PII 规则（按 source+pattern 判重）
func (s *Store) seedBuiltinRules(ctx context.Context) error {
	for _, rule := range BuiltinPIIRules() {
		var count int
		if err := s.db.QueryRowContext(ctx,
			`SELECT COUNT(1) FROM privacy_rules WHERE source = ? AND pattern = ?`,
			SourceBuiltin, rule.Pattern).Scan(&count); err != nil {
			return fmt.Errorf("check builtin rule %q: %w", rule.Pattern, err)
		}
		if count > 0 {
			continue
		}
		if _, err := s.insertRule(ctx, rule); err != nil {
			return fmt.Errorf("seed builtin rule %q: %w", rule.Pattern, err)
		}
	}
	return nil
}

func (s *Store) LoadSettings(ctx context.Context) (Settings, error) {
	if s == nil || s.db == nil {
		return Settings{}, fmt.Errorf("privacy store not initialized")
	}
	var settings Settings
	err := s.db.QueryRowContext(ctx, `
		SELECT mode, scan_max_bytes, over_limit_action, on_error, unsupported_path_action
		FROM privacy_settings WHERE id = 1
	`).Scan(&settings.Mode, &settings.ScanMaxBytes, &settings.OverLimitAction,
		&settings.OnError, &settings.UnsupportedPathAction)
	if err == sql.ErrNoRows {
		return DefaultSettings(), nil
	}
	if err != nil {
		return Settings{}, fmt.Errorf("load privacy settings: %w", err)
	}
	return settings, nil
}

func (s *Store) SaveSettings(ctx context.Context, settings Settings) error {
	if s == nil || s.db == nil {
		return fmt.Errorf("privacy store not initialized")
	}
	if err := ValidateSettings(settings); err != nil {
		return err
	}
	_, err := s.db.ExecContext(ctx, `
		UPDATE privacy_settings
		SET mode = ?, scan_max_bytes = ?, over_limit_action = ?, on_error = ?,
		    unsupported_path_action = ?, updated_at = `+sqlNowExpr+`
		WHERE id = 1
	`, settings.Mode, settings.ScanMaxBytes, settings.OverLimitAction,
		settings.OnError, settings.UnsupportedPathAction)
	if err != nil {
		return fmt.Errorf("save privacy settings: %w", err)
	}
	return nil
}

func (s *Store) ListRules(ctx context.Context) ([]StoredRule, error) {
	return s.queryRules(ctx, "")
}

func (s *Store) ListEnabledRules(ctx context.Context) ([]StoredRule, error) {
	return s.queryRules(ctx, "WHERE enabled = 1")
}

func (s *Store) queryRules(ctx context.Context, where string) ([]StoredRule, error) {
	if s == nil || s.db == nil {
		return nil, fmt.Errorf("privacy store not initialized")
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, enabled, name, description, priority, match_type, pattern,
		       action, replacement_mode, placeholder, pseudonym_kind, source, created_at, updated_at
		FROM privacy_rules `+where+` ORDER BY priority ASC, id ASC`)
	if err != nil {
		return nil, fmt.Errorf("query privacy rules: %w", err)
	}
	defer rows.Close()

	var rules []StoredRule
	for rows.Next() {
		rule, err := scanStoredRule(rows)
		if err != nil {
			return nil, err
		}
		rules = append(rules, rule)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate privacy rules: %w", err)
	}
	return rules, nil
}

func (s *Store) GetRule(ctx context.Context, id int64) (StoredRule, error) {
	if s == nil || s.db == nil {
		return StoredRule{}, fmt.Errorf("privacy store not initialized")
	}
	row := s.db.QueryRowContext(ctx, `
		SELECT id, enabled, name, description, priority, match_type, pattern,
		       action, replacement_mode, placeholder, pseudonym_kind, source, created_at, updated_at
		FROM privacy_rules WHERE id = ?`, id)
	rule, err := scanStoredRule(row)
	if err == sql.ErrNoRows {
		return StoredRule{}, fmt.Errorf("隐私规则不存在: %d", id)
	}
	if err != nil {
		return StoredRule{}, err
	}
	return rule, nil
}

func (s *Store) CreateRule(ctx context.Context, rule Rule) (StoredRule, error) {
	if s == nil || s.db == nil {
		return StoredRule{}, fmt.Errorf("privacy store not initialized")
	}
	id, err := s.insertRule(ctx, rule)
	if err != nil {
		return StoredRule{}, fmt.Errorf("create privacy rule: %w", err)
	}
	return s.GetRule(ctx, id)
}

func (s *Store) insertRule(ctx context.Context, rule Rule) (int64, error) {
	result, err := s.db.ExecContext(ctx, `
		INSERT INTO privacy_rules
			(enabled, name, description, priority, match_type, pattern, action,
			 replacement_mode, placeholder, pseudonym_kind, source)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, boolToInt(rule.Enabled), rule.Name, rule.Description, rule.Priority,
		rule.MatchType, rule.Pattern, rule.Action,
		rule.ReplacementMode, rule.Placeholder, rule.PseudonymKind, rule.Source)
	if err != nil {
		return 0, err
	}
	return result.LastInsertId()
}

func (s *Store) UpdateRule(ctx context.Context, id int64, rule Rule) (StoredRule, error) {
	if s == nil || s.db == nil {
		return StoredRule{}, fmt.Errorf("privacy store not initialized")
	}
	result, err := s.db.ExecContext(ctx, `
		UPDATE privacy_rules
		SET enabled = ?, name = ?, description = ?, priority = ?, match_type = ?, pattern = ?,
		    action = ?, replacement_mode = ?, placeholder = ?, pseudonym_kind = ?,
		    updated_at = `+sqlNowExpr+`
		WHERE id = ?
	`, boolToInt(rule.Enabled), rule.Name, rule.Description, rule.Priority,
		rule.MatchType, rule.Pattern, rule.Action,
		rule.ReplacementMode, rule.Placeholder, rule.PseudonymKind, id)
	if err != nil {
		return StoredRule{}, fmt.Errorf("update privacy rule: %w", err)
	}
	if rows, _ := result.RowsAffected(); rows == 0 {
		return StoredRule{}, fmt.Errorf("隐私规则不存在: %d", id)
	}
	return s.GetRule(ctx, id)
}

func (s *Store) DeleteRule(ctx context.Context, id int64) error {
	if s == nil || s.db == nil {
		return fmt.Errorf("privacy store not initialized")
	}
	result, err := s.db.ExecContext(ctx, `DELETE FROM privacy_rules WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("delete privacy rule: %w", err)
	}
	if rows, _ := result.RowsAffected(); rows == 0 {
		return fmt.Errorf("隐私规则不存在: %d", id)
	}
	return nil
}

// DeleteRules 批量删除。内置规则不允许删除（只能停用），
// 因此这里显式排除，避免前端误传把内置 PII 防护整组抹掉。
// 返回实际删除条数。
func (s *Store) DeleteRules(ctx context.Context, ids []int64) (int64, error) {
	if s == nil || s.db == nil {
		return 0, fmt.Errorf("privacy store not initialized")
	}
	if len(ids) == 0 {
		return 0, nil
	}
	query := `DELETE FROM privacy_rules WHERE source != ? AND id IN (` + sqlPlaceholders(len(ids)) + `)`
	args := make([]any, 0, len(ids)+1)
	args = append(args, SourceBuiltin)
	for _, id := range ids {
		args = append(args, id)
	}
	result, err := s.db.ExecContext(ctx, query, args...)
	if err != nil {
		return 0, fmt.Errorf("batch delete privacy rules: %w", err)
	}
	affected, _ := result.RowsAffected()
	return affected, nil
}

// SetRulesEnabled 批量启用/停用。返回实际影响条数。
func (s *Store) SetRulesEnabled(ctx context.Context, ids []int64, enabled bool) (int64, error) {
	if s == nil || s.db == nil {
		return 0, fmt.Errorf("privacy store not initialized")
	}
	if len(ids) == 0 {
		return 0, nil
	}
	query := `UPDATE privacy_rules SET enabled = ?, updated_at = ` + sqlNowExpr +
		` WHERE id IN (` + sqlPlaceholders(len(ids)) + `)`
	args := make([]any, 0, len(ids)+1)
	args = append(args, boolToInt(enabled))
	for _, id := range ids {
		args = append(args, id)
	}
	result, err := s.db.ExecContext(ctx, query, args...)
	if err != nil {
		return 0, fmt.Errorf("batch toggle privacy rules: %w", err)
	}
	affected, _ := result.RowsAffected()
	return affected, nil
}

func sqlPlaceholders(n int) string {
	if n <= 0 {
		return ""
	}
	buf := make([]byte, 0, n*2-1)
	for i := 0; i < n; i++ {
		if i > 0 {
			buf = append(buf, ',')
		}
		buf = append(buf, '?')
	}
	return string(buf)
}

// ListLegacyRules 列出旧版规则只读清单。
// exact 类旧规则的 pattern 本身就是用户登记的敏感值，一律掩码后返回。
//
// 一次性迁移代码：表不存在（全新安装，或用户已确认重建并清除）时返回空。
// 移除条件见 migrateLegacyRulesTable 的说明。
func (s *Store) ListLegacyRules(ctx context.Context) ([]LegacyRule, error) {
	if s == nil || s.db == nil {
		return nil, fmt.Errorf("privacy store not initialized")
	}
	exists, err := s.tableExists(ctx, "privacy_legacy_rules")
	if err != nil || !exists {
		return nil, err
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, name, type, category, pattern_or_value, replacement_mode, enabled, priority
		FROM privacy_legacy_rules ORDER BY id ASC`)
	if err != nil {
		return nil, fmt.Errorf("query legacy privacy rules: %w", err)
	}
	defer rows.Close()

	var out []LegacyRule
	for rows.Next() {
		var rule LegacyRule
		var pattern string
		var enabled int
		if err := rows.Scan(&rule.ID, &rule.Name, &rule.Type, &rule.Category,
			&pattern, &rule.ReplacementMode, &enabled, &rule.Priority); err != nil {
			return nil, err
		}
		rule.Enabled = enabled == 1
		rule.ValueLength = len([]rune(pattern))
		if rule.Type == "exact" {
			rule.PatternMasked = maskValue(pattern)
		} else {
			rule.PatternMasked = pattern
		}
		out = append(out, rule)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate legacy privacy rules: %w", err)
	}
	return out, nil
}

// DiscardLegacyRules 用户确认重建完毕后清除旧清单
func (s *Store) DiscardLegacyRules(ctx context.Context) error {
	if s == nil || s.db == nil {
		return fmt.Errorf("privacy store not initialized")
	}
	if _, err := s.db.ExecContext(ctx, `DROP TABLE IF EXISTS privacy_legacy_rules`); err != nil {
		return fmt.Errorf("drop privacy_legacy_rules: %w", err)
	}
	return nil
}

func (s *Store) tableExists(ctx context.Context, name string) (bool, error) {
	var found string
	err := s.db.QueryRowContext(ctx,
		`SELECT name FROM sqlite_master WHERE type = 'table' AND name = ?`, name).Scan(&found)
	if err == sql.ErrNoRows {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("inspect table %s: %w", name, err)
	}
	return true, nil
}

func (s *Store) hasColumn(ctx context.Context, tableName, columnName string) (bool, error) {
	rows, err := s.db.QueryContext(ctx, `PRAGMA table_info(`+tableName+`)`)
	if err != nil {
		return false, fmt.Errorf("inspect %s columns: %w", tableName, err)
	}
	defer rows.Close()

	for rows.Next() {
		var cid int
		var name, dataType string
		var notNull int
		var defaultValue any
		var pk int
		if err := rows.Scan(&cid, &name, &dataType, &notNull, &defaultValue, &pk); err != nil {
			return false, err
		}
		if name == columnName {
			return true, nil
		}
	}
	return false, rows.Err()
}

type ruleScanner interface {
	Scan(dest ...any) error
}

func scanStoredRule(scanner ruleScanner) (StoredRule, error) {
	var rule StoredRule
	var enabled int
	var createdAt, updatedAt string
	if err := scanner.Scan(
		&rule.ID, &enabled, &rule.Name, &rule.Description, &rule.Priority,
		&rule.MatchType, &rule.Pattern, &rule.Action,
		&rule.ReplacementMode, &rule.Placeholder, &rule.PseudonymKind, &rule.Source,
		&createdAt, &updatedAt,
	); err != nil {
		return StoredRule{}, err
	}
	rule.Enabled = enabled == 1
	rule.CreatedAt = parseTime(createdAt)
	rule.UpdatedAt = parseTime(updatedAt)
	return rule, nil
}

// maskValue 掩码敏感值。短值不暴露任何片段。
func maskValue(value string) string {
	runes := []rune(value)
	switch length := len(runes); {
	case length <= 0:
		return ""
	case length <= 8:
		return "****"
	default:
		return string(runes[:2]) + "******" + string(runes[length-2:])
	}
}

func parseTime(value string) time.Time {
	if value == "" {
		return time.Time{}
	}
	layouts := []string{
		time.RFC3339Nano,
		"2006-01-02 15:04:05.999999999-07:00",
		"2006-01-02 15:04:05.999999-07:00",
		"2006-01-02 15:04:05.999-07:00",
		"2006-01-02 15:04:05-07:00",
		"2006-01-02 15:04:05.999999999",
		"2006-01-02 15:04:05.999999",
		"2006-01-02 15:04:05",
	}
	for _, layout := range layouts {
		if t, err := time.Parse(layout, value); err == nil {
			return t
		}
	}
	return time.Time{}
}

func boolToInt(value bool) int {
	if value {
		return 1
	}
	return 0
}
