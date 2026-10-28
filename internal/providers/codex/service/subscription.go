package service

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/wsyangzy/Vane/internal/providers/codex/proxy/subscription"
	"github.com/wsyangzy/Vane/internal/providers/codex/store"
	"github.com/wsyangzy/Vane/internal/routing"
)

func (s *CodexCredentialGroupService) SetAccountValidator(validate func(context.Context, string) error) {
	s.validateAccount = validate
}

func (s *CodexCredentialGroupService) normalizeSubscription(ctx context.Context, r, old *store.CodexCredentialGroupRecord) error {
	if r == nil {
		return fmt.Errorf("record 不能为空")
	}
	if r.AuthMode == "" {
		r.AuthMode = subscription.GroupCredential
		if old != nil && old.AuthMode != "" {
			r.AuthMode = old.AuthMode
		}
	}
	if r.AuthMode != subscription.GroupCredential && r.AuthMode != subscription.ClientPassthrough {
		return fmt.Errorf("未知的 Codex 认证模式")
	}
	if r.Subscription == nil && old != nil && old.Subscription != nil {
		copy := *old.Subscription
		r.Subscription = &copy
	}
	if r.AuthMode == subscription.GroupCredential {
		if old != nil && old.AuthMode == subscription.ClientPassthrough && r.Subscription != nil {
			r.Subscription.Version++
			r.Subscription.Enabled = false
		}
		return nil
	}
	if strings.TrimSpace(r.ApiKey) != "" {
		return fmt.Errorf("订阅分组不能保存 API Key")
	}
	if r.Subscription == nil {
		return fmt.Errorf("请配置订阅凭证来源")
	}
	b := r.Subscription
	if b.Action == "rebind" && b.AccountID != "" && old != nil && old.Subscription != nil && old.Subscription.AccountID == b.AccountID {
		return fmt.Errorf("托管账号未改变，请使用请求验证恢复；重新绑定需选择新的独立账号")
	}
	if old != nil && old.Subscription != nil {
		prior := old.Subscription
		if b.Version != 0 && b.Version != prior.Version {
			return fmt.Errorf("订阅配置已变化，请刷新后重试")
		}
		if b.KeyHash == "" {
			b.KeyHash = prior.KeyHash
		}
		before, _ := json.Marshal(prior.BackupTargets)
		after, _ := json.Marshal(b.BackupTargets)
		b.Version = prior.Version
		if b.Enabled != prior.Enabled || b.KeyHash != prior.KeyHash || b.Action != "" || string(before) != string(after) || b.AccountID != prior.AccountID || old.AuthMode != r.AuthMode {
			b.Version++
		}
	} else {
		b.Version = 1
	}
	if b.Action != "" && b.Action != "verify" && b.Action != "rebind" {
		return fmt.Errorf("未知的订阅绑定操作")
	}
	digest, e := hex.DecodeString(b.KeyHash)
	if b.KeyHash != "" && (e != nil || len(digest) != 32) {
		return fmt.Errorf("历史本地密钥哈希无效")
	}
	b.KeyHash = strings.ToLower(b.KeyHash)
	b.GroupID = r.ID
	b.ChannelID = r.ChannelID
	channel, e := s.channelStore.GetByID(ctx, r.ChannelID)
	if e != nil {
		return e
	}
	if channel == nil {
		return fmt.Errorf("Codex 渠道不存在")
	}
	b.Channel = channel.Name
	if b.AccountID != "" && (s.validateAccount == nil || s.validateAccount(ctx, b.AccountID) != nil) {
		return fmt.Errorf("订阅账号不存在、已禁用或需要重新登录")
	}
	channels, e := s.channelStore.List(ctx)
	if e != nil {
		return e
	}
	for _, ch := range channels {
		groups, e := s.groupStore.ListByChannelID(ctx, ch.ID)
		if e != nil {
			return e
		}
		for _, g := range groups {
			if g.ID == r.ID || g.AuthMode != subscription.ClientPassthrough || g.Subscription == nil {
				continue
			}
			if b.KeyHash != "" && g.Subscription.KeyHash == b.KeyHash {
				return fmt.Errorf("本地密钥已绑定其他调用方")
			}
			if b.AccountID != "" && g.Subscription.AccountID == b.AccountID {
				return fmt.Errorf("该订阅账号已绑定其他调用方，请使用独立订阅")
			}
		}
	}
	if len(b.BackupTargets) > 16 {
		return fmt.Errorf("订阅备用目标最多 16 个")
	}
	for _, t := range b.BackupTargets {
		if t.ChannelID <= 0 || t.GroupID <= 0 {
			return fmt.Errorf("备用目标必须精确指定渠道及普通凭证分组")
		}
	}
	b.BackupTargets = routing.NormalizeTargets(b.BackupTargets)
	if len(b.BackupTargets) > 0 {
		catalog, e := BuildRouteCatalog(ctx, s.channelStore, s.groupStore)
		if e != nil {
			return e
		}
		if e = catalog.Validate(b.BackupTargets); e != nil {
			return e
		}
	}
	for _, t := range b.BackupTargets {
		g, e := s.groupStore.GetByID(ctx, t.GroupID)
		if e != nil {
			return e
		}
		if g == nil || g.AuthMode == subscription.ClientPassthrough || g.ChannelID != t.ChannelID || g.ApiKey == "" {
			return fmt.Errorf("备用目标必须是已配置 API Key 的普通分组")
		}
	}
	if s.endpointStore != nil && r.ID > 0 {
		eps, e := s.endpointStore.ListByChannelID(ctx, r.ChannelID)
		if e != nil {
			return e
		}
		for _, ep := range eps {
			if ep.CredentialGroupID != r.ID {
				continue
			}
			if e = subscription.ValidateEndpoint(ep.BaseURL); e != nil {
				return e
			}
			if ep.ApiKey != "" {
				return fmt.Errorf("订阅端点不能保存 API Key")
			}
		}
	}
	return nil
}
