package service

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/wsyangzy/Vane/internal/providers/claude/proxy/subscription"
	"github.com/wsyangzy/Vane/internal/providers/claude/store"
	"github.com/wsyangzy/Vane/internal/routing"
)

func (s *ClaudeCredentialGroupService) SetAccountValidator(validate func(context.Context, string) error) {
	s.validateAccount = validate
}

func (s *ClaudeCredentialGroupService) normalizeSubscription(ctx context.Context, record, existing *store.ClaudeCredentialGroupRecord) error {
	record.Channel = strings.TrimSpace(record.Channel)
	record.Token = strings.TrimSpace(record.Token)
	record.ApiKey = strings.TrimSpace(record.ApiKey)
	if record.AuthMode == "" {
		if existing != nil {
			record.AuthMode = existing.AuthMode
		}
		if record.AuthMode == "" {
			record.AuthMode = subscription.GroupCredential
		}
	}
	if record.AuthMode != subscription.GroupCredential && record.AuthMode != subscription.ClientPassthrough {
		return fmt.Errorf("未知的 Claude 认证模式")
	}
	if record.Subscription == nil && existing != nil && existing.Subscription != nil {
		copy := *existing.Subscription
		record.Subscription = &copy
	}
	if record.AuthMode == subscription.GroupCredential {
		if existing != nil && existing.AuthMode == subscription.ClientPassthrough && record.Subscription != nil {
			record.Subscription.Version++
			record.Subscription.Enabled = false
		}
		return nil
	}
	if record.Token != "" || record.ApiKey != "" {
		return fmt.Errorf("订阅透传不保存 Token 或 API Key；请清空分组凭证")
	}
	if record.Subscription == nil {
		return fmt.Errorf("请配置订阅凭证来源")
	}
	b := record.Subscription
	if b.Action == "rebind" && b.AccountID != "" && existing != nil && existing.Subscription != nil && existing.Subscription.AccountID == b.AccountID {
		return fmt.Errorf("托管账号未改变，请使用请求验证恢复；重新绑定需选择新的独立账号")
	}
	if b.AccountID != "" && (s.validateAccount == nil || s.validateAccount(ctx, b.AccountID) != nil) {
		return fmt.Errorf("订阅账号不存在、已禁用或需要重新登录")
	}
	if existing != nil && existing.Subscription != nil {
		old := existing.Subscription
		if b.Version != 0 && b.Version != old.Version {
			return fmt.Errorf("订阅配置已变化，请刷新后重试")
		}
		if b.KeyHash == "" {
			b.KeyHash = old.KeyHash
		}
		b.Version = old.Version
		before, _ := json.Marshal(old.BackupTargets)
		after, _ := json.Marshal(b.BackupTargets)
		if b.Action != "" || b.AccountID != old.AccountID || b.Enabled != old.Enabled || b.KeyHash != old.KeyHash || string(before) != string(after) || existing.AuthMode != record.AuthMode {
			b.Version++
		}
	} else {
		b.Version = 1
	}
	if b.Action != "" && b.Action != "rebind" && b.Action != "verify" {
		return fmt.Errorf("未知的订阅绑定操作")
	}
	b.GroupID = record.ID
	b.Channel = record.Channel
	decoded, err := hex.DecodeString(b.KeyHash)
	if b.KeyHash != "" && (err != nil || len(decoded) != 32) {
		return fmt.Errorf("历史本地密钥哈希无效")
	}
	b.KeyHash = strings.ToLower(b.KeyHash)
	if s.channelStore != nil {
		channels, err := s.channelStore.List(ctx)
		if err != nil {
			return err
		}
		for _, channel := range channels {
			groups, err := s.groupStore.ListByChannel(ctx, channel.Name)
			if err != nil {
				return err
			}
			for _, group := range groups {
				if group.ID != record.ID && b.AccountID != "" && group.AuthMode == subscription.ClientPassthrough && group.Subscription != nil && group.Subscription.AccountID == b.AccountID {
					return fmt.Errorf("该订阅账号已绑定其他调用方，请使用独立订阅")
				}
				if b.KeyHash != "" && group.ID != record.ID && group.AuthMode == subscription.ClientPassthrough && group.Subscription != nil && group.Subscription.KeyHash == b.KeyHash {
					return fmt.Errorf("本地密钥已绑定其他调用方，请生成独立密钥")
				}
			}
		}
	}
	if len(b.BackupTargets) > 16 {
		return fmt.Errorf("订阅备用目标最多 16 个")
	}
	for _, target := range b.BackupTargets {
		if target.ChannelID <= 0 || target.GroupID <= 0 {
			return fmt.Errorf("订阅备用目标必须指定有效渠道及凭证分组")
		}
	}
	b.BackupTargets = routing.NormalizeTargets(b.BackupTargets)
	if len(b.BackupTargets) > 0 {
		catalog, err := BuildRouteCatalog(ctx, s.channelStore, s.groupStore, "")
		if err != nil {
			return err
		}
		if err := catalog.Validate(b.BackupTargets); err != nil {
			return err
		}
		for _, target := range b.BackupTargets {
			if target.GroupID <= 0 {
				return fmt.Errorf("订阅备用目标必须指定凭证分组")
			}
			group, err := s.groupStore.GetByID(ctx, target.GroupID)
			if err != nil {
				return err
			}
			if group == nil || group.AuthMode == subscription.ClientPassthrough {
				return fmt.Errorf("备用目标必须是普通分组凭证，不能使用其他调用方订阅")
			}
			if group.Token == "" && group.ApiKey == "" {
				return fmt.Errorf("备用目标尚未配置凭证")
			}
		}
	}
	if s.endpointStore != nil && record.ID > 0 {
		endpoints, err := s.endpointStore.ListByChannel(ctx, record.Channel)
		if err != nil {
			return err
		}
		for _, ep := range endpoints {
			if ep.CredentialGroupID == record.ID {
				if err := subscription.ValidateEndpoint(ep.URL, ep.Headers); err != nil {
					return err
				}
				if ep.Token != "" || ep.ApiKey != "" {
					return fmt.Errorf("订阅端点不能保存凭证")
				}
			}
		}
	}
	return nil
}
