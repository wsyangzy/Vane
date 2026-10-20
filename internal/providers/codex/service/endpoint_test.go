package service

import (
	"context"
	"database/sql"
	"strings"
	"testing"

	"github.com/wsyangzy/Vane/internal/providers/codex/store"

	_ "modernc.org/sqlite"
)

func TestCodexEndpointAllowsDuplicateNameAndURLAcrossCredentialGroups(t *testing.T) {
	ctx := context.Background()
	db := newCodexEndpointTestDB(t)
	channelStore := store.NewSQLiteCodexChannelStore(db)
	groupStore := store.NewSQLiteCodexCredentialGroupStore(db)
	endpointStore := store.NewSQLiteCodexEndpointStore(db)
	endpointService := NewCodexEndpointService(endpointStore, channelStore, groupStore)

	channel, err := channelStore.Create(ctx, &store.CodexChannelRecord{
		Name:         "anyrouter",
		ProviderType: "openai-compatible",
	})
	if err != nil {
		t.Fatalf("create channel: %v", err)
	}
	defaultGroup, err := groupStore.Create(ctx, &store.CodexCredentialGroupRecord{
		ChannelID: channel.ID,
		Name:      "default",
		ApiKey:    "sk-default",
		IsDefault: true,
	})
	if err != nil {
		t.Fatalf("create default group: %v", err)
	}
	otherGroup, err := groupStore.Create(ctx, &store.CodexCredentialGroupRecord{
		ChannelID: channel.ID,
		Name:      "github",
		ApiKey:    "sk-github",
	})
	if err != nil {
		t.Fatalf("create other group: %v", err)
	}

	first, err := endpointService.CreateEndpoint(ctx, &store.CodexEndpointRecord{
		ChannelID:         channel.ID,
		CredentialGroupID: defaultGroup.ID,
		Name:              "大陆网络优化",
		BaseURL:           "https://a-ocnfniawgw.cn-shanghai.fcapp.run",
		FailoverEnabled:   true,
	})
	if err != nil {
		t.Fatalf("create first endpoint: %v", err)
	}
	second, err := endpointService.CreateEndpoint(ctx, &store.CodexEndpointRecord{
		ChannelID:         channel.ID,
		CredentialGroupID: otherGroup.ID,
		Name:              first.Name,
		BaseURL:           first.BaseURL,
		FailoverEnabled:   true,
	})
	if err != nil {
		t.Fatalf("expected same name/base_url in another credential group to be allowed, got: %v", err)
	}
	if second.CredentialGroupID != otherGroup.ID {
		t.Fatalf("expected second endpoint group %d, got %d", otherGroup.ID, second.CredentialGroupID)
	}

	_, err = endpointService.CreateEndpoint(ctx, &store.CodexEndpointRecord{
		ChannelID:         channel.ID,
		CredentialGroupID: otherGroup.ID,
		Name:              first.Name,
		BaseURL:           "https://another.example.invalid/codex/v1",
		FailoverEnabled:   true,
	})
	if err == nil || !strings.Contains(err.Error(), "相同的端点名") {
		t.Fatalf("expected duplicate name in same credential group to be rejected, got: %v", err)
	}
}

func TestNormalizeAndValidateCodexEndpointAllowsInternalSpaces(t *testing.T) {
	cases := []string{
		"主站直连",
		"主站 直连",
		"ark api",
	}

	for _, name := range cases {
		t.Run(name, func(t *testing.T) {
			record := &store.CodexEndpointRecord{
				ChannelID:       1,
				Name:            name,
				BaseURL:         "https://example.invalid/codex/v1",
				FailoverEnabled: true,
			}
			if err := normalizeAndValidateCodexEndpoint(record); err != nil {
				t.Fatalf("expected endpoint name %q to be valid, got: %v", name, err)
			}
		})
	}
}

func TestCodexEndpointCreateAutoAssignsNegativePriority(t *testing.T) {
	ctx := context.Background()
	db := newCodexEndpointTestDB(t)
	channelStore := store.NewSQLiteCodexChannelStore(db)
	groupStore := store.NewSQLiteCodexCredentialGroupStore(db)
	endpointStore := store.NewSQLiteCodexEndpointStore(db)
	endpointService := NewCodexEndpointService(endpointStore, channelStore, groupStore)

	channel, err := channelStore.Create(ctx, &store.CodexChannelRecord{
		Name:         "anyrouter",
		ProviderType: "openai-compatible",
	})
	if err != nil {
		t.Fatalf("create channel: %v", err)
	}
	defaultGroup, err := groupStore.Create(ctx, &store.CodexCredentialGroupRecord{
		ChannelID: channel.ID,
		Name:      "default",
		ApiKey:    "sk-default",
		IsDefault: true,
	})
	if err != nil {
		t.Fatalf("create default group: %v", err)
	}

	first, err := endpointService.CreateEndpoint(ctx, &store.CodexEndpointRecord{
		ChannelID:         channel.ID,
		CredentialGroupID: defaultGroup.ID,
		Name:              "first",
		BaseURL:           "https://example.invalid/first/v1",
		FailoverEnabled:   true,
		Priority:          -1,
	})
	if err != nil {
		t.Fatalf("create first endpoint: %v", err)
	}
	second, err := endpointService.CreateEndpoint(ctx, &store.CodexEndpointRecord{
		ChannelID:         channel.ID,
		CredentialGroupID: defaultGroup.ID,
		Name:              "second",
		BaseURL:           "https://example.invalid/second/v1",
		FailoverEnabled:   true,
		Priority:          -1,
	})
	if err != nil {
		t.Fatalf("create second endpoint: %v", err)
	}

	if first.Priority != 0 || second.Priority != 1 {
		t.Fatalf("expected priorities 0 and 1, got %d and %d", first.Priority, second.Priority)
	}
}

