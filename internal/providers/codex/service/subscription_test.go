package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"testing"

	"github.com/wsyangzy/Vane/internal/providers/codex/proxy/subscription"
	"github.com/wsyangzy/Vane/internal/providers/codex/store"
)

func TestSubscriptionCodexGroupLifecycleAndEndpointBoundary(t *testing.T) {
	db := newCodexEndpointTestDB(t)
	ctx := context.Background()
	channels := store.NewSQLiteCodexChannelStore(db)
	groups := store.NewSQLiteCodexCredentialGroupStore(db)
	eps := store.NewSQLiteCodexEndpointStore(db)
	svc := NewCodexCredentialGroupService(groups, channels, eps)
	channel, e := channels.Create(ctx, &store.CodexChannelRecord{Name: "official", FailoverEnabled: true})
	if e != nil {
		t.Fatal(e)
	}
	hash := sha256.Sum256([]byte("local-caller-key"))
	binding := func() *subscription.Binding {
		return &subscription.Binding{Enabled: true, KeyHash: hex.EncodeToString(hash[:])}
	}
	if _, e = svc.CreateGroup(ctx, &store.CodexCredentialGroupRecord{ChannelID: channel.ID, Name: "bad", ApiKey: "oauth-must-not-store", AuthMode: subscription.ClientPassthrough, Subscription: binding()}); e == nil {
		t.Fatal("subscription credential accepted as API key")
	}
	created, e := svc.CreateGroup(ctx, &store.CodexCredentialGroupRecord{ChannelID: channel.ID, Name: "caller", AuthMode: subscription.ClientPassthrough, Subscription: binding()})
	if e != nil {
		t.Fatal(e)
	}
	if e = svc.UpdateGroup(ctx, created.ID, &store.CodexCredentialGroupRecord{Name: "renamed"}); e != nil {
		t.Fatal(e)
	}
	loaded, e := svc.GetByID(ctx, created.ID)
	if e != nil || loaded.AuthMode != subscription.ClientPassthrough || loaded.Subscription.Version != 1 || loaded.Subscription.KeyHash != created.Subscription.KeyHash {
		t.Fatal("legacy edit erased subscription fields")
	}
	if _, e = svc.CreateGroup(ctx, &store.CodexCredentialGroupRecord{ChannelID: channel.ID, Name: "duplicate", AuthMode: subscription.ClientPassthrough, Subscription: binding()}); e == nil {
		t.Fatal("shared local caller accepted")
	}
	endpointSvc := NewCodexEndpointService(eps, channels, groups)
	if _, e = endpointSvc.CreateEndpoint(ctx, &store.CodexEndpointRecord{ChannelID: channel.ID, CredentialGroupID: created.ID, Name: "unsafe", BaseURL: "https://evil.invalid"}); e == nil {
		t.Fatal("unsafe subscription origin accepted")
	}
	ep, e := endpointSvc.CreateEndpoint(ctx, &store.CodexEndpointRecord{ChannelID: channel.ID, CredentialGroupID: created.ID, Name: "official", BaseURL: subscription.OfficialBaseURL, FailoverEnabled: true})
	if e != nil {
		t.Fatal(e)
	}
	if e = endpointSvc.UpdateEndpointByID(ctx, ep.ID, ep.Name, "https://chatgpt.com.evil.invalid/backend-api/codex", created.ID, "", true, false, 0, 0, 1, 1, 1, 1, "output_tokens", nil); e == nil {
		t.Fatal("unsafe endpoint edit accepted")
	}
	loaded.Subscription.Version = 999
	if e = svc.UpdateGroup(ctx, created.ID, loaded); e == nil {
		t.Fatal("stale binding edit accepted")
	}
}

func TestCodexManagedRebindCannotClearSameAccountQuota(t *testing.T) {
	svc := &CodexCredentialGroupService{}
	old := &store.CodexCredentialGroupRecord{Subscription: &subscription.Binding{AccountID: "same-account"}}
	next := &store.CodexCredentialGroupRecord{AuthMode: subscription.ClientPassthrough, Subscription: &subscription.Binding{AccountID: "same-account", Action: "rebind"}}
	if err := svc.normalizeSubscription(context.Background(), next, old); err == nil {
		t.Fatal("same managed account could clear quota by rebind")
	}
}

func TestSubscriptionCodexManagedAccountMustBeIndependentAndProviderValid(t *testing.T) {
	db := newCodexEndpointTestDB(t)
	ctx := context.Background()
	channels := store.NewSQLiteCodexChannelStore(db)
	groups := store.NewSQLiteCodexCredentialGroupStore(db)
	svc := NewCodexCredentialGroupService(groups, channels, nil)
	channel, e := channels.Create(ctx, &store.CodexChannelRecord{Name: "official"})
	if e != nil {
		t.Fatal(e)
	}
	svc.SetAccountValidator(func(_ context.Context, id string) error {
		if id != "account-a" {
			return errors.New("wrong provider or disabled")
		}
		return nil
	})
	create := func(name, key, id string) error {
		hash := sha256.Sum256([]byte(key))
		_, e := svc.CreateGroup(ctx, &store.CodexCredentialGroupRecord{ChannelID: channel.ID, Name: name, AuthMode: subscription.ClientPassthrough, Subscription: &subscription.Binding{Enabled: true, AccountID: id, KeyHash: hex.EncodeToString(hash[:])}})
		return e
	}
	if e = create("caller-a", "key-a", "account-a"); e != nil {
		t.Fatal(e)
	}
	if e = create("caller-b", "key-b", "account-a"); e == nil {
		t.Fatal("same managed account shared between callers")
	}
	if e = create("caller-c", "key-c", "wrong-provider"); e == nil {
		t.Fatal("provider mismatch accepted")
	}
}

func TestCodexSubscriptionGroupsDoNotRequireCallerKeys(t *testing.T) {
	db := newCodexEndpointTestDB(t)
	ctx := context.Background()
	channels := store.NewSQLiteCodexChannelStore(db)
	channel, err := channels.Create(ctx, &store.CodexChannelRecord{Name: "official"})
	if err != nil {
		t.Fatal(err)
	}
	svc := NewCodexCredentialGroupService(store.NewSQLiteCodexCredentialGroupStore(db), channels, nil)
	for _, name := range []string{"first", "second"} {
		group, err := svc.CreateGroup(ctx, &store.CodexCredentialGroupRecord{
			ChannelID: channel.ID, Name: name, AuthMode: subscription.ClientPassthrough,
			Subscription: &subscription.Binding{Enabled: true},
		})
		if err != nil {
			t.Fatal(err)
		}
		if group.Subscription.KeyHash != "" || group.Subscription.Version != 1 {
			t.Fatalf("unexpected binding: %+v", group.Subscription)
		}
	}
}
