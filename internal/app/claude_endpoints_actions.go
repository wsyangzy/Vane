package app

import (
	"context"
	"fmt"
	"time"

	claudeservice "github.com/wsyangzy/Vane/internal/providers/claude/service"
)

func (a *App) SetEndpointFailoverEnabled(name string, enabled bool) error {
	a.mu.RLock()
	endpointService := a.endpointService
	logger := a.logger
	a.mu.RUnlock()

	if endpointService == nil {
		return fmt.Errorf("端点存储服务未启用")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	record, err := endpointService.GetEndpoint(ctx, name)
	if err != nil {
		return fmt.Errorf("获取端点失败: %w", err)
	}
	if record == nil {
		return fmt.Errorf("端点 '%s' 不存在", name)
	}

	record.FailoverEnabled = enabled
	if err := endpointService.UpdateEndpoint(ctx, record); err != nil {
		return fmt.Errorf("更新端点失败: %w", err)
	}

	if logger != nil {
		logger.Info("✅ 端点故障转移参与状态已更新", "name", name, "enabled", enabled)
	}

	return nil
}

func (a *App) SetEndpointFailoverEnabledByID(id int64, enabled bool) error {
	a.mu.RLock()
	endpointService := a.endpointService
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

	record, err := endpointService.GetEndpointByID(ctx, id)
	if err != nil {
		return fmt.Errorf("获取端点失败: %w", err)
	}
	if record == nil {
		return fmt.Errorf("端点不存在: %d", id)
	}

	record.FailoverEnabled = enabled
	if err := endpointService.UpdateEndpoint(ctx, record); err != nil {
		return fmt.Errorf("更新端点失败: %w", err)
	}

	if logger != nil {
		logger.Info("✅ 端点故障转移参与状态已更新", "id", id, "name", record.Name, "enabled", enabled)
	}

	a.emitEndpointUpdate()
	return nil
}

func (a *App) SetEndpointSkipHealthCheckByID(id int64, skip bool) error {
	a.mu.RLock()
	endpointService := a.endpointService
	endpointManager := a.endpointManager
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

	record, err := endpointService.GetEndpointByID(ctx, id)
	if err != nil {
		return fmt.Errorf("获取端点失败: %w", err)
	}
	if record == nil {
		return fmt.Errorf("端点不存在: %d", id)
	}

	record.SkipHealthCheck = skip
	if err := endpointService.UpdateEndpoint(ctx, record); err != nil {
		return fmt.Errorf("更新端点失败: %w", err)
	}

	// 同步到运行时 endpoint manager
	if endpointManager != nil {
		endpointManager.SetEndpointSkipHealthCheck(claudeservice.EndpointRuntimeKey(record), skip)
	}

	if logger != nil {
		logger.Info("✅ 端点跳过健康检测状态已更新", "id", id, "name", record.Name, "skip", skip)
	}

	a.emitEndpointUpdate()
	return nil
}

func (a *App) DeleteEndpointRecord(name string) error {
	a.mu.RLock()
	endpointService := a.endpointService
	logger := a.logger
	a.mu.RUnlock()

	if endpointService == nil {
		return fmt.Errorf("端点存储服务未启用")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := endpointService.DeleteEndpoint(ctx, name); err != nil {
		return fmt.Errorf("删除端点失败: %w", err)
	}

	if logger != nil {
		logger.Info("✅ 端点已删除", "name", name)
	}

	// 删除成功后，异步同步端点倍率到 UsageTracker
	go a.syncEndpointMultipliersToTracker(context.Background())

	return nil
}

func (a *App) DeleteEndpointRecordByID(id int64) error {
	a.mu.RLock()
	endpointService := a.endpointService
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

	if err := endpointService.DeleteEndpointByID(ctx, id); err != nil {
		return fmt.Errorf("删除端点失败: %w", err)
	}

	if logger != nil {
		logger.Info("✅ 端点已删除", "id", id)
	}

	go a.syncEndpointMultipliersToTracker(context.Background())
	a.emitEndpointUpdate()
	return nil
}

func (a *App) ToggleEndpointRecord(name string, enabled bool) error {
	a.mu.RLock()
	endpointService := a.endpointService
	logger := a.logger
	a.mu.RUnlock()

	if endpointService == nil {
		return fmt.Errorf("端点存储服务未启用")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if enabled {
		// 激活：以端点所属渠道为单位
		record, err := endpointService.GetEndpoint(ctx, name)
		if err != nil {
			return fmt.Errorf("获取端点配置失败: %w", err)
		}
		if record == nil {
			return fmt.Errorf("端点 '%s' 不存在", name)
		}
		if err := endpointService.ActivateChannel(ctx, record.Channel); err != nil {
			return fmt.Errorf("激活渠道失败: %w", err)
		}
	} else {
		// 停用：以端点所属渠道为单位
		record, err := endpointService.GetEndpoint(ctx, name)
		if err != nil {
			return fmt.Errorf("获取端点配置失败: %w", err)
		}
		if record == nil {
			return fmt.Errorf("端点 '%s' 不存在", name)
		}
		if err := endpointService.DeactivateChannel(ctx, record.Channel); err != nil {
			return fmt.Errorf("停用渠道失败: %w", err)
		}
	}

	status := map[bool]string{true: "启用", false: "禁用"}[enabled]

	if logger != nil {
		logger.Info("✅ 端点状态已切换", "name", name, "status", status)
	}

	return nil
}

func (a *App) ToggleEndpointRecordByID(id int64, enabled bool) error {
	a.mu.RLock()
	endpointService := a.endpointService
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

	record, err := endpointService.GetEndpointByID(ctx, id)
	if err != nil {
		return fmt.Errorf("获取端点配置失败: %w", err)
	}
	if record == nil {
		return fmt.Errorf("端点不存在: %d", id)
	}

	if enabled {
		if err := endpointService.ActivateChannel(ctx, record.Channel); err != nil {
			return fmt.Errorf("激活渠道失败: %w", err)
		}
	} else {
		if err := endpointService.DeactivateChannel(ctx, record.Channel); err != nil {
			return fmt.Errorf("停用渠道失败: %w", err)
		}
	}

	status := map[bool]string{true: "启用", false: "禁用"}[enabled]
	if logger != nil {
		logger.Info("✅ 端点状态已切换", "id", id, "channel", record.Channel, "name", record.Name, "status", status)
	}

	a.emitEndpointUpdate()
	return nil
}

func (a *App) ActivateEndpointRecordByID(id int64) error {
	a.mu.RLock()
	endpointService := a.endpointService
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

	record, err := endpointService.ActivateEndpointByID(ctx, id)
	if err != nil {
		return err
	}

	if logger != nil && record != nil {
		logger.Info("✅ 当前端点已切换", "id", id, "channel", record.Channel, "name", record.Name)
	}

	a.emitEndpointUpdate()
	return nil
}
