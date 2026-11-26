package store

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/wsyangzy/Vane/internal/routing"
)

func legacyIntentStore(t *testing.T, provider string) (*ProfileStore, *sql.DB, routing.RoutingProfile, routing.RoutingProfile) {
	t.Helper()
	s, db := newProfileTestStore(t)
	ctx := context.Background()
	affected := testRoutingProfile(provider, "affected")
	affected.IsDefault = true
	affected.Rules = []routing.RoutingRule{{ID: 1, Kind: "model", MatchModel: "kept-model", Name: "kept", Enabled: true, Targets: []routing.RouteTarget{{ChannelID: 1, GroupID: 1}}}}
	if provider == "codex" {
		search := routing.DefaultSearchPolicy()
		affected.Search = &search
	}
	require.NoError(t, s.Save(ctx, &affected))
	untouched := testRoutingProfile(provider, "untouched")
	require.NoError(t, s.Save(ctx, &untouched))
	_, err := db.Exec(`ALTER TABLE routing_profile_rules ADD COLUMN intent_id TEXT NOT NULL DEFAULT '';
CREATE TABLE routing_profile_intents (
 id TEXT NOT NULL, profile_id INTEGER NOT NULL REFERENCES routing_profiles(id) ON DELETE CASCADE,
 name TEXT NOT NULL, description TEXT NOT NULL, priority INTEGER NOT NULL DEFAULT 0,
 enabled INTEGER NOT NULL DEFAULT 1, targets_json TEXT NOT NULL DEFAULT '[]', mode TEXT NOT NULL DEFAULT 'prefer',
 fallback_intent TEXT NOT NULL DEFAULT '', fallback_to_default INTEGER, PRIMARY KEY(profile_id,id));
CREATE TABLE routing_profile_classifier_config (
 profile_id INTEGER PRIMARY KEY REFERENCES routing_profiles(id) ON DELETE CASCADE, payload TEXT NOT NULL);`)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO routing_profile_intents(id,profile_id,name,description) VALUES('coding',?,'coding','retired');
INSERT INTO routing_profile_classifier_config(profile_id,payload) VALUES(?, '{"enabled":true,"model":"router"}');`, affected.ID, affected.ID)
	require.NoError(t, err)
	// Disabled and composite rules must disappear too; stripping intent_id would
	// let these conditions match later without the original restriction.
	for i, kind := range []string{"intent", "model", "mapping", "composite", "non_stream"} {
		_, err = db.Exec(`INSERT INTO routing_profile_rules(id,profile_id,kind,name,enabled,match_model,intent_id,targets_json)
VALUES(?,?,?,'retired',?,'*','coding','[{"channel_id":2,"group_id":4}]')`, i+2, affected.ID, kind, i%2)
		require.NoError(t, err)
	}
	for _, profile := range []routing.RoutingProfile{affected, untouched} {
		_, err = db.Exec(`INSERT INTO routing_affinity(provider,profile_id,affinity_key,payload,expires_at) VALUES(?,?,'old','{}','2099-01-01')`, provider, profile.ID)
		require.NoError(t, err)
	}
	return s, db, affected, untouched
}

func TestIntentMigrationPreservesOrdinaryRoutingSearchAndAccounting(t *testing.T) {
	for _, provider := range []string{"claude", "codex"} {
		t.Run(provider, func(t *testing.T) {
			s, db, affected, untouched := legacyIntentStore(t, provider)
			ctx := context.Background()
			const historical = `{"decision":{"kind":"intent","intent_id":"coding"},"intent":{"cost":0.002},"successful":true}`
			_, err := db.Exec(`INSERT INTO routing_dispatch_traces(provider,profile_id,request_id,payload) VALUES(?,?,'old-request',?)`, provider, affected.ID, historical)
			require.NoError(t, err)
			balance, err := s.UpdateManualBalance(ctx, routing.ManualBalanceUpdate{Provider: provider, ChannelID: 1, GroupID: 1, Action: "calibrate", AmountUSD: "12.3456789", StopWhenExhausted: true, OperationID: "migration-preserve"}, "account-hash", time.Now().UTC())
			require.NoError(t, err)
			// Reopening uses the same migration as startup. A second run must not
			// bump revisions, clear unrelated affinity or change stored settings.
			for i := 0; i < 2; i++ {
				reopened := NewProfileStore(db)
				require.NoError(t, reopened.EnsureSchema(ctx))
				got, err := reopened.Get(ctx, provider, affected.ID)
				require.NoError(t, err)
				expected := affected
				expected.Revision++
				require.Equal(t, expected, *got)
				other, err := reopened.Get(ctx, provider, untouched.ID)
				require.NoError(t, err)
				require.Equal(t, untouched, *other)
				require.Equal(t, "default", routing.ResolveDecision(*got, "unmatched-model", false).Kind)
				require.Equal(t, int64(1), routing.ResolveDecision(*got, "kept-model", true).RuleID)
				var count int
				for _, query := range []string{
					`SELECT COUNT(*) FROM sqlite_master WHERE name IN ('routing_profile_intents','routing_profile_classifier_config')`,
					`SELECT COUNT(*) FROM pragma_table_info('routing_profile_rules') WHERE name='intent_id'`,
				} {
					require.NoError(t, db.QueryRow(query).Scan(&count))
					require.Zero(t, count)
				}
				require.NoError(t, db.QueryRow(`SELECT COUNT(*) FROM routing_affinity WHERE profile_id=?`, affected.ID).Scan(&count))
				require.Zero(t, count)
				require.NoError(t, db.QueryRow(`SELECT COUNT(*) FROM routing_affinity WHERE profile_id=?`, untouched.ID).Scan(&count))
				require.Equal(t, 1, count)
				var payload string
				require.NoError(t, db.QueryRow(`SELECT payload FROM routing_dispatch_traces WHERE request_id='old-request'`).Scan(&payload))
				require.Equal(t, historical, payload)
				gotBalance, err := reopened.ManualBalance(ctx, provider, 1, 1)
				require.NoError(t, err)
				require.Equal(t, balance, gotBalance)
				require.NoError(t, db.QueryRow(`SELECT COUNT(*) FROM routing_balance_entries`).Scan(&count))
				require.Equal(t, 1, count)
			}
		})
	}
}

func TestIntentMigrationFailureRollsBackSchemaRulesRevisionAndAffinity(t *testing.T) {
	s, db, affected, _ := legacyIntentStore(t, "claude")
	_, err := db.Exec(`CREATE TRIGGER block_retired_rule_delete BEFORE DELETE ON routing_profile_rules
WHEN OLD.intent_id<>'' BEGIN SELECT RAISE(ABORT,'migration fault'); END;`)
	require.NoError(t, err)
	require.ErrorContains(t, s.EnsureSchema(context.Background()), "migration fault")
	var revision int64
	require.NoError(t, db.QueryRow(`SELECT revision FROM routing_profiles WHERE id=?`, affected.ID).Scan(&revision))
	require.Equal(t, affected.Revision, revision)
	for _, tc := range []struct {
		query string
		want  int
	}{
		{`SELECT COUNT(*) FROM routing_affinity`, 2},
		{`SELECT COUNT(*) FROM routing_profile_rules WHERE intent_id<>''`, 5},
		{`SELECT COUNT(*) FROM routing_profile_intents`, 1},
		{`SELECT COUNT(*) FROM routing_profile_classifier_config`, 1},
	} {
		var count int
		require.NoError(t, db.QueryRow(tc.query).Scan(&count))
		require.Equal(t, tc.want, count)
	}
	_, err = db.Exec(`DROP TRIGGER block_retired_rule_delete`)
	require.NoError(t, err)
	require.NoError(t, s.EnsureSchema(context.Background()))
}
