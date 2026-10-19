package store

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"testing"
	"time"

	"ai-relay-forwarder/internal/providers/claude/proxy/subscription"
)

func TestSubscriptionMigrationAndConfigurationPreserveQuota(t *testing.T) {
	db := newClaudeCredentialGroupTestDB(t)
	defer db.Close()
	ctx := context.Background()
	if err := EnsureClaudeSubscriptionSchema(ctx, db); err != nil {
		t.Fatal(err)
	}
	groups := NewSQLiteClaudeCredentialGroupStore(db)
	pauses := NewSQLiteClaudeSubscriptionStore(db)
	legacy, err := groups.Create(ctx, &ClaudeCredentialGroupRecord{Channel: "legacy", Name: "old", Token: "existing-key"})
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
	record, err := groups.Create(ctx, &ClaudeCredentialGroupRecord{Channel: "official", Name: "caller-a", AuthMode: subscription.ClientPassthrough, Subscription: &subscription.Binding{Version: 1, Enabled: true, KeyHash: hex.EncodeToString(digest[:])}})
	if err != nil {
		t.Fatal(err)
	}
	pause := subscription.Pause{GroupID: record.ID, Version: 1, Model: "family:opus", ResumeNotBefore: time.Now().Add(time.Hour), ResetKnown: true, Revision: 1}
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
	controller := subscription.NewController(NewSQLiteClaudeSubscriptionStore(db))
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
	restarted := subscription.NewController(NewSQLiteClaudeSubscriptionStore(db))
	if _, err := restarted.Acquire(ctx, *binding, "claude-opus-4-6"); err != nil {
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
	if err := db.QueryRow(`SELECT subscription_config FROM claude_credential_groups WHERE id=?`, record.ID).Scan(&raw); err != nil {
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
	db := newClaudeCredentialGroupTestDB(t)
	defer db.Close()
	ctx := context.Background()
	groups := NewSQLiteClaudeCredentialGroupStore(db)
	record, err := groups.Create(ctx, &ClaudeCredentialGroupRecord{Channel: "a", Name: "caller", AuthMode: subscription.ClientPassthrough, Subscription: &subscription.Binding{Version: 1, Enabled: true}})
	if err != nil {
		t.Fatal(err)
	}
	store := NewSQLiteClaudeSubscriptionStore(db)
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
