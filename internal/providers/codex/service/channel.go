package service

import (
	"context"
	"database/sql"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"sync"

	"ai-relay-forwarder/internal/providers/codex/store"
)

var codexChannelNamePattern = regexp.MustCompile(`^[\p{L}\p{N}][\p{L}\p{N} _-]{0,63}$`)

// CodexChannelService 管理 Codex 渠道（共用配置层）。
type CodexChannelService struct {
	channelStore  store.CodexChannelStore
	endpointStore store.CodexEndpointStore
}

type codexChannelTransactionStarter interface {
	BeginTx(context.Context) (*sql.Tx, error)
}

type codexChannelActiveClearer interface {
	ClearActive(context.Context, int64) error
}

var codexChannelPriorityMu sync.Mutex

func NewCodexChannelService(channelStore store.CodexChannelStore, endpointStore store.CodexEndpointStore) *CodexChannelService {
	return &CodexChannelService{channelStore: channelStore, endpointStore: endpointStore}
}

func (s *CodexChannelService) ListChannels(ctx context.Context) ([]*store.CodexChannelRecord, error) {
	return s.channelStore.List(ctx)
}

func (s *CodexChannelService) GetChannelByID(ctx context.Context, id int64) (*store.CodexChannelRecord, error) {
	return s.channelStore.GetByID(ctx, id)
}

func (s *CodexChannelService) GetChannelByName(ctx context.Context, name string) (*store.CodexChannelRecord, error) {
	return s.channelStore.GetByName(ctx, name)
}

func (s *CodexChannelService) GetActiveChannel(ctx context.Context) (*store.CodexChannelRecord, error) {
	return s.channelStore.GetActive(ctx)
}

// CreateChannel 写入新渠道。若当前没有任何激活渠道，新建后自动激活。
func (s *CodexChannelService) CreateChannel(ctx context.Context, record *store.CodexChannelRecord) (*store.CodexChannelRecord, error) {
	if err := normalizeAndValidateCodexChannel(record); err != nil {
		return nil, err
	}
	working := *record
	var created *store.CodexChannelRecord
	err := s.withPriorityTransaction(ctx, func(channelStore store.CodexChannelStore) error {
		var err error
		created, err = createCodexChannelWithStore(ctx, channelStore, &working)
		return err
	})
	if err != nil {
		return nil, err
	}
	*record = working
	return created, nil
}

func createCodexChannelWithStore(ctx context.Context, channelStore store.CodexChannelStore, record *store.CodexChannelRecord) (*store.CodexChannelRecord, error) {
	existing, err := channelStore.GetByName(ctx, record.Name)
	if err != nil {
		return nil, err
	}
	if existing != nil {
		return nil, fmt.Errorf("Codex 渠道 '%s' 已存在", record.Name)
	}

	record.IsActive = false
	channels, err := channelStore.List(ctx)
	if err != nil {
		return nil, err
	}
	maxPriority := -1
	var conflicting *store.CodexChannelRecord
	for _, channel := range channels {
		if channel == nil {
			continue
		}
		if channel.Priority > maxPriority {
			maxPriority = channel.Priority
		}
		if channel.Priority == record.Priority {
			conflicting = channel
		}
	}
	if record.Priority < 0 {
		record.Priority = maxPriority + 1
	} else if conflicting != nil {
		moved := *conflicting
		moved.Priority = maxPriority + 1
		if err := channelStore.UpdateByID(ctx, moved.ID, &moved); err != nil {
			return nil, fmt.Errorf("Codex 渠道优先级顺延失败: %w", err)
		}
	}
	created, err := channelStore.Create(ctx, record)
	if err != nil {
		return nil, err
	}

	active, err := channelStore.GetActive(ctx)
	if err != nil {
		return nil, err
	}
	if active == nil {
		if err := channelStore.SetActive(ctx, created.ID); err != nil {
			return nil, err
		}
		return channelStore.GetByID(ctx, created.ID)
	}
	return created, nil
}

func (s *CodexChannelService) UpdateChannelByID(ctx context.Context, id int64, record *store.CodexChannelRecord) error {
	if err := normalizeAndValidateCodexChannel(record); err != nil {
		return err
	}
	working := *record
	return s.withPriorityTransaction(ctx, func(channelStore store.CodexChannelStore) error {
		return updateCodexChannelWithStore(ctx, channelStore, id, &working)
	})
}