func TestCodexEndpointCreateKeepsExplicitZeroPriority(t *testing.T) {
	ctx := context.Background()
	db := newCodexEndpointTestDB(t)
	channelStore := store.NewSQLiteCodexChannelStore(db)
	groupStore := store.NewSQLiteCodexCredentialGroupStore(db)
	endpointStore := store.NewSQLiteCodexEndpointStore(db)
	endpointService := NewCodexEndpointService(endpointStore, channelStore, groupStore)

	channel, err := channelStore.Create(ctx, &store.CodexChannelRecord{
		Name:         "anyrouter",
		ProviderType: "openai-compatible",
	})
	if err != nil {
		t.Fatalf("create channel: %v", err)
	}
	defaultGroup, err := groupStore.Create(ctx, &store.CodexCredentialGroupRecord{
		ChannelID: channel.ID,
		Name:      "default",
		ApiKey:    "sk-default",
		IsDefault: true,
	})
	if err != nil {
		t.Fatalf("create default group: %v", err)
	}

	created, err := endpointService.CreateEndpoint(ctx, &store.CodexEndpointRecord{
		ChannelID:         channel.ID,
		CredentialGroupID: defaultGroup.ID,
		Name:              "first",
		BaseURL:           "https://example.invalid/first/v1",
		FailoverEnabled:   true,
		Priority:          0,
	})
	if err != nil {
		t.Fatalf("create endpoint: %v", err)
	}
	if created.Priority != 0 {
		t.Fatalf("expected explicit priority 0, got %d", created.Priority)
	}
}

func TestCodexEndpointRestoreRevertsPrioritySwap(t *testing.T) {
	ctx := context.Background()
	db := newCodexEndpointTestDB(t)
	channelStore := store.NewSQLiteCodexChannelStore(db)
	groupStore := store.NewSQLiteCodexCredentialGroupStore(db)
	endpointStore := store.NewSQLiteCodexEndpointStore(db)
	endpointService := NewCodexEndpointService(endpointStore, channelStore, groupStore)

	channel, err := channelStore.Create(ctx, &store.CodexChannelRecord{Name: "restore-endpoint", ProviderType: "openai-compatible"})
	if err != nil {
		t.Fatalf("create channel: %v", err)
	}
	group, err := groupStore.Create(ctx, &store.CodexCredentialGroupRecord{ChannelID: channel.ID, Name: "default", ApiKey: "sk-test", IsDefault: true})
	if err != nil {
		t.Fatalf("create group: %v", err)
	}
	first, err := endpointService.CreateEndpoint(ctx, &store.CodexEndpointRecord{
		ChannelID: channel.ID, CredentialGroupID: group.ID, Name: "first", BaseURL: "https://first.example.invalid/v1", FailoverEnabled: true, Priority: 0,
	})
	if err != nil {
		t.Fatalf("create first endpoint: %v", err)
	}
	second, err := endpointService.CreateEndpoint(ctx, &store.CodexEndpointRecord{
		ChannelID: channel.ID, CredentialGroupID: group.ID, Name: "second", BaseURL: "https://second.example.invalid/v1", FailoverEnabled: true, Priority: 1,
	})
	if err != nil {
		t.Fatalf("create second endpoint: %v", err)
	}
	firstSnapshot := *first
	secondSnapshot := *second

	if err := endpointService.UpdateEndpointByID(ctx, first.ID, first.Name, first.BaseURL, first.CredentialGroupID, first.ApiKey, first.FailoverEnabled, first.SkipHealthCheck, 1, first.TimeoutSeconds, first.CostMultiplier, first.InputCostMultiplier, first.OutputCostMultiplier, first.CacheReadCostMultiplier, first.BillingOutputTokenSource, first.ModelMultipliers); err != nil {
		t.Fatalf("swap endpoint priorities: %v", err)
	}
	if err := endpointService.RestoreEndpoint(ctx, &firstSnapshot); err != nil {
		t.Fatalf("restore first endpoint: %v", err)
	}
	if err := endpointService.RestoreEndpoint(ctx, &secondSnapshot); err != nil {
		t.Fatalf("restore second endpoint: %v", err)
	}

	restoredFirst, err := endpointStore.GetByID(ctx, first.ID)
	if err != nil {
		t.Fatalf("reload first endpoint: %v", err)
	}
	restoredSecond, err := endpointStore.GetByID(ctx, second.ID)
	if err != nil {
		t.Fatalf("reload second endpoint: %v", err)
	}
	if restoredFirst.Priority != 0 || restoredSecond.Priority != 1 {
		t.Fatalf("restored priorities = %d/%d, want 0/1", restoredFirst.Priority, restoredSecond.Priority)
	}
	if !restoredFirst.IsActive || restoredSecond.IsActive {
		t.Fatalf("restored active state = %v/%v, want true/false", restoredFirst.IsActive, restoredSecond.IsActive)
	}
}

