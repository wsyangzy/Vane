package app

import (
	"context"
	"time"

	codexproxy "github.com/wsyangzy/Vane/internal/providers/codex/proxy"
	"github.com/wsyangzy/Vane/internal/providers/codex/proxy/subscription"
	codexstore "github.com/wsyangzy/Vane/internal/providers/codex/store"
	"github.com/wsyangzy/Vane/internal/tracking"
)

func codexChannelInputToRecord(name, providerType, apiKey, model string, modelMapping map[string]string, reasoningEffort, website string, enable1M bool) *codexstore.CodexChannelRecord {
	return &codexstore.CodexChannelRecord{
		Name:            name,
		ProviderType:    providerType,
		ApiKey:          apiKey,
		Model:           model,
		ModelMapping:    modelMapping,
		ReasoningEffort: reasoningEffort,
		Enable1MContext: enable1M,
		Website:         website,
	}
}

func codexCredentialGroupInputToRecord(channelID int64, name, apiKey string, isDefault bool, dailyCostLimitUSD float64, modelPolicy string, modelMapping map[string]string, supportedModels []string, costMultiplier, inputCostMultiplier, outputCostMultiplier, cacheCreationCostMultiplier, cacheReadCostMultiplier, longContextCostMultiplier, longContextInputCostMultiplier, longContextOutputCostMultiplier, longContextCacheCreationCostMultiplier, longContextCacheReadCostMultiplier float64, disableLongContextPricing bool, billingOutputTokenSource string, modelMultipliers []codexstore.CodexModelMultiplierRecord, billingRules []codexstore.CodexBillingRuleRecord) *codexstore.CodexCredentialGroupRecord {
	return &codexstore.CodexCredentialGroupRecord{
		ChannelID:                              channelID,
		Name:                                   name,
		ApiKey:                                 apiKey,
		IsDefault:                              isDefault,
		DailyCostLimitUSD:                      dailyCostLimitUSD,
		ModelPolicy:                            modelPolicy,
		ModelMapping:                           modelMapping,
		SupportedModels:                        supportedModels,
		CostMultiplier:                         costMultiplier,
		InputCostMultiplier:                    inputCostMultiplier,
		OutputCostMultiplier:                   outputCostMultiplier,
		CacheCreationCostMultiplier:            cacheCreationCostMultiplier,
		CacheReadCostMultiplier:                cacheReadCostMultiplier,
		LongContextCostMultiplier:              longContextCostMultiplier,
		LongContextInputCostMultiplier:         longContextInputCostMultiplier,
		LongContextOutputCostMultiplier:        longContextOutputCostMultiplier,
		LongContextCacheCreationCostMultiplier: longContextCacheCreationCostMultiplier,
		LongContextCacheReadCostMultiplier:     longContextCacheReadCostMultiplier,
		DisableLongContextPricing:              disableLongContextPricing,
		BillingOutputTokenSource:               tracking.NormalizeBillingOutputTokenSource(billingOutputTokenSource),
		ModelMultipliers:                       modelMultipliers,
		BillingRules:                           billingRules,
	}
}

