package app

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	claudeproxyendpoint "github.com/wsyangzy/Vane/internal/providers/claude/proxy/endpoint"
	claudeservice "github.com/wsyangzy/Vane/internal/providers/claude/service"
	codexservice "github.com/wsyangzy/Vane/internal/providers/codex/service"
	"github.com/wsyangzy/Vane/internal/routing"
	routingservice "github.com/wsyangzy/Vane/internal/routing/service"
	routingstore "github.com/wsyangzy/Vane/internal/routing/store"
)

// RouteTargetInput 路由目标：用 ID 引用渠道与凭证分组，group_id=0 表示渠道下全部分组
type RouteTargetInput struct {
	ChannelID     int64  `json:"channel_id"`
	GroupID       int64  `json:"group_id"`
	UpstreamModel string `json:"upstream_model"`
}

// RouteTargetInfo 路由目标及其当前解析出的渠道/分组名
type RouteTargetInfo struct {
	ChannelID     int64  `json:"channel_id"`
	GroupID       int64  `json:"group_id"`
	UpstreamModel string `json:"upstream_model"`
	ChannelName   string `json:"channel_name"`
	GroupName     string `json:"group_name"`
	// Missing 目标渠道或凭证分组已被删除，命中时会被跳过
	Missing bool `json:"missing"`
}

// ModelRouteInfo 模型路由规则
type ModelRouteInfo struct {
	ID              int64             `json:"id"`
	Provider        string            `json:"provider"`
	SourceChannelID int64             `json:"source_channel_id"`
	Name            string            `json:"name"`
	Enabled         bool              `json:"enabled"`
	Priority        int               `json:"priority"`
	MatchModel      string            `json:"match_model"`
	Mode            string            `json:"mode"`
	Targets         []RouteTargetInfo `json:"targets"`
	UpdatedAt       string            `json:"updated_at"`
}

// SaveModelRouteInput 保存模型路由规则；ID 为 0 时新建。
// SourceChannelID 为规则所属渠道：只在该渠道为激活渠道时生效，创建后不可修改。
type SaveModelRouteInput struct {
	ID              int64              `json:"id"`
	Provider        string             `json:"provider"`
	SourceChannelID int64              `json:"source_channel_id"`
	Name            string             `json:"name"`
	Enabled         bool               `json:"enabled"`
	Priority        int                `json:"priority"`
	MatchModel      string             `json:"match_model"`
	Mode            string             `json:"mode"`
	Targets         []RouteTargetInput `json:"targets"`
}

// RouteSaveResult 保存结果；warnings 不阻止保存，只提示可能的配置问题
type RouteSaveResult struct {
	ID       int64    `json:"id"`
	Warnings []string `json:"warnings"`
}

// NonStreamRouteInput 渠道非流改道配置；targets 为空表示关闭
type NonStreamRouteInput struct {
	Mode    string             `json:"mode"`
	Targets []RouteTargetInput `json:"targets"`
}

// NonStreamRouteInfo 渠道非流改道配置（渠道列表 DTO 中返回）
type NonStreamRouteInfo struct {
	Mode    string            `json:"mode"`
	Targets []RouteTargetInfo `json:"targets"`
}

// RouteTargetGroupOption 目标选择器中的凭证分组
type RouteTargetGroupOption struct {
	ID   int64  `json:"id"`
	Name string `json:"name"`
}

// RouteTargetChannelOption 目标选择器中的渠道
type RouteTargetChannelOption struct {
	ID                    int64                    `json:"id"`
	Name                  string                   `json:"name"`
	Priority              int                      `json:"priority"`
	Paused                bool                     `json:"paused"`
	Active                bool                     `json:"active"`
	NonStreamRouteEnabled bool                     `json:"non_stream_route_enabled"`
	Groups                []RouteTargetGroupOption `json:"groups"`
}

// RouteTargetOptions 规则表单的候选项
type RouteTargetOptions struct {
	Channels []RouteTargetChannelOption `json:"channels"`
	// Models 本 provider 定价表中已有的模型名，作为"上游模型"的输入提示
	Models []string `json:"models"`
}

