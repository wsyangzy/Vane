package app

import (
	"context"
	"database/sql"
	"encoding/json"
	"strings"
	"testing"

	"github.com/wsyangzy/Vane/internal/providers/claude/proxy/subscription"
	claudestore "github.com/wsyangzy/Vane/internal/providers/claude/store"
	codexsubscription "github.com/wsyangzy/Vane/internal/providers/codex/proxy/subscription"
	codexstore "github.com/wsyangzy/Vane/internal/providers/codex/store"
	"github.com/wsyangzy/Vane/internal/routing"
)

func TestClaudeSubscriptionDTOOnlyReturnsPublicBinding(t *testing.T) {
	group := &claudestore.ClaudeCredentialGroupRecord{ID: 7, Channel: "official", Name: "independent caller", AuthMode: subscription.ClientPassthrough, Subscription: &subscription.Binding{Version: 3, Enabled: true, KeyHash: strings.Repeat("a", 64), Action: "rebind", BackupTargets: []routing.Target{{ChannelID: 2, GroupID: 8}}}}
	app := &App{}
	info := app.claudeCredentialGroupToInfo(context.Background(), nil, group)
	raw, err := json.Marshal(info)
	if err != nil {
		t.Fatal(err)
	}
	if !info.LocalKeyConfigured || info.Subscription == nil || info.Subscription.GroupID != 7 || info.Subscription.Version != 3 || info.OfficialVerification != "not_verified" {
		t.Fatalf("public fields missing: %+v", info)
	}
	if strings.Contains(string(raw), group.Subscription.KeyHash) || strings.Contains(string(raw), "key_hash") || strings.Contains(string(raw), "rebind") {
		t.Fatal("private/write-only field escaped DTO")
	}
	if group.Subscription.KeyHash == "" || group.Subscription.Action != "rebind" {
		t.Fatal("mapping mutated persisted group")
	}
	record := claudeCredentialGroupInputToRecord(CreateClaudeCredentialGroupInput{AuthMode: subscription.ClientPassthrough, Subscription: &subscription.Binding{Version: 3, KeyHash: strings.Repeat("b", 64), Enabled: false}})
	if record.AuthMode != subscription.ClientPassthrough || record.Subscription.Version != 3 || record.Subscription.Enabled {
		t.Fatal("input fields lost")
	}
}

func TestCodexSubscriptionDTOOnlyReturnsPublicBinding(t *testing.T) {
	group := &codexstore.CodexCredentialGroupRecord{ID: 7, ChannelID: 3, AuthMode: codexsubscription.ClientPassthrough, Subscription: &codexsubscription.Binding{Version: 2, AccountID: "managed-id", Enabled: true, KeyHash: strings.Repeat("a", 64), Action: "rebind"}}
	info := buildCodexCredentialGroupInfo(group)
	raw, err := json.Marshal(info)
	if err != nil {
		t.Fatal(err)
	}
	if !info.LocalKeyConfigured || info.Subscription == nil || info.Subscription.AccountID != "managed-id" || info.OfficialVerification != "not_verified" {
		t.Fatal("public binding lost")
	}
	if strings.Contains(string(raw), group.Subscription.KeyHash) || strings.Contains(string(raw), "key_hash") || strings.Contains(string(raw), "rebind") {
		t.Fatal("private field escaped DTO")
	}
	if group.Subscription.KeyHash == "" || group.Subscription.Action != "rebind" {
		t.Fatal("mapping mutated binding")
	}
}

func TestAppCodexSchemaIncludesSubscriptionOnEveryInitializationPath(t *testing.T) {
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	app := &App{storeDB: db}
	for i := 0; i < 2; i++ {
		if err := app.ensureCodexProviderSchema(t.Context()); err != nil {
			t.Fatal(err)
		}
	}
	result, err := db.Exec(`INSERT INTO codex_channels (name, provider_type, api_key, model, is_active) VALUES ('official-subscription', 'openai-compatible', '', '', 0)`)
	if err != nil {
		t.Fatal(err)
	}
	channelID, err := result.LastInsertId()
	if err != nil {
		t.Fatal(err)
	}
	result, err = db.Exec(`INSERT INTO codex_credential_groups (channel_id, name, is_default) VALUES (?, 'caller', 1)`, channelID)
	if err != nil {
		t.Fatal(err)
	}
	groupID, err := result.LastInsertId()
	if err != nil {
		t.Fatal(err)
	}
	group, err := codexstore.NewSQLiteCodexCredentialGroupStore(db).GetByID(t.Context(), groupID)
	if err != nil || group.AuthMode != codexsubscription.GroupCredential {
		t.Fatalf("App schema cannot read default auth mode: %v", err)
	}
	if _, err = db.Exec(`SELECT payload FROM codex_subscription_pauses LIMIT 0`); err != nil {
		t.Fatal(err)
	}
}
