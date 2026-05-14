package main

import (
	"context"
	"fmt"
	"time"

	"claude-request-forwarder/internal/store"
)

// ============================================================================
// 旧 Provider DTO（保留以维持 wailsjs/go/models.ts 生成稳定）
// 自 2026-05-13 起，Codex 已切换到「渠道-端点」两层模型；旧接口转调用新接口。
// ============================================================================

// CodexProviderInfo 旧前端 DTO；现在通过组装 channel + 激活端点回退构造。
type CodexProviderInfo struct {
	ID              int64  `json:"id"`
	Name            string `json:"name"`
	ProviderType    string `json:"provider_type"`
	BaseURL         string `json:"base_url"`
	ApiKey          string `json:"api_key"`
	ApiKeyMasked    string `json:"api_key_masked"`
	Model           string `json:"model"`
	ReasoningEffort string `json:"model_reasoning_effort"`
	Enable1MContext bool   `json:"enable_1m_context"`
	Website         string `json:"website"`
	IsActive        bool   `json:"is_active"`
	CreatedAt       string `json:"created_at"`
	UpdatedAt       string `json:"updated_at"`
}

type CreateCodexProviderInput struct {
	Name            string `json:"name"`
	ProviderType    string `json:"provider_type"`
	BaseURL         string `json:"base_url"`
	ApiKey          string `json:"api_key"`
	Model           string `json:"model"`
	ReasoningEffort string `json:"model_reasoning_effort"`
	Enable1MContext bool   `json:"enable_1m_context"`
	Website         string `json:"website"`
}

type UpdateCodexProviderInput struct {
	Name            string `json:"name"`
	ProviderType    string `json:"provider_type"`
	BaseURL         string `json:"base_url"`
	ApiKey          string `json:"api_key"`
	Model           string `json:"model"`
	ReasoningEffort string `json:"model_reasoning_effort"`
	Enable1MContext bool   `json:"enable_1m_context"`
	Website         string `json:"website"`
}

type CodexProviderStorageStatus struct {
	Enabled            bool   `json:"enabled"`
	TotalCount         int    `json:"total_count"`
	HasActive          bool   `json:"has_active"`
	ActiveProviderID   int64  `json:"active_provider_id"`
	ActiveProviderName string `json:"active_provider_name"`
}

// ============================================================================
// 新「渠道-端点」DTO
// ============================================================================

// CodexChannelInfo 提供给前端展示的渠道信息（含端点列表）。
type CodexChannelInfo struct {
	ID              int64               `json:"id"`
	Name            string              `json:"name"`
	ProviderType    string              `json:"provider_type"`
	ApiKey          string              `json:"api_key"`
	ApiKeyMasked    string              `json:"api_key_masked"`
	Model           string              `json:"model"`
	ReasoningEffort string              `json:"model_reasoning_effort"`
	Enable1MContext bool                `json:"enable_1m_context"`
	Website         string              `json:"website"`
	IsActive        bool                `json:"is_active"`
	Endpoints       []CodexEndpointInfo `json:"endpoints"`
	CreatedAt       string              `json:"created_at"`
	UpdatedAt       string              `json:"updated_at"`
}

// CodexEndpointInfo 提供给前端展示的端点信息（name + base_url）。
type CodexEndpointInfo struct {
	ID        int64  `json:"id"`
	ChannelID int64  `json:"channel_id"`
	Name      string `json:"name"`
	BaseURL   string `json:"base_url"`
	IsActive  bool   `json:"is_active"`
	CreatedAt string `json:"created_at"`
	UpdatedAt string `json:"updated_at"`
}

// CreateCodexChannelInput 创建渠道（不含端点）。
type CreateCodexChannelInput struct {
	Name            string `json:"name"`
	ProviderType    string `json:"provider_type"`
	ApiKey          string `json:"api_key"`
	Model           string `json:"model"`
	ReasoningEffort string `json:"model_reasoning_effort"`
	Enable1MContext bool   `json:"enable_1m_context"`
	Website         string `json:"website"`
}