// RestoreChannel restores a previously persisted channel snapshot exactly.
// Rollback paths must not reinterpret a false failover flag as an omitted edit
// or apply the normal priority-edit semantics a second time.
func (s *CodexChannelService) RestoreChannel(ctx context.Context, record *store.CodexChannelRecord) error {
	if s == nil || s.channelStore == nil {
		return fmt.Errorf("Codex 渠道存储服务未启用")
	}
	if record == nil || record.ID <= 0 {
		return fmt.Errorf("恢复 Codex 渠道记录无效")
	}

	return s.withPriorityTransaction(ctx, func(channelStore store.CodexChannelStore) error {
		current, err := channelStore.GetByID(ctx, record.ID)
		if err != nil {
			return fmt.Errorf("读取待恢复 Codex 渠道失败: %w", err)
		}
		if current == nil {
			return fmt.Errorf("待恢复 Codex 渠道不存在: %d", record.ID)
		}

		channels, err := channelStore.List(ctx)
		if err != nil {
			return fmt.Errorf("检查恢复后的 Codex 渠道优先级失败: %w", err)
		}
		maxPriority := record.Priority
		for _, channel := range channels {
			if channel != nil && channel.Priority > maxPriority {
				maxPriority = channel.Priority
			}
		}
		for _, channel := range channels {
			if channel == nil || channel.ID == record.ID || channel.Priority != record.Priority {
				continue
			}
			moved := *channel
			moved.Priority = maxPriority + 1
			if err := channelStore.UpdateByID(ctx, moved.ID, &moved); err != nil {
				return fmt.Errorf("为恢复优先级腾挪 Codex 渠道失败: %w", err)
			}
			break
		}

		if err := channelStore.UpdateByID(ctx, record.ID, record); err != nil {
			return fmt.Errorf("恢复 Codex 渠道记录失败: %w", err)
		}
		if record.IsActive {
			if err := channelStore.SetActive(ctx, record.ID); err != nil {
				return fmt.Errorf("恢复 Codex 激活渠道失败: %w", err)
			}
		}
		return nil
	})
}

func updateCodexChannelWithStore(ctx context.Context, channelStore store.CodexChannelStore, id int64, record *store.CodexChannelRecord) error {
	existing, err := channelStore.GetByID(ctx, id)
	if err != nil {
		return err
	}
	if existing == nil {
		return fmt.Errorf("Codex 渠道不存在: %d", id)
	}

	nameConflict, err := channelStore.GetByName(ctx, record.Name)
	if err != nil {
		return err
	}
	if nameConflict != nil && nameConflict.ID != id {
		return fmt.Errorf("Codex 渠道 '%s' 已存在", record.Name)
	}
	if existing.IsActive && !record.FailoverEnabled {
		return fmt.Errorf("当前接管渠道不能暂停，请先切换到其他渠道")
	}
	if existing.Priority != record.Priority {
		channels, err := channelStore.List(ctx)
		if err != nil {
			return err
		}
		for _, channel := range channels {
			if channel == nil || channel.ID == id || channel.Priority != record.Priority {
				continue
			}
			swapped := *channel
			swapped.Priority = existing.Priority
			if err := channelStore.UpdateByID(ctx, swapped.ID, &swapped); err != nil {
				return fmt.Errorf("Codex 渠道优先级对调失败: %w", err)
			}
			break
		}
	} else if record.FailoverEnabled && !existing.FailoverEnabled {
		if err := validateCodexChannelUniquePriority(ctx, channelStore, id, record.Priority); err != nil {
			return err
		}
	}

	return channelStore.UpdateByID(ctx, id, record)
}

func (s *CodexChannelService) validateUniquePriority(ctx context.Context, excludeID int64, priority int) error {
	return validateCodexChannelUniquePriority(ctx, s.channelStore, excludeID, priority)
}

func validateCodexChannelUniquePriority(ctx context.Context, channelStore store.CodexChannelStore, excludeID int64, priority int) error {
	channels, err := channelStore.List(ctx)
	if err != nil {
		return err
	}
	for _, channel := range channels {
		if channel.ID != excludeID && channel.Priority == priority {
			return fmt.Errorf("Codex 渠道优先级 P%d 已被渠道 '%s' 使用", priority, channel.Name)
		}
	}
	return nil
}

func (s *CodexChannelService) withPriorityTransaction(ctx context.Context, fn func(store.CodexChannelStore) error) error {
	return s.withChannelEndpointTransaction(ctx, func(channelStore store.CodexChannelStore, _ store.CodexEndpointStore) error {
		return fn(channelStore)
	})
}

