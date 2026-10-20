package endpoint

import (
	"strings"

	"github.com/wsyangzy/Vane/internal/routing"
)

// RouteConfig Claude 请求级路由的运行时快照：各渠道的模型路由规则与非流改道配置。
// 两者都挂在来源渠道上，只在该渠道为激活渠道时生效。
// 配置保存后由 App 整体替换，请求处理期间只读，不做原地修改。
type RouteConfig struct {
	// ModelRules 键为来源渠道名：Claude 运行时以渠道名路由
	ModelRules map[string][]routing.ModelRule
	// NonStreamRoutes 键为渠道名：Claude 运行时以渠道名路由
	NonStreamRoutes map[string]*routing.NonStreamRoute
	// ChannelNames 渠道 ID → 渠道名，用于解析以 ID 引用的路由目标
	ChannelNames map[int64]string
	// PrivacyLevels 渠道名 → 隐私级别（third_party / trusted）；未列出的渠道跟随全局
	PrivacyLevels map[string]string
}

// PrivacyLevel 返回渠道隐私级别；未配置时为空串（跟随全局）
func (c *RouteConfig) PrivacyLevel(channel string) string {
	if c == nil || channel == "" {
		return ""
	}
	return c.PrivacyLevels[channel]
}

// SetRouteConfig 发布新的路由快照；nil 表示关闭全部路由。
func (m *Manager) SetRouteConfig(cfg *RouteConfig) {
	if m == nil {
		return
	}
	m.routeConfig.Store(cfg)
}

// RouteConfig 返回当前路由快照，可能为 nil。
func (m *Manager) RouteConfig() *RouteConfig {
	if m == nil {
		return nil
	}
	return m.routeConfig.Load()
}

// ChannelName 把路由目标引用的渠道 ID 解析为运行时渠道名；渠道已删除时返回空串。
func (c *RouteConfig) ChannelName(channelID int64) string {
	if c == nil {
		return ""
	}
	return strings.TrimSpace(c.ChannelNames[channelID])
}

// NonStreamRoute 返回指定渠道的非流改道配置，未开启时为 nil。
func (c *RouteConfig) NonStreamRoute(channel string) *routing.NonStreamRoute {
	if c == nil || channel == "" {
		return nil
	}
	return c.NonStreamRoutes[channel]
}

// Rules 返回指定来源渠道已启用的模型路由规则
func (c *RouteConfig) Rules(channel string) []routing.ModelRule {
	if c == nil || channel == "" {
		return nil
	}
	return c.ModelRules[channel]
}