func TestCodexChannelRestoreRevertsPrioritySwapAndParticipation(t *testing.T) {
	ctx := context.Background()
	db := newCodexEndpointTestDB(t)
	channelStore := store.NewSQLiteCodexChannelStore(db)
	endpointStore := store.NewSQLiteCodexEndpointStore(db)
	channelService := NewCodexChannelService(channelStore, endpointStore)

	first, err := channelService.CreateChannel(ctx, &store.CodexChannelRecord{Name: "first-channel", ProviderType: "openai-compatible", Priority: 0, FailoverEnabled: true})
	if err != nil {
		t.Fatalf("create first channel: %v", err)
	}
	second, err := channelService.CreateChannel(ctx, &store.CodexChannelRecord{Name: "second-channel", ProviderType: "openai-compatible", Priority: 1, FailoverEnabled: false})
	if err != nil {
		t.Fatalf("create second channel: %v", err)
	}
	firstSnapshot := *first
	secondSnapshot := *second

	updatedSecond := secondSnapshot
	updatedSecond.Priority = first.Priority
	updatedSecond.FailoverEnabled = true
	if err := channelService.UpdateChannelByID(ctx, second.ID, &updatedSecond); err != nil {
		t.Fatalf("swap channel priorities: %v", err)
	}
	if err := channelService.RestoreChannel(ctx, &secondSnapshot); err != nil {
		t.Fatalf("restore second channel: %v", err)
	}
	if err := channelService.RestoreChannel(ctx, &firstSnapshot); err != nil {
		t.Fatalf("restore first channel: %v", err)
	}

	restoredFirst, err := channelStore.GetByID(ctx, first.ID)
	if err != nil {
		t.Fatalf("reload first channel: %v", err)
	}
	restoredSecond, err := channelStore.GetByID(ctx, second.ID)
	if err != nil {
		t.Fatalf("reload second channel: %v", err)
	}
	if restoredFirst.Priority != 0 || restoredSecond.Priority != 1 {
		t.Fatalf("restored channel priorities = %d/%d, want 0/1", restoredFirst.Priority, restoredSecond.Priority)
	}
	if !restoredFirst.FailoverEnabled || restoredSecond.FailoverEnabled {
		t.Fatalf("restored participation = %v/%v, want true/false", restoredFirst.FailoverEnabled, restoredSecond.FailoverEnabled)
	}
}

func TestCodexEndpointCreateRequiresUsableCredentialGroup(t *testing.T) {
	ctx := context.Background()
	db := newCodexEndpointTestDB(t)
	channelStore := store.NewSQLiteCodexChannelStore(db)
	groupStore := store.NewSQLiteCodexCredentialGroupStore(db)
	endpointStore := store.NewSQLiteCodexEndpointStore(db)
	endpointService := NewCodexEndpointService(endpointStore, channelStore, groupStore)

	channel, err := channelStore.Create(ctx, &store.CodexChannelRecord{
		Name: "no-credential-channel", ProviderType: "openai-compatible",
	})
	if err != nil {
		t.Fatalf("create channel: %v", err)
	}
	_, err = endpointService.CreateEndpoint(ctx, &store.CodexEndpointRecord{
		ChannelID: channel.ID, Name: "without-group", BaseURL: "https://without-group.example.invalid/v1", FailoverEnabled: true,
	})
	if err == nil || !strings.Contains(err.Error(), "尚未配置凭证分组") {
		t.Fatalf("expected missing-group error, got %v", err)
	}

	group, err := groupStore.Create(ctx, &store.CodexCredentialGroupRecord{
		ChannelID: channel.ID, Name: "default", IsDefault: true,
	})
	if err != nil {
		t.Fatalf("create empty credential group: %v", err)
	}
	_, err = endpointService.CreateEndpoint(ctx, &store.CodexEndpointRecord{
		ChannelID: channel.ID, CredentialGroupID: group.ID, Name: "empty-group", BaseURL: "https://empty-group.example.invalid/v1", FailoverEnabled: true,
	})
	if err == nil || !strings.Contains(err.Error(), "API Key") {
		t.Fatalf("expected empty-api-key error, got %v", err)
	}
	if _, err = endpointService.CreateEndpoint(ctx, &store.CodexEndpointRecord{
		ChannelID: channel.ID, CredentialGroupID: group.ID, Name: "endpoint-override", BaseURL: "https://endpoint-override.example.invalid/v1", ApiKey: "sk-endpoint", FailoverEnabled: true,
	}); err != nil {
		t.Fatalf("endpoint-level API key should allow an empty group: %v", err)
	}
	if err := channelStore.UpdateByID(ctx, channel.ID, &store.CodexChannelRecord{
		ID: channel.ID, Name: channel.Name, ProviderType: channel.ProviderType, ApiKey: "sk-legacy", Priority: channel.Priority, FailoverEnabled: channel.FailoverEnabled,
	}); err != nil {
		t.Fatalf("set legacy channel API key: %v", err)
	}
	if _, err = endpointService.CreateEndpoint(ctx, &store.CodexEndpointRecord{
		ChannelID: channel.ID, CredentialGroupID: group.ID, Name: "channel-override", BaseURL: "https://channel-override.example.invalid/v1", FailoverEnabled: true,
	}); err != nil {
		t.Fatalf("legacy channel API key should allow an empty group: %v", err)
	}

	endpoints, err := endpointStore.ListByChannelID(ctx, channel.ID)
	if err != nil {
		t.Fatalf("list endpoints: %v", err)
	}
	if len(endpoints) != 2 {
		t.Fatalf("only endpoint/channel credential fallbacks should create records, count=%d", len(endpoints))
	}
}

