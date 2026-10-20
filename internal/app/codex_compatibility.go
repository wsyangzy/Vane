package app

import (
	"context"
	"fmt"
	"time"
)

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

func (a *App) GetCodexSuspendedCount() int {
	a.mu.RLock()
	sm := a.codexSuspensionMgr
	a.mu.RUnlock()
	if sm == nil {
		return 0
	}
	return sm.GetSuspendedCount()
}

func (a *App) broadcastCodexRecovery() {
	a.mu.RLock()
	sm := a.codexSuspensionMgr
	a.mu.RUnlock()
	if sm == nil {
		return
	}
	sm.BroadcastRecovery()
}