func (s *CodexChannelService) withChannelEndpointTransaction(ctx context.Context, fn func(store.CodexChannelStore, store.CodexEndpointStore) error) error {
	if s == nil || s.channelStore == nil {
		return fmt.Errorf("Codex 渠道存储服务未启用")
	}
	codexChannelPriorityMu.Lock()
	defer codexChannelPriorityMu.Unlock()

	starter, ok := s.channelStore.(codexChannelTransactionStarter)
	if !ok {
		return fn(s.channelStore, s.endpointStore)
	}
	tx, err := starter.BeginTx(ctx)
	if err != nil {
		return fmt.Errorf("开启 Codex 渠道优先级事务失败: %w", err)
	}
	txChannelStore := s.channelStore.WithTx(tx)
	var txEndpointStore store.CodexEndpointStore
	if s.endpointStore != nil {
		txEndpointStore = s.endpointStore.WithTx(tx)
	}
	if err := fn(txChannelStore, txEndpointStore); err != nil {
		_ = tx.Rollback()
		return err
	}
	if err := tx.Commit(); err != nil {
		_ = tx.Rollback()
		return fmt.Errorf("提交 Codex 渠道优先级事务失败: %w", err)
	}
	return nil
}

// GetChannelDeletionSuccessor returns the exact eligible channel/endpoint pair
// used by deletion: participating channels first by priority, then
// participating endpoints by priority.
func (s *CodexChannelService) GetChannelDeletionSuccessor(ctx context.Context, id int64) (*store.CodexChannelRecord, *store.CodexEndpointRecord, error) {
	if s == nil || s.channelStore == nil || s.endpointStore == nil {
		return nil, nil, fmt.Errorf("Codex 渠道或端点存储未就绪")
	}
	existing, err := s.channelStore.GetByID(ctx, id)
	if err != nil {
		return nil, nil, err
	}
	if existing == nil {
		return nil, nil, fmt.Errorf("Codex 渠道不存在: %d", id)
	}
	if !existing.IsActive {
		return nil, nil, nil
	}
	return selectCodexFailoverChannel(ctx, s.channelStore, s.endpointStore, id)
}

// PrepareChannelDeletion switches the persisted active route before the API
// reloads runtime state. The expected IDs prevent pre-sync and persistence from
// choosing different successors.
func (s *CodexChannelService) PrepareChannelDeletion(ctx context.Context, id, expectedChannelID, expectedEndpointID int64) error {
	return s.withChannelEndpointTransaction(ctx, func(channelStore store.CodexChannelStore, endpointStore store.CodexEndpointStore) error {
		if endpointStore == nil {
			return fmt.Errorf("Codex 端点存储未就绪")
		}
		existing, err := channelStore.GetByID(ctx, id)
		if err != nil {
			return err
		}
		if existing == nil {
			return fmt.Errorf("Codex 渠道不存在: %d", id)
		}
		if !existing.IsActive {
			return fmt.Errorf("Codex 待删除渠道的激活状态已变化，请重试")
		}
		nextChannel, nextEndpoint, err := selectCodexFailoverChannel(ctx, channelStore, endpointStore, id)
		if err != nil {
			return err
		}
		if channelRecordID(nextChannel) != expectedChannelID || endpointRecordID(nextEndpoint) != expectedEndpointID {
			return fmt.Errorf("Codex 删除渠道候选已变化，请重试")
		}
		if err := channelStore.SetFailoverEnabled(ctx, id, false); err != nil {
			return fmt.Errorf("暂停待删除 Codex 渠道失败: %w", err)
		}
		if nextChannel == nil {
			return clearCodexActiveChannel(ctx, channelStore, id)
		}
		if err := endpointStore.SetActive(ctx, nextEndpoint.ID); err != nil {
			return fmt.Errorf("切换 Codex 后续激活端点失败: %w", err)
		}
		if err := channelStore.SetActive(ctx, nextChannel.ID); err != nil {
			return fmt.Errorf("切换 Codex 后续激活渠道失败: %w", err)
		}
		return nil
	})
}

func clearCodexActiveChannel(ctx context.Context, channelStore store.CodexChannelStore, id int64) error {
	clearer, ok := channelStore.(codexChannelActiveClearer)
	if !ok {
		return fmt.Errorf("Codex 渠道存储不支持清空激活渠道")
	}
	return clearer.ClearActive(ctx, id)
}

func channelRecordID(record *store.CodexChannelRecord) int64 {
	if record == nil {
		return 0
	}
	return record.ID
}

