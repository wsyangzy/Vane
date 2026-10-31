package service

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/wsyangzy/Vane/internal/config"
	"github.com/wsyangzy/Vane/internal/providers/claude/proxy/endpoint"
	"github.com/wsyangzy/Vane/internal/providers/claude/proxy/subscription"
	"github.com/wsyangzy/Vane/internal/providers/claude/store"
)

// recordToConfig 将数据库记录转换为配置对象
func (s *EndpointService) recordToConfig(record *store.EndpointRecord) config.EndpointConfig {
	token := record.Token
	apiKey := record.ApiKey
	credentialGroupName := record.CredentialGroupName
	credentialGroupID := record.CredentialGroupID
	credentialGroupOrder := 0
	dailyCostLimitUSD := 0.0
	modelPolicy := store.ClaudeModelPolicyInherit
	authMode := "group_credential"
	var modelMapping map[string]string
	var supportedModels []string
	if s.credentialGroupStore != nil {
		var group *store.ClaudeCredentialGroupRecord
		if record.CredentialGroupID > 0 {
			group, _ = s.credentialGroupStore.GetByID(context.Background(), record.CredentialGroupID)
			if group != nil && strings.TrimSpace(group.Channel) != strings.TrimSpace(record.Channel) {
				// Legacy rows can point at a group from another channel. Never leak
				// that identity into routing or request tracking.
				group = nil
			}
		}
		if group == nil {
			group, _ = s.credentialGroupStore.GetDefaultByChannel(context.Background(), record.Channel)
		}
		if group != nil {
			authMode = group.AuthMode
			credentialGroupID = group.ID
			credentialGroupName = group.Name
			credentialGroupOrder = s.credentialGroupOrder(context.Background(), record.Channel, group.ID)
			dailyCostLimitUSD = group.DailyCostLimitUSD
			modelPolicy = group.ModelPolicy
			modelMapping = group.ModelMapping
			supportedModels = group.SupportedModels
			if token == "" {
				token = group.Token
			}
			if apiKey == "" {
				apiKey = group.ApiKey
			}
		}
		if group == nil {
			credentialGroupID = 0
			credentialGroupName = ""
			credentialGroupOrder = 0
			dailyCostLimitUSD = 0
		}
	}

	if authMode == subscription.ClientPassthrough {
		token = ""
		apiKey = ""
	}
	cfg := config.EndpointConfig{
		Name:                 record.Name,
		URL:                  record.URL,
		Channel:              record.Channel,
		RuntimeKey:           EndpointRuntimeKey(record),
		CredentialGroupID:    credentialGroupID,
		CredentialGroupName:  credentialGroupName,
		AuthMode:             authMode,
		CredentialGroupOrder: credentialGroupOrder,
		DailyCostLimitUSD:    dailyCostLimitUSD,
		ModelPolicy:          modelPolicy,
		ModelMapping:         modelMapping,
		SupportedModels:      supportedModels,
		Priority:             record.Priority,
		Token:                token,
		ApiKey:               apiKey,
		Headers:              record.Headers,
		Timeout:              time.Duration(record.TimeoutSeconds) * time.Second,
		SupportsCountTokens:  record.SupportsCountTokens,
		SkipHealthCheck:      record.SkipHealthCheck,
	}

	// 设置 Enabled（是否作为代理端点）
	enabled := record.Enabled
	cfg.Enabled = &enabled

	// 设置 FailoverEnabled
	fe := record.FailoverEnabled
	cfg.FailoverEnabled = &fe

	// 设置 Cooldown
	if record.CooldownSeconds != nil {
		cd := time.Duration(*record.CooldownSeconds) * time.Second
		cfg.Cooldown = &cd
	}

	return cfg
}

func (s *EndpointService) credentialGroupOrder(ctx context.Context, channel string, credentialGroupID int64) int {
	if s.credentialGroupStore == nil || credentialGroupID <= 0 {
		return 0
	}
	groups, err := s.credentialGroupStore.ListByChannel(ctx, channel)
	if err != nil {
		return 0
	}
	for idx, group := range groups {
		if group != nil && group.ID == credentialGroupID {
			return idx
		}
	}
	return len(groups)
}

// EndpointRuntimeKey returns a collision-free runtime key for SQLite endpoints.
func EndpointRuntimeKey(record *store.EndpointRecord) string {
	if record == nil {
		return ""
	}
	if record.CredentialGroupID > 0 {
		return fmt.Sprintf("%s::g%d::%s", record.Channel, record.CredentialGroupID, record.Name)
	}
	return endpoint.EndpointKey(record.Channel, record.Name)
}

// configToRecord 将配置对象转换为数据库记录
func (s *EndpointService) configToRecord(cfg config.EndpointConfig) *store.EndpointRecord {
	record := &store.EndpointRecord{
		Channel:             cfg.Channel,
		Name:                cfg.Name,
		URL:                 cfg.URL,
		Token:               cfg.Token,
		ApiKey:              cfg.ApiKey,
		Headers:             cfg.Headers,
		Priority:            cfg.Priority,
		FailoverEnabled:     true, // 默认参与故障转移
		SkipHealthCheck:     cfg.SkipHealthCheck,
		TimeoutSeconds:      int(cfg.Timeout.Seconds()),
		SupportsCountTokens: cfg.SupportsCountTokens,
		CostMultiplier:      1.0,
		Enabled:             true,
	}

	if record.Channel == "" {
		// 兼容：未配置 channel 时回退为端点名
		record.Channel = cfg.Name
	}

	if cfg.FailoverEnabled != nil {
		record.FailoverEnabled = *cfg.FailoverEnabled
	}

	if cfg.Cooldown != nil {
		cd := int(cfg.Cooldown.Seconds())
		record.CooldownSeconds = &cd
	}

	return record
}

// maskToken 脱敏 Token
func maskToken(token string) string {
	if len(token) <= 8 {
		return "****"
	}
	return token[:4] + "****" + token[len(token)-4:]
}