type UpdateCodexChannelInput struct {
	Name            string `json:"name"`
	ProviderType    string `json:"provider_type"`
	ApiKey          string `json:"api_key"`
	Model           string `json:"model"`
	ReasoningEffort string `json:"model_reasoning_effort"`
	Enable1MContext bool   `json:"enable_1m_context"`
	Website         string `json:"website"`
}

type CreateCodexEndpointInput struct {
	ChannelID int64  `json:"channel_id"`
	Name      string `json:"name"`
	BaseURL   string `json:"base_url"`
}

type UpdateCodexEndpointInput struct {
	Name    string `json:"name"`
	BaseURL string `json:"base_url"`
}

// ============================================================================
// Storage Status
// ============================================================================

func (a *App) GetCodexProviderStorageStatus() CodexProviderStorageStatus {
	status := CodexProviderStorageStatus{}
	a.ensureCodexProviderService()

	a.mu.RLock()
	channelService := a.codexChannelService
	a.mu.RUnlock()
	if channelService == nil {
		return status
	}

	status.Enabled = true

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	channels, err := channelService.ListChannels(ctx)
	if err == nil {
		status.TotalCount = len(channels)
	}

	active, err := channelService.GetActiveChannel(ctx)
	if err == nil && active != nil {
		status.HasActive = true
		status.ActiveProviderID = active.ID
		status.ActiveProviderName = active.Name
	}

	return status
}

// ============================================================================
// 新 Channel API
// ============================================================================

func (a *App) GetCodexChannels() ([]CodexChannelInfo, error) {
	a.ensureCodexProviderService()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	a.mu.RLock()
	channelService := a.codexChannelService
	endpointService := a.codexEndpointService
	a.mu.RUnlock()
	if channelService == nil || endpointService == nil {
		return nil, fmt.Errorf("Codex 渠道服务未就绪，请稍后重试")
	}

	channels, err := channelService.ListChannels(ctx)
	if err != nil {
		return nil, fmt.Errorf("获取 Codex 渠道列表失败: %w", err)
	}

	result := make([]CodexChannelInfo, 0, len(channels))
	for _, ch := range channels {
		endpoints, err := endpointService.ListByChannelID(ctx, ch.ID)
		if err != nil {
			return nil, fmt.Errorf("获取渠道 '%s' 端点列表失败: %w", ch.Name, err)
		}
		result = append(result, a.buildCodexChannelInfo(ch, endpoints))
	}
	return result, nil
}

func (a *App) CreateCodexChannel(input CreateCodexChannelInput) error {
	a.ensureCodexProviderService()

	a.mu.RLock()
	channelService := a.codexChannelService
	logger := a.logger
	a.mu.RUnlock()
	if channelService == nil {
		return fmt.Errorf("Codex 渠道服务未就绪，请稍后重试")
	}

	record := codexChannelInputToRecord(input.Name, input.ProviderType, input.ApiKey, input.Model,
		input.ReasoningEffort, input.Website, input.Enable1MContext)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	created, err := channelService.CreateChannel(ctx, record)
	if err != nil {
		return fmt.Errorf("创建 Codex 渠道失败: %w", err)
	}

	if logger != nil {
		logger.Info("✅ Codex 渠道已创建", "name", created.Name, "is_active", created.IsActive)
	}
	go a.emitCodexProviderUpdate()
	return nil
}

