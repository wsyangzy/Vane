package service

import (
	"context"
	"fmt"
	"strings"

	"github.com/wsyangzy/Vane/internal/providers/claude/proxy/subscription"
	"github.com/wsyangzy/Vane/internal/providers/claude/store"
)

func (s *EndpointService) ensureEndpointCredentialGroup(ctx context.Context, record *store.EndpointRecord) error {
	if s.credentialGroupStore == nil || record == nil {
		return nil
	}
	var group *store.ClaudeCredentialGroupRecord
	if record.CredentialGroupID > 0 {
		var err error
		group, err = s.credentialGroupStore.GetByID(ctx, record.CredentialGroupID)
		if err != nil {
			return err
		}
		if group == nil {
			return fmt.Errorf("Claude 凭证分组不存在: %d", record.CredentialGroupID)
		}
		if group.Channel != record.Channel {
			return fmt.Errorf("凭证分组不属于当前渠道")
		}
	} else {
		var err error
		group, err = s.credentialGroupStore.GetDefaultByChannel(ctx, record.Channel)
		if err != nil {
			return err
		}
		if group == nil {
			return fmt.Errorf("Claude 渠道 '%s' 尚未配置凭证分组，不能创建端点", record.Channel)
		}
	}
	if group.AuthMode == "client_passthrough" {
		if err := subscription.ValidateEndpoint(record.URL, record.Headers); err != nil {
			return err
		}
		if record.Token != "" || record.ApiKey != "" {
			return fmt.Errorf("订阅端点不能保存 Token 或 API Key")
		}
	} else if strings.TrimSpace(group.Token) == "" && strings.TrimSpace(group.ApiKey) == "" &&
		strings.TrimSpace(record.Token) == "" && strings.TrimSpace(record.ApiKey) == "" {
		return fmt.Errorf("Claude 凭证分组 '%s' 尚未配置 Token 或 API Key，不能创建端点", group.Name)
	}
	record.CredentialGroupID = group.ID
	record.CredentialGroupName = group.Name
	return nil
}

func (s *EndpointService) validateCredentialGroup(ctx context.Context, channel string, credentialGroupID int64) error {
	if s.credentialGroupStore == nil || credentialGroupID <= 0 {
		return nil
	}
	group, err := s.credentialGroupStore.GetByID(ctx, credentialGroupID)
	if err != nil {
		return err
	}
	if group == nil {
		return fmt.Errorf("Claude 凭证分组不存在: %d", credentialGroupID)
	}
	if group.Channel != channel {
		return fmt.Errorf("凭证分组不属于当前渠道")
	}
	return nil
}

func (s *EndpointService) findEndpointNameConflict(ctx context.Context, channel string, credentialGroupID int64, name string) (*store.EndpointRecord, error) {
	conflict, err := s.store.GetByChannelCredentialGroupAndName(ctx, channel, credentialGroupID, name)
	if err != nil || conflict != nil {
		return conflict, err
	}
	if !s.isDefaultCredentialGroup(ctx, channel, credentialGroupID) {
		return nil, nil
	}
	return s.store.GetByChannelCredentialGroupAndName(ctx, channel, 0, name)
}

func (s *EndpointService) findEndpointURLConflict(ctx context.Context, channel string, credentialGroupID int64, url string) (*store.EndpointRecord, error) {
	conflict, err := s.store.GetByChannelCredentialGroupAndURL(ctx, channel, credentialGroupID, url)
	if err != nil || conflict != nil {
		return conflict, err
	}
	if !s.isDefaultCredentialGroup(ctx, channel, credentialGroupID) {
		return nil, nil
	}
	return s.store.GetByChannelCredentialGroupAndURL(ctx, channel, 0, url)
}

func (s *EndpointService) isDefaultCredentialGroup(ctx context.Context, channel string, credentialGroupID int64) bool {
	if s.credentialGroupStore == nil || credentialGroupID <= 0 {
		return false
	}
	group, err := s.credentialGroupStore.GetDefaultByChannel(ctx, channel)
	return err == nil && group != nil && group.ID == credentialGroupID
}