// RoutePreviewStage dry-run 候选链中的一个阶段
type RoutePreviewStage struct {
	Target        int    `json:"target"` // 0 为来源渠道（请求发起时的激活渠道）
	ChannelID     int64  `json:"channel_id"`
	ChannelName   string `json:"channel_name"`
	GroupID       int64  `json:"group_id"`
	GroupName     string `json:"group_name"`
	UpstreamModel string `json:"upstream_model"`
	Missing       bool   `json:"missing"`
	Paused        bool   `json:"paused"`
}

// RoutePreviewResult dry-run 结果：最终决策与候选链，不发任何上游请求
type RoutePreviewResult struct {
	Kind          string              `json:"kind"` // 空串表示走默认流程；model_rule / non_stream
	Mode          string              `json:"mode"`
	RuleID        int64               `json:"rule_id"`
	RuleName      string              `json:"rule_name"`
	SourceChannel string              `json:"source_channel"`
	Stages        []RoutePreviewStage `json:"stages"`
}

func routeCtx() (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), 10*time.Second)
}

// modelRouteSvc 懒初始化模型路由服务：与模型定价共用管理数据库
func (a *App) modelRouteSvc() (*routingservice.ModelRouteService, error) {
	a.mu.RLock()
	svc := a.modelRouteService
	db := a.storeDB
	if db == nil && a.usageTracker != nil {
		db = a.usageTracker.GetDB()
	}
	pricingStore := a.modelPricingStore
	a.mu.RUnlock()
	if svc != nil {
		return svc, nil
	}
	if db == nil {
		return nil, fmt.Errorf("模型路由服务未启用（管理数据库未就绪）")
	}
	svc = routingservice.NewModelRouteService(routingstore.NewSQLiteModelRouteStore(db), pricingStore)
	a.mu.Lock()
	if a.modelRouteService == nil {
		a.modelRouteService = svc
	}
	svc = a.modelRouteService
	a.mu.Unlock()
	return svc, nil
}

func normalizeProviderParam(provider string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(provider)) {
	case "", "claude":
		return "claude", nil
	case "codex":
		return "codex", nil
	default:
		return "", fmt.Errorf("不支持的 provider: %q", provider)
	}
}

// routeCatalog 构造 provider 的路由目标目录（全部渠道 + 凭证分组，按渠道优先级）
func (a *App) routeCatalog(ctx context.Context, provider string) (routing.Catalog, error) {
	if provider == "codex" {
		a.ensureCodexProviderService()
		a.mu.RLock()
		channelStore := a.codexChannelStore
		groupStore := a.codexCredentialGroupStore
		a.mu.RUnlock()
		return codexservice.BuildRouteCatalog(ctx, channelStore, groupStore)
	}
	a.mu.RLock()
	channelStore := a.channelStore
	groupStore := a.claudeCredentialGroupStore
	endpointManager := a.endpointManager
	a.mu.RUnlock()
	return claudeservice.BuildRouteCatalog(ctx, channelStore, groupStore, endpointManager.ActiveChannelName())
}

func routeTargetsFromInput(inputs []RouteTargetInput) []routing.Target {
	targets := make([]routing.Target, 0, len(inputs))
	for _, input := range inputs {
		targets = append(targets, routing.Target{ChannelID: input.ChannelID, GroupID: input.GroupID, UpstreamModel: input.UpstreamModel})
	}
	return targets
}

func routeTargetInfos(targets []routing.Target, catalog routing.Catalog) []RouteTargetInfo {
	infos := make([]RouteTargetInfo, 0, len(targets))
	for _, target := range targets {
		info := RouteTargetInfo{ChannelID: target.ChannelID, GroupID: target.GroupID, UpstreamModel: target.UpstreamModel}
		if channel := catalog.Channel(target.ChannelID); channel != nil {
			info.ChannelName = channel.Name
			info.GroupName = channel.GroupName(target.GroupID)
			info.Missing = target.GroupID > 0 && info.GroupName == ""
		} else {
			info.Missing = true
		}
		infos = append(infos, info)
	}
	return infos
}

