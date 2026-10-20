package app

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	claudeservice "github.com/wsyangzy/Vane/internal/providers/claude/service"
	claudestore "github.com/wsyangzy/Vane/internal/providers/claude/store"
	"github.com/wsyangzy/Vane/internal/routing"
)

type ChannelInfo struct {
	ID               int64                       `json:"id"`
	Name             string                      `json:"name"`
	Website          string                      `json:"website,omitempty"`
	Priority         int                         `json:"priority"`
	ModelMapping     map[string]string           `json:"model_mapping"`
	NonStreamRoute   *NonStreamRouteInfo         `json:"non_stream_route"` // 非流改道配置，关闭时为 null
	PrivacyLevel     string                      `json:"privacy_level"`    // 隐私级别：""=跟随全局 / third_party / trusted
	CreatedAt        string                      `json:"created_at"`
	EndpointCount    int                         `json:"endpoint_count"`
	CredentialGroups []ClaudeCredentialGroupInfo `json:"credential_groups"`
}

func (a *App) GetChannels() ([]ChannelInfo, error) {
	a.mu.RLock()
	endpointService := a.endpointService
	channelService := a.channelService
	usageTracker := a.usageTracker
	claudeCredentialGroupService := a.claudeCredentialGroupService
	claudeCredentialGroupStore := a.claudeCredentialGroupStore
	a.mu.RUnlock()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	channelCount := make(map[string]int)
	// 统计每个渠道的端点数（兼容：端点表中存在但 channels 表未收录的渠道）
	// 优先用 EndpointService（字段最全）；若因旧库缺字段导致失败，则降级为聚合 SQL（只依赖 channel 列）
	if endpointService != nil {
		records, err := endpointService.ListEndpoints(ctx)
		if err == nil {
			for _, r := range records {
				if r.Channel == "" {
					continue
				}
				channelCount[r.Channel]++
			}
		} else if usageTracker != nil && usageTracker.GetDB() != nil {
			rows, qerr := usageTracker.GetDB().QueryContext(ctx, "SELECT channel, COUNT(*) FROM endpoints GROUP BY channel")
			if qerr == nil {
				defer rows.Close()
				for rows.Next() {
					var name string
					var count int
					if err := rows.Scan(&name, &count); err != nil {
						continue
					}
					if name == "" {
						continue
					}
					channelCount[name] = count
				}
			}
		}
	}

	channelWebsite := make(map[string]string)
	channelPriority := make(map[string]int)
	channelModelMapping := make(map[string]map[string]string)
	channelCreatedAt := make(map[string]string)
	channelIDs := make(map[string]int64)
	channelNonStream := make(map[string]*routing.NonStreamRoute)
	channelPrivacyLevel := make(map[string]string)
	orderedChannels := make([]string, 0, 16)
	if channelService != nil {
		channelRecords, err := channelService.ListChannels(ctx)
		if err != nil {
			return nil, fmt.Errorf("获取渠道列表失败: %w", err)
		}
		for _, c := range channelRecords {
			if c == nil || c.Name == "" {
				continue
			}
			channelIDs[c.Name] = c.ID
			channelPrivacyLevel[c.Name] = c.PrivacyLevel
			if c.NonStreamRoute != nil {
				channelNonStream[c.Name] = c.NonStreamRoute
			}
			channelWebsite[c.Name] = c.Website
			if c.Priority <= 0 {
				channelPriority[c.Name] = 1
			} else {
				channelPriority[c.Name] = c.Priority
			}
			channelModelMapping[c.Name] = c.ModelMapping
			if !c.CreatedAt.IsZero() {
				channelCreatedAt[c.Name] = c.CreatedAt.Format("2006-01-02 15:04:05")
			}
			if _, ok := channelCount[c.Name]; !ok {
				channelCount[c.Name] = 0
			}
			orderedChannels = append(orderedChannels, c.Name)
		}
	}

	seen := make(map[string]struct{}, len(channelCount))
	result := make([]ChannelInfo, 0, len(channelCount))
	// 只有存在非流改道配置时才需要目标目录来解析目标名称
	var routeCatalog routing.Catalog
	if len(channelNonStream) > 0 {
		routeCatalog, _ = a.routeCatalog(ctx, "claude")
	}

	// 1) channels 表中的渠道（已按 store 层排序），优先输出，保证 UI 顺序稳定
	for _, name := range orderedChannels {
		count := channelCount[name]
		credentialGroups := a.listClaudeCredentialGroupInfos(ctx, claudeCredentialGroupService, claudeCredentialGroupStore, name)
		result = append(result, ChannelInfo{
			ID:               channelIDs[name],
			Name:             name,
			Website:          channelWebsite[name],
			Priority:         channelPriority[name],
			ModelMapping:     channelModelMapping[name],
			NonStreamRoute:   nonStreamRouteInfo(channelNonStream[name], routeCatalog),
			PrivacyLevel:     channelPrivacyLevel[name],
			CreatedAt:        channelCreatedAt[name],
			EndpointCount:    count,
			CredentialGroups: credentialGroups,
		})
		seen[name] = struct{}{}
	}

	// 2) 兼容：端点表里存在但 channels 表未收录的渠道（作为兜底）
	for name, count := range channelCount {
		if _, ok := seen[name]; ok {
			continue
		}
		credentialGroups := a.listClaudeCredentialGroupInfos(ctx, claudeCredentialGroupService, claudeCredentialGroupStore, name)
		result = append(result, ChannelInfo{
			Name:             name,
			Website:          channelWebsite[name],
			Priority:         999,
			ModelMapping:     channelModelMapping[name],
			CreatedAt:        "",
			EndpointCount:    count,
			CredentialGroups: credentialGroups,
		})
	}

	sort.Slice(result, func(i, j int) bool {
		pi := result[i].Priority
		pj := result[j].Priority
		if pi <= 0 {
			pi = 1
		}
		if pj <= 0 {
			pj = 1
		}
		if pi != pj {
			return pi < pj
		}
		// 同优先级：按创建时间倒序（新创建排在前面）
		if result[i].CreatedAt != result[j].CreatedAt {
			return result[i].CreatedAt > result[j].CreatedAt
		}
		return result[i].Name < result[j].Name
	})
	return result, nil
}

