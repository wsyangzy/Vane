package service

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/wsyangzy/Vane/internal/config"
	"github.com/wsyangzy/Vane/internal/providers/claude/proxy/endpoint"
	"github.com/wsyangzy/Vane/internal/providers/claude/store"
)

// EndpointService 端点管理业务服务
// 连接 EndpointStore（数据持久化）和 EndpointManager（运行时管理）
type EndpointService struct {
	store                store.EndpointStore
	manager              *endpoint.Manager
	config               *config.Config
	credentialGroupStore store.ClaudeCredentialGroupStore
	activationMu         sync.Mutex
}

type endpointTransactionStarter interface {
	BeginTx(context.Context) (*sql.Tx, error)
}

var endpointPriorityMu sync.Mutex

const endpointRecoveryTimeout = 3 * time.Second

// NewEndpointService 创建端点服务实例
func NewEndpointService(
	endpointStore store.EndpointStore,
	manager *endpoint.Manager,
	cfg *config.Config,
	credentialGroupStore ...store.ClaudeCredentialGroupStore,
) *EndpointService {
	var groupStore store.ClaudeCredentialGroupStore
	if len(credentialGroupStore) > 0 {
		groupStore = credentialGroupStore[0]
	}
	return &EndpointService{
		store:                endpointStore,
		manager:              manager,
		config:               cfg,
		credentialGroupStore: groupStore,
	}
}