// nonStreamRouteInfo 渠道列表 DTO 用的非流改道配置；关闭时返回 nil
func nonStreamRouteInfo(route *routing.NonStreamRoute, catalog routing.Catalog) *NonStreamRouteInfo {
	if route == nil {
		return nil
	}
	return &NonStreamRouteInfo{Mode: route.Mode, Targets: routeTargetInfos(route.Targets, catalog)}
}

// ListModelRoutes 列出某个来源渠道下的模型路由规则
func (a *App) ListModelRoutes(provider string, sourceChannelID int64) ([]ModelRouteInfo, error) {
	provider, err := normalizeProviderParam(provider)
	if err != nil {
		return nil, err
	}
	svc, err := a.modelRouteSvc()
	if err != nil {
		return nil, err
	}
	ctx, cancel := routeCtx()
	defer cancel()

	records, err := svc.ListBySource(ctx, provider, sourceChannelID)
	if err != nil {
		return nil, fmt.Errorf("获取模型路由规则失败: %w", err)
	}
	catalog, err := a.routeCatalog(ctx, provider)
	if err != nil {
		return nil, fmt.Errorf("获取路由目标失败: %w", err)
	}
	result := make([]ModelRouteInfo, 0, len(records))
	for _, record := range records {
		if record == nil {
			continue
		}
		info := ModelRouteInfo{
			ID:              record.ID,
			Provider:        record.Provider,
			SourceChannelID: record.SourceChannelID,
			Name:            record.Name,
			Enabled:         record.Enabled,
			Priority:        record.Priority,
			MatchModel:      record.MatchModel,
			Mode:            record.Mode,
			Targets:         routeTargetInfos(record.Targets, catalog),
		}
		if !record.UpdatedAt.IsZero() {
			info.UpdatedAt = record.UpdatedAt.Format("2006-01-02 15:04:05")
		}
		result = append(result, info)
	}
	return result, nil
}

// SaveModelRoute 新建或更新模型路由规则，保存后立即生效
func (a *App) SaveModelRoute(input SaveModelRouteInput) (RouteSaveResult, error) {
	provider, err := normalizeProviderParam(input.Provider)
	if err != nil {
		return RouteSaveResult{}, err
	}
	svc, err := a.modelRouteSvc()
	if err != nil {
		return RouteSaveResult{}, err
	}
	ctx, cancel := routeCtx()
	defer cancel()
	if err := a.requireLegacyRouting(ctx, provider); err != nil {
		return RouteSaveResult{}, err
	}

	catalog, err := a.routeCatalog(ctx, provider)
	if err != nil {
		return RouteSaveResult{}, fmt.Errorf("获取路由目标失败: %w", err)
	}
	saved, warnings, err := svc.Save(ctx, &routingstore.ModelRouteRecord{
		ID:              input.ID,
		Provider:        provider,
		SourceChannelID: input.SourceChannelID,
		Name:            input.Name,
		Enabled:         input.Enabled,
		Priority:        input.Priority,
		MatchModel:      input.MatchModel,
		Mode:            input.Mode,
		Targets:         routeTargetsFromInput(input.Targets),
	}, catalog)
	if err != nil {
		return RouteSaveResult{}, err
	}
	a.syncRouteRuntime(ctx, provider)
	return RouteSaveResult{ID: saved.ID, Warnings: warnings}, nil
}

// DeleteModelRoute 删除模型路由规则
func (a *App) DeleteModelRoute(id int64) error {
	svc, err := a.modelRouteSvc()
	if err != nil {
		return err
	}
	ctx, cancel := routeCtx()
	defer cancel()

	existing, err := svc.Get(ctx, id)
	if err != nil {
		return err
	}
	if existing == nil {
		return fmt.Errorf("模型路由规则不存在: %d", id)
	}
	if err := a.requireLegacyRouting(ctx, existing.Provider); err != nil {
		return err
	}
	if err := svc.Delete(ctx, id); err != nil {
		return err
	}
	a.syncRouteRuntime(ctx, existing.Provider)
	return nil
}