func TestCodexEndpointCreatePriorityConflictRollsBackOnInsertFailure(t *testing.T) {
	ctx := context.Background()
	db := newCodexEndpointTestDB(t)
	if _, err := db.Exec(`CREATE TRIGGER fail_target_codex_endpoint_insert BEFORE INSERT ON codex_endpoints WHEN NEW.name = 'target' BEGIN SELECT RAISE(ABORT, 'forced endpoint insert failure'); END`); err != nil {
		t.Fatalf("create failure trigger: %v", err)
	}
	channelStore := store.NewSQLiteCodexChannelStore(db)
	groupStore := store.NewSQLiteCodexCredentialGroupStore(db)
	endpointStore := store.NewSQLiteCodexEndpointStore(db)
	endpointService := NewCodexEndpointService(endpointStore, channelStore, groupStore)
	channel, err := channelStore.Create(ctx, &store.CodexChannelRecord{
		Name: "rollback-channel", ProviderType: "openai-compatible",
	})
	if err != nil {
		t.Fatalf("create channel: %v", err)
	}
	group, err := groupStore.Create(ctx, &store.CodexCredentialGroupRecord{
		ChannelID: channel.ID, Name: "default", ApiKey: "sk-test", IsDefault: true,
	})
	if err != nil {
		t.Fatalf("create credential group: %v", err)
	}
	conflicting, err := endpointService.CreateEndpoint(ctx, &store.CodexEndpointRecord{
		ChannelID: channel.ID, CredentialGroupID: group.ID, Name: "existing", BaseURL: "https://existing.example.invalid/v1", FailoverEnabled: true, Priority: 0,
	})
	if err != nil {
		t.Fatalf("create existing endpoint: %v", err)
	}
	_, err = endpointService.CreateEndpoint(ctx, &store.CodexEndpointRecord{
		ChannelID: channel.ID, CredentialGroupID: group.ID, Name: "target", BaseURL: "https://target.example.invalid/v1", FailoverEnabled: true, Priority: 0,
	})
	if err == nil || !strings.Contains(err.Error(), "创建 Codex 端点失败") {
		t.Fatalf("expected insert failure, got %v", err)
	}
	reloaded, err := endpointStore.GetByID(ctx, conflicting.ID)
	if err != nil {
		t.Fatalf("reload conflicting endpoint: %v", err)
	}
	if reloaded == nil || reloaded.Priority != 0 {
		t.Fatalf("priority move must roll back, got %#v", reloaded)
	}
	endpoints, err := endpointStore.ListByChannelID(ctx, channel.ID)
	if err != nil {
		t.Fatalf("list endpoints: %v", err)
	}
	if len(endpoints) != 1 {
		t.Fatalf("failed create must not leave target endpoint, count=%d", len(endpoints))
	}
}

func TestCodexEndpointCreateMovesExplicitPriorityConflictToNextPriority(t *testing.T) {
	ctx := context.Background()
	db := newCodexEndpointTestDB(t)
	channelStore := store.NewSQLiteCodexChannelStore(db)
	groupStore := store.NewSQLiteCodexCredentialGroupStore(db)
	endpointStore := store.NewSQLiteCodexEndpointStore(db)
	endpointService := NewCodexEndpointService(endpointStore, channelStore, groupStore)

	channel, err := channelStore.Create(ctx, &store.CodexChannelRecord{
		Name:         "anyrouter",
		ProviderType: "openai-compatible",
	})
	if err != nil {
		t.Fatalf("create channel: %v", err)
	}
	defaultGroup, err := groupStore.Create(ctx, &store.CodexCredentialGroupRecord{
		ChannelID: channel.ID,
		Name:      "default",
		ApiKey:    "sk-default",
		IsDefault: true,
	})
	if err != nil {
		t.Fatalf("create default group: %v", err)
	}

	first, err := endpointService.CreateEndpoint(ctx, &store.CodexEndpointRecord{
		ChannelID:         channel.ID,
		CredentialGroupID: defaultGroup.ID,
		Name:              "first",
		BaseURL:           "https://example.invalid/first/v1",
		FailoverEnabled:   true,
		Priority:          0,
	})
	if err != nil {
		t.Fatalf("create first endpoint: %v", err)
	}
	second, err := endpointService.CreateEndpoint(ctx, &store.CodexEndpointRecord{
		ChannelID:         channel.ID,
		CredentialGroupID: defaultGroup.ID,
		Name:              "second",
		BaseURL:           "https://example.invalid/second/v1",
		FailoverEnabled:   true,
		Priority:          0,
	})
	if err != nil {
		t.Fatalf("create second endpoint: %v", err)
	}

	updatedFirst, err := endpointStore.GetByID(ctx, first.ID)
	if err != nil {
		t.Fatalf("reload first endpoint: %v", err)
	}
	if second.Priority != 0 || updatedFirst.Priority != 1 {
		t.Fatalf("expected new endpoint to take priority 0 and existing endpoint to move to 1, got second=%d first=%d", second.Priority, updatedFirst.Priority)
	}
}

func TestCodexEndpointCreatePriorityIsScopedToCredentialGroup(t *testing.T) {
	ctx := context.Background()
	db := newCodexEndpointTestDB(t)
	channelStore := store.NewSQLiteCodexChannelStore(db)
	groupStore := store.NewSQLiteCodexCredentialGroupStore(db)
	endpointStore := store.NewSQLiteCodexEndpointStore(db)
	endpointService := NewCodexEndpointService(endpointStore, channelStore, groupStore)

	channel, err := channelStore.Create(ctx, &store.CodexChannelRecord{
		Name:         "anyrouter",
		ProviderType: "openai-compatible",
	})
	if err != nil {
		t.Fatalf("create channel: %v", err)
	}
	defaultGroup, err := groupStore.Create(ctx, &store.CodexCredentialGroupRecord{
		ChannelID: channel.ID,
		Name:      "default",
		ApiKey:    "sk-default",
		IsDefault: true,
	})
	if err != nil {
		t.Fatalf("create default group: %v", err)
	}
	otherGroup, err := groupStore.Create(ctx, &store.CodexCredentialGroupRecord{
		ChannelID: channel.ID,
		Name:      "github",
		ApiKey:    "sk-github",
	})
	if err != nil {
		t.Fatalf("create other group: %v", err)
	}

	defaultEndpoint, err := endpointService.CreateEndpoint(ctx, &store.CodexEndpointRecord{
		ChannelID:         channel.ID,
		CredentialGroupID: defaultGroup.ID,
		Name:              "default-first",
		BaseURL:           "https://example.invalid/default-first/v1",
		FailoverEnabled:   true,
		Priority:          -1,
	})
	if err != nil {
		t.Fatalf("create default endpoint: %v", err)
	}
	otherEndpoint, err := endpointService.CreateEndpoint(ctx, &store.CodexEndpointRecord{
		ChannelID:         channel.ID,
		CredentialGroupID: otherGroup.ID,
		Name:              "other-first",
		BaseURL:           "https://example.invalid/other-first/v1",
		FailoverEnabled:   true,
		Priority:          -1,
	})
	if err != nil {
		t.Fatalf("create other endpoint: %v", err)
	}

	if defaultEndpoint.Priority != 0 || otherEndpoint.Priority != 0 {
		t.Fatalf("expected each credential group to start at priority 0, got default=%d other=%d", defaultEndpoint.Priority, otherEndpoint.Priority)
	}
}