type CreateChannelInput struct {
	Name         string            `json:"name"`
	Website      string            `json:"website,omitempty"`
	Priority     int               `json:"priority"`
	ModelMapping map[string]string `json:"model_mapping"`
}

func (a *App) CreateChannel(input CreateChannelInput) error {
	a.mu.RLock()
	channelService := a.channelService
	claudeCredentialGroupService := a.claudeCredentialGroupService
	logger := a.logger
	a.mu.RUnlock()

	if channelService == nil {
		return fmt.Errorf("渠道存储服务未启用")
	}
	input.Name = strings.TrimSpace(input.Name)
	input.Website = strings.TrimSpace(input.Website)
	if input.Name == "" {
		return fmt.Errorf("渠道名称不能为空")
	}
	// Priority 由 service 层处理（自动递增或冲突交换）

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	_, err := channelService.CreateChannel(ctx, &claudestore.ChannelRecord{
		Name:         input.Name,
		Website:      input.Website,
		Priority:     input.Priority,
		ModelMapping: input.ModelMapping,
	})
	if err != nil {
		return err
	}
	if claudeCredentialGroupService != nil {
		if _, err := claudeCredentialGroupService.EnsureDefaultGroup(ctx, input.Name); err != nil && logger != nil {
			logger.Warn("⚠️ 创建渠道默认凭证分组失败", "channel", input.Name, "error", err)
		}
	}
	if logger != nil {
		logger.Info("✅ 渠道已创建", "name", input.Name)
	}

	// 同步渠道优先级到运行时（用于渠道间故障转移顺序）
	if syncErr := a.syncChannelRuntimeState(ctx); syncErr != nil {
		retryCtx, retryCancel := context.WithTimeout(context.Background(), 3*time.Second)
		retryErr := a.syncChannelRuntimeState(retryCtx)
		retryCancel()
		if retryErr != nil && logger != nil {
			logger.Warn("⚠️ 渠道已创建，但运行时状态同步失败；将在后续更新或重启时重试",
				"channel", input.Name,
				"error", syncErr,
				"retry_error", retryErr,
			)
		}
	}
	return nil
}

type UpdateChannelInput struct {
	Name         string            `json:"name"`
	Website      string            `json:"website,omitempty"`
	Priority     int               `json:"priority"`
	ModelMapping map[string]string `json:"model_mapping"`
}

func (a *App) UpdateChannel(input UpdateChannelInput) error {
	a.mu.RLock()
	channelService := a.channelService
	logger := a.logger
	a.mu.RUnlock()

	if channelService == nil {
		return fmt.Errorf("渠道存储服务未启用")
	}

	input.Name = strings.TrimSpace(input.Name)
	input.Website = strings.TrimSpace(input.Website)
	if input.Name == "" {
		return fmt.Errorf("渠道名称不能为空")
	}
	// Priority 由 service 层处理（自动递增或冲突交换）

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := channelService.UpdateChannel(ctx, &claudestore.ChannelRecord{
		Name:         input.Name,
		Website:      input.Website,
		Priority:     input.Priority,
		ModelMapping: input.ModelMapping,
	}); err != nil {
		return err
	}

	if err := a.syncChannelRuntimeState(ctx); err != nil {
		return fmt.Errorf("渠道已更新，但同步运行时状态失败: %w", err)
	}

	// 推送前端刷新（channelsMeta & groups）
	a.emitEndpointUpdate()

	if logger != nil {
		logger.Info("✅ 渠道已更新", "name", input.Name, "priority", input.Priority)
	}
	return nil
}