// SetModelRouteEnabled 启用或停用模型路由规则
func (a *App) SetModelRouteEnabled(id int64, enabled bool) error {
	svc, err := a.modelRouteSvc()
	if err != nil {
		return err
	}
	ctx, cancel := routeCtx()
	defer cancel()

	existing, err := svc.Get(ctx, id)
	if err != nil {
		return err
	}
	if existing == nil {
		return fmt.Errorf("模型路由规则不存在: %d", id)
	}
	if err := a.requireLegacyRouting(ctx, existing.Provider); err != nil {
		return err
	}
	if err := svc.SetEnabled(ctx, id, enabled); err != nil {
		return err
	}
	a.syncRouteRuntime(ctx, existing.Provider)
	return nil
}

// GetRouteTargetOptions 规则表单的候选项：渠道 → 凭证分组，以及定价表中的模型名
func (a *App) GetRouteTargetOptions(provider string) (RouteTargetOptions, error) {
	provider, err := normalizeProviderParam(provider)
	if err != nil {
		return RouteTargetOptions{}, err
	}
	ctx, cancel := routeCtx()
	defer cancel()

	catalog, err := a.routeCatalog(ctx, provider)
	if err != nil {
		return RouteTargetOptions{}, fmt.Errorf("获取路由目标失败: %w", err)
	}
	options := RouteTargetOptions{Channels: make([]RouteTargetChannelOption, 0, len(catalog)), Models: []string{}}
	for _, channel := range catalog {
		option := RouteTargetChannelOption{
			ID:                    channel.ID,
			Name:                  channel.Name,
			Priority:              channel.Priority,
			Paused:                channel.Paused,
			Active:                channel.Active,
			NonStreamRouteEnabled: channel.NonStream != nil,
			Groups:                make([]RouteTargetGroupOption, 0, len(channel.Groups)),
		}
		for _, group := range channel.Groups {
			option.Groups = append(option.Groups, RouteTargetGroupOption{ID: group.ID, Name: group.Name})
		}
		options.Channels = append(options.Channels, option)
	}

	a.mu.RLock()
	pricingStore := a.modelPricingStore
	a.mu.RUnlock()
	if pricingStore != nil {
		if records, err := pricingStore.ListByProvider(ctx, provider); err == nil {
			seen := make(map[string]struct{}, len(records))
			for _, record := range records {
				if record == nil || strings.HasPrefix(record.ModelName, "_") {
					continue
				}
				if _, ok := seen[record.ModelName]; ok {
					continue
				}
				seen[record.ModelName] = struct{}{}
				options.Models = append(options.Models, record.ModelName)
			}
			sort.Strings(options.Models)
		}
	}
	return options, nil
}

// PreviewRoute dry-run：假设 sourceChannelID 为激活渠道，按已保存的配置给出最终决策与候选链，不发任何上游请求
func (a *App) PreviewRoute(provider string, sourceChannelID int64, model string, streaming bool) (RoutePreviewResult, error) {
	provider, err := normalizeProviderParam(provider)
	if err != nil {
		return RoutePreviewResult{}, err
	}
	svc, err := a.modelRouteSvc()
	if err != nil {
		return RoutePreviewResult{}, err
	}
	ctx, cancel := routeCtx()
	defer cancel()

	rules, err := svc.EnabledRules(ctx, provider)
	if err != nil {
		return RoutePreviewResult{}, err
	}
	catalog, err := a.routeCatalog(ctx, provider)
	if err != nil {
		return RoutePreviewResult{}, fmt.Errorf("获取路由目标失败: %w", err)
	}
	source := catalog.Channel(sourceChannelID)
	if source == nil {
		return RoutePreviewResult{}, fmt.Errorf("渠道不存在: %d", sourceChannelID)
	}
	result := RoutePreviewResult{Stages: []RoutePreviewStage{}, SourceChannel: source.Name}
	decision := routing.Decide(routing.RulesBySource(rules)[source.ID], source.NonStream, strings.TrimSpace(model), streaming)
	if !decision.Routed() {
		return result, nil
	}
	result.Kind = string(decision.Kind)
	result.Mode = decision.Mode
	if decision.Rule != nil {
		result.RuleID = decision.Rule.ID
		result.RuleName = decision.Rule.Name
	}
	for _, stage := range decision.Stages() {
		preview := RoutePreviewStage{Target: stage.Index}
		channel := source
		if stage.Target != nil {
			preview.ChannelID = stage.Target.ChannelID
			preview.GroupID = stage.Target.GroupID
			preview.UpstreamModel = stage.Target.UpstreamModel
			channel = catalog.Channel(stage.Target.ChannelID)
		}
		if channel == nil {
			preview.Missing = true
		} else {
			preview.ChannelID = channel.ID
			preview.ChannelName = channel.Name
			preview.GroupName = channel.GroupName(preview.GroupID)
			preview.Missing = preview.GroupID > 0 && preview.GroupName == ""
			// 预演假设来源渠道为激活渠道：它不会因暂停被跳过；目标渠道暂停时才会被跳过
			preview.Paused = channel.Paused && channel.ID != source.ID
		}
		result.Stages = append(result.Stages, preview)
	}
	return result, nil
}

