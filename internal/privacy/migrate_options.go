package privacy

import (
	"context"
	"encoding/json"
	"fmt"
)

func runtimeOptionsJSON(settings Settings) ([]byte, error) {
	return json.Marshal(map[string]bool{
		"restore_responses": settings.RestoreResponses,
		"keep_key_format":   settings.KeepKeyFormat,
	})
}

// migrateMatchingOptions replaces the second layer of matching controls with
// ordinary editable rules. Clearing the old options in the same transaction
// makes restarts idempotent and prevents them from overriding later edits.
func (s *Store) migrateMatchingOptions(ctx context.Context) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	var options string
	if err := tx.QueryRowContext(ctx, `SELECT options_json FROM privacy_settings WHERE id = 1`).Scan(&options); err != nil {
		return err
	}
	var settings Settings
	if err := json.Unmarshal([]byte(options), &settings); err != nil {
		return fmt.Errorf("invalid privacy options")
	}
	if !hasLegacyMatchingOptions(settings) {
		return nil
	}
	settings, err = normalizedLegacyOptions(settings)
	if err != nil {
		return err
	}

	rows, err := tx.QueryContext(ctx, `SELECT id, enabled, name, description, priority, match_type, pattern,
		action, replacement_mode, placeholder, pseudonym_kind, source, created_at, updated_at FROM privacy_rules`)
	if err != nil {
		return err
	}
	var existing []Rule
	for rows.Next() {
		rule, err := scanStoredRule(rows)
		if err != nil {
			rows.Close()
			return err
		}
		existing = append(existing, rule.Rule)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return err
	}
	if err := rows.Close(); err != nil {
		return err
	}

	resolve := func(rule Rule) (Rule, error) {
		if on, ok := settings.Categories[ruleCategory(rule)]; ok && !on {
			rule.Enabled = false
		}
		if action := settings.Actions[legacyRuleKind(rule)]; action != "" {
			rule.Action = action
		}
		// Previously non-redacting rules may not have replacement fields yet.
		if rule.Action == ActionRedact {
			if rule.ReplacementMode == "" {
				rule.ReplacementMode = ReplacementPlaceholder
			}
			if rule.ReplacementMode == ReplacementPlaceholder && rule.Placeholder == "" {
				rule.Placeholder = DefaultPlaceholder
			}
		}
		if _, err := CompileRule(rule); err != nil {
			return Rule{}, err
		}
		return rule, nil
	}
	for _, rule := range existing {
		next, err := resolve(rule)
		if err != nil {
			return err
		}
		if next == rule {
			continue
		}
		if _, err := tx.ExecContext(ctx, `UPDATE privacy_rules SET enabled = ?, action = ?,
			replacement_mode = ?, placeholder = ?, updated_at = `+sqlNowExpr+` WHERE id = ?`,
			boolToInt(next.Enabled), next.Action, next.ReplacementMode, next.Placeholder, rule.ID); err != nil {
			return err
		}
	}
	for _, rule := range legacyWordRules(settings) {
		next, err := resolve(rule)
		if err != nil {
			return err
		}
		// Preserve the old synthetic IDs so equal-priority overlaps keep exactly
		// the same order. These are now persisted rules with normal CRUD support.
		if _, err := insertRule(ctx, tx, next); err != nil {
			return err
		}
	}
	for _, value := range settings.Allow {
		if _, err := insertRule(ctx, tx, Rule{Name: "例外放行", Description: "完整命中值匹配时放行，忽略大小写", Enabled: true,
			MatchType: MatchTypeLiteral, Pattern: value, Action: ActionAllow, Source: SourceCustom, Priority: -200,
			ReplacementMode: ReplacementPlaceholder, Placeholder: DefaultPlaceholder}); err != nil {
			return err
		}
	}
	clean, err := runtimeOptionsJSON(settings)
	if err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE privacy_settings SET options_json = ?, updated_at = `+sqlNowExpr+` WHERE id = 1`, string(clean)); err != nil {
		return err
	}
	return tx.Commit()
}