func (a *App) syncChannelRuntimeState(ctx context.Context) error {
	a.mu.RLock()
	endpointManager := a.endpointManager
	a.mu.RUnlock()
	if endpointManager == nil {
		return fmt.Errorf("端点管理器未初始化")
	}

	if err := a.syncChannelPrioritiesToEndpointManager(ctx); err != nil {
		return err
	}
	if err := a.syncChannelFailoverEnabledToEndpointManager(ctx); err != nil {
		return err
	}
	a.syncChannelModelMappingsToEndpointManager(ctx)
	// 渠道增删改会改变路由目标的 ID→名称映射与非流改道配置
	a.syncClaudeRouteConfig(ctx)
	return nil
}

func (a *App) syncChannelPrioritiesToEndpointManager(ctx context.Context) error {
	a.mu.RLock()
	channelStore := a.channelStore
	endpointManager := a.endpointManager
	logger := a.logger
	a.mu.RUnlock()

	if channelStore == nil {
		return fmt.Errorf("渠道存储服务未启用")
	}
	if endpointManager == nil {
		return fmt.Errorf("端点管理器未初始化")
	}

	baseCtx := ctx
	if baseCtx == nil || baseCtx.Err() != nil {
		baseCtx = context.Background()
	}

	syncCtx, cancel := context.WithTimeout(baseCtx, 3*time.Second)
	defer cancel()

	records, err := channelStore.List(syncCtx)
	if err != nil {
		if logger != nil {
			logger.Warn("⚠️ 同步渠道优先级失败", "error", err)
		}
		return fmt.Errorf("同步渠道优先级失败: %w", err)
	}

	priorities := make(map[string]int, len(records))
	for _, r := range records {
		if r == nil || r.Name == "" {
			continue
		}
		p := r.Priority
		if p <= 0 {
			p = 1
		}
		priorities[r.Name] = p
	}
	endpointManager.UpdateChannelPriorities(priorities)
	return nil
}

func (a *App) syncChannelFailoverEnabledToEndpointManager(ctx context.Context) error {
	a.mu.RLock()
	channelStore := a.channelStore
	endpointManager := a.endpointManager
	logger := a.logger
	a.mu.RUnlock()

	if channelStore == nil {
		return fmt.Errorf("渠道存储服务未启用")
	}
	if endpointManager == nil {
		return fmt.Errorf("端点管理器未初始化")
	}

	baseCtx := ctx
	if baseCtx == nil || baseCtx.Err() != nil {
		baseCtx = context.Background()
	}

	syncCtx, cancel := context.WithTimeout(baseCtx, 3*time.Second)
	defer cancel()

	records, err := channelStore.List(syncCtx)
	if err != nil {
		if logger != nil {
			logger.Warn("⚠️ 同步渠道故障转移开关失败", "error", err)
		}
		return fmt.Errorf("同步渠道故障转移开关失败: %w", err)
	}

	enabled := make(map[string]bool, len(records))
	for _, r := range records {
		if r == nil || r.Name == "" {
			continue
		}
		enabled[r.Name] = r.FailoverEnabled
	}
	endpointManager.UpdateChannelFailoverEnabled(enabled)
	return nil
}

func (a *App) syncChannelModelMappingsToEndpointManager(ctx context.Context) {
	a.mu.RLock()
	channelStore := a.channelStore
	endpointManager := a.endpointManager
	logger := a.logger
	a.mu.RUnlock()

	if channelStore == nil || endpointManager == nil {
		return
	}

	baseCtx := ctx
	if baseCtx == nil || baseCtx.Err() != nil {
		baseCtx = context.Background()
	}

	syncCtx, cancel := context.WithTimeout(baseCtx, 3*time.Second)
	defer cancel()

	records, err := channelStore.List(syncCtx)
	if err != nil {
		if logger != nil {
			logger.Warn("⚠️ 同步渠道模型映射失败", "error", err)
		}
		return
	}

	mappings := make(map[string]map[string]string, len(records))
	for _, r := range records {
		if r == nil || r.Name == "" || len(r.ModelMapping) == 0 {
			continue
		}
		mappings[r.Name] = r.ModelMapping
	}
	endpointManager.UpdateChannelModelMappings(mappings)
}