// CreateEndpoint 创建新端点
// 1. 保存到数据库
// 2. 添加到运行时管理器
// 优先级自动递增（负数表示自动分配；0 是最高优先级）
func (s *EndpointService) CreateEndpoint(ctx context.Context, record *store.EndpointRecord) (*store.EndpointRecord, error) {
	// 验证必填字段
	if err := s.validateRecord(record); err != nil {
		return nil, err
	}
	if err := s.ensureEndpointCredentialGroup(ctx, record); err != nil {
		return nil, err
	}

	// 允许不同凭证分组同名端点，但同一凭证分组内必须唯一
	if existing, err := s.findEndpointNameConflict(ctx, record.Channel, record.CredentialGroupID, record.Name); err != nil {
		return nil, fmt.Errorf("校验端点名称失败: %w", err)
	} else if existing != nil {
		return nil, fmt.Errorf(
			"同一凭证分组内端点名称必须唯一：渠道 '%s' 已存在同名端点 '%s'，请修改名称或选择其他凭证分组",
			record.Channel,
			record.Name,
		)
	}
	if existing, err := s.findEndpointURLConflict(ctx, record.Channel, record.CredentialGroupID, record.URL); err != nil {
		return nil, fmt.Errorf("校验端点 URL 失败: %w", err)
	} else if existing != nil {
		return nil, fmt.Errorf("同一凭证分组内已存在相同的端点 URL，请修改 URL 或选择其他凭证分组")
	}

	working := *record
	var created *store.EndpointRecord
	var moved *store.EndpointRecord
	var movedPreviousPriority int
	err := s.withPriorityTransaction(ctx, func(endpointStore store.EndpointStore) error {
		if working.Priority < 0 {
			maxPriority, err := endpointStore.GetMaxPriorityByChannelCredentialGroup(ctx, working.Channel, working.CredentialGroupID)
			if err != nil {
				return fmt.Errorf("获取凭证分组最大优先级失败: %w", err)
			}
			working.Priority = maxPriority + 1
			slog.Info(fmt.Sprintf("🔢 [EndpointService] 自动分配优先级: %s -> %d", working.Name, working.Priority))
		}
		if working.Priority >= 0 {
			conflicting, err := endpointStore.GetByChannelCredentialGroupAndPriority(ctx, working.Channel, working.CredentialGroupID, working.Priority)
			if err != nil {
				return fmt.Errorf("检查端点优先级冲突失败: %w", err)
			}
			if conflicting != nil {
				maxPriority, err := endpointStore.GetMaxPriorityByChannelCredentialGroup(ctx, working.Channel, working.CredentialGroupID)
				if err != nil {
					return fmt.Errorf("获取凭证分组最大优先级失败: %w", err)
				}
				movedCopy := *conflicting
				movedPreviousPriority = movedCopy.Priority
				movedCopy.Priority = maxPriority + 1
				if err := endpointStore.UpdateByID(ctx, &movedCopy); err != nil {
					return fmt.Errorf("端点优先级替换失败: %w", err)
				}
				moved = &movedCopy
			}
		}
		var err error
		created, err = endpointStore.Create(ctx, &working)
		if err != nil {
			return fmt.Errorf("保存端点到数据库失败: %w", err)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	*record = working

	// 转换为配置并添加到管理器
	cfg := s.recordToConfig(created)
	if s.manager == nil {
		return nil, s.rollbackCreatedEndpoint(ctx, created, moved, movedPreviousPriority, fmt.Errorf("端点管理器未初始化"))
	}
	if err := s.manager.AddEndpoint(cfg); err != nil {
		return nil, s.rollbackCreatedEndpoint(ctx, created, moved, movedPreviousPriority, fmt.Errorf("添加端点到管理器失败: %w", err))
	}
	if moved != nil {
		conflictingCfg := s.recordToConfig(moved)
		if err := s.manager.UpdateEndpointConfig(EndpointRuntimeKey(moved), conflictingCfg); err != nil {
			slog.Warn(fmt.Sprintf("⚠️ [EndpointService] 更新冲突端点运行时配置失败: %v", err))
		}
	}

	slog.Info(fmt.Sprintf("✅ [EndpointService] 创建端点成功: %s (优先级: %d)", record.Name, record.Priority))
	return created, nil
}

// rollbackCreatedEndpoint restores the database state after the runtime manager
// rejected a newly-created endpoint. Priority movement and endpoint creation
// are committed together, so both records must be compensated together here.
func (s *EndpointService) rollbackCreatedEndpoint(ctx context.Context, created, moved *store.EndpointRecord, movedPreviousPriority int, cause error) error {
	rollbackErrors := make([]string, 0, 2)
	if created != nil && s.store != nil {
		if err := s.store.DeleteByID(ctx, created.ID); err != nil {
			rollbackErrors = append(rollbackErrors, fmt.Sprintf("删除已创建端点失败: %v", err))
		}
	}
	if moved != nil && s.store != nil {
		restored := *moved
		restored.Priority = movedPreviousPriority
		if err := s.store.UpdateByID(ctx, &restored); err != nil {
			rollbackErrors = append(rollbackErrors, fmt.Sprintf("恢复冲突端点优先级失败: %v", err))
		}
	}
	if len(rollbackErrors) > 0 {
		return fmt.Errorf("%w；回滚失败: %s", cause, strings.Join(rollbackErrors, "; "))
	}
	return cause
}

// GetEndpoint 获取端点详情
func (s *EndpointService) GetEndpoint(ctx context.Context, name string) (*store.EndpointRecord, error) {
	record, err := s.store.Get(ctx, name)
	if err != nil {
		return nil, fmt.Errorf("获取端点失败: %w", err)
	}
	return record, nil
}

// GetEndpointByID 获取端点详情（按ID）
func (s *EndpointService) GetEndpointByID(ctx context.Context, id int64) (*store.EndpointRecord, error) {
	record, err := s.store.GetByID(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("获取端点失败: %w", err)
	}
	return record, nil
}

// ListEndpoints 列出所有端点
func (s *EndpointService) ListEndpoints(ctx context.Context) ([]*store.EndpointRecord, error) {
	records, err := s.store.List(ctx)
	if err != nil {
		return nil, fmt.Errorf("列出端点失败: %w", err)
	}
	return records, nil
}

// ListEndpointsByChannel 按渠道列出端点
func (s *EndpointService) ListEndpointsByChannel(ctx context.Context, channel string) ([]*store.EndpointRecord, error) {
	records, err := s.store.ListByChannel(ctx, channel)
	if err != nil {
		return nil, fmt.Errorf("按渠道列出端点失败: %w", err)
	}
	return records, nil
}

// UpdateEndpoint 更新端点配置
// 1. 更新数据库
// 2. 更新运行时管理器
// 优先级冲突时自动对调
func (s *EndpointService) UpdateEndpoint(ctx context.Context, record *store.EndpointRecord) error {
	if record == nil || record.ID <= 0 {
		return fmt.Errorf("端点ID不能为空")
	}

	// 验证端点存在
	existing, err := s.store.GetByID(ctx, record.ID)
	if err != nil {
		return fmt.Errorf("查询端点失败: %w", err)
	}
	if existing == nil {
		return fmt.Errorf("端点不存在: %d", record.ID)
	}

	// 验证必填字段
	if err := s.validateRecord(record); err != nil {
		return err
	}
	if record.CredentialGroupID <= 0 {
		record.CredentialGroupID = existing.CredentialGroupID
	}
	if record.Channel != existing.Channel && record.CredentialGroupID == existing.CredentialGroupID {
		record.CredentialGroupID = 0
		record.CredentialGroupName = ""
	}
	if err := s.ensureEndpointCredentialGroup(ctx, record); err != nil {
		return err
	}

	// 允许不同凭证分组同名，需确保"同分组内唯一"约束
	if other, err := s.findEndpointNameConflict(ctx, record.Channel, record.CredentialGroupID, record.Name); err != nil {
		return fmt.Errorf("校验端点名称失败: %w", err)
	} else if other != nil && other.ID != record.ID {
		return fmt.Errorf(
			"同一凭证分组内端点名称必须唯一：渠道 '%s' 已存在同名端点 '%s'，请修改名称或选择其他凭证分组",
			record.Channel,
			record.Name,
		)
	}
	if other, err := s.findEndpointURLConflict(ctx, record.Channel, record.CredentialGroupID, record.URL); err != nil {
		return fmt.Errorf("校验端点 URL 失败: %w", err)
	} else if other != nil && other.ID != record.ID {
		return fmt.Errorf("同一凭证分组内已存在相同的端点 URL，请修改 URL 或选择其他凭证分组")
	}

	scopeChanged := record.Channel != existing.Channel || record.CredentialGroupID != existing.CredentialGroupID
	activeEndpointSwitched := false

	if existing.Enabled && !record.FailoverEnabled {
		nextEndpoint, err := s.findNextFailoverEndpoint(ctx, existing.Channel, existing.ID)
		if err != nil {
			return err
		}
		if nextEndpoint == nil {
			return fmt.Errorf("当前路由端点关闭参与转移失败：没有其他参与渠道内故障转移的端点")
		}
		if _, err := s.ActivateEndpointByID(ctx, nextEndpoint.ID); err != nil {
			return fmt.Errorf("切换当前路由端点失败: %w", err)
		}
		activeEndpointSwitched = true
		record.Enabled = false
	}

	var moved *store.EndpointRecord
	err = s.withPriorityTransaction(ctx, func(endpointStore store.EndpointStore) error {
		// 目标分组的尾部优先级必须和移动写入处于同一事务，避免并发移动读取到同一个尾部值。
		if scopeChanged {
			maxPriority, err := endpointStore.GetMaxPriorityByChannelCredentialGroup(ctx, record.Channel, record.CredentialGroupID)
			if err != nil {
				return fmt.Errorf("获取目标凭证分组最大优先级失败: %w", err)
			}
			newPriority := maxPriority + 1
			slog.Info(fmt.Sprintf("🔄 [EndpointService] 端点归属变更: %s/%d -> %s/%d，优先级自动调整: %d -> %d",
				existing.Channel, existing.CredentialGroupID, record.Channel, record.CredentialGroupID, record.Priority, newPriority))
			record.Priority = newPriority
		}
		if record.Priority != existing.Priority && !scopeChanged {
			conflicting, err := endpointStore.GetByChannelCredentialGroupAndPriority(ctx, record.Channel, record.CredentialGroupID, record.Priority)
			if err != nil {
				return fmt.Errorf("检查优先级冲突失败: %w", err)
			}
			if conflicting != nil && conflicting.ID != record.ID {
				oldPriority := existing.Priority
				movedCopy := *conflicting
				movedCopy.Priority = oldPriority
				if err := endpointStore.UpdateByID(ctx, &movedCopy); err != nil {
					return fmt.Errorf("优先级对调失败（更新冲突端点）: %w", err)
				}
				moved = &movedCopy
			}
		}
		if err := endpointStore.UpdateByID(ctx, record); err != nil {
			return fmt.Errorf("更新数据库失败: %w", err)
		}
		return nil
	})
	if err != nil {
		if activeEndpointSwitched {
			rollbackCtx, cancel := context.WithTimeout(context.Background(), endpointRecoveryTimeout)
			_, rollbackErr := s.ActivateEndpointByID(rollbackCtx, existing.ID)
			cancel()
			if rollbackErr != nil {
				return fmt.Errorf("%w；恢复原当前路由端点失败: %v", err, rollbackErr)
			}
		}
		return err
	}

	runtimeErrors := make([]string, 0, 2)
	if moved != nil && s.manager != nil {
		conflictingCfg := s.recordToConfig(moved)
		if err := s.manager.UpdateEndpointConfig(EndpointRuntimeKey(moved), conflictingCfg); err != nil {
			runtimeErrors = append(runtimeErrors, fmt.Sprintf("更新冲突端点运行时配置失败: %v", err))
		}
	}

	// 更新运行时管理器（单元测试/降级模式可能暂时没有管理器）
	if s.manager != nil {
		cfg := s.recordToConfig(record)
		oldKey := EndpointRuntimeKey(existing)
		if err := s.manager.UpdateEndpointConfig(oldKey, cfg); err != nil {
			runtimeErrors = append(runtimeErrors, fmt.Sprintf("更新端点运行时配置失败: %v", err))
		}
	}
	if len(runtimeErrors) > 0 {
		if err := s.recoverRuntimeFromDatabase(fmt.Errorf("%s", strings.Join(runtimeErrors, "; "))); err != nil {
			return err
		}
	}

	slog.Info(fmt.Sprintf("✅ [EndpointService] 更新端点成功: %s (优先级: %d)", record.Name, record.Priority))
	return nil
}

// DeleteEndpoint 删除端点
func (s *EndpointService) DeleteEndpoint(ctx context.Context, name string) error {
	// 验证端点存在
	existing, err := s.store.Get(ctx, name)
	if err != nil {
		return fmt.Errorf("查询端点失败: %w", err)
	}
	if existing == nil {
		return fmt.Errorf("端点 '%s' 不存在", name)
	}

	return s.deleteEndpointRecord(ctx, existing)
}

// DeleteEndpointByID 删除端点（按ID，避免同名歧义）
func (s *EndpointService) DeleteEndpointByID(ctx context.Context, id int64) error {
	if id <= 0 {
		return fmt.Errorf("端点ID不能为空")
	}
	existing, err := s.store.GetByID(ctx, id)
	if err != nil {
		return fmt.Errorf("查询端点失败: %w", err)
	}
	if existing == nil {
		return fmt.Errorf("端点不存在: %d", id)
	}

	return s.deleteEndpointRecord(ctx, existing)
}

func (s *EndpointService) deleteEndpointRecord(ctx context.Context, existing *store.EndpointRecord) error {
	s.activationMu.Lock()
	defer s.activationMu.Unlock()

	current, err := s.store.GetByID(ctx, existing.ID)
	if err != nil {
		return fmt.Errorf("查询端点失败: %w", err)
	}
	if current == nil {
		return fmt.Errorf("端点不存在: %d", existing.ID)
	}

	previousActive, err := s.store.ListEnabled(ctx)
	if err != nil {
		return fmt.Errorf("获取当前活跃端点失败: %w", err)
	}
	activeSwitched := false
	if current.Enabled && len(previousActive) == 1 {
		nextEndpoint, err := s.findNextFailoverEndpoint(ctx, current.Channel, current.ID)
		if err != nil {
			return err
		}
		if nextEndpoint != nil {
			if _, err := s.activateEndpointByIDLocked(ctx, nextEndpoint.ID); err != nil {
				return fmt.Errorf("删除当前路由端点前切换后续端点失败: %w", err)
			}
			activeSwitched = true
		}
	}

	if err := s.store.DeleteByID(ctx, existing.ID); err != nil {
		if activeSwitched {
			if restoreErr := s.restoreActiveSelectionLocked(previousActive); restoreErr != nil {
				return fmt.Errorf("从数据库删除失败: %w；恢复原当前路由端点失败: %v", err, restoreErr)
			}
		}
		return fmt.Errorf("从数据库删除失败: %w", err)
	}
	if s.manager != nil {
		if err := s.manager.RemoveEndpoint(EndpointRuntimeKey(current)); err != nil {
			if recoveryErr := s.recoverRuntimeFromDatabase(fmt.Errorf("从管理器移除端点失败: %w", err)); recoveryErr != nil {
				return recoveryErr
			}
		}
	}
	slog.Info(fmt.Sprintf("✅ [EndpointService] 删除端点成功: %s", existing.Name))
	return nil
}

// ToggleEndpoint 切换端点启用状态
func (s *EndpointService) ToggleEndpoint(ctx context.Context, name string, enabled bool) error {
	record, err := s.store.Get(ctx, name)
	if err != nil {
		return fmt.Errorf("获取端点失败: %w", err)
	}
	if record == nil {
		return fmt.Errorf("端点 '%s' 不存在", name)
	}

	record.Enabled = enabled
	return s.UpdateEndpoint(ctx, record)
}

// GetEndpointWithHealth 获取端点详情（包含健康状态）
func (s *EndpointService) GetEndpointWithHealth(ctx context.Context, name string) (map[string]interface{}, error) {
	record, err := s.store.Get(ctx, name)
	if err != nil {
		return nil, fmt.Errorf("获取端点失败: %w", err)
	}
	if record == nil {
		return nil, fmt.Errorf("端点 '%s' 不存在", name)
	}

	// 获取健康状态
	status := s.manager.GetEndpointStatus(EndpointRuntimeKey(record))

	return map[string]interface{}{
		"id":                                record.ID,
		"channel":                           record.Channel,
		"credential_group_id":               record.CredentialGroupID,
		"credential_group_name":             record.CredentialGroupName,
		"name":                              record.Name,
		"url":                               record.URL,
		"token_masked":                      maskToken(record.Token),
		"priority":                          record.Priority,
		"failover_enabled":                  record.FailoverEnabled,
		"timeout_seconds":                   record.TimeoutSeconds,
		"cost_multiplier":                   record.CostMultiplier,
		"input_cost_multiplier":             record.InputCostMultiplier,
		"output_cost_multiplier":            record.OutputCostMultiplier,
		"cache_creation_cost_multiplier":    record.CacheCreationCostMultiplier,
		"cache_creation_cost_multiplier_1h": record.CacheCreationCostMultiplier1h,
		"cache_read_cost_multiplier":        record.CacheReadCostMultiplier,
		"model_multipliers":                 record.ModelMultipliers,
		"enabled":                           record.Enabled,
		"created_at":                        record.CreatedAt,
		"updated_at":                        record.UpdatedAt,
		"health": map[string]interface{}{
			"healthy":          status.Healthy,
			"never_checked":    status.NeverChecked,
			"last_check":       status.LastCheck.Format("2006-01-02 15:04:05"),
			"response_time_ms": status.ResponseTime.Milliseconds(),
		},
	}, nil
}
