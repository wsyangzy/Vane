package app

import (
	"context"
	"fmt"
	"strings"
	"time"

	appcache "github.com/wsyangzy/Vane/internal/platform/cache"
	"github.com/wsyangzy/Vane/internal/providers/claude/proxy/endpoint"
	claudestore "github.com/wsyangzy/Vane/internal/providers/claude/store"
)

func (a *App) CreateEndpointRecord(input CreateEndpointInput) error {
	a.mu.RLock()
	endpointService := a.endpointService
	channelService := a.channelService
	logger := a.logger
	a.mu.RUnlock()

	if endpointService == nil {
		return fmt.Errorf("端点存储服务未启用")
	}

	// 设置默认值（注意：Priority 由 service 层自动递增，这里不设默认值）
	if input.CostMultiplier == 0 {
		input.CostMultiplier = 1.0
	}
	if input.CacheCreationCostMultiplier1h == 0 {
		input.CacheCreationCostMultiplier1h = 1.0
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// SQLite 模式下 enabled 表示渠道当前是否活跃；向活跃渠道新增端点时应继承该状态。
	shouldEnableNewEndpoint := false
	if input.Channel != "" {
		existingEndpoints, err := endpointService.ListEndpointsByChannel(ctx, input.Channel)
		if err != nil {
			return fmt.Errorf("获取渠道端点状态失败: %w", err)
		}
		for _, endpoint := range existingEndpoints {
			if endpoint != nil && endpoint.Enabled {
				shouldEnableNewEndpoint = true
				break
			}
		}
	}

	record := &claudestore.EndpointRecord{
		Channel:                       input.Channel,
		CredentialGroupID:             input.CredentialGroupID,
		Name:                          input.Name,
		URL:                           input.URL,
		Token:                         input.Token,
		ApiKey:                        input.ApiKey,
		Headers:                       input.Headers,
		Priority:                      input.Priority,
		FailoverEnabled:               input.FailoverEnabled,
		SkipHealthCheck:               input.SkipHealthCheck,
		CooldownSeconds:               input.CooldownSeconds,
		TimeoutSeconds:                input.TimeoutSeconds,
		SupportsCountTokens:           input.SupportsCountTokens,
		CostMultiplier:                input.CostMultiplier,
		InputCostMultiplier:           input.InputCostMultiplier,
		OutputCostMultiplier:          input.OutputCostMultiplier,
		CacheCreationCostMultiplier:   input.CacheCreationCostMultiplier,
		CacheCreationCostMultiplier1h: input.CacheCreationCostMultiplier1h,
		CacheReadCostMultiplier:       input.CacheReadCostMultiplier,
		ModelMultipliers:              input.ModelMultipliers,
		Enabled:                       shouldEnableNewEndpoint,
	}

	// 若渠道表已启用，确保渠道已存在（兼容旧数据：允许端点创建时补齐渠道记录）
	if channelService != nil && input.Channel != "" {
		_ = channelService.EnsureChannel(ctx, input.Channel)
	}

	_, err := endpointService.CreateEndpoint(ctx, record)
	if err != nil {
		return fmt.Errorf("创建端点失败: %w", err)
	}

	if logger != nil {
		logger.Info("✅ 端点已创建", "name", input.Name, "channel", input.Channel)
	}

	// endpointService.CreateEndpoint 已经将端点添加到内存并触发健康检测
	// 不需要再次调用 AddEndpoint，否则会导致 "端点已存在" 错误

	// 创建成功后，异步同步端点倍率到 UsageTracker
	go a.syncEndpointMultipliersToTracker(context.Background())

	return nil
}

func (a *App) UpdateEndpointRecord(name string, input CreateEndpointInput) error {
	a.mu.RLock()
	endpointService := a.endpointService
	channelService := a.channelService
	usageTracker := a.usageTracker
	storeDB := a.storeDB
	logger := a.logger
	a.mu.RUnlock()

	if endpointService == nil {
		return fmt.Errorf("端点存储服务未启用")
	}

	oldName := strings.TrimSpace(name)
	if oldName == "" {
		return fmt.Errorf("端点名称不能为空")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// endpoints 允许不同渠道同名，旧接口仅靠 name 定位会产生歧义。
	// 当检测到 name 在多个渠道重复时，要求前端使用按 ID 更新接口。
	db := storeDB
	if db == nil && usageTracker != nil {
		db = usageTracker.GetDB()
	}
	if db != nil {
		var cnt int
		if err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM endpoints WHERE name = ?", oldName).Scan(&cnt); err == nil {
			if cnt > 1 {
				return fmt.Errorf("端点名称 '%s' 在多个渠道中重复，请升级客户端后使用按ID更新", oldName)
			}
		}
	}

	// 从数据库获取当前记录，用于保留敏感字段
	existingRecord, err := endpointService.GetEndpoint(ctx, oldName)
	if err != nil {
		return fmt.Errorf("获取端点失败: %w", err)
	}
	if existingRecord == nil {
		return fmt.Errorf("端点 '%s' 不存在", oldName)
	}

	channel := strings.TrimSpace(input.Channel)
	if channel == "" {
		channel = existingRecord.Channel
	}
	newName := strings.TrimSpace(input.Name)
	if newName == "" {
		newName = existingRecord.Name
	}

	// 确保目标渠道存在（避免出现“孤儿渠道/端点”）
	if channelService != nil && channel != "" {
		_ = channelService.EnsureChannel(ctx, channel)
	}

	// 处理 Token: 如果前端传空值，保留原有 Token（防止误删）
	token := input.Token
	if token == "" {
		token = existingRecord.Token
	}

	// 处理 ApiKey: 如果前端传空值，保留原有 ApiKey（防止误删）
	apiKey := input.ApiKey
	if apiKey == "" {
		apiKey = existingRecord.ApiKey
	}

	// 兼容：前端未传 1h 倍率时，保留旧值
	cacheCreationCostMultiplier1h := input.CacheCreationCostMultiplier1h
	if cacheCreationCostMultiplier1h == 0 {
		cacheCreationCostMultiplier1h = existingRecord.CacheCreationCostMultiplier1h
	}
	modelMultipliers := input.ModelMultipliers
	if modelMultipliers == nil {
		modelMultipliers = existingRecord.ModelMultipliers
	}

	record := &claudestore.EndpointRecord{
		ID:                            existingRecord.ID,
		Channel:                       channel,
		CredentialGroupID:             resolveEndpointCredentialGroupID(input.CredentialGroupID, existingRecord, channel),
		Name:                          newName,
		URL:                           input.URL,
		Token:                         token,  // 空值时保留原有值
		ApiKey:                        apiKey, // 空值时保留原有值
		Headers:                       input.Headers,
		Priority:                      input.Priority,
		FailoverEnabled:               input.FailoverEnabled,
		SkipHealthCheck:               input.SkipHealthCheck,
		CooldownSeconds:               input.CooldownSeconds,
		TimeoutSeconds:                input.TimeoutSeconds,
		SupportsCountTokens:           input.SupportsCountTokens,
		CostMultiplier:                input.CostMultiplier,
		InputCostMultiplier:           input.InputCostMultiplier,
		OutputCostMultiplier:          input.OutputCostMultiplier,
		CacheCreationCostMultiplier:   input.CacheCreationCostMultiplier,
		CacheCreationCostMultiplier1h: cacheCreationCostMultiplier1h,
		CacheReadCostMultiplier:       input.CacheReadCostMultiplier,
		ModelMultipliers:              modelMultipliers,
		Enabled:                       existingRecord.Enabled, // 保持原有激活状态
	}

	if err := endpointService.UpdateEndpoint(ctx, record); err != nil {
		// 提示更清晰的“同渠道同名”冲突信息
		msg := err.Error()
		// EndpointService 已返回用户可读的冲突原因（避免上层泛化覆盖）
		if strings.Contains(msg, "同一渠道内端点名称必须唯一") {
			return err
		}
		if strings.Contains(msg, "UNIQUE constraint failed: endpoints.channel, endpoints.name") ||
			strings.Contains(msg, "渠道 '") && strings.Contains(msg, " 已存在") {
			return fmt.Errorf("同一渠道 '%s' 下已存在端点名称 '%s'，请修改名称或选择其他渠道", channel, newName)
		}
		return fmt.Errorf("更新端点失败: %w", err)
	}

	if logger != nil {
		if newName == existingRecord.Name && channel == existingRecord.Channel {
			logger.Info("✅ 端点已更新", "name", oldName)
		} else {
			logger.Info("✅ 端点已更新", "from", endpoint.EndpointKey(existingRecord.Channel, existingRecord.Name), "to", endpoint.EndpointKey(channel, newName))
		}
	}

	// best-effort：同步历史记录中的 endpoint_name（仅限“同一渠道内改名”，避免跨渠道误更新）
	if db != nil && existingRecord.Channel == channel && existingRecord.Name != newName {
		_, _ = db.ExecContext(ctx,
			"UPDATE request_logs SET endpoint_name = ? WHERE endpoint_name = ? AND (group_name = ? OR channel = ?)",
			newName, existingRecord.Name, channel, channel,
		)
		_, _ = db.ExecContext(ctx,
			"UPDATE usage_summary SET endpoint_name = ? WHERE endpoint_name = ? AND group_name = ?",
			newName, existingRecord.Name, channel,
		)
		a.notifyUsageDataChanged(usageTracker, appcache.Change{Providers: []string{"claude"}})
	}

	// 更新成功后，异步同步端点倍率到 UsageTracker
	// 确保成本计算使用最新的倍率配置
	go a.syncEndpointMultipliersToTracker(context.Background())

	// 推送前端刷新
	a.emitEndpointUpdate()

	return nil
}

func (a *App) UpdateEndpointRecordByID(id int64, input CreateEndpointInput) error {
	a.mu.RLock()
	endpointService := a.endpointService
	channelService := a.channelService
	usageTracker := a.usageTracker
	storeDB := a.storeDB
	logger := a.logger
	a.mu.RUnlock()

	if endpointService == nil {
		return fmt.Errorf("端点存储服务未启用")
	}
	if id <= 0 {
		return fmt.Errorf("端点ID不能为空")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	existingRecord, err := endpointService.GetEndpointByID(ctx, id)
	if err != nil {
		return fmt.Errorf("获取端点失败: %w", err)
	}
	if existingRecord == nil {
		return fmt.Errorf("端点不存在: %d", id)
	}

	db := storeDB
	if db == nil && usageTracker != nil {
		db = usageTracker.GetDB()
	}

	channel := strings.TrimSpace(input.Channel)
	if channel == "" {
		channel = existingRecord.Channel
	}
	newName := strings.TrimSpace(input.Name)
	if newName == "" {
		newName = existingRecord.Name
	}

	// 确保目标渠道存在（避免出现“孤儿渠道/端点”）
	if channelService != nil && channel != "" {
		_ = channelService.EnsureChannel(ctx, channel)
	}

	// 处理 Token: 如果前端传空值，保留原有 Token（防止误删）
	token := input.Token
	if token == "" {
		token = existingRecord.Token
	}

	// 处理 ApiKey: 如果前端传空值，保留原有 ApiKey（防止误删）
	apiKey := input.ApiKey
	if apiKey == "" {
		apiKey = existingRecord.ApiKey
	}

	// 兼容：前端未传 1h 倍率时，保留旧值
	cacheCreationCostMultiplier1h := input.CacheCreationCostMultiplier1h
	if cacheCreationCostMultiplier1h == 0 {
		cacheCreationCostMultiplier1h = existingRecord.CacheCreationCostMultiplier1h
	}
	modelMultipliers := input.ModelMultipliers
	if modelMultipliers == nil {
		modelMultipliers = existingRecord.ModelMultipliers
	}

	record := &claudestore.EndpointRecord{
		ID:                            existingRecord.ID,
		Channel:                       channel,
		CredentialGroupID:             resolveEndpointCredentialGroupID(input.CredentialGroupID, existingRecord, channel),
		Name:                          newName,
		URL:                           input.URL,
		Token:                         token,  // 空值时保留原有值
		ApiKey:                        apiKey, // 空值时保留原有值
		Headers:                       input.Headers,
		Priority:                      input.Priority,
		FailoverEnabled:               input.FailoverEnabled,
		SkipHealthCheck:               input.SkipHealthCheck,
		CooldownSeconds:               input.CooldownSeconds,
		TimeoutSeconds:                input.TimeoutSeconds,
		SupportsCountTokens:           input.SupportsCountTokens,
		CostMultiplier:                input.CostMultiplier,
		InputCostMultiplier:           input.InputCostMultiplier,
		OutputCostMultiplier:          input.OutputCostMultiplier,
		CacheCreationCostMultiplier:   input.CacheCreationCostMultiplier,
		CacheCreationCostMultiplier1h: cacheCreationCostMultiplier1h,
		CacheReadCostMultiplier:       input.CacheReadCostMultiplier,
		ModelMultipliers:              modelMultipliers,
		Enabled:                       existingRecord.Enabled, // 保持原有激活状态
	}

	if err := endpointService.UpdateEndpoint(ctx, record); err != nil {
		msg := err.Error()
		if strings.Contains(msg, "同一渠道内端点名称必须唯一") {
			return err
		}
		if strings.Contains(msg, "UNIQUE constraint failed: endpoints.channel, endpoints.name") ||
			(strings.Contains(msg, "渠道 '") && strings.Contains(msg, " 已存在")) {
			return fmt.Errorf("同一渠道 '%s' 下已存在端点名称 '%s'，请修改名称或选择其他渠道", channel, newName)
		}
		return fmt.Errorf("更新端点失败: %w", err)
	}

	if logger != nil {
		if newName == existingRecord.Name && channel == existingRecord.Channel {
			logger.Info("✅ 端点已更新", "id", id, "name", newName)
		} else {
			logger.Info("✅ 端点已更新", "id", id, "from", endpoint.EndpointKey(existingRecord.Channel, existingRecord.Name), "to", endpoint.EndpointKey(channel, newName))
		}
	}

	// best-effort：同步历史记录中的 endpoint_name（仅限“同一渠道内改名”，避免跨渠道误更新）
	if db != nil && existingRecord.Channel == channel && existingRecord.Name != newName {
		_, _ = db.ExecContext(ctx,
			"UPDATE request_logs SET endpoint_name = ? WHERE endpoint_name = ? AND (group_name = ? OR channel = ?)",
			newName, existingRecord.Name, channel, channel,
		)
		_, _ = db.ExecContext(ctx,
			"UPDATE usage_summary SET endpoint_name = ? WHERE endpoint_name = ? AND group_name = ?",
			newName, existingRecord.Name, channel,
		)
		a.notifyUsageDataChanged(usageTracker, appcache.Change{Providers: []string{"claude"}})
	}

	go a.syncEndpointMultipliersToTracker(context.Background())
	a.emitEndpointUpdate()

	return nil
}