func (a *App) DeleteChannel(name string, deleteEndpoints bool) error {
	a.mu.RLock()
	channelStore := a.channelStore
	channelService := a.channelService
	endpointStore := a.endpointStore
	endpointService := a.endpointService
	claudeCredentialGroupStore := a.claudeCredentialGroupStore
	usageTracker := a.usageTracker
	storeDB := a.storeDB
	logger := a.logger
	a.mu.RUnlock()

	if channelService == nil || channelStore == nil {
		return fmt.Errorf("渠道存储服务未启用")
	}
	if usageTracker == nil || usageTracker.GetDB() == nil {
		if storeDB == nil {
			return fmt.Errorf("数据库未初始化")
		}
	}

	name = strings.TrimSpace(name)
	if name == "" {
		return fmt.Errorf("渠道名称不能为空")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	db := storeDB
	if db == nil && usageTracker != nil {
		db = usageTracker.GetDB()
	}
	if db == nil {
		return fmt.Errorf("数据库未初始化")
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("开启事务失败: %w", err)
	}
	defer func() {
		_ = tx.Rollback()
	}()

	if endpointStore == nil {
		endpointStore = claudestore.NewSQLiteEndpointStore(db)
	}
	endpointStore = endpointStore.WithTx(tx)
	channelStore = channelStore.WithTx(tx)
	if claudeCredentialGroupStore != nil {
		claudeCredentialGroupStore = claudeCredentialGroupStore.WithTx(tx)
	}

	// 端点检查/删除
	endpoints, err := endpointStore.ListByChannel(ctx, name)
	if err != nil {
		return fmt.Errorf("获取渠道端点失败: %w", err)
	}
	if len(endpoints) > 0 && !deleteEndpoints {
		return fmt.Errorf("渠道 '%s' 下仍有 %d 个端点，请勾选一并删除", name, len(endpoints))
	}
	if len(endpoints) > 0 {
		names := make([]string, 0, len(endpoints))
		for _, ep := range endpoints {
			if ep != nil && ep.Name != "" {
				names = append(names, ep.Name)
			}
		}
		if len(names) > 0 {
			if err := endpointStore.BatchDelete(ctx, names); err != nil {
				return fmt.Errorf("删除渠道端点失败: %w", err)
			}
		}
	}
	if claudeCredentialGroupStore != nil {
		if err := claudeCredentialGroupStore.DeleteByChannel(ctx, name); err != nil {
			return err
		}
	}

	// 删除前记下渠道 ID：模型路由规则按 ID 挂在渠道上
	var deletedChannelID int64
	if record, err := channelStore.Get(ctx, name); err == nil && record != nil {
		deletedChannelID = record.ID
	}

	// 删除渠道
	if err := channelStore.Delete(ctx, name); err != nil {
		return err
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("提交事务失败: %w", err)
	}
	a.deleteChannelModelRoutes(ctx, "claude", deletedChannelID)

	// 同步运行时配置（删除端点/组）
	if endpointService != nil {
		if err := endpointService.SyncFromDatabase(ctx); err != nil && logger != nil {
			logger.Warn("⚠️ 删除渠道后同步运行时配置失败", "error", err)
		}
	}
	a.syncChannelModelMappingsToEndpointManager(ctx)
	a.syncClaudeRouteConfig(ctx)

	// 推送前端刷新
	a.emitEndpointUpdate()

	if logger != nil {
		logger.Info("✅ 渠道已删除", "name", name, "delete_endpoints", deleteEndpoints)
	}

	return nil
}

func (a *App) GetEndpointsByChannel(channel string) ([]EndpointRecordInfo, error) {
	a.mu.RLock()
	endpointService := a.endpointService
	endpointManager := a.endpointManager
	a.mu.RUnlock()

	if endpointService == nil {
		return nil, fmt.Errorf("端点存储服务未启用")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	records, err := endpointService.ListEndpointsByChannel(ctx, channel)
	if err != nil {
		return nil, fmt.Errorf("获取渠道端点列表失败: %w", err)
	}

	result := make([]EndpointRecordInfo, 0, len(records))
	for _, r := range records {
		info := a.recordToInfo(r)

		// 获取运行时健康状态
		if endpointManager != nil {
			status := endpointManager.GetEndpointStatus(claudeservice.EndpointRuntimeKey(r))
			info.Healthy = status.Healthy
			info.ResponseTimeMs = float64(status.ResponseTime) / float64(time.Millisecond)
			// 格式化最后检查时间
			if !status.LastCheck.IsZero() {
				info.LastCheck = status.LastCheck.Format("2006-01-02 15:04:05")
			}
			// 冷却机制已移除
			info.InCooldown = false
			info.CooldownUntil = ""
			info.CooldownReason = ""
		}

		result = append(result, info)
	}

	return result, nil
}