// DeleteChannel 删除渠道。若被删除的是当前激活渠道，在同一事务中按
// failover_enabled + priority 激活后续渠道及端点。端点/凭证表由 SQLite 级联删除。
func (s *CodexChannelService) DeleteChannelByID(ctx context.Context, id int64) error {
	return s.withChannelEndpointTransaction(ctx, func(channelStore store.CodexChannelStore, endpointStore store.CodexEndpointStore) error {
		existing, err := channelStore.GetByID(ctx, id)
		if err != nil {
			return err
		}
		if existing == nil {
			return fmt.Errorf("Codex 渠道不存在: %d", id)
		}
		if existing.IsActive {
			nextChannel, nextEndpoint, err := selectCodexFailoverChannel(ctx, channelStore, endpointStore, id)
			if err != nil {
				return err
			}
			if nextChannel != nil {
				if err := endpointStore.SetActive(ctx, nextEndpoint.ID); err != nil {
					return fmt.Errorf("切换 Codex 后续激活端点失败: %w", err)
				}
				if err := channelStore.SetActive(ctx, nextChannel.ID); err != nil {
					return fmt.Errorf("切换 Codex 后续激活渠道失败: %w", err)
				}
			}
		}
		return channelStore.DeleteByID(ctx, id)
	})
}

func (s *CodexChannelService) ActivateChannel(ctx context.Context, id int64, requireFailoverEnabled bool) error {
	existing, err := s.channelStore.GetByID(ctx, id)
	if err != nil {
		return err
	}
	if existing == nil {
		return fmt.Errorf("Codex 渠道不存在: %d", id)
	}
	if requireFailoverEnabled && !existing.FailoverEnabled {
		return fmt.Errorf("Codex 渠道 '%s' 已暂停，请先恢复渠道间故障转移", existing.Name)
	}
	return s.channelStore.SetActive(ctx, id)
}

// GetNextFailoverChannel 返回最高优先级且至少包含一个参与转移端点的候选渠道。
func (s *CodexChannelService) GetNextFailoverChannel(ctx context.Context, excludeID int64) (*store.CodexChannelRecord, error) {
	if s.endpointStore == nil {
		return nil, fmt.Errorf("Codex 端点存储未就绪")
	}
	next, _, err := selectCodexFailoverChannel(ctx, s.channelStore, s.endpointStore, excludeID)
	if err != nil {
		return nil, err
	}
	if next == nil {
		return nil, fmt.Errorf("没有可用于接管的 Codex 渠道")
	}
	return next, nil
}

func selectCodexFailoverChannel(ctx context.Context, channelStore store.CodexChannelStore, endpointStore store.CodexEndpointStore, excludeID int64) (*store.CodexChannelRecord, *store.CodexEndpointRecord, error) {
	channels, err := channelStore.List(ctx)
	if err != nil {
		return nil, nil, err
	}
	sort.SliceStable(channels, func(i, j int) bool {
		if channels[i].Priority != channels[j].Priority {
			return channels[i].Priority < channels[j].Priority
		}
		return channels[i].ID < channels[j].ID
	})
	for _, channel := range channels {
		if channel == nil || channel.ID == excludeID || !channel.FailoverEnabled {
			continue
		}
		endpoint, err := selectCodexFailoverEndpoint(ctx, endpointStore, channel.ID, 0)
		if err != nil {
			return nil, nil, err
		}
		if endpoint != nil {
			return channel, endpoint, nil
		}
	}
	return nil, nil, nil
}

func (s *CodexChannelService) SetFailoverEnabled(ctx context.Context, id int64, enabled bool) error {
	channel, err := s.channelStore.GetByID(ctx, id)
	if err != nil {
		return err
	}
	if channel == nil {
		return fmt.Errorf("Codex 渠道不存在: %d", id)
	}
	if !enabled && channel.IsActive {
		return fmt.Errorf("当前接管渠道不能暂停，请先切换到其他渠道")
	}
	if enabled {
		if err := s.validateUniquePriority(ctx, id, channel.Priority); err != nil {
			return err
		}
	}
	return s.channelStore.SetFailoverEnabled(ctx, id, enabled)
}

// EnsureActiveChannelParticipates 恢复被单渠道模式激活的暂停渠道，避免开启渠道间转移后状态冲突。
func (s *CodexChannelService) EnsureActiveChannelParticipates(ctx context.Context) (bool, error) {
	active, err := s.GetActiveChannel(ctx)
	if err != nil {
		return false, err
	}
	if active == nil || active.FailoverEnabled {
		return false, nil
	}
	// 启用全局渠道间转移时，当前接管渠道必须恢复参与，即使它与仍处于暂停状态的渠道暂时同优先级。
	// 普通用户恢复仍由 SetFailoverEnabled 校验优先级；这里不能复用该校验，否则会留下“全局开启但活跃渠道暂停”的矛盾状态。
	if err := s.channelStore.SetFailoverEnabled(ctx, active.ID, true); err != nil {
		return false, err
	}
	return true, nil
}

