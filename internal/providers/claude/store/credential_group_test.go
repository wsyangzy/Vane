package store

import (
	"context"
	"database/sql"
	"testing"

	_ "modernc.org/sqlite"
)

func newClaudeCredentialGroupTestDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.Exec(`CREATE TABLE claude_credential_groups (
		id INTEGER PRIMARY KEY AUTOINCREMENT, channel TEXT NOT NULL, name TEXT NOT NULL,
		token TEXT DEFAULT '', api_key TEXT DEFAULT '', is_default INTEGER DEFAULT 0,
		daily_cost_limit_usd REAL DEFAULT 0, model_policy TEXT DEFAULT 'inherit',
		model_mapping TEXT DEFAULT '{}', supported_models TEXT DEFAULT '[]',
		cost_multiplier REAL DEFAULT 1, input_cost_multiplier REAL DEFAULT 1, output_cost_multiplier REAL DEFAULT 1,
		cache_creation_cost_multiplier REAL DEFAULT 1, cache_creation_cost_multiplier_1h REAL DEFAULT 1,
		cache_read_cost_multiplier REAL DEFAULT 1, long_context_cost_multiplier REAL DEFAULT 1,
		long_context_input_cost_multiplier REAL DEFAULT 1, long_context_output_cost_multiplier REAL DEFAULT 1,
		long_context_cache_creation_cost_multiplier REAL DEFAULT 1, long_context_cache_read_cost_multiplier REAL DEFAULT 1,
		disable_long_context_pricing INTEGER DEFAULT 1, disable_service_tier_pricing INTEGER DEFAULT 1,
		service_tier_cost_multiplier REAL DEFAULT 1, service_tier_input_cost_multiplier REAL DEFAULT 1,
		service_tier_output_cost_multiplier REAL DEFAULT 1, service_tier_cache_read_cost_multiplier REAL DEFAULT 1,
		model_multipliers TEXT DEFAULT '[]', created_at TEXT DEFAULT '', updated_at TEXT DEFAULT '')`)
	if err != nil {
		db.Close()
		t.Fatal(err)
	}
	return db
}

func TestClaudeCredentialGroupModelPolicyRoundTrip(t *testing.T) {
	db := newClaudeCredentialGroupTestDB(t)
	defer db.Close()
	groupStore := NewSQLiteClaudeCredentialGroupStore(db)
	created, err := groupStore.Create(context.Background(), &ClaudeCredentialGroupRecord{
		Channel: "claude", Name: "mini", ModelPolicy: ClaudeModelPolicyOverride,
		ModelMapping: map[string]string{"claude-small": "claude-haiku"}, SupportedModels: []string{"claude-haiku", "claude-haiku"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if created.ModelPolicy != ClaudeModelPolicyOverride || created.ModelMapping["claude-small"] != "claude-haiku" || len(created.SupportedModels) != 1 {
		t.Fatalf("unexpected model policy round trip: %#v", created)
	}

	created.ModelPolicy = ClaudeModelPolicyPassthrough
	created.ModelMapping = nil
	created.SupportedModels = []string{"claude-opus"}
	if err := groupStore.Update(context.Background(), created); err != nil {
		t.Fatal(err)
	}
	updated, err := groupStore.GetByID(context.Background(), created.ID)
	if err != nil {
		t.Fatal(err)
	}
	if updated.ModelPolicy != ClaudeModelPolicyPassthrough || updated.ModelMapping == nil || len(updated.ModelMapping) != 0 || len(updated.SupportedModels) != 1 {
		t.Fatalf("unexpected updated model policy: %#v", updated)
	}
}
