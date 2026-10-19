package store

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	_ "modernc.org/sqlite"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"ai-relay-forwarder/internal/providers/codex/proxy/subscription"
)

func TestSubscriptionMigrationAndConfigurationPreserveQuota(t *testing.T) {
	db := newCodexSubscriptionTestDB(t)
	defer db.Close()
	ctx := context.Background()
	if err := EnsureCodexSubscriptionSchema(ctx, db); err != nil {
		t.Fatal(err)
	}
	groups := NewSQLiteCodexCredentialGroupStore(db)
	pauses := NewSQLiteCodexSubscriptionStore(db)
	legacy, err := groups.Create(ctx, &CodexCredentialGroupRecord{ChannelID: 1, Name: "old", ApiKey: "existing-key"})
	if err != nil {
		t.Fatal(err)
	}
	if legacy.AuthMode != "group_credential" {
		loaded, err := groups.GetByID(ctx, legacy.ID)
		if err != nil || loaded.AuthMode != "group_credential" {
			t.Fatal("default mode changed")
		}
	}
	digest := sha256.Sum256([]byte("local-key-sentinel"))
	record, err := groups.Create(ctx, &CodexCredentialGroupRecord{ChannelID: 1, Name: "caller-a", AuthMode: subscription.ClientPassthrough, Subscription: &subscription.Binding{Version: 1, Enabled: true, KeyHash: hex.EncodeToString(digest[:])}})
	if err != nil {
		t.Fatal(err)
	}
	pause := subscription.Pause{GroupID: record.ID, Version: 1, Model: "gpt-test", ResumeNotBefore: time.Now().Add(time.Hour), ResetKnown: true, Revision: 1}
	if err := pauses.PutPause(ctx, pause); err != nil {
		t.Fatal(err)
	}
	record.Subscription.Version = 2
	record.Subscription.Enabled = false
	if err := groups.Update(ctx, record); err != nil {
		t.Fatal(err)
	}
	binding := record.Subscription
	binding.GroupID = record.ID
	controller := subscription.NewController(NewSQLiteCodexSubscriptionStore(db))
	if state := controller.Status(ctx, *binding); len(state) != 1 || state[0].State != "manual_disabled" {
		t.Fatalf("disable erased pause: %+v", state)
	}
	// Stale in-flight responses cannot overwrite the new binding or revision.
	pause.Revision = 100
	pause.ResumeNotBefore = time.Now().Add(10 * time.Hour)
	if err := pauses.PutPause(ctx, pause); err != nil {
		t.Fatal(err)
	}
	record.Subscription.Version = 3
	record.Subscription.Enabled = true
	record.Subscription.Action = "verify"
	if err := groups.Update(ctx, record); err != nil {
		t.Fatal(err)
	}
	binding = record.Subscription
	binding.GroupID = record.ID
	if state := controller.Status(ctx, *binding); len(state) != 1 || state[0].State != "due_unverified" || state[0].ResetKnown {
		t.Fatalf("manual verify claimed recovery: %+v", state)
	}
	// Restart and a renamed endpoint still use the same persisted group binding.
	restarted := subscription.NewController(NewSQLiteCodexSubscriptionStore(db))
	if _, err := restarted.Acquire(ctx, *binding, "gpt-test"); err != nil {
		t.Fatal(err)
	}
	record.Subscription.Version = 4
	record.Subscription.Action = "rebind"
	if err := groups.Update(ctx, record); err != nil {
		t.Fatal(err)
	}
	binding = record.Subscription
	binding.GroupID = record.ID
	if state := controller.Status(ctx, *binding); len(state) != 0 {
		t.Fatal("new identity inherited old pause")
	}
	var raw string
	if err := db.QueryRow(`SELECT subscription_config FROM codex_credential_groups WHERE id=?`, record.ID).Scan(&raw); err != nil {
		t.Fatal(err)
	}
	if raw == "" || containsSecret(raw, "local-key-sentinel") {
		t.Fatal("raw local key persisted")
	}
}

func containsSecret(raw, key string) bool {
	for i := 0; i+len(key) <= len(raw); i++ {
		if raw[i:i+len(key)] == key {
			return true
		}
	}
	return false
}

func TestSubscriptionPauseRevisionPreventsStaleDelete(t *testing.T) {
	db := newCodexSubscriptionTestDB(t)
	defer db.Close()
	ctx := context.Background()
	groups := NewSQLiteCodexCredentialGroupStore(db)
	record, err := groups.Create(ctx, &CodexCredentialGroupRecord{ChannelID: 1, Name: "caller", AuthMode: subscription.ClientPassthrough, Subscription: &subscription.Binding{Version: 1, Enabled: true}})
	if err != nil {
		t.Fatal(err)
	}
	store := NewSQLiteCodexSubscriptionStore(db)
	p := subscription.Pause{GroupID: record.ID, Version: 1, Model: "x", Revision: 1}
	if err := store.PutPause(ctx, p); err != nil {
		t.Fatal(err)
	}
	newer := p
	newer.Revision = 2
	newer.Reason = "new-quota"
	store.PutPause(ctx, newer)
	store.PutPause(ctx, p)
	store.DeletePause(ctx, p)
	got, err := store.ListPauses(ctx)
	if err != nil || len(got) != 1 || got[0].Reason != "new-quota" {
		t.Fatalf("old response won: %+v %v", got, err)
	}
}

func newCodexSubscriptionTestDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "subscription.db"))
	if err != nil {
		t.Fatal(err)
	}
	schema, err := os.ReadFile(filepath.Join("..", "..", "..", "tracking", "schema.sql"))
	if err != nil {
		t.Fatal(err)
	}
	// Exercise the production migration from the previous Codex group schema.
	start := strings.Index(string(schema), "CREATE TABLE IF NOT EXISTS codex_credential_groups")
	text := string(schema[:start]) + strings.ReplaceAll(strings.ReplaceAll(string(schema[start:]), "    auth_mode TEXT NOT NULL DEFAULT 'group_credential',\n", ""), "    subscription_config TEXT NOT NULL DEFAULT '{}',\n", "")
	if _, err = db.Exec(text); err != nil {
		t.Fatal(err)
	}
	if err = EnsureCodexSubscriptionSchema(context.Background(), db); err != nil {
		t.Fatal(err)
	}
	if err = EnsureCodexSubscriptionSchema(context.Background(), db); err != nil {
		t.Fatal(err)
	}
	if _, err = NewSQLiteCodexChannelStore(db).Create(context.Background(), &CodexChannelRecord{Name: "official", FailoverEnabled: true}); err != nil {
		t.Fatal(err)
	}
	return db
}