func TestCodexEndpointCreateAutoAssignsAfterExistingPriority(t *testing.T) {
	ctx := context.Background()
	db := newCodexEndpointTestDB(t)
	channelStore := store.NewSQLiteCodexChannelStore(db)
	groupStore := store.NewSQLiteCodexCredentialGroupStore(db)
	endpointStore := store.NewSQLiteCodexEndpointStore(db)
	endpointService := NewCodexEndpointService(endpointStore, channelStore, groupStore)

	channel, err := channelStore.Create(ctx, &store.CodexChannelRecord{
		Name:         "anyrouter",
		ProviderType: "openai-compatible",
	})
	if err != nil {
		t.Fatalf("create channel: %v", err)
	}
	defaultGroup, err := groupStore.Create(ctx, &store.CodexCredentialGroupRecord{
		ChannelID: channel.ID,
		Name:      "default",
		ApiKey:    "sk-default",
		IsDefault: true,
	})
	if err != nil {
		t.Fatalf("create default group: %v", err)
	}

	_, err = endpointService.CreateEndpoint(ctx, &store.CodexEndpointRecord{
		ChannelID:         channel.ID,
		CredentialGroupID: defaultGroup.ID,
		Name:              "first",
		BaseURL:           "https://example.invalid/first/v1",
		FailoverEnabled:   true,
		Priority:          5,
	})
	if err != nil {
		t.Fatalf("create first endpoint: %v", err)
	}
	created, err := endpointService.CreateEndpoint(ctx, &store.CodexEndpointRecord{
		ChannelID:         channel.ID,
		CredentialGroupID: defaultGroup.ID,
		Name:              "second",
		BaseURL:           "https://example.invalid/second/v1",
		FailoverEnabled:   true,
		Priority:          -1,
	})
	if err != nil {
		t.Fatalf("create second endpoint: %v", err)
	}
	if created.Priority != 6 {
		t.Fatalf("expected auto-assigned priority 6, got %d", created.Priority)
	}
}

func TestCodexEndpointDisablingActiveEndpointFailoverSwitchesToNextCandidate(t *testing.T) {
	ctx := context.Background()
	db := newCodexEndpointTestDB(t)
	channelStore := store.NewSQLiteCodexChannelStore(db)
	groupStore := store.NewSQLiteCodexCredentialGroupStore(db)
	endpointStore := store.NewSQLiteCodexEndpointStore(db)
	endpointService := NewCodexEndpointService(endpointStore, channelStore, groupStore)

	channel, err := channelStore.Create(ctx, &store.CodexChannelRecord{
		Name:         "anyrouter",
		ProviderType: "openai-compatible",
	})
	if err != nil {
		t.Fatalf("create channel: %v", err)
	}
	defaultGroup, err := groupStore.Create(ctx, &store.CodexCredentialGroupRecord{
		ChannelID: channel.ID,
		Name:      "default",
		ApiKey:    "sk-default",
		IsDefault: true,
	})
	if err != nil {
		t.Fatalf("create default group: %v", err)
	}
	active, err := endpointService.CreateEndpoint(ctx, &store.CodexEndpointRecord{
		ChannelID:         channel.ID,
		CredentialGroupID: defaultGroup.ID,
		Name:              "active",
		BaseURL:           "https://example.invalid/codex/v1",
		FailoverEnabled:   true,
	})
	if err != nil {
		t.Fatalf("create active endpoint: %v", err)
	}
	if !active.IsActive {
		t.Fatalf("expected first endpoint to be active")
	}
	standby, err := endpointService.CreateEndpoint(ctx, &store.CodexEndpointRecord{
		ChannelID:         channel.ID,
		CredentialGroupID: defaultGroup.ID,
		Name:              "standby",
		BaseURL:           "https://example.invalid/standby/v1",
		FailoverEnabled:   true,
		Priority:          1,
	})
	if err != nil {
		t.Fatalf("create standby endpoint: %v", err)
	}
	laterStandby, err := endpointService.CreateEndpoint(ctx, &store.CodexEndpointRecord{
		ChannelID:         channel.ID,
		CredentialGroupID: defaultGroup.ID,
		Name:              "later-standby",
		BaseURL:           "https://example.invalid/later-standby/v1",
		FailoverEnabled:   true,
		Priority:          2,
	})
	if err != nil {
		t.Fatalf("create later standby endpoint: %v", err)
	}

	err = endpointService.UpdateEndpointByID(
		ctx,
		active.ID,
		active.Name,
		active.BaseURL,
		active.CredentialGroupID,
		active.ApiKey,
		false,
		active.SkipHealthCheck,
		active.Priority,
		active.TimeoutSeconds,
		active.CostMultiplier,
		active.InputCostMultiplier,
		active.OutputCostMultiplier,
		active.CacheReadCostMultiplier,
		active.BillingOutputTokenSource,
		active.ModelMultipliers,
	)
	if err != nil {
		t.Fatalf("disable active endpoint failover: %v", err)
	}
	updatedActive, err := endpointStore.GetByID(ctx, active.ID)
	if err != nil {
		t.Fatalf("reload active endpoint: %v", err)
	}
	updatedStandby, err := endpointStore.GetByID(ctx, standby.ID)
	if err != nil {
		t.Fatalf("reload standby endpoint: %v", err)
	}
	if updatedActive.FailoverEnabled || updatedActive.IsActive {
		t.Fatalf("expected original active endpoint to be inactive and excluded, got active=%v failover=%v", updatedActive.IsActive, updatedActive.FailoverEnabled)
	}
	if !updatedStandby.IsActive {
		t.Fatalf("expected standby endpoint to become active")
	}
	updatedLaterStandby, err := endpointStore.GetByID(ctx, laterStandby.ID)
	if err != nil {
		t.Fatalf("reload later standby endpoint: %v", err)
	}
	if updatedLaterStandby.IsActive {
		t.Fatalf("expected earlier standby endpoint to win equal-priority tie")
	}
}