func (a *App) UpdateCodexChannelByID(id int64, input UpdateCodexChannelInput) error {
	a.ensureCodexProviderService()

	a.mu.RLock()
	channelService := a.codexChannelService
	endpointService := a.codexEndpointService
	logger := a.logger
	a.mu.RUnlock()
	if channelService == nil || endpointService == nil {
		return fmt.Errorf("Codex 渠道服务未就绪，请稍后重试")
	}

	record := codexChannelInputToRecord(input.Name, input.ProviderType, input.ApiKey, input.Model,
		input.ReasoningEffort, input.Website, input.Enable1MContext)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	previous, err := channelService.GetChannelByID(ctx, id)
	if err != nil {
		return fmt.Errorf("读取 Codex 渠道失败: %w", err)
	}
	if previous == nil {
		return fmt.Errorf("Codex 渠道不存在: %d", id)
	}

	if err := channelService.UpdateChannelByID(ctx, id, record); err != nil {
		return fmt.Errorf("更新 Codex 渠道失败: %w", err)
	}

	updated, err := channelService.GetChannelByID(ctx, id)
	if err != nil {
		return fmt.Errorf("读取更新后的 Codex 渠道失败: %w", err)
	}

	if updated != nil && updated.IsActive {
		// 配置同步：把更新后的渠道+激活端点写回 config.toml
		activeEndpoint, endpointErr := endpointService.GetActiveByChannelID(ctx, updated.ID)
		if endpointErr != nil {
			return fmt.Errorf("读取激活端点失败: %w", endpointErr)
		}
		if activeEndpoint != nil {
			if syncErr := a.syncActiveCodexChannelToConfig(updated, activeEndpoint, previous.Name); syncErr != nil {
				rollbackCtx, rollbackCancel := context.WithTimeout(context.Background(), 5*time.Second)
				defer rollbackCancel()
				_ = channelService.UpdateChannelByID(rollbackCtx, id, previous)
				return fmt.Errorf("更新 Codex 渠道失败: 配置接管未生效: %w", syncErr)
			}
		}
	}

	if logger != nil {
		logger.Info("✅ Codex 渠道已更新", "id", id, "name", record.Name)
	}
	go a.emitCodexProviderUpdate()
	return nil
}

