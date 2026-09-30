package service

import (
	"context"
	"fmt"
	"sort"

	"ai-relay-forwarder/internal/providers/codex/store"
	"ai-relay-forwarder/internal/routing"
)

// BuildRouteCatalog 列出 Codex 全部渠道及其凭证分组，作为请求级路由的目标候选（按渠道优先级排列）。
func BuildRouteCatalog(ctx context.Context, channelStore store.CodexChannelStore, groupStore store.CodexCredentialGroupStore) (routing.Catalog, error) {
	if channelStore == nil {
		return nil, fmt.Errorf("Codex 渠道存储未初始化")
	}
	channels, err := channelStore.List(ctx)
	if err != nil {
		return nil, err
	}
	sort.SliceStable(channels, func(i, j int) bool {
		if channels[i].Priority != channels[j].Priority {
			return channels[i].Priority < channels[j].Priority
		}
		return channels[i].ID < channels[j].ID
	})
	catalog := make(routing.Catalog, 0, len(channels))
	for _, channel := range channels {
		if channel == nil {
			continue
		}
		entry := routing.CatalogChannel{
			ID:           channel.ID,
			Name:         channel.Name,
			Priority:     channel.Priority,
			Paused:       !channel.FailoverEnabled,
			Active:       channel.IsActive,
			NonStream:    channel.NonStreamRoute,
			PrivacyLevel: channel.PrivacyLevel,
		}
		if groupStore != nil {
			groups, err := groupStore.ListByChannelID(ctx, channel.ID)
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

type codexNonStreamRouteSetter interface {
	SetNonStreamRoute(ctx context.Context, id int64, route *routing.NonStreamRoute) error
}

type codexPrivacyLevelSetter interface {
	SetPrivacyLevel(ctx context.Context, id int64, level string) error
}

// SetPrivacyLevel 设置渠道隐私级别；level 须已由调用方用 privacy.NormalizeChannelLevel 规范化
func (s *CodexChannelService) SetPrivacyLevel(ctx context.Context, id int64, level string) error {
	if s == nil || s.channelStore == nil {
		return fmt.Errorf("Codex 渠道存储未初始化")
	}
	setter, ok := s.channelStore.(codexPrivacyLevelSetter)
	if !ok {
		return fmt.Errorf("当前 Codex 渠道存储不支持隐私级别")
	}
	return setter.SetPrivacyLevel(ctx, id, level)
}

// SetNonStreamRoute 设置渠道的非流改道：渠道不接非流请求时，非流请求改送到这些目标。
// route 为 nil 或没有目标表示关闭。目标必须是本 provider 下存在的其它渠道。
func (s *CodexChannelService) SetNonStreamRoute(ctx context.Context, id int64, route *routing.NonStreamRoute, catalog routing.Catalog) error {
	if s == nil || s.channelStore == nil {
		return fmt.Errorf("Codex 渠道存储未初始化")
	}
	existing, err := s.channelStore.GetByID(ctx, id)
	if err != nil {
		return err
	}
	if existing == nil {
		return fmt.Errorf("Codex 渠道不存在: %d", id)
	}
	var normalized *routing.NonStreamRoute
	if route != nil {
		targets := routing.NormalizeTargets(route.Targets)
		if len(targets) > 0 {
			mode := routing.NormalizeMode(route.Mode)
			if mode == "" {
				return fmt.Errorf("非流改道模式无效: %q（可选 prefer / fallback）", route.Mode)
			}
			for _, target := range targets {
				if target.ChannelID == id {
					return fmt.Errorf("非流改道的目标不能是渠道自身")
				}
			}
			if err := catalog.Validate(targets); err != nil {
				return err
			}
			normalized = &routing.NonStreamRoute{Mode: mode, Targets: targets}
		}
	}
	setter, ok := s.channelStore.(codexNonStreamRouteSetter)
	if !ok {
		return fmt.Errorf("当前 Codex 渠道存储不支持非流改道")
	}
	return setter.SetNonStreamRoute(ctx, id, normalized)
}