func TestCodexEndpointUpdateSwapsPriorityConflict(t *testing.T) {
	ctx := context.Background()
	db := newCodexEndpointTestDB(t)
	channelStore := store.NewSQLiteCodexChannelStore(db)
	groupStore := store.NewSQLiteCodexCredentialGroupStore(db)
	endpointStore := store.NewSQLiteCodexEndpointStore(db)
	endpointService := NewCodexEndpointService(endpointStore, channelStore, groupStore)

	channel, err := channelStore.Create(ctx, &store.CodexChannelRecord{
		Name:         "anyrouter",
		ProviderType: "openai-compatible",
	})
	if err != nil {
		t.Fatalf("create channel: %v", err)
	}
	defaultGroup, err := groupStore.Create(ctx, &store.CodexCredentialGroupRecord{
		ChannelID: channel.ID,
		Name:      "default",
		ApiKey:    "sk-default",
		IsDefault: true,
	})
	if err != nil {
		t.Fatalf("create default group: %v", err)
	}
	first, err := endpointService.CreateEndpoint(ctx, &store.CodexEndpointRecord{
		ChannelID:         channel.ID,
		CredentialGroupID: defaultGroup.ID,
		Name:              "first",
		BaseURL:           "https://example.invalid/first/v1",
		FailoverEnabled:   true,
		Priority:          1,
	})
	if err != nil {
		t.Fatalf("create first endpoint: %v", err)
	}
	second, err := endpointService.CreateEndpoint(ctx, &store.CodexEndpointRecord{
		ChannelID:         channel.ID,
		CredentialGroupID: defaultGroup.ID,
		Name:              "second",
		BaseURL:           "https://example.invalid/second/v1",
		FailoverEnabled:   true,
		Priority:          5,
	})
	if err != nil {
		t.Fatalf("create second endpoint: %v", err)
	}

	if err := endpointService.UpdateEndpointByID(
		ctx,
		second.ID,
		second.Name,
		second.BaseURL,
		second.CredentialGroupID,
		second.ApiKey,
		second.FailoverEnabled,
		second.SkipHealthCheck,
		first.Priority,
		second.TimeoutSeconds,
		second.CostMultiplier,
		second.InputCostMultiplier,
		second.OutputCostMultiplier,
		second.CacheReadCostMultiplier,
		second.BillingOutputTokenSource,
		second.ModelMultipliers,
	); err != nil {
		t.Fatalf("update second endpoint priority: %v", err)
	}

	updatedFirst, err := endpointStore.GetByID(ctx, first.ID)
	if err != nil {
		t.Fatalf("reload first endpoint: %v", err)
	}
	updatedSecond, err := endpointStore.GetByID(ctx, second.ID)
	if err != nil {
		t.Fatalf("reload second endpoint: %v", err)
	}
	if updatedFirst.Priority != 5 || updatedSecond.Priority != 1 {
		t.Fatalf("expected priority swap, got first=%d second=%d", updatedFirst.Priority, updatedSecond.Priority)
	}
}

func TestCodexEndpointUpdatePriorityConflictIsScopedToCredentialGroup(t *testing.T) {
	ctx := context.Background()
	db := newCodexEndpointTestDB(t)
	channelStore := store.NewSQLiteCodexChannelStore(db)
	groupStore := store.NewSQLiteCodexCredentialGroupStore(db)
	endpointStore := store.NewSQLiteCodexEndpointStore(db)
	endpointService := NewCodexEndpointService(endpointStore, channelStore, groupStore)

	channel, err := channelStore.Create(ctx, &store.CodexChannelRecord{
		Name:         "anyrouter",
		ProviderType: "openai-compatible",
	})
	if err != nil {
		t.Fatalf("create channel: %v", err)
	}
	defaultGroup, err := groupStore.Create(ctx, &store.CodexCredentialGroupRecord{
		ChannelID: channel.ID,
		Name:      "default",
		ApiKey:    "sk-default",
		IsDefault: true,
	})
	if err != nil {
		t.Fatalf("create default group: %v", err)
	}
	otherGroup, err := groupStore.Create(ctx, &store.CodexCredentialGroupRecord{
		ChannelID: channel.ID,
		Name:      "github",
		ApiKey:    "sk-github",
	})
	if err != nil {
		t.Fatalf("create other group: %v", err)
	}
	defaultEndpoint, err := endpointService.CreateEndpoint(ctx, &store.CodexEndpointRecord{
		ChannelID:         channel.ID,
		CredentialGroupID: defaultGroup.ID,
		Name:              "default-first",
		BaseURL:           "https://example.invalid/default-first/v1",
		FailoverEnabled:   true,
		Priority:          1,
	})
	if err != nil {
		t.Fatalf("create default endpoint: %v", err)
	}
	otherEndpoint, err := endpointService.CreateEndpoint(ctx, &store.CodexEndpointRecord{
		ChannelID:         channel.ID,
		CredentialGroupID: otherGroup.ID,
		Name:              "other-first",
		BaseURL:           "https://example.invalid/other-first/v1",
		FailoverEnabled:   true,
		Priority:          5,
	})
	if err != nil {
		t.Fatalf("create other endpoint: %v", err)
	}

	if err := endpointService.UpdateEndpointByID(
		ctx,
		otherEndpoint.ID,
		otherEndpoint.Name,
		otherEndpoint.BaseURL,
		otherEndpoint.CredentialGroupID,
		otherEndpoint.ApiKey,
		otherEndpoint.FailoverEnabled,
		otherEndpoint.SkipHealthCheck,
		defaultEndpoint.Priority,
		otherEndpoint.TimeoutSeconds,
		otherEndpoint.CostMultiplier,
		otherEndpoint.InputCostMultiplier,
		otherEndpoint.OutputCostMultiplier,
		otherEndpoint.CacheReadCostMultiplier,
		otherEndpoint.BillingOutputTokenSource,
		otherEndpoint.ModelMultipliers,
	); err != nil {
		t.Fatalf("update other endpoint priority: %v", err)
	}

	updatedDefault, err := endpointStore.GetByID(ctx, defaultEndpoint.ID)
	if err != nil {
		t.Fatalf("reload default endpoint: %v", err)
	}
	updatedOther, err := endpointStore.GetByID(ctx, otherEndpoint.ID)
	if err != nil {
		t.Fatalf("reload other endpoint: %v", err)
	}
	if updatedDefault.Priority != 1 || updatedOther.Priority != 1 {
		t.Fatalf("expected same priority across different groups without swap, got default=%d other=%d", updatedDefault.Priority, updatedOther.Priority)
	}
}

