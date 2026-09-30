package service

import (
	"context"
	"fmt"
	"strings"

	"ai-relay-forwarder/internal/providers/claude/store"
	"ai-relay-forwarder/internal/routing"
)

// BuildRouteCatalog 列出 Claude 全部渠道及其凭证分组，作为请求级路由的目标候选。
// activeChannel 为当前全局激活渠道名，用于标注来源渠道。
func BuildRouteCatalog(ctx context.Context, channelStore store.ChannelStore, groupStore store.ClaudeCredentialGroupStore, activeChannel string) (routing.Catalog, error) {
	if channelStore == nil {
		return nil, fmt.Errorf("渠道存储服务未启用")
	}
	channels, err := channelStore.List(ctx)
	if err != nil {
		return nil, err
	}
	catalog := make(routing.Catalog, 0, len(channels))
	for _, channel := range channels {
		if channel == nil || channel.Name == "" {
			continue
		}
		entry := routing.CatalogChannel{
			ID:        channel.ID,
			Name:      channel.Name,
			Priority:  channel.Priority,
			Paused:    !channel.FailoverEnabled,
			Active:    channel.Name == activeChannel,
			NonStream: channel.NonStreamRoute,
		}
		if groupStore != nil {
			groups, err := groupStore.ListByChannel(ctx, channel.Name)
			if err != nil {
				return nil, err
			}
			for _, group := range groups {
				if group != nil {
					entry.Groups = append(entry.Groups, routing.CatalogGroup{ID: group.ID, Name: group.Name})
				}
			}
		}
		catalog = append(catalog, entry)
	}
	return catalog, nil
}

type nonStreamRouteSetter interface {
	SetNonStreamRoute(ctx context.Context, name string, route *routing.NonStreamRoute) error
}

// SetNonStreamRoute 设置渠道的非流改道：渠道不接非流请求时，非流请求改送到这些目标。
// route 为 nil 或没有目标表示关闭。目标必须是本 provider 下存在的其它渠道。
func (s *ChannelService) SetNonStreamRoute(ctx context.Context, name string, route *routing.NonStreamRoute, catalog routing.Catalog) error {
	if s == nil || s.store == nil {
		return fmt.Errorf("渠道存储服务未启用")
	}
	name = strings.TrimSpace(name)
	existing, err := s.store.Get(ctx, name)
	if err != nil {
		return err
	}
	if existing == nil {
		return fmt.Errorf("渠道不存在: %s", name)
	}
	normalized, err := normalizeNonStreamRoute(route, existing.ID, catalog)
	if err != nil {
		return err
	}
	setter, ok := s.store.(nonStreamRouteSetter)
	if !ok {
		return fmt.Errorf("当前渠道存储不支持非流改道")
	}
	return setter.SetNonStreamRoute(ctx, name, normalized)
}

// normalizeNonStreamRoute 校验并规整非流改道配置；返回 nil 表示关闭
func normalizeNonStreamRoute(route *routing.NonStreamRoute, sourceChannelID int64, catalog routing.Catalog) (*routing.NonStreamRoute, error) {
	if route == nil {
		return nil, nil
	}
	targets := routing.NormalizeTargets(route.Targets)
	if len(targets) == 0 {
		return nil, nil
	}
	mode := routing.NormalizeMode(route.Mode)
	if mode == "" {
		return nil, fmt.Errorf("非流改道模式无效: %q（可选 prefer / fallback）", route.Mode)
	}
	for _, target := range targets {
		if target.ChannelID == sourceChannelID {
			return nil, fmt.Errorf("非流改道的目标不能是渠道自身")
		}
	}
	if err := catalog.Validate(targets); err != nil {
		return nil, err
	}
	return &routing.NonStreamRoute{Mode: mode, Targets: targets}, nil
}
