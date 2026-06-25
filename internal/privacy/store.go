package privacy

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"fmt"
	"time"
)

type Store struct {
	db *sql.DB
}

func NewStore(db *sql.DB) *Store {
	return &Store{db: db}
}

func (s *Store) EnsureSchema(ctx context.Context) error {
	if s == nil || s.db == nil {
		return fmt.Errorf("privacy store not initialized")
	}
	if _, err := s.db.ExecContext(ctx, `
		CREATE TABLE IF NOT EXISTS privacy_synthetic_values (
			value TEXT PRIMARY KEY,
			kind TEXT NOT NULL,
			created_at DATETIME DEFAULT (strftime('%Y-%m-%d %H:%M:%f', 'now', 'localtime') || '+08:00'),
			last_seen_at DATETIME DEFAULT (strftime('%Y-%m-%d %H:%M:%f', 'now', 'localtime') || '+08:00'),
			hit_count INTEGER NOT NULL DEFAULT 0
		)
	`); err != nil {
		return fmt.Errorf("create privacy_synthetic_values: %w", err)
	}
	if _, err := s.db.ExecContext(ctx, `CREATE INDEX IF NOT EXISTS idx_privacy_synthetic_values_kind ON privacy_synthetic_values(kind)`); err != nil {
		return fmt.Errorf("create idx_privacy_synthetic_values_kind: %w", err)
	}
	if _, err := s.db.ExecContext(ctx, `
			CREATE TABLE IF NOT EXISTS privacy_synthetic_mappings (
				kind TEXT NOT NULL,
				raw_hash TEXT NOT NULL,
				raw_value TEXT NOT NULL DEFAULT '',
				replacement TEXT NOT NULL,
				created_at DATETIME DEFAULT (strftime('%Y-%m-%d %H:%M:%f', 'now', 'localtime') || '+08:00'),
				last_seen_at DATETIME DEFAULT (strftime('%Y-%m-%d %H:%M:%f', 'now', 'localtime') || '+08:00'),
				hit_count INTEGER NOT NULL DEFAULT 0,
			PRIMARY KEY (kind, raw_hash),
			UNIQUE (kind, replacement)
		)
	`); err != nil {
		return fmt.Errorf("create privacy_synthetic_mappings: %w", err)
	}
	if _, err := s.db.ExecContext(ctx, `CREATE INDEX IF NOT EXISTS idx_privacy_synthetic_mappings_replacement ON privacy_synthetic_mappings(kind, replacement)`); err != nil {
		return fmt.Errorf("create idx_privacy_synthetic_mappings_replacement: %w", err)
	}
	hasRawValue, err := s.hasColumn(ctx, "privacy_synthetic_mappings", "raw_value")
	if err != nil {
		return err
	}
	if !hasRawValue {
		if _, err := s.db.ExecContext(ctx, `ALTER TABLE privacy_synthetic_mappings ADD COLUMN raw_value TEXT NOT NULL DEFAULT ''`); err != nil {
			return fmt.Errorf("add privacy_synthetic_mappings.raw_value: %w", err)
		}
	}
	hasReplacementMode, err := s.hasColumn(ctx, "privacy_rules", "replacement_mode")
	if err != nil {
		return err
	}
	if !hasReplacementMode {
		if _, err := s.db.ExecContext(ctx, `ALTER TABLE privacy_rules ADD COLUMN replacement_mode TEXT NOT NULL DEFAULT 'pseudonym'`); err != nil {
			return fmt.Errorf("add privacy_rules.replacement_mode: %w", err)
		}
	}
	return nil
}

func (s *Store) ListSyntheticValues(ctx context.Context) ([]SyntheticValueRecord, error) {
	if s == nil || s.db == nil {
		return nil, fmt.Errorf("privacy store not initialized")
	}

	rows, err := s.db.QueryContext(ctx, `
		SELECT
			m.raw_hash,
			m.raw_value,
			m.replacement,
			m.kind,
			m.created_at,
			m.last_seen_at,
			m.hit_count,
			COALESCE(v.last_seen_at, ''),
			COALESCE(v.hit_count, 0)
		FROM privacy_synthetic_mappings m
		LEFT JOIN privacy_synthetic_values v
			ON v.kind = m.kind AND v.value = m.replacement
		ORDER BY m.created_at DESC, m.raw_hash ASC
	`)
	if err != nil {
		return nil, fmt.Errorf("query privacy synthetic mappings: %w", err)
	}
	defer rows.Close()

	var records []SyntheticValueRecord
	for rows.Next() {
		var record SyntheticValueRecord
		var createdAt, lastSeenAt, lastSkipAt string
		if err := rows.Scan(
			&record.RawHash,
			&record.RawValue,
			&record.Replacement,
			&record.Kind,
			&createdAt,
			&lastSeenAt,
			&record.ReuseCount,
			&lastSkipAt,
			&record.SkipCount,
		); err != nil {
			return nil, err
		}
		record.Value = record.Replacement
		record.CreatedAt = parseTime(createdAt)
		record.LastSeenAt = parseTime(lastSeenAt)
		record.LastSkipAt = parseTime(lastSkipAt)
		record.HitCount = record.ReuseCount + record.SkipCount
		records = append(records, record)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate privacy synthetic mappings: %w", err)
	}
	return records, nil
}