func TestCodexEndpointMoveToCredentialGroupAssignsNextPriority(t *testing.T) {
	ctx := context.Background()
	db := newCodexEndpointTestDB(t)
	channelStore := store.NewSQLiteCodexChannelStore(db)
	groupStore := store.NewSQLiteCodexCredentialGroupStore(db)
	endpointStore := store.NewSQLiteCodexEndpointStore(db)
	endpointService := NewCodexEndpointService(endpointStore, channelStore, groupStore)

	channel, err := channelStore.Create(ctx, &store.CodexChannelRecord{
		Name:         "anyrouter",
		ProviderType: "openai-compatible",
	})
	if err != nil {
		t.Fatalf("create channel: %v", err)
	}
	defaultGroup, err := groupStore.Create(ctx, &store.CodexCredentialGroupRecord{
		ChannelID: channel.ID,
		Name:      "default",
		ApiKey:    "sk-default",
		IsDefault: true,
	})
	if err != nil {
		t.Fatalf("create default group: %v", err)
	}
	otherGroup, err := groupStore.Create(ctx, &store.CodexCredentialGroupRecord{
		ChannelID: channel.ID,
		Name:      "github",
		ApiKey:    "sk-github",
	})
	if err != nil {
		t.Fatalf("create other group: %v", err)
	}
	if _, err := endpointService.CreateEndpoint(ctx, &store.CodexEndpointRecord{
		ChannelID:         channel.ID,
		CredentialGroupID: defaultGroup.ID,
		Name:              "target-existing",
		BaseURL:           "https://example.invalid/target-existing/v1",
		FailoverEnabled:   true,
		Priority:          1,
	}); err != nil {
		t.Fatalf("create target existing endpoint: %v", err)
	}
	moving, err := endpointService.CreateEndpoint(ctx, &store.CodexEndpointRecord{
		ChannelID:         channel.ID,
		CredentialGroupID: otherGroup.ID,
		Name:              "moving",
		BaseURL:           "https://example.invalid/moving/v1",
		FailoverEnabled:   true,
		Priority:          1,
	})
	if err != nil {
		t.Fatalf("create moving endpoint: %v", err)
	}

	if err := endpointService.UpdateEndpointByID(
		ctx,
		moving.ID,
		moving.Name,
		moving.BaseURL,
		defaultGroup.ID,
		moving.ApiKey,
		moving.FailoverEnabled,
		moving.SkipHealthCheck,
		moving.Priority,
		moving.TimeoutSeconds,
		moving.CostMultiplier,
		moving.InputCostMultiplier,
		moving.OutputCostMultiplier,
		moving.CacheReadCostMultiplier,
		moving.BillingOutputTokenSource,
		moving.ModelMultipliers,
	); err != nil {
		t.Fatalf("move endpoint to default group: %v", err)
	}

	updatedMoving, err := endpointStore.GetByID(ctx, moving.ID)
	if err != nil {
		t.Fatalf("reload moving endpoint: %v", err)
	}
	if updatedMoving.Priority != 2 {
		t.Fatalf("expected moving endpoint to be appended to target group priority order, got %d", updatedMoving.Priority)
	}
}

func TestCodexEndpointDisablingActiveEndpointFailoverRequiresCandidate(t *testing.T) {
	ctx := context.Background()
	db := newCodexEndpointTestDB(t)
	channelStore := store.NewSQLiteCodexChannelStore(db)
	groupStore := store.NewSQLiteCodexCredentialGroupStore(db)
	endpointStore := store.NewSQLiteCodexEndpointStore(db)
	endpointService := NewCodexEndpointService(endpointStore, channelStore, groupStore)

	channel, err := channelStore.Create(ctx, &store.CodexChannelRecord{
		Name:         "anyrouter",
		ProviderType: "openai-compatible",
	})
	if err != nil {
		t.Fatalf("create channel: %v", err)
	}
	defaultGroup, err := groupStore.Create(ctx, &store.CodexCredentialGroupRecord{
		ChannelID: channel.ID,
		Name:      "default",
		ApiKey:    "sk-default",
		IsDefault: true,
	})
	if err != nil {
		t.Fatalf("create default group: %v", err)
	}
	active, err := endpointService.CreateEndpoint(ctx, &store.CodexEndpointRecord{
		ChannelID:         channel.ID,
		CredentialGroupID: defaultGroup.ID,
		Name:              "active",
		BaseURL:           "https://example.invalid/codex/v1",
		FailoverEnabled:   true,
	})
	if err != nil {
		t.Fatalf("create active endpoint: %v", err)
	}

	err = endpointService.UpdateEndpointByID(
		ctx,
		active.ID,
		active.Name,
		active.BaseURL,
		active.CredentialGroupID,
		active.ApiKey,
		false,
		active.SkipHealthCheck,
		active.Priority,
		active.TimeoutSeconds,
		active.CostMultiplier,
		active.InputCostMultiplier,
		active.OutputCostMultiplier,
		active.CacheReadCostMultiplier,
		active.BillingOutputTokenSource,
		active.ModelMultipliers,
	)
	if err == nil || !strings.Contains(err.Error(), "没有其他参与故障转移") {
		t.Fatalf("expected missing failover candidate error, got: %v", err)
	}

	updatedActive, err := endpointStore.GetByID(ctx, active.ID)
	if err != nil {
		t.Fatalf("reload active endpoint: %v", err)
	}
	if !updatedActive.FailoverEnabled || !updatedActive.IsActive {
		t.Fatalf("expected active endpoint to remain active and included, got active=%v failover=%v", updatedActive.IsActive, updatedActive.FailoverEnabled)
	}
}

