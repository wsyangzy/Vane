package store

import (
	"context"
	"database/sql"
	"strings"
)

// removeIntentRouting retires the old configuration in the schema transaction.
// Removing only a condition would turn a restricted rule into a broader match.
// Historical request/trace payloads remain intact for accounting and diagnostics.
func removeIntentRouting(ctx context.Context, tx *sql.Tx) error {
	var hasCondition int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM pragma_table_info('routing_profile_rules') WHERE name='intent_id'`).Scan(&hasCondition); err != nil {
		return err
	}
	condition := `kind='intent'`
	if hasCondition != 0 {
		condition += ` OR intent_id<>''`
	}
	affected := []string{`SELECT profile_id FROM routing_profile_rules WHERE ` + condition}
	var retiredTables []string
	for _, table := range []string{"routing_profile_intents", "routing_profile_classifier_config"} {
		var exists int
		if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name=?`, table).Scan(&exists); err != nil {
			return err
		}
		if exists != 0 {
			retiredTables = append(retiredTables, table)
			affected = append(affected, `SELECT profile_id FROM `+table)
		}
	}
	if hasCondition == 0 && len(retiredTables) == 0 {
		return nil
	}
	profiles := strings.Join(affected, " UNION ")
	if _, err := tx.ExecContext(ctx, `UPDATE routing_profiles SET revision=revision+1,updated_at=CURRENT_TIMESTAMP WHERE id IN (`+profiles+`)`); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM routing_affinity WHERE profile_id IN (`+profiles+`)`); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM routing_profile_rules WHERE `+condition); err != nil {
		return err
	}
	for _, table := range retiredTables {
		if _, err := tx.ExecContext(ctx, `DROP TABLE `+table); err != nil {
			return err
		}
	}
	if hasCondition != 0 {
		if _, err := tx.ExecContext(ctx, `ALTER TABLE routing_profile_rules DROP COLUMN intent_id`); err != nil {
			return err
		}
	}
	return nil
}