func (a *App) DeleteCodexChannelByID(id int64) error {
	a.ensureCodexProviderService()

	a.mu.RLock()
	channelService := a.codexChannelService
	endpointService := a.codexEndpointService
	logger := a.logger
	a.mu.RUnlock()
	if channelService == nil || endpointService == nil {
		return fmt.Errorf("Codex 渠道服务未就绪，请稍后重试")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	target, err := channelService.GetChannelByID(ctx, id)
	if err != nil {
		return fmt.Errorf("读取待删除的 Codex 渠道失败: %w", err)
	}
	if target == nil {
		return fmt.Errorf("Codex 渠道不存在: %d", id)
	}

	if err := channelService.DeleteChannelByID(ctx, id); err != nil {
		return fmt.Errorf("删除 Codex 渠道失败: %w", err)
	}

	if target.IsActive {
		nextChannel, _ := channelService.GetActiveChannel(ctx)
		var nextEndpoint *store.CodexEndpointRecord
		if nextChannel != nil {
			nextEndpoint, err = endpointService.GetActiveByChannelID(ctx, nextChannel.ID)
			if err != nil {
				if logger != nil {
					logger.Warn("⚠️ 读取后续激活端点失败", "error", err)
				}
			}
		}

		if syncErr := a.removeCodexChannelConfig(target.Name, nextChannel, nextEndpoint); syncErr != nil {
			if logger != nil {
				logger.Warn("⚠️ 同步 Codex 配置失败", "error", syncErr, "deleted_channel", target.Name)
			}
		}
	}

	if logger != nil {
		logger.Info("✅ Codex 渠道已删除", "id", id)
	}
	go a.emitCodexProviderUpdate()
	return nil
}

func (a *App) ActivateCodexChannel(id int64) error {
	a.ensureCodexProviderService()

	a.mu.RLock()
	channelService := a.codexChannelService
	endpointService := a.codexEndpointService
	logger := a.logger
	a.mu.RUnlock()
	if channelService == nil || endpointService == nil {
		return fmt.Errorf("Codex 渠道服务未就绪，请稍后重试")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	previousActive, err := channelService.GetActiveChannel(ctx)
	if err != nil {
		return fmt.Errorf("读取当前激活渠道失败: %w", err)
	}

	if err := channelService.ActivateChannel(ctx, id); err != nil {
		return fmt.Errorf("切换 Codex 渠道失败: %w", err)
	}

	activated, err := channelService.GetChannelByID(ctx, id)
	if err != nil {
		return fmt.Errorf("读取激活后的渠道失败: %w", err)
	}
	if activated == nil {
		return fmt.Errorf("激活后的渠道不存在: %d", id)
	}

	endpoint, err := channelService.EnsureActiveEndpoint(ctx, activated.ID)
	if err != nil {
		return fmt.Errorf("确保激活端点失败: %w", err)
	}
	if endpoint == nil {
		// 渠道下没有任何端点，仍允许激活但提示用户
		if logger != nil {
			logger.Warn("⚠️ 渠道已激活但无可用端点，config.toml 暂未更新", "channel", activated.Name)
		}
		go a.emitCodexProviderUpdate()
		return nil
	}

	previousName := ""
	if previousActive != nil && previousActive.ID != id {
		previousName = previousActive.Name
	}

	if err := a.syncActiveCodexChannelToConfig(activated, endpoint, previousName); err != nil {
		if previousActive != nil && previousActive.ID != id {
			rollbackCtx, rollbackCancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer rollbackCancel()
			_ = channelService.ActivateChannel(rollbackCtx, previousActive.ID)
		}
		return fmt.Errorf("切换 Codex 渠道失败: 配置接管未生效: %w", err)
	}

	if logger != nil {
		logger.Info("✅ Codex 渠道已激活", "id", id, "channel", activated.Name, "base_url", endpoint.BaseURL)
	}
	go a.emitCodexProviderUpdate()
	return nil
}

// ============================================================================
// 新 Endpoint API
// ============================================================================

func (a *App) CreateCodexEndpoint(input CreateCodexEndpointInput) error {
	a.ensureCodexProviderService()

	a.mu.RLock()
	channelService := a.codexChannelService
	endpointService := a.codexEndpointService
	logger := a.logger
	a.mu.RUnlock()
	if channelService == nil || endpointService == nil {
		return fmt.Errorf("Codex 端点服务未就绪，请稍后重试")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	record := &store.CodexEndpointRecord{
		ChannelID: input.ChannelID,
		Name:      input.Name,
		BaseURL:   input.BaseURL,
	}

	created, err := endpointService.CreateEndpoint(ctx, record)
	if err != nil {
		return fmt.Errorf("创建 Codex 端点失败: %w", err)
	}

	// 若该渠道当前激活 + 新端点被自动激活（首端点） → 同步 config.toml
	channel, _ := channelService.GetChannelByID(ctx, input.ChannelID)
	if created != nil && created.IsActive && channel != nil && channel.IsActive {
		if syncErr := a.syncActiveCodexChannelToConfig(channel, created, ""); syncErr != nil {
			if logger != nil {
				logger.Warn("⚠️ 同步 Codex 配置失败", "error", syncErr)
			}
		}
	}

	if logger != nil {
		logger.Info("✅ Codex 端点已创建", "channel_id", input.ChannelID, "base_url", input.BaseURL)
	}
	go a.emitCodexProviderUpdate()
	return nil
}

func (a *App) UpdateCodexEndpointByID(id int64, input UpdateCodexEndpointInput) error {
	a.ensureCodexProviderService()

	a.mu.RLock()
	channelService := a.codexChannelService
	endpointService := a.codexEndpointService
	logger := a.logger
	a.mu.RUnlock()
	if channelService == nil || endpointService == nil {
		return fmt.Errorf("Codex 端点服务未就绪，请稍后重试")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	previous, err := endpointService.GetEndpointByID(ctx, id)
	if err != nil {
		return fmt.Errorf("读取 Codex 端点失败: %w", err)
	}
	if previous == nil {
		return fmt.Errorf("Codex 端点不存在: %d", id)
	}

	if err := endpointService.UpdateEndpointByID(ctx, id, input.Name, input.BaseURL); err != nil {
		return fmt.Errorf("更新 Codex 端点失败: %w", err)
	}

	// 配置同步策略（2026-05-14 扩大同步范围）：
	// 只要被编辑的端点所属渠道是「当前激活渠道」，就重新同步 config.toml。
	// - 编辑激活端点的 base_url → toml 中的 base_url 行被改写
	// - 编辑非激活端点 → 用当前激活端点的 base_url 重写（toml 内容通常不变，但保证 DB↔toml 一致）
	// - 编辑端点 name → 不影响 toml（name 不出现在 toml 中），但同步动作幂等无害
	channel, err := channelService.GetChannelByID(ctx, previous.ChannelID)
	if err != nil {
		// 读取渠道失败不应阻塞写入成功，仅记录告警；DB 已更新
		if logger != nil {
			logger.Warn("⚠️ 读取所属 Codex 渠道失败，跳过 config.toml 同步", "endpoint_id", id, "error", err)
		}
		go a.emitCodexProviderUpdate()
		return nil
	}
	if channel != nil && channel.IsActive {
		// 取该渠道下的当前激活端点（可能就是刚编辑的，也可能是另一个）
		activeEndpoint, epErr := endpointService.GetActiveByChannelID(ctx, channel.ID)
		if epErr != nil {
			return fmt.Errorf("更新 Codex 端点失败: 读取激活端点失败: %w", epErr)
		}
		if activeEndpoint != nil {
			if syncErr := a.syncActiveCodexChannelToConfig(channel, activeEndpoint, ""); syncErr != nil {
				return fmt.Errorf("更新 Codex 端点失败: 配置同步未生效: %w", syncErr)
			}
			if logger != nil {
				logger.Info("✅ Codex 端点已更新并同步 config.toml",
					"id", id, "name", input.Name, "base_url", input.BaseURL,
					"channel", channel.Name, "active_endpoint_url", activeEndpoint.BaseURL)
			}
		} else if logger != nil {
			logger.Warn("⚠️ Codex 渠道无激活端点，config.toml 未更新", "channel", channel.Name)
		}
	} else if logger != nil {
		logger.Info("ℹ️ Codex 端点已更新（所属渠道未激活，config.toml 未变化）",
			"id", id, "name", input.Name, "base_url", input.BaseURL)
	}

	go a.emitCodexProviderUpdate()
	return nil
}

func (a *App) DeleteCodexEndpointByID(id int64) error {
	a.ensureCodexProviderService()

	a.mu.RLock()
	channelService := a.codexChannelService
	endpointService := a.codexEndpointService
	logger := a.logger
	a.mu.RUnlock()
	if channelService == nil || endpointService == nil {
		return fmt.Errorf("Codex 端点服务未就绪，请稍后重试")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	target, err := endpointService.GetEndpointByID(ctx, id)
	if err != nil {
		return fmt.Errorf("读取待删除的 Codex 端点失败: %w", err)
	}
	if target == nil {
		return fmt.Errorf("Codex 端点不存在: %d", id)
	}

	wasActive := target.IsActive
	channelID := target.ChannelID

	if err := endpointService.DeleteEndpointByID(ctx, id); err != nil {
		return fmt.Errorf("删除 Codex 端点失败: %w", err)
	}

	// 若被删的是激活渠道下的激活端点，同步 config.toml（service 层会自动激活下一个端点）
	if wasActive {
		channel, _ := channelService.GetChannelByID(ctx, channelID)
		if channel != nil && channel.IsActive {
			nextEndpoint, _ := endpointService.GetActiveByChannelID(ctx, channelID)
			if nextEndpoint != nil {
				if syncErr := a.syncActiveCodexChannelToConfig(channel, nextEndpoint, ""); syncErr != nil && logger != nil {
					logger.Warn("⚠️ 同步 Codex 配置失败", "error", syncErr)
				}
			} else if logger != nil {
				logger.Warn("⚠️ 删除最后一个激活端点，config.toml 中 base_url 未更新", "channel", channel.Name)
			}
		}
	}

	if logger != nil {
		logger.Info("✅ Codex 端点已删除", "id", id)
	}
	go a.emitCodexProviderUpdate()
	return nil
}

func (a *App) ActivateCodexEndpoint(id int64) error {
	a.ensureCodexProviderService()

	a.mu.RLock()
	channelService := a.codexChannelService
	endpointService := a.codexEndpointService
	logger := a.logger
	a.mu.RUnlock()
	if channelService == nil || endpointService == nil {
		return fmt.Errorf("Codex 端点服务未就绪，请稍后重试")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	target, err := endpointService.GetEndpointByID(ctx, id)
	if err != nil {
		return fmt.Errorf("读取目标端点失败: %w", err)
	}
	if target == nil {
		return fmt.Errorf("Codex 端点不存在: %d", id)
	}

	if err := endpointService.ActivateEndpoint(ctx, id); err != nil {
		return fmt.Errorf("激活 Codex 端点失败: %w", err)
	}

	// 仅当端点所属渠道为当前激活渠道时才改写 config.toml（KISS：核心规则之一）
	channel, _ := channelService.GetChannelByID(ctx, target.ChannelID)
	if channel != nil && channel.IsActive {
		if syncErr := a.syncActiveCodexChannelToConfig(channel, target, ""); syncErr != nil {
			return fmt.Errorf("激活 Codex 端点失败: 配置同步未生效: %w", syncErr)
		}
		if logger != nil {
			logger.Info("✅ Codex 端点已激活并同步", "id", id, "base_url", target.BaseURL)
		}
	} else if logger != nil {
		logger.Info("ℹ️ Codex 端点已激活（渠道未激活，config.toml 暂未变化）", "id", id, "base_url", target.BaseURL)
	}

	go a.emitCodexProviderUpdate()
	return nil
}

// ============================================================================
// 旧 Provider API（兼容垫片，转调用新 Channel/Endpoint 接口）
// id 视为 channel_id；旧 input 拆为 channel + endpoint。
// 当前前端已切换到新接口，旧接口仅保留用于兼容外部调用。
// ============================================================================

func (a *App) GetCodexProviders() ([]CodexProviderInfo, error) {
	channels, err := a.GetCodexChannels()
	if err != nil {
		return nil, err
	}
	result := make([]CodexProviderInfo, 0, len(channels))
	for _, ch := range channels {
		var baseURL string
		for _, ep := range ch.Endpoints {
			if ep.IsActive {
				baseURL = ep.BaseURL
				break
			}
		}
		if baseURL == "" && len(ch.Endpoints) > 0 {
			baseURL = ch.Endpoints[0].BaseURL
		}
		result = append(result, CodexProviderInfo{
			ID:              ch.ID,
			Name:            ch.Name,
			ProviderType:    ch.ProviderType,
			BaseURL:         baseURL,
			ApiKey:          ch.ApiKey,
			ApiKeyMasked:    ch.ApiKeyMasked,
			Model:           ch.Model,
			ReasoningEffort: ch.ReasoningEffort,
			Enable1MContext: ch.Enable1MContext,
			Website:         ch.Website,
			IsActive:        ch.IsActive,
			CreatedAt:       ch.CreatedAt,
			UpdatedAt:       ch.UpdatedAt,
		})
	}
	return result, nil
}

func (a *App) CreateCodexProvider(input CreateCodexProviderInput) error {
	if err := a.CreateCodexChannel(CreateCodexChannelInput{
		Name:            input.Name,
		ProviderType:    input.ProviderType,
		ApiKey:          input.ApiKey,
		Model:           input.Model,
		ReasoningEffort: input.ReasoningEffort,
		Enable1MContext: input.Enable1MContext,
		Website:         input.Website,
	}); err != nil {
		return err
	}

	a.mu.RLock()
	channelService := a.codexChannelService
	a.mu.RUnlock()
	if channelService == nil {
		return fmt.Errorf("Codex 渠道服务未就绪，请稍后重试")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	created, err := channelService.GetActiveChannel(ctx)
	if err != nil || created == nil || created.Name != input.Name {
		ch, _ := channelService.GetChannelByName(ctx, input.Name)
		if ch == nil {
			return fmt.Errorf("创建 Codex 端点失败: 找不到名为 '%s' 的渠道", input.Name)
		}
		return a.CreateCodexEndpoint(CreateCodexEndpointInput{ChannelID: ch.ID, Name: "默认端点", BaseURL: input.BaseURL})
	}
	return a.CreateCodexEndpoint(CreateCodexEndpointInput{ChannelID: created.ID, Name: "默认端点", BaseURL: input.BaseURL})
}

func (a *App) UpdateCodexProviderByID(id int64, input UpdateCodexProviderInput) error {
	if err := a.UpdateCodexChannelByID(id, UpdateCodexChannelInput{
		Name:            input.Name,
		ProviderType:    input.ProviderType,
		ApiKey:          input.ApiKey,
		Model:           input.Model,
		ReasoningEffort: input.ReasoningEffort,
		Enable1MContext: input.Enable1MContext,
		Website:         input.Website,
	}); err != nil {
		return err
	}

	a.mu.RLock()
	endpointService := a.codexEndpointService
	a.mu.RUnlock()
	if endpointService == nil {
		return fmt.Errorf("Codex 端点服务未就绪，请稍后重试")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	active, err := endpointService.GetActiveByChannelID(ctx, id)
	if err != nil {
		return err
	}
	if active != nil && active.BaseURL != input.BaseURL {
		return a.UpdateCodexEndpointByID(active.ID, UpdateCodexEndpointInput{Name: active.Name, BaseURL: input.BaseURL})
	}
	if active == nil {
		// 渠道下没有端点，新建一个
		return a.CreateCodexEndpoint(CreateCodexEndpointInput{ChannelID: id, Name: "默认端点", BaseURL: input.BaseURL})
	}
	return nil
}

func (a *App) DeleteCodexProviderByID(id int64) error {
	return a.DeleteCodexChannelByID(id)
}

func (a *App) ActivateCodexProvider(id int64) error {
	return a.ActivateCodexChannel(id)
}

// ============================================================================
// 内部转换辅助
// ============================================================================

func codexChannelInputToRecord(name, providerType, apiKey, model, reasoningEffort, website string, enable1M bool) *store.CodexChannelRecord {
	return &store.CodexChannelRecord{
		Name:            name,
		ProviderType:    providerType,
		ApiKey:          apiKey,
		Model:           model,
		ReasoningEffort: reasoningEffort,
		Enable1MContext: enable1M,
		Website:         website,
	}
}

func (a *App) buildCodexChannelInfo(record *store.CodexChannelRecord, endpoints []*store.CodexEndpointRecord) CodexChannelInfo {
	info := CodexChannelInfo{
		ID:              record.ID,
		Name:            record.Name,
		ProviderType:    record.ProviderType,
		ApiKey:          record.ApiKey,
		ApiKeyMasked:    maskToken(record.ApiKey),
		Model:           record.Model,
		ReasoningEffort: record.ReasoningEffort,
		Enable1MContext: record.Enable1MContext,
		Website:         record.Website,
		IsActive:        record.IsActive,
		Endpoints:       make([]CodexEndpointInfo, 0, len(endpoints)),
	}

	for _, ep := range endpoints {
		info.Endpoints = append(info.Endpoints, CodexEndpointInfo{
			ID:        ep.ID,
			ChannelID: ep.ChannelID,
			Name:      ep.Name,
			BaseURL:   ep.BaseURL,
			IsActive:  ep.IsActive,
			CreatedAt: formatCodexTime(ep.CreatedAt),
			UpdatedAt: formatCodexTime(ep.UpdatedAt),
		})
	}

	info.CreatedAt = formatCodexTime(record.CreatedAt)
	info.UpdatedAt = formatCodexTime(record.UpdatedAt)
	return info
}

func formatCodexTime(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.Format("2006-01-02 15:04:05")
}