// SetActiveChannelParticipation updates the persisted participation flag for a
// known active channel during startup/hot-setting rollback. It intentionally
// bypasses the manual pause guard because the caller is restoring a consistent
// global failover state, not performing a user pause action.
func (s *CodexChannelService) SetActiveChannelParticipation(ctx context.Context, channelID int64, enabled bool) error {
	if s == nil || s.channelStore == nil || channelID <= 0 {
		return fmt.Errorf("Codex 渠道存储服务未启用")
	}
	active, err := s.channelStore.GetActive(ctx)
	if err != nil {
		return err
	}
	if active == nil || active.ID != channelID {
		return fmt.Errorf("Codex 当前激活渠道已变化，拒绝回滚参与状态")
	}
	// This method is only used by runtime rollback after the active-channel
	// identity has been checked. Bypass the manual pause guard so a failed
	// hot-reload can restore the exact persisted state.
	return s.channelStore.SetFailoverEnabled(ctx, channelID, enabled)
}

// EnsureActiveEndpoint 确保激活渠道下至少有一个激活端点。
// 若当前激活渠道下没有 is_active=1 的端点：
//   - 有端点：自动激活创建时间最早的那个
//   - 无端点：返回 nil endpoint（调用方需要决定如何处理 config.toml）
func (s *CodexChannelService) EnsureActiveEndpoint(ctx context.Context, channelID int64, requireFailoverEnabled bool) (*store.CodexEndpointRecord, error) {
	active, err := s.endpointStore.GetActiveByChannelID(ctx, channelID)
	if err != nil {
		return nil, err
	}
	if active != nil && (!requireFailoverEnabled || active.FailoverEnabled) {
		return active, nil
	}

	endpoints, err := s.endpointStore.ListByChannelID(ctx, channelID)
	if err != nil {
		return nil, err
	}
	for _, endpoint := range endpoints {
		if endpoint == nil || (requireFailoverEnabled && !endpoint.FailoverEnabled) {
			continue
		}
		if err := s.endpointStore.SetActive(ctx, endpoint.ID); err != nil {
			return nil, err
		}
		return s.endpointStore.GetByID(ctx, endpoint.ID)
	}
	return nil, nil
}

func normalizeAndValidateCodexChannel(record *store.CodexChannelRecord) error {
	if record == nil {
		return fmt.Errorf("record 不能为空")
	}

	record.Name = strings.TrimSpace(record.Name)
	record.ProviderType = strings.TrimSpace(record.ProviderType)
	record.ApiKey = strings.TrimSpace(record.ApiKey)
	record.Model = strings.TrimSpace(record.Model)
	record.ReasoningEffort = strings.TrimSpace(record.ReasoningEffort)
	record.Website = strings.TrimSpace(record.Website)
	record.ModelMapping = normalizeCodexModelMapping(record.ModelMapping)

	if record.Name == "" {
		return fmt.Errorf("渠道名不能为空")
	}
	if !codexChannelNamePattern.MatchString(record.Name) {
		return fmt.Errorf("渠道名必须以字母或数字开头，仅允许字母、数字、空格、连字符或下划线，长度 1-64")
	}
	if record.ProviderType == "" {
		record.ProviderType = "openai-compatible"
	}
	// ApiKey is now a compatibility fallback. New Codex credentials live in
	// codex_credential_groups, so channels can be saved without a standalone key.
	// 注：model / reasoning_effort / enable_1m_context 三个字段已迁移到 Codex 全局配置
	// （~/.codex/config.toml），由「Codex 配置」弹窗独立管理；渠道层不再校验也不强制填充
	// 默认值。数据库列允许为空，Codex 代理启用时同步逻辑会跳过 config.toml 写入。

	return nil
}

func normalizeCodexModelMapping(modelMapping map[string]string) map[string]string {
	if len(modelMapping) == 0 {
		return map[string]string{}
	}

	normalized := make(map[string]string, len(modelMapping))
	for source, target := range modelMapping {
		source = strings.TrimSpace(source)
		target = strings.TrimSpace(target)
		if source == "" || target == "" {
			continue
		}
		normalized[source] = target
	}
	return normalized
}
