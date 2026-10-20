package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"testing"

	"github.com/wsyangzy/Vane/internal/providers/claude/proxy/subscription"
	"github.com/wsyangzy/Vane/internal/providers/claude/store"
)

func TestSubscriptionGroupValidationAndLegacyFieldPreservation(t *testing.T) {
	db, cleanup := createEndpointServiceTestDB(t)
	defer cleanup()
	groups := store.NewSQLiteClaudeCredentialGroupStore(db)
	svc := NewClaudeCredentialGroupService(groups, nil, store.NewSQLiteEndpointStore(db))
	ctx := context.Background()
	hash := sha256.Sum256([]byte("caller-key"))
	binding := func() *subscription.Binding {
		return &subscription.Binding{Enabled: true, KeyHash: hex.EncodeToString(hash[:])}
	}
	bad := &store.ClaudeCredentialGroupRecord{Channel: "official", Name: "bad", AuthMode: subscription.ClientPassthrough, Token: "oauth-must-not-store", Subscription: binding()}
	if _, err := svc.CreateGroup(ctx, bad); err == nil {
		t.Fatal("OAuth accepted for persistence")
	}
	good := &store.ClaudeCredentialGroupRecord{Channel: "official", Name: "caller", AuthMode: subscription.ClientPassthrough, Subscription: binding()}
	created, err := svc.CreateGroup(ctx, good)
	if err != nil {
		t.Fatal(err)
	}
	// An older caller editing an unrelated property omits the new fields.
	update := &store.ClaudeCredentialGroupRecord{Name: "renamed"}
	if err := svc.UpdateGroup(ctx, created.ID, update); err != nil {
		t.Fatal(err)
	}
	loaded, err := svc.GetByID(ctx, created.ID)
	if err != nil || loaded.AuthMode != subscription.ClientPassthrough || loaded.Subscription.KeyHash != good.Subscription.KeyHash || loaded.Subscription.Version != 1 {
		t.Fatalf("new fields erased: %+v %v", loaded, err)
	}
	loaded.Subscription.Version = 999
	if err := svc.UpdateGroup(ctx, created.ID, loaded); err == nil {
		t.Fatal("stale config accepted")
	}
	// Endpoint storage cannot convert a fixed official origin into a third party.
	endpointSvc := NewEndpointService(store.NewSQLiteEndpointStore(db), nil, nil, groups)
	if err := endpointSvc.ensureEndpointCredentialGroup(ctx, &store.EndpointRecord{Channel: "official", CredentialGroupID: created.ID, URL: "https://evil.invalid"}); err == nil {
		t.Fatal("unsafe official URL accepted")
	}
}

func TestClaudeManagedRebindCannotClearSameAccountQuota(t *testing.T) {
	svc := &ClaudeCredentialGroupService{}
	old := &store.ClaudeCredentialGroupRecord{Subscription: &subscription.Binding{AccountID: "same-account"}}
	next := &store.ClaudeCredentialGroupRecord{AuthMode: subscription.ClientPassthrough, Subscription: &subscription.Binding{AccountID: "same-account", Action: "rebind"}}
	if err := svc.normalizeSubscription(context.Background(), next, old); err == nil {
		t.Fatal("same managed account could clear quota by rebind")
	}
}