// SetChannelNonStreamRoute 设置渠道的非流改道（渠道编辑表单的扩展选项），保存后立即生效
func (a *App) SetChannelNonStreamRoute(provider string, channelID int64, input NonStreamRouteInput) (RouteSaveResult, error) {
	provider, err := normalizeProviderParam(provider)
	if err != nil {
		return RouteSaveResult{}, err
	}
	ctx, cancel := routeCtx()
	defer cancel()
	if err := a.requireLegacyRouting(ctx, provider); err != nil {
		return RouteSaveResult{}, err
	}

	catalog, err := a.routeCatalog(ctx, provider)
	if err != nil {
		return RouteSaveResult{}, fmt.Errorf("获取路由目标失败: %w", err)
	}
	channel := catalog.Channel(channelID)
	if channel == nil {
		return RouteSaveResult{}, fmt.Errorf("渠道不存在: %d", channelID)
	}
	route := &routing.NonStreamRoute{Mode: input.Mode, Targets: routeTargetsFromInput(input.Targets)}

	if provider == "codex" {
		a.mu.RLock()
		channelService := a.codexChannelService
		a.mu.RUnlock()
		if channelService == nil {
			return RouteSaveResult{}, fmt.Errorf("Codex 渠道服务未就绪，请稍后重试")
		}
		a.codexActivationMu.Lock()
		err = channelService.SetNonStreamRoute(ctx, channelID, route, catalog)
		if err == nil {
			err = a.reloadCodexEndpointManager()
		}
		a.codexActivationMu.Unlock()
		if err != nil {
			return RouteSaveResult{}, err
		}
		a.emitCodexProviderUpdate()
	} else {
		a.mu.RLock()
		channelService := a.channelService
		a.mu.RUnlock()
		if channelService == nil {
			return RouteSaveResult{}, fmt.Errorf("渠道存储服务未启用")
		}
		if err := channelService.SetNonStreamRoute(ctx, channel.Name, route, catalog); err != nil {
			return RouteSaveResult{}, err
		}
		a.syncRouteRuntime(ctx, provider)
		a.emitEndpointUpdate()
	}

	var warnings []string
	if svc, err := a.modelRouteSvc(); err == nil {
		warnings = svc.TargetWarnings(ctx, provider, routing.NormalizeTargets(route.Targets), catalog)
	}
	return RouteSaveResult{ID: channelID, Warnings: warnings}, nil
}

// deleteChannelModelRoutes 渠道删除后清理其下的模型路由规则；失败只记日志，
// 残留规则的 source_channel_id 指向已不存在的渠道，运行时不会匹配到。
func (a *App) deleteChannelModelRoutes(ctx context.Context, provider string, channelID int64) {
	svc, err := a.modelRouteSvc()
	if err != nil || channelID <= 0 {
		return
	}
	if err := svc.DeleteBySource(ctx, provider, channelID); err != nil {
		a.mu.RLock()
		logger := a.logger
		a.mu.RUnlock()
		if logger != nil {
			logger.Warn("⚠️ 清理已删除渠道的模型路由规则失败", "provider", provider, "channel_id", channelID, "error", err)
		}
	}
}