func buildCodexCredentialGroupInfo(record *codexstore.CodexCredentialGroupRecord) CodexCredentialGroupInfo {
	if record == nil {
		return CodexCredentialGroupInfo{}
	}
	info := CodexCredentialGroupInfo{
		Enabled:  record.Enabled,
		AuthMode: record.AuthMode, OfficialVerification: "not_verified",
		ID:                                     record.ID,
		ChannelID:                              record.ChannelID,
		Name:                                   record.Name,
		ApiKey:                                 record.ApiKey,
		ApiKeyMasked:                           maskToken(record.ApiKey),
		IsDefault:                              record.IsDefault,
		DailyCostLimitUSD:                      record.DailyCostLimitUSD,
		ModelPolicy:                            record.ModelPolicy,
		ModelMapping:                           record.ModelMapping,
		SupportedModels:                        record.SupportedModels,
		CostMultiplier:                         record.CostMultiplier,
		InputCostMultiplier:                    record.InputCostMultiplier,
		OutputCostMultiplier:                   record.OutputCostMultiplier,
		CacheCreationCostMultiplier:            record.CacheCreationCostMultiplier,
		CacheReadCostMultiplier:                record.CacheReadCostMultiplier,
		LongContextCostMultiplier:              record.LongContextCostMultiplier,
		LongContextInputCostMultiplier:         record.LongContextInputCostMultiplier,
		LongContextOutputCostMultiplier:        record.LongContextOutputCostMultiplier,
		LongContextCacheCreationCostMultiplier: record.LongContextCacheCreationCostMultiplier,
		LongContextCacheReadCostMultiplier:     record.LongContextCacheReadCostMultiplier,
		DisableLongContextPricing:              record.DisableLongContextPricing,
		BillingOutputTokenSource:               tracking.NormalizeBillingOutputTokenSource(record.BillingOutputTokenSource),
		ModelMultipliers:                       record.ModelMultipliers,
		BillingRules:                           record.BillingRules,
		CreatedAt:                              formatCodexTime(record.CreatedAt),
		UpdatedAt:                              formatCodexTime(record.UpdatedAt),
	}
	if record.Subscription != nil {
		binding := *record.Subscription
		info.LocalKeyConfigured = binding.KeyHash != ""
		binding.KeyHash = ""
		binding.Action = ""
		binding.GroupID = record.ID
		binding.ChannelID = record.ChannelID
		info.Subscription = &binding
	}
	return info
}

func copyCodexModelMapping(modelMapping map[string]string) map[string]string {
	if len(modelMapping) == 0 {
		return map[string]string{}
	}
	copied := make(map[string]string, len(modelMapping))
	for source, target := range modelMapping {
		copied[source] = target
	}
	return copied
}