func (s *Store) ClearSyntheticValues(ctx context.Context) error {
	if s == nil || s.db == nil {
		return fmt.Errorf("privacy store not initialized")
	}
	if _, err := s.db.ExecContext(ctx, `DELETE FROM privacy_synthetic_mappings`); err != nil {
		return fmt.Errorf("clear privacy synthetic mappings: %w", err)
	}
	if _, err := s.db.ExecContext(ctx, `DELETE FROM privacy_synthetic_values`); err != nil {
		return fmt.Errorf("clear privacy synthetic values: %w", err)
	}
	clearSyntheticReplacementRegistry()
	return nil
}

func (s *Store) DeleteSyntheticValue(ctx context.Context, kind, rawHash string) error {
	if s == nil || s.db == nil {
		return fmt.Errorf("privacy store not initialized")
	}
	if rawHash == "" {
		return fmt.Errorf("模拟值记录标识不能为空")
	}
	if kind == "" {
		kind = syntheticKindCustom
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin delete privacy synthetic mapping: %w", err)
	}
	defer tx.Rollback()

	var replacement string
	err = tx.QueryRowContext(ctx, `
		SELECT replacement
		FROM privacy_synthetic_mappings
		WHERE kind = ? AND raw_hash = ?
	`, kind, rawHash).Scan(&replacement)
	if err == sql.ErrNoRows {
		return fmt.Errorf("模拟值记录不存在")
	}
	if err != nil {
		return fmt.Errorf("query privacy synthetic mapping for delete: %w", err)
	}

	if _, err := tx.ExecContext(ctx, `
		DELETE FROM privacy_synthetic_mappings
		WHERE kind = ? AND raw_hash = ?
	`, kind, rawHash); err != nil {
		return fmt.Errorf("delete privacy synthetic mapping: %w", err)
	}

	var mappingRefs int
	if err := tx.QueryRowContext(ctx, `
		SELECT COUNT(*)
		FROM privacy_synthetic_mappings
		WHERE kind = ? AND replacement = ?
	`, kind, replacement).Scan(&mappingRefs); err != nil {
		return fmt.Errorf("query privacy synthetic mapping refs: %w", err)
	}
	if mappingRefs == 0 {
		if _, err := tx.ExecContext(ctx, `
			DELETE FROM privacy_synthetic_values
			WHERE kind = ? AND value = ?
		`, kind, replacement); err != nil {
			return fmt.Errorf("delete privacy synthetic value: %w", err)
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit delete privacy synthetic mapping: %w", err)
	}
	clearSyntheticReplacementRegistry()
	return nil
}

func (s *Store) RegisterSyntheticValue(ctx context.Context, value, kind string) error {
	if s == nil || s.db == nil {
		return fmt.Errorf("privacy store not initialized")
	}
	if value == "" {
		return nil
	}
	if kind == "" {
		kind = syntheticKindCustom
	}
	if _, err := s.db.ExecContext(ctx, `
		INSERT INTO privacy_synthetic_values (value, kind, created_at, last_seen_at, hit_count)
		VALUES (?, ?, strftime('%Y-%m-%d %H:%M:%f', 'now', 'localtime') || '+08:00', strftime('%Y-%m-%d %H:%M:%f', 'now', 'localtime') || '+08:00', 0)
		ON CONFLICT(value) DO UPDATE SET
			kind = excluded.kind,
			last_seen_at = strftime('%Y-%m-%d %H:%M:%f', 'now', 'localtime') || '+08:00'
	`, value, kind); err != nil {
		return fmt.Errorf("register privacy synthetic value: %w", err)
	}
	rememberSyntheticReplacementValue(value)
	return nil
}

func (s *Store) LookupSyntheticMapping(ctx context.Context, raw, kind string) (string, bool, error) {
	if s == nil || s.db == nil {
		return "", false, fmt.Errorf("privacy store not initialized")
	}
	if raw == "" {
		return "", false, nil
	}
	if kind == "" {
		kind = syntheticKindCustom
	}
	rawHash := syntheticRawHash(raw)
	var replacement string
	err := s.db.QueryRowContext(ctx, `
		SELECT replacement
		FROM privacy_synthetic_mappings
		WHERE kind = ? AND raw_hash = ?
	`, kind, rawHash).Scan(&replacement)
	if err == sql.ErrNoRows {
		return "", false, nil
	}
	if err != nil {
		return "", false, fmt.Errorf("lookup privacy synthetic mapping: %w", err)
	}
	if _, err := s.db.ExecContext(ctx, `
		UPDATE privacy_synthetic_mappings
		SET last_seen_at = strftime('%Y-%m-%d %H:%M:%f', 'now', 'localtime') || '+08:00',
			hit_count = hit_count + 1
		WHERE kind = ? AND raw_hash = ?
	`, kind, rawHash); err != nil {
		return "", false, fmt.Errorf("touch privacy synthetic mapping: %w", err)
	}
	rememberSyntheticReplacementValue(replacement)
	return replacement, true, nil
}

func (s *Store) RegisterSyntheticMapping(ctx context.Context, raw, kind, replacement string) error {
	if s == nil || s.db == nil {
		return fmt.Errorf("privacy store not initialized")
	}
	if raw == "" || replacement == "" {
		return nil
	}
	if kind == "" {
		kind = syntheticKindCustom
	}
	if _, err := s.db.ExecContext(ctx, `
		INSERT INTO privacy_synthetic_mappings (kind, raw_hash, raw_value, replacement, created_at, last_seen_at, hit_count)
		VALUES (?, ?, ?, ?, strftime('%Y-%m-%d %H:%M:%f', 'now', 'localtime') || '+08:00', strftime('%Y-%m-%d %H:%M:%f', 'now', 'localtime') || '+08:00', 0)
		ON CONFLICT(kind, raw_hash) DO UPDATE SET
			raw_value = CASE
				WHEN privacy_synthetic_mappings.raw_value = '' THEN excluded.raw_value
				ELSE privacy_synthetic_mappings.raw_value
			END,
			last_seen_at = strftime('%Y-%m-%d %H:%M:%f', 'now', 'localtime') || '+08:00'
	`, kind, syntheticRawHash(raw), raw, replacement); err != nil {
		return fmt.Errorf("register privacy synthetic mapping: %w", err)
	}
	rememberSyntheticReplacementValue(replacement)
	return nil
}

func (s *Store) SyntheticReplacementInUse(ctx context.Context, kind, replacement string) (bool, error) {
	if s == nil || s.db == nil {
		return false, fmt.Errorf("privacy store not initialized")
	}
	if replacement == "" {
		return false, nil
	}
	if kind == "" {
		kind = syntheticKindCustom
	}
	var exists int
	err := s.db.QueryRowContext(ctx, `
		SELECT 1
		FROM (
			SELECT replacement
			FROM privacy_synthetic_mappings
			WHERE kind = ? AND replacement = ?
			UNION ALL
			SELECT value AS replacement
			FROM privacy_synthetic_values
			WHERE kind = ? AND value = ?
		)
		LIMIT 1
	`, kind, replacement, kind, replacement).Scan(&exists)
	if err == sql.ErrNoRows {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("query privacy synthetic mapping replacement: %w", err)
	}
	return exists == 1, nil
}

func (s *Store) TouchSyntheticValue(ctx context.Context, value string) (bool, error) {
	if s == nil || s.db == nil {
		return false, fmt.Errorf("privacy store not initialized")
	}
	if value == "" {
		return false, nil
	}
	result, err := s.db.ExecContext(ctx, `
		UPDATE privacy_synthetic_values
		SET last_seen_at = strftime('%Y-%m-%d %H:%M:%f', 'now', 'localtime') || '+08:00',
			hit_count = hit_count + 1
		WHERE value = ?
	`, value)
	if err != nil {
		return false, fmt.Errorf("touch privacy synthetic value: %w", err)
	}
	rows, _ := result.RowsAffected()
	if rows > 0 {
		rememberSyntheticReplacementValue(value)
		return true, nil
	}
	return false, nil
}

func syntheticRawHash(raw string) string {
	sum := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(sum[:])
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
	if err := rows.Err(); err != nil {
		return false, err
	}
	return false, nil
}

func (s *Store) ListRules(ctx context.Context) ([]PrivacyRule, error) {
	return s.listRules(ctx, "", true)
}

func (s *Store) ListEnabledRules(ctx context.Context) ([]PrivacyRule, error) {
	return s.listRules(ctx, "WHERE enabled = 1", false)
}

func (s *Store) listRules(ctx context.Context, where string, mask bool) ([]PrivacyRule, error) {
	if s == nil || s.db == nil {
		return nil, fmt.Errorf("privacy store not initialized")
	}

	query := `SELECT id, name, type, category, pattern_or_value, replacement_mode, enabled, priority, created_at, updated_at
		FROM privacy_rules ` + where + ` ORDER BY id ASC`

	rows, err := s.db.QueryContext(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("query privacy rules: %w", err)
	}
	defer rows.Close()

	var rules []PrivacyRule
	for rows.Next() {
		rule, err := scanRule(rows)
		if err != nil {
			return nil, err
		}
		if mask {
			rule = maskRule(rule)
		}
		rules = append(rules, rule)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate privacy rules: %w", err)
	}
	return rules, nil
}

func (s *Store) CreateRule(ctx context.Context, input PrivacyRuleInput) (PrivacyRule, error) {
	if s == nil || s.db == nil {
		return PrivacyRule{}, fmt.Errorf("privacy store not initialized")
	}

	result, err := s.db.ExecContext(ctx, `
		INSERT INTO privacy_rules (name, type, category, pattern_or_value, replacement_mode, enabled, priority)
		VALUES (?, ?, ?, ?, ?, ?, ?)
	`, input.Name, input.Type, input.Category, input.PatternOrValue, input.ReplacementMode, boolToInt(input.Enabled), input.Priority)
	if err != nil {
		return PrivacyRule{}, fmt.Errorf("create privacy rule: %w", err)
	}

	id, err := result.LastInsertId()
	if err != nil {
		return PrivacyRule{}, fmt.Errorf("get privacy rule id: %w", err)
	}
	return s.GetRule(ctx, id)
}

func (s *Store) UpdateRule(ctx context.Context, id int64, input PrivacyRuleInput) (PrivacyRule, error) {
	if s == nil || s.db == nil {
		return PrivacyRule{}, fmt.Errorf("privacy store not initialized")
	}

	result, err := s.db.ExecContext(ctx, `
		UPDATE privacy_rules
		SET name = ?, type = ?, category = ?, pattern_or_value = ?, replacement_mode = ?, enabled = ?, priority = ?
		WHERE id = ?
	`, input.Name, input.Type, input.Category, input.PatternOrValue, input.ReplacementMode, boolToInt(input.Enabled), input.Priority, id)
	if err != nil {
		return PrivacyRule{}, fmt.Errorf("update privacy rule: %w", err)
	}
	if rows, _ := result.RowsAffected(); rows == 0 {
		return PrivacyRule{}, fmt.Errorf("隐私规则不存在: %d", id)
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

func (s *Store) GetRule(ctx context.Context, id int64) (PrivacyRule, error) {
	rule, err := s.getRule(ctx, id)
	if err != nil {
		return PrivacyRule{}, err
	}
	return maskRule(rule), nil
}

func (s *Store) GetRuleRaw(ctx context.Context, id int64) (PrivacyRule, error) {
	return s.getRule(ctx, id)
}

func (s *Store) getRule(ctx context.Context, id int64) (PrivacyRule, error) {
	if s == nil || s.db == nil {
		return PrivacyRule{}, fmt.Errorf("privacy store not initialized")
	}

	row := s.db.QueryRowContext(ctx, `
		SELECT id, name, type, category, pattern_or_value, replacement_mode, enabled, priority, created_at, updated_at
		FROM privacy_rules WHERE id = ?
	`, id)

	rule, err := scanRule(row)
	if err != nil {
		if err == sql.ErrNoRows {
			return PrivacyRule{}, fmt.Errorf("隐私规则不存在: %d", id)
		}
		return PrivacyRule{}, err
	}
	return rule, nil
}

type ruleScanner interface {
	Scan(dest ...any) error
}

func scanRule(scanner ruleScanner) (PrivacyRule, error) {
	var rule PrivacyRule
	var enabled int
	var createdAt, updatedAt string
	if err := scanner.Scan(
		&rule.ID,
		&rule.Name,
		&rule.Type,
		&rule.Category,
		&rule.PatternOrValue,
		&rule.ReplacementMode,
		&enabled,
		&rule.Priority,
		&createdAt,
		&updatedAt,
	); err != nil {
		return PrivacyRule{}, err
	}
	if rule.ReplacementMode == "" {
		rule.ReplacementMode = ReplacementModePseudonym
	}
	rule.Enabled = enabled == 1
	rule.CreatedAt = parseTime(createdAt)
	rule.UpdatedAt = parseTime(updatedAt)
	return rule, nil
}

func maskRule(rule PrivacyRule) PrivacyRule {
	rule.ValueLength = len([]rune(rule.PatternOrValue))
	if rule.PatternOrValue == "" {
		return rule
	}
	if rule.Type == RuleTypeExact {
		rule.PatternMasked = maskValue(rule.PatternOrValue)
		rule.PatternOrValue = ""
		return rule
	}
	rule.PatternMasked = rule.PatternOrValue
	return rule
}

func maskValue(value string) string {
	length := len([]rune(value))
	switch {
	case length <= 0:
		return ""
	case length <= 4:
		return "****"
	case length <= 8:
		return "****" + string([]rune(value)[length-2:])
	default:
		runes := []rune(value)
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
