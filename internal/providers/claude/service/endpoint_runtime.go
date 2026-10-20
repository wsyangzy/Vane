package service

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/wsyangzy/Vane/internal/config"
)

func (s *EndpointService) recoverRuntimeFromDatabase(cause error) error {
	recoveryCtx, cancel := context.WithTimeout(context.Background(), endpointRecoveryTimeout)
	defer cancel()
	if err := s.SyncFromDatabase(recoveryCtx); err != nil {
		return fmt.Errorf("%w；从数据库重新同步运行时失败: %v", cause, err)
	}
	slog.Warn("⚠️ [EndpointService] 增量更新运行时失败，已从数据库重新同步", "error", cause)
	return nil
}

// SyncFromDatabase 从数据库同步端点到管理器
// Desktop: 加载所有端点参与健康检查，并同步 enabled=true 的端点到组激活状态
func (s *EndpointService) SyncFromDatabase(ctx context.Context) error {
	if s == nil || s.store == nil {
		return fmt.Errorf("端点存储服务未启用")
	}
	if s.manager == nil {
		return fmt.Errorf("端点管理器未初始化")
	}
	// 获取所有端点（包括 enabled=false 的）
	records, err := s.store.List(ctx)
	if err != nil {
		return fmt.Errorf("获取端点列表失败: %w", err)
	}

	slog.Info(fmt.Sprintf("🔄 [EndpointService] 从数据库同步 %d 个端点", len(records)))

	// 转换为配置数组
	endpoints := make([]config.EndpointConfig, len(records))
	enabledChannelCount := make(map[string]int)
	for i, record := range records {
		endpoints[i] = s.recordToConfig(record)
		// 统计 enabled=true 的渠道分布
		if record.Enabled && record.Channel != "" {
			enabledChannelCount[record.Channel]++
		}
	}

	// 使用专门的同步方法（不走 UpdateConfig）
	s.manager.SyncEndpoints(endpoints)

	// 同步 enabled=true 的渠道到组激活状态（以渠道为路由单位）
	if len(enabledChannelCount) > 0 {
		// 选择 enabled 端点数最多的渠道；若并列则按字典序稳定选择
		var selectedChannel string
		var maxCount int
		for ch, cnt := range enabledChannelCount {
			if cnt > maxCount || (cnt == maxCount && (selectedChannel == "" || ch < selectedChannel)) {
				selectedChannel = ch
				maxCount = cnt
			}
		}

		// 若存在多个 enabled 渠道，修正为互斥激活（避免 UI/路由语义混乱）
		if len(enabledChannelCount) > 1 {
			slog.Warn(fmt.Sprintf("⚠️ [EndpointService] 检测到多个 enabled 渠道，自动修正为只保留: %s", selectedChannel))
			for _, record := range records {
				if record.Enabled && record.Channel != selectedChannel {
					record.Enabled = false
					if err := s.store.Update(ctx, record); err != nil {
						return fmt.Errorf("修正禁用端点失败 %s: %w", record.Name, err)
					}
					cfg := s.recordToConfig(record)
					if err := s.manager.UpdateEndpointConfig(EndpointRuntimeKey(record), cfg); err != nil {
						return fmt.Errorf("更新运行时管理器失败 %s: %w", record.Name, err)
					}
				}
			}
		}

		if err := s.manager.ManualActivateGroup(selectedChannel); err != nil {
			return fmt.Errorf("激活渠道失败 %s: %w", selectedChannel, err)
		} else {
			slog.Info(fmt.Sprintf("✅ [EndpointService] 已激活渠道: %s", selectedChannel))
		}
	}

	return nil
}