func (a *App) buildCodexChannelInfo(record *codexstore.CodexChannelRecord, endpoints []*codexstore.CodexEndpointRecord, credentialGroups []*codexstore.CodexCredentialGroupRecord) CodexChannelInfo {
	a.mu.RLock()
	endpointMgr := a.codexEndpointMgr
	healthCheck := a.codexHealthCheck
	a.mu.RUnlock()

	activeChannelID := int64(0)
	if endpointMgr != nil {
		if active := endpointMgr.GetActiveChannel(); active != nil {
			activeChannelID = active.ID
		}
	}

	var latencies map[int64]int64
	var lastChecks map[int64]time.Time
	if healthCheck != nil {
		latencies = healthCheck.GetLatencies()
		lastChecks = healthCheck.GetLastChecks()
	}

	info := CodexChannelInfo{
		ID:              record.ID,
		Name:            record.Name,
		ProviderType:    record.ProviderType,
		ApiKey:          record.ApiKey,
		ApiKeyMasked:    maskToken(record.ApiKey),
		Model:           record.Model,
		ModelMapping:    copyCodexModelMapping(record.ModelMapping),
		ReasoningEffort: record.ReasoningEffort,
		Enable1MContext: record.Enable1MContext,
		Website:         record.Website,
		IsActive:        record.IsActive,
		Priority:        record.Priority,
		FailoverEnabled: record.FailoverEnabled,
		Endpoints:       make([]CodexEndpointInfo, 0, len(endpoints)),
	}
	info.CredentialGroups = make([]CodexCredentialGroupInfo, 0, len(credentialGroups))
	groupNames := make(map[int64]string, len(credentialGroups))
	for _, group := range credentialGroups {
		groupInfo := buildCodexCredentialGroupInfo(group)
		if a.codexSubscriptions != nil && group.Subscription != nil && group.AuthMode == subscription.ClientPassthrough {
			groupInfo.SubscriptionStatus = a.codexSubscriptions.Status(context.Background(), *group.Subscription)
		}
		info.CredentialGroups = append(info.CredentialGroups, groupInfo)
		groupNames[group.ID] = group.Name
	}

	channelFailoverEnabled := a.config != nil && a.config.CodexFailover.Enabled
	for _, ep := range endpoints {
		healthy, healthState, healthInScope, responseTimeMS := codexEndpointRuntimeHealth(record, ep, endpointMgr, activeChannelID, channelFailoverEnabled, latencies)
		lastFailCode := 0
		lastFailReason := ""
		if endpointMgr != nil && healthInScope {
			lastFailCode, lastFailReason, _ = endpointMgr.GetFailureDetail(ep.ID)
		}
		info.Endpoints = append(info.Endpoints, CodexEndpointInfo{
			ID:                       ep.ID,
			ChannelID:                ep.ChannelID,
			CredentialGroupID:        ep.CredentialGroupID,
			CredentialGroupName:      groupNames[ep.CredentialGroupID],
			ApiKey:                   ep.ApiKey,
			ApiKeyMasked:             maskToken(ep.ApiKey),
			Name:                     ep.Name,
			BaseURL:                  ep.BaseURL,
			IsActive:                 ep.IsActive,
			FailoverEnabled:          ep.FailoverEnabled,
			SkipHealthCheck:          ep.SkipHealthCheck,
			Healthy:                  healthy,
			HealthState:              healthState,
			HealthInScope:            healthInScope,
			ResponseTimeMS:           responseTimeMS,
			LastCheck:                formatCodexTime(lastChecks[ep.ID]),
			LastFailCode:             lastFailCode,
			LastFailReason:           lastFailReason,
			Priority:                 ep.Priority,
			TimeoutSeconds:           ep.TimeoutSeconds,
			CostMultiplier:           ep.CostMultiplier,
			InputCostMultiplier:      ep.InputCostMultiplier,
			OutputCostMultiplier:     ep.OutputCostMultiplier,
			CacheReadCostMultiplier:  ep.CacheReadCostMultiplier,
			BillingOutputTokenSource: tracking.NormalizeBillingOutputTokenSource(ep.BillingOutputTokenSource),
			ModelMultipliers:         ep.ModelMultipliers,
			CreatedAt:                formatCodexTime(ep.CreatedAt),
			UpdatedAt:                formatCodexTime(ep.UpdatedAt),
		})
	}

	info.CreatedAt = formatCodexTime(record.CreatedAt)
	info.UpdatedAt = formatCodexTime(record.UpdatedAt)
	return info
}

func codexEndpointRuntimeHealth(
	channel *codexstore.CodexChannelRecord,
	endpoint *codexstore.CodexEndpointRecord,
	manager *codexproxy.EndpointManager,
	activeChannelID int64,
	channelFailoverEnabled bool,
	latencies map[int64]int64,
) (healthy bool, healthState string, healthInScope bool, responseTimeMS int64) {
	if channel == nil || endpoint == nil {
		return false, "unchecked", false, 0
	}
	channelInScope := channel.IsActive
	if channelFailoverEnabled {
		channelInScope = channel.FailoverEnabled
	}
	if !channelInScope || (!channelFailoverEnabled && manager != nil && activeChannelID != channel.ID) {
		return false, "inactive", false, 0
	}
	if !endpoint.FailoverEnabled {
		return false, "excluded", false, 0
	}
	latency, hasLatency := latencies[endpoint.ID]
	if latency > 0 {
		responseTimeMS = latency
	}
	if endpoint.SkipHealthCheck {
		return false, "skipped", true, responseTimeMS
	}

	if hasLatency && latency < 0 {
		return false, "unhealthy", true, responseTimeMS
	}
	if manager != nil && !manager.IsHealthy(endpoint.ID) {
		return false, "unhealthy", true, responseTimeMS
	}
	if hasLatency {
		return true, "healthy", true, responseTimeMS
	}
	return false, "unchecked", true, 0
}

func formatCodexTime(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.Format("2006-01-02 15:04:05")
}