func TestNormalizeAndValidateCodexEndpointRejectsControlWhitespaceInNames(t *testing.T) {
	cases := []string{
		"主站\t直连",
		"主站\n直连",
	}

	for _, name := range cases {
		t.Run(name, func(t *testing.T) {
			record := &store.CodexEndpointRecord{
				ChannelID:       1,
				Name:            name,
				BaseURL:         "https://example.invalid/codex/v1",
				FailoverEnabled: true,
			}
			if err := normalizeAndValidateCodexEndpoint(record); err == nil {
				t.Fatalf("expected endpoint name %q to be invalid", name)
			}
		})
	}
}

func newCodexEndpointTestDB(t *testing.T) *sql.DB {
	t.Helper()

	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	t.Cleanup(func() {
		_ = db.Close()
	})

	statements := []string{
		`CREATE TABLE codex_channels (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			name TEXT NOT NULL,
			provider_type TEXT NOT NULL DEFAULT 'openai-compatible',
			api_key TEXT NOT NULL DEFAULT '',
			model TEXT NOT NULL DEFAULT '',
			model_mapping TEXT NOT NULL DEFAULT '{}',
			model_reasoning_effort TEXT NOT NULL DEFAULT '',
			enable_1m_context INTEGER NOT NULL DEFAULT 0,
			website TEXT,
			is_active INTEGER NOT NULL DEFAULT 0,
			priority INTEGER NOT NULL DEFAULT 1,
			failover_enabled INTEGER NOT NULL DEFAULT 1,
			non_stream_route TEXT,
			privacy_level TEXT,
			created_at TEXT NOT NULL DEFAULT '',
			updated_at TEXT NOT NULL DEFAULT ''
		)`,
		`CREATE TABLE codex_credential_groups (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			channel_id INTEGER NOT NULL,
			name TEXT NOT NULL,
			api_key TEXT NOT NULL DEFAULT '',
			is_default INTEGER NOT NULL DEFAULT 0,
			daily_cost_limit_usd REAL NOT NULL DEFAULT 0,
			model_policy TEXT NOT NULL DEFAULT 'inherit',
			model_mapping TEXT NOT NULL DEFAULT '{}',
			supported_models TEXT NOT NULL DEFAULT '[]',
			cost_multiplier REAL NOT NULL DEFAULT 1,
			input_cost_multiplier REAL NOT NULL DEFAULT 1,
			output_cost_multiplier REAL NOT NULL DEFAULT 1,
			cache_creation_cost_multiplier REAL NOT NULL DEFAULT 1,
			cache_read_cost_multiplier REAL NOT NULL DEFAULT 1,
			long_context_cost_multiplier REAL NOT NULL DEFAULT 1,
			long_context_input_cost_multiplier REAL NOT NULL DEFAULT 1,
			long_context_output_cost_multiplier REAL NOT NULL DEFAULT 1,
			long_context_cache_creation_cost_multiplier REAL NOT NULL DEFAULT 1,
			long_context_cache_read_cost_multiplier REAL NOT NULL DEFAULT 1,
			disable_long_context_pricing INTEGER NOT NULL DEFAULT 1,
			disable_service_tier_pricing INTEGER NOT NULL DEFAULT 1,
			service_tier_multipliers TEXT NOT NULL DEFAULT '[]',
			billing_output_token_source TEXT NOT NULL DEFAULT 'output_tokens',
			model_multipliers TEXT NOT NULL DEFAULT '[]',
			billing_rules TEXT NOT NULL DEFAULT '[]',
			created_at TEXT NOT NULL DEFAULT '',
			updated_at TEXT NOT NULL DEFAULT ''
		)`,
		`CREATE TABLE codex_endpoints (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			channel_id INTEGER NOT NULL,
			credential_group_id INTEGER,
			api_key TEXT NOT NULL DEFAULT '',
			name TEXT NOT NULL,
			base_url TEXT NOT NULL,
			is_active INTEGER NOT NULL DEFAULT 0,
			failover_enabled INTEGER NOT NULL DEFAULT 1,
			skip_health_check INTEGER NOT NULL DEFAULT 0,
			priority INTEGER NOT NULL DEFAULT 0,
			timeout_seconds INTEGER NOT NULL DEFAULT 0,
			cost_multiplier REAL NOT NULL DEFAULT 1,
			input_cost_multiplier REAL NOT NULL DEFAULT 1,
			output_cost_multiplier REAL NOT NULL DEFAULT 1,
			cache_read_cost_multiplier REAL NOT NULL DEFAULT 1,
			billing_output_token_source TEXT NOT NULL DEFAULT 'output_tokens',
			model_multipliers TEXT NOT NULL DEFAULT '[]',
			created_at TEXT NOT NULL DEFAULT '',
			updated_at TEXT NOT NULL DEFAULT ''
		)`,
	}
	for _, statement := range statements {
		if _, err := db.Exec(statement); err != nil {
			t.Fatalf("create test schema: %v", err)
		}
	}
	if err := store.EnsureCodexSubscriptionSchema(context.Background(), db); err != nil {
		t.Fatalf("subscription schema: %v", err)
	}
	return db
}