// SetChannelPrivacyLevel retains the desktop API signature for older callers.
// Privacy decisions now belong exclusively to the global protection settings.
func (a *App) SetChannelPrivacyLevel(_ string, _ int64, _ string) error {
	return fmt.Errorf("渠道隐私级别已取消，请在隐私防护中统一设置")
}

// syncRouteRuntime 把已保存的路由配置发布到运行时；失败只记日志，运行时保留上一份快照。
func (a *App) syncRouteRuntime(ctx context.Context, provider string) {
	if provider == "codex" {
		a.syncCodexModelRules(ctx)
		return
	}
	a.syncClaudeRouteConfig(ctx)
}

// syncClaudeRouteConfig 重建 Claude 路由快照：已启用规则 + 各渠道非流改道 + 渠道 ID→名称
func (a *App) syncClaudeRouteConfig(ctx context.Context) {
	if _, err := a.refreshPoolRuntime(ctx, "claude"); err != nil {
		a.closePoolRuntime("claude")
		if a.logger != nil {
			a.logger.Error("Claude 账号池加载失败", "error", err)
		}
	} else {
		a.publishPoolRuntime("claude")
	}
	a.mu.RLock()
	endpointManager := a.endpointManager
	logger := a.logger
	a.mu.RUnlock()
	if endpointManager == nil {
		return
	}
	cfg, err := a.buildClaudeRouteConfig(ctx)
	if err != nil {
		if logger != nil {
			logger.Warn("⚠️ 同步 Claude 请求级路由配置失败，保留上一份配置", "error", err)
		}
		return
	}
	endpointManager.SetRouteConfig(cfg)
}

func (a *App) buildClaudeRouteConfig(ctx context.Context) (*claudeproxyendpoint.RouteConfig, error) {
	svc, err := a.modelRouteSvc()
	if err != nil {
		return nil, err
	}
	rules, err := svc.EnabledRules(ctx, "claude")
	if err != nil {
		return nil, err
	}
	catalog, err := a.routeCatalog(ctx, "claude")
	if err != nil {
		return nil, err
	}
	cfg := &claudeproxyendpoint.RouteConfig{
		ModelRules:      make(map[string][]routing.ModelRule),
		NonStreamRoutes: make(map[string]*routing.NonStreamRoute),
		ChannelNames:    make(map[int64]string, len(catalog)),
	}
	bySource := routing.RulesBySource(rules)
	for _, channel := range catalog {
		cfg.ChannelNames[channel.ID] = channel.Name
		if channel.NonStream != nil {
			cfg.NonStreamRoutes[channel.Name] = channel.NonStream
		}
		if channelRules := bySource[channel.ID]; len(channelRules) > 0 {
			cfg.ModelRules[channel.Name] = channelRules
		}
	}
	return cfg, nil
}

// syncCodexModelRules 把 Codex 已启用规则发布到端点管理器（渠道非流改道随 Reload 加载）
func (a *App) syncCodexModelRules(ctx context.Context) {
	if _, err := a.refreshPoolRuntime(ctx, "codex"); err != nil {
		a.closePoolRuntime("codex")
		if a.logger != nil {
			a.logger.Error("Codex 账号池加载失败", "error", err)
		}
	} else {
		a.publishPoolRuntime("codex")
	}
	a.mu.RLock()
	manager := a.codexEndpointMgr
	logger := a.logger
	a.mu.RUnlock()
	if manager == nil {
		return
	}
	svc, err := a.modelRouteSvc()
	if err != nil {
		return
	}
	rules, err := svc.EnabledRules(ctx, "codex")
	if err != nil {
		if logger != nil {
			logger.Warn("⚠️ 同步 Codex 模型路由规则失败，保留上一份规则", "error", err)
		}
		return
	}
	manager.SetModelRules(rules)
}
