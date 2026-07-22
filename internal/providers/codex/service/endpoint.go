package service

import (
	"context"
	"database/sql"
	"fmt"
	"net/url"
	"reflect"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"ai-relay-forwarder/internal/providers/codex/store"
	"ai-relay-forwarder/internal/tracking"
)

var codexEndpointNamePattern = regexp.MustCompile(`^[\p{L}\p{N}][\p{L}\p{N} _-]{0,63}$`)

// CodexEndpointService 管理 Codex 端点（name + base_url）。
type CodexEndpointService struct {
	endpointStore        store.CodexEndpointStore
	channelStore         store.CodexChannelStore
	credentialGroupStore store.CodexCredentialGroupStore
}

type codexEndpointTransactionStarter interface {
	BeginTx(context.Context) (*sql.Tx, error)
}

type codexEndpointActiveClearer interface {
	ClearActiveByChannel(context.Context, int64) error
}

type codexEndpointRecordRestorer interface {
	Restore(context.Context, *store.CodexEndpointRecord) error
}

var codexEndpointPriorityMu sync.Mutex

func NewCodexEndpointService(endpointStore store.CodexEndpointStore, channelStore store.CodexChannelStore, credentialGroupStore ...store.CodexCredentialGroupStore) *CodexEndpointService {
	var groupStore store.CodexCredentialGroupStore
	if len(credentialGroupStore) > 0 {
		groupStore = credentialGroupStore[0]
	}
	return &CodexEndpointService{endpointStore: endpointStore, channelStore: channelStore, credentialGroupStore: groupStore}
}

func (s *CodexEndpointService) ListByChannelID(ctx context.Context, channelID int64) ([]*store.CodexEndpointRecord, error) {
	return s.endpointStore.ListByChannelID(ctx, channelID)
}

func (s *CodexEndpointService) GetEndpointByID(ctx context.Context, id int64) (*store.CodexEndpointRecord, error) {
	return s.endpointStore.GetByID(ctx, id)
}

func (s *CodexEndpointService) GetActiveByChannelID(ctx context.Context, channelID int64) (*store.CodexEndpointRecord, error) {
	return s.endpointStore.GetActiveByChannelID(ctx, channelID)
}

// CreateEndpoint 在指定渠道下新建端点。若该渠道下没有激活端点，则自动激活新端点。
func (s *CodexEndpointService) CreateEndpoint(ctx context.Context, record *store.CodexEndpointRecord) (*store.CodexEndpointRecord, error) {
	if err := normalizeAndValidateCodexEndpoint(record); err != nil {
		return nil, err
	}

	channel, err := s.channelStore.GetByID(ctx, record.ChannelID)
	if err != nil {
		return nil, err
	}
	if channel == nil {
		return nil, fmt.Errorf("Codex 渠道不存在: %d", record.ChannelID)
	}
	if err := s.ensureEndpointCredentialGroup(ctx, record); err != nil {
		return nil, err
	}

	urlConflict, err := s.findEndpointURLConflict(ctx, record.ChannelID, record.CredentialGroupID, record.BaseURL)
	if err != nil {
		return nil, err
	}
	if urlConflict != nil {
		return nil, fmt.Errorf("该凭证分组下已存在相同的 base_url")
	}

	nameConflict, err := s.findEndpointNameConflict(ctx, record.ChannelID, record.CredentialGroupID, record.Name)
	if err != nil {
		return nil, err
	}
	if nameConflict != nil {
		return nil, fmt.Errorf("该凭证分组下已存在相同的端点名 '%s'", record.Name)
	}

	working := *record
	var created *store.CodexEndpointRecord
	err = s.withPriorityTransaction(ctx, func(endpointStore store.CodexEndpointStore) error {
		active, err := endpointStore.GetActiveByChannelID(ctx, working.ChannelID)
		if err != nil {
			return fmt.Errorf("查询 Codex 当前激活端点失败: %w", err)
		}
		working.IsActive = active == nil
		if working.Priority < 0 {
			maxPriority, err := endpointStore.GetMaxPriorityByChannelCredentialGroup(ctx, working.ChannelID, working.CredentialGroupID)
			if err != nil {
				return fmt.Errorf("获取 Codex 凭证分组最大优先级失败: %w", err)
			}
			working.Priority = maxPriority + 1
		}
		if working.Priority >= 0 {
			conflicting, err := endpointStore.GetByChannelCredentialGroupAndPriority(ctx, working.ChannelID, working.CredentialGroupID, working.Priority)
			if err != nil {
				return fmt.Errorf("检查 Codex 端点优先级冲突失败: %w", err)
			}
			if conflicting != nil {
				maxPriority, err := endpointStore.GetMaxPriorityByChannelCredentialGroup(ctx, working.ChannelID, working.CredentialGroupID)
				if err != nil {
					return fmt.Errorf("获取 Codex 凭证分组最大优先级失败: %w", err)
				}
				if err := updateCodexEndpointStoreRecord(ctx, endpointStore, conflicting, maxPriority+1); err != nil {
					return fmt.Errorf("Codex 端点优先级替换失败: %w", err)
				}
			}
		}
		created, err = endpointStore.Create(ctx, &working)
		if err != nil {
			return err
		}
		if !working.IsActive {
			return nil
		}
		if err := endpointStore.SetActive(ctx, created.ID); err != nil {
			return fmt.Errorf("设置 Codex 首个激活端点失败: %w", err)
		}
		created, err = endpointStore.GetByID(ctx, created.ID)
		return err
	})
	if err != nil {
		return nil, err
	}
	*record = working

	return created, nil
}

func updateCodexEndpointStoreRecord(ctx context.Context, endpointStore store.CodexEndpointStore, record *store.CodexEndpointRecord, priority int) error {
	return endpointStore.Update(
		ctx,
		record.ID,
		record.Name,
		record.BaseURL,
		record.CredentialGroupID,
		record.ApiKey,
		record.FailoverEnabled,
		record.SkipHealthCheck,
		priority,
		record.TimeoutSeconds,
		record.CostMultiplier,
		record.InputCostMultiplier,
		record.OutputCostMultiplier,
		record.CacheReadCostMultiplier,
		record.BillingOutputTokenSource,
		record.ModelMultipliers,
	)
}

func (s *CodexEndpointService) UpdateEndpointByID(ctx context.Context, id int64, name, baseURL string, credentialGroupID int64, apiKey string, failoverEnabled bool, skipHealthCheck bool, priority, timeoutSeconds int, costMultiplier, inputCostMultiplier, outputCostMultiplier, cacheReadCostMultiplier float64, billingOutputTokenSource string, modelMultipliers []store.CodexModelMultiplierRecord) error {
	name = strings.TrimSpace(name)
	baseURL = strings.TrimSpace(baseURL)
	apiKey = strings.TrimSpace(apiKey)
	billingOutputTokenSource = tracking.NormalizeBillingOutputTokenSource(billingOutputTokenSource)
	normalizedModelMultipliers, err := normalizeCodexModelMultipliers(modelMultipliers)
	if err != nil {
		return err
	}
	if err := validateCodexEndpointName(name); err != nil {
		return err
	}
	if err := validateBaseURL(baseURL); err != nil {
		return err
	}
	if err := validateCodexEndpointTimeoutSeconds(timeoutSeconds); err != nil {
		return err
	}

	existing, err := s.endpointStore.GetByID(ctx, id)
	if err != nil {
		return err
	}
	if existing == nil {
		return fmt.Errorf("Codex 端点不存在: %d", id)
	}
	if credentialGroupID <= 0 {
		credentialGroupID = existing.CredentialGroupID
	}
	if err := s.validateCredentialGroupWithOverride(ctx, existing.ChannelID, credentialGroupID, apiKey); err != nil {
		return err
	}
	groupChanged := existing.CredentialGroupID != credentialGroupID

	if !groupChanged && existing.Name == name && existing.BaseURL == baseURL && existing.CredentialGroupID == credentialGroupID && existing.ApiKey == apiKey && existing.FailoverEnabled == failoverEnabled && existing.SkipHealthCheck == skipHealthCheck && existing.Priority == priority && existing.TimeoutSeconds == timeoutSeconds && existing.CostMultiplier == costMultiplier && existing.InputCostMultiplier == inputCostMultiplier && existing.OutputCostMultiplier == outputCostMultiplier && existing.CacheReadCostMultiplier == cacheReadCostMultiplier && existing.BillingOutputTokenSource == billingOutputTokenSource && reflect.DeepEqual(existing.ModelMultipliers, normalizedModelMultipliers) {
		return nil
	}

	if existing.BaseURL != baseURL || existing.CredentialGroupID != credentialGroupID {
		urlConflict, err := s.findEndpointURLConflict(ctx, existing.ChannelID, credentialGroupID, baseURL)
		if err != nil {
			return err
		}
		if urlConflict != nil && urlConflict.ID != id {
			return fmt.Errorf("该凭证分组下已存在相同的 base_url")
		}
	}

	if existing.Name != name || existing.CredentialGroupID != credentialGroupID {
		nameConflict, err := s.findEndpointNameConflict(ctx, existing.ChannelID, credentialGroupID, name)
		if err != nil {
			return err
		}
		if nameConflict != nil && nameConflict.ID != id {
			return fmt.Errorf("该凭证分组下已存在相同的端点名 '%s'", name)
		}
	}

	switchedActiveEndpoint := false
	if existing.IsActive && !failoverEnabled {
		nextEndpoint, err := s.findNextFailoverEndpoint(ctx, existing.ChannelID, existing.ID)
		if err != nil {
			return err
		}
		if nextEndpoint == nil {
			return fmt.Errorf("当前激活端点关闭参与转移失败：没有其他参与故障转移的 Codex 端点")
		}
		if err := s.endpointStore.SetActive(ctx, nextEndpoint.ID); err != nil {
			return fmt.Errorf("切换 Codex 激活端点失败: %w", err)
		}
		switchedActiveEndpoint = true
	}

	if err := s.withPriorityTransaction(ctx, func(endpointStore store.CodexEndpointStore) error {
		if groupChanged {
			maxPriority, err := endpointStore.GetMaxPriorityByChannelCredentialGroup(ctx, existing.ChannelID, credentialGroupID)
			if err != nil {
				return fmt.Errorf("获取 Codex 目标凭证分组最大优先级失败: %w", err)
			}
			priority = maxPriority + 1
		}
		if !groupChanged && existing.Priority != priority {
			conflicting, err := endpointStore.GetByChannelCredentialGroupAndPriority(ctx, existing.ChannelID, credentialGroupID, priority)
			if err != nil {
				return fmt.Errorf("检查 Codex 端点优先级冲突失败: %w", err)
			}
			if conflicting != nil && conflicting.ID != id {
				if err := updateCodexEndpointStoreRecord(ctx, endpointStore, conflicting, existing.Priority); err != nil {
					return fmt.Errorf("Codex 端点优先级对调失败: %w", err)
				}
			}
		}

		return endpointStore.Update(ctx, id, name, baseURL, credentialGroupID, apiKey, failoverEnabled, skipHealthCheck, priority, timeoutSeconds, costMultiplier, inputCostMultiplier, outputCostMultiplier, cacheReadCostMultiplier, billingOutputTokenSource, normalizedModelMultipliers)
	}); err != nil {
		if switchedActiveEndpoint {
			rollbackCtx, rollbackCancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer rollbackCancel()
			if restoreErr := s.endpointStore.SetActive(rollbackCtx, existing.ID); restoreErr != nil {
				return fmt.Errorf("%w；恢复原激活端点失败: %v", err, restoreErr)
			}
		}
		return err
	}
	return nil
}

// RestoreEndpoint restores a previously persisted endpoint snapshot exactly.
// It is intentionally separate from UpdateEndpointByID: rollback must preserve
// the original credential group and priority instead of applying edit-time
// append/swap semantics again.
func (s *CodexEndpointService) RestoreEndpoint(ctx context.Context, record *store.CodexEndpointRecord) error {
	if s == nil || s.endpointStore == nil {
		return fmt.Errorf("Codex 端点存储服务未启用")
	}
	if record == nil || record.ID <= 0 {
		return fmt.Errorf("恢复 Codex 端点记录无效")
	}

	return s.withPriorityTransaction(ctx, func(endpointStore store.CodexEndpointStore) error {
		current, err := endpointStore.GetByID(ctx, record.ID)
		if err != nil {
			return fmt.Errorf("读取待恢复 Codex 端点失败: %w", err)
		}
		if current == nil {
			return fmt.Errorf("待恢复 Codex 端点不存在: %d", record.ID)
		}

		conflicting, err := endpointStore.GetByChannelCredentialGroupAndPriority(ctx, record.ChannelID, record.CredentialGroupID, record.Priority)
		if err != nil {
			return fmt.Errorf("检查恢复后的 Codex 端点优先级失败: %w", err)
		}
		if conflicting != nil && conflicting.ID != record.ID {
			maxPriority, maxErr := endpointStore.GetMaxPriorityByChannelCredentialGroup(ctx, record.ChannelID, record.CredentialGroupID)
			if maxErr != nil {
				return fmt.Errorf("为恢复优先级腾挪 Codex 端点失败: %w", maxErr)
			}
			if err := updateCodexEndpointStoreRecord(ctx, endpointStore, conflicting, maxPriority+1); err != nil {
				return fmt.Errorf("为恢复优先级腾挪 Codex 冲突端点失败: %w", err)
			}
		}

		if err := endpointStore.Update(ctx, record.ID, record.Name, record.BaseURL, record.CredentialGroupID, record.ApiKey, record.FailoverEnabled, record.SkipHealthCheck, record.Priority, record.TimeoutSeconds, record.CostMultiplier, record.InputCostMultiplier, record.OutputCostMultiplier, record.CacheReadCostMultiplier, record.BillingOutputTokenSource, record.ModelMultipliers); err != nil {
			return fmt.Errorf("恢复 Codex 端点记录失败: %w", err)
		}
		if record.IsActive {
			if err := endpointStore.SetActive(ctx, record.ID); err != nil {
				return fmt.Errorf("恢复 Codex 激活端点失败: %w", err)
			}
		}
		return nil
	})
}

func (s *CodexEndpointService) findNextFailoverEndpoint(ctx context.Context, channelID int64, currentID int64) (*store.CodexEndpointRecord, error) {
	if s == nil || s.endpointStore == nil {
		return nil, fmt.Errorf("Codex 端点存储服务未启用")
	}
	return selectCodexFailoverEndpoint(ctx, s.endpointStore, channelID, currentID)
}

func selectCodexFailoverEndpoint(ctx context.Context, endpointStore store.CodexEndpointStore, channelID, currentID int64) (*store.CodexEndpointRecord, error) {
	candidates, err := endpointStore.ListByChannelID(ctx, channelID)
	if err != nil {
		return nil, fmt.Errorf("获取 Codex 端点列表失败: %w", err)
	}
	filtered := make([]*store.CodexEndpointRecord, 0, len(candidates))
	for _, candidate := range candidates {
		if candidate == nil || candidate.ID == currentID || !candidate.FailoverEnabled {
			continue
		}
		filtered = append(filtered, candidate)
	}
	if len(filtered) == 0 {
		return nil, nil
	}
	sort.SliceStable(filtered, func(i, j int) bool {
		if filtered[i].Priority != filtered[j].Priority {
			return filtered[i].Priority < filtered[j].Priority
		}
		if !filtered[i].CreatedAt.Equal(filtered[j].CreatedAt) {
			return filtered[i].CreatedAt.Before(filtered[j].CreatedAt)
		}
		return filtered[i].ID < filtered[j].ID
	})
	return filtered[0], nil
}

// GetEndpointDeletionSuccessor returns the same eligible, priority-ordered
// endpoint that DeleteEndpointByID will activate for an active target.
func (s *CodexEndpointService) GetEndpointDeletionSuccessor(ctx context.Context, id int64) (*store.CodexEndpointRecord, error) {
	if s == nil || s.endpointStore == nil {
		return nil, fmt.Errorf("Codex 端点存储服务未启用")
	}
	existing, err := s.endpointStore.GetByID(ctx, id)
	if err != nil {
		return nil, err
	}
	if existing == nil {
		return nil, fmt.Errorf("Codex 端点不存在: %d", id)
	}
	if !existing.IsActive {
		return nil, nil
	}
	return selectCodexFailoverEndpoint(ctx, s.endpointStore, existing.ChannelID, existing.ID)
}

func (s *CodexEndpointService) withPriorityTransaction(ctx context.Context, fn func(store.CodexEndpointStore) error) error {
	if s == nil || s.endpointStore == nil {
		return fmt.Errorf("Codex 端点存储服务未启用")
	}
	codexEndpointPriorityMu.Lock()
	defer codexEndpointPriorityMu.Unlock()

	starter, ok := s.endpointStore.(codexEndpointTransactionStarter)
	if !ok {
		return fn(s.endpointStore)
	}
	tx, err := starter.BeginTx(ctx)
	if err != nil {
		return fmt.Errorf("开启 Codex 端点优先级事务失败: %w", err)
	}
	txStore := s.endpointStore.WithTx(tx)
	if err := fn(txStore); err != nil {
		_ = tx.Rollback()
		return err
	}
	if err := tx.Commit(); err != nil {
		_ = tx.Rollback()
		return fmt.Errorf("提交 Codex 端点优先级事务失败: %w", err)
	}
	return nil
}

// PrepareEndpointDeletion makes an endpoint inert before the runtime reload.
// The expected successor keeps API pre-sync and persisted activation on the
// same eligible, priority-ordered candidate.
func (s *CodexEndpointService) PrepareEndpointDeletion(ctx context.Context, id, expectedSuccessorID int64) error {
	return s.withPriorityTransaction(ctx, func(endpointStore store.CodexEndpointStore) error {
		existing, err := endpointStore.GetByID(ctx, id)
		if err != nil {
			return err
		}
		if existing == nil {
			return fmt.Errorf("Codex 端点不存在: %d", id)
		}
		if existing.IsActive {
			next, err := selectCodexFailoverEndpoint(ctx, endpointStore, existing.ChannelID, existing.ID)
			if err != nil {
				return err
			}
			if endpointRecordID(next) != expectedSuccessorID {
				return fmt.Errorf("Codex 删除端点候选已变化，请重试")
			}
			if next != nil {
				if err := endpointStore.SetActive(ctx, next.ID); err != nil {
					return fmt.Errorf("切换 Codex 后续激活端点失败: %w", err)
				}
			} else if err := clearCodexActiveEndpoints(ctx, endpointStore, existing.ChannelID); err != nil {
				return err
			}
		} else if expectedSuccessorID != 0 {
			return fmt.Errorf("Codex 删除端点候选已变化，请重试")
		}

		staged := *existing
		staged.FailoverEnabled = false
		return updateCodexEndpointStoreRecord(ctx, endpointStore, &staged, staged.Priority)
	})
}

// RestoreDeletedEndpoint recreates a deleted snapshot, including its active
// marker, inside one transaction.
func (s *CodexEndpointService) RestoreDeletedEndpoint(ctx context.Context, record *store.CodexEndpointRecord) error {
	if record == nil || record.ID <= 0 {
		return fmt.Errorf("恢复已删除 Codex 端点记录无效")
	}
	return s.withPriorityTransaction(ctx, func(endpointStore store.CodexEndpointStore) error {
		restorer, ok := endpointStore.(codexEndpointRecordRestorer)
		if !ok {
			return fmt.Errorf("Codex 端点存储不支持恢复已删除记录")
		}
		copy := *record
		copy.IsActive = false
		if err := restorer.Restore(ctx, &copy); err != nil {
			return err
		}
		if record.IsActive {
			if err := endpointStore.SetActive(ctx, record.ID); err != nil {
				return fmt.Errorf("恢复 Codex 激活端点失败: %w", err)
			}
		}
		return nil
	})
}

func (s *CodexEndpointService) ClearActiveByChannel(ctx context.Context, channelID int64) error {
	if s == nil || s.endpointStore == nil {
		return fmt.Errorf("Codex 端点存储服务未启用")
	}
	return clearCodexActiveEndpoints(ctx, s.endpointStore, channelID)
}

func clearCodexActiveEndpoints(ctx context.Context, endpointStore store.CodexEndpointStore, channelID int64) error {
	clearer, ok := endpointStore.(codexEndpointActiveClearer)
	if !ok {
		return fmt.Errorf("Codex 端点存储不支持清空激活端点")
	}
	return clearer.ClearActiveByChannel(ctx, channelID)
}

func endpointRecordID(record *store.CodexEndpointRecord) int64 {
	if record == nil {
		return 0
	}
	return record.ID
}

// DeleteEndpoint 删除端点。若被删除的是当前激活端点，在同一事务中按
// failover_enabled + priority 选择并激活后续端点。
func (s *CodexEndpointService) DeleteEndpointByID(ctx context.Context, id int64) error {
	return s.withPriorityTransaction(ctx, func(endpointStore store.CodexEndpointStore) error {
		existing, err := endpointStore.GetByID(ctx, id)
		if err != nil {
			return err
		}
		if existing == nil {
			return fmt.Errorf("Codex 端点不存在: %d", id)
		}
		var switched bool
		if existing.IsActive {
			next, err := selectCodexFailoverEndpoint(ctx, endpointStore, existing.ChannelID, existing.ID)
			if err != nil {
				return err
			}
			if next != nil {
				if err := endpointStore.SetActive(ctx, next.ID); err != nil {
					return fmt.Errorf("切换 Codex 后续激活端点失败: %w", err)
				}
				switched = true
			}
		}
		if err := endpointStore.DeleteByID(ctx, id); err != nil {
			if switched {
				rollbackCtx, rollbackCancel := context.WithTimeout(context.Background(), 5*time.Second)
				defer rollbackCancel()
				if restoreErr := endpointStore.SetActive(rollbackCtx, existing.ID); restoreErr != nil {
					return fmt.Errorf("%w；恢复原激活端点失败: %v", err, restoreErr)
				}
			}
			return err
		}
		return nil
	})
}

func (s *CodexEndpointService) ActivateEndpoint(ctx context.Context, id int64) error {
	existing, err := s.endpointStore.GetByID(ctx, id)
	if err != nil {
		return err
	}
	if existing == nil {
		return fmt.Errorf("Codex 端点不存在: %d", id)
	}
	if !existing.FailoverEnabled {
		return fmt.Errorf("Codex 端点未参与故障转移，不能设为当前端点")
	}
	return s.endpointStore.SetActive(ctx, id)
}

func normalizeAndValidateCodexEndpoint(record *store.CodexEndpointRecord) error {
	if record == nil {
		return fmt.Errorf("record 不能为空")
	}
	if record.ChannelID <= 0 {
		return fmt.Errorf("channel_id 无效")
	}
	record.Name = strings.TrimSpace(record.Name)
	record.BaseURL = strings.TrimSpace(record.BaseURL)
	record.ApiKey = strings.TrimSpace(record.ApiKey)
	record.BillingOutputTokenSource = tracking.NormalizeBillingOutputTokenSource(record.BillingOutputTokenSource)
	modelMultipliers, err := normalizeCodexModelMultipliers(record.ModelMultipliers)
	if err != nil {
		return err
	}
	record.ModelMultipliers = modelMultipliers
	if err := validateCodexEndpointName(record.Name); err != nil {
		return err
	}
	if err := validateCodexEndpointTimeoutSeconds(record.TimeoutSeconds); err != nil {
		return err
	}
	return validateBaseURL(record.BaseURL)
}

func (s *CodexEndpointService) ensureEndpointCredentialGroup(ctx context.Context, record *store.CodexEndpointRecord) error {
	if s.credentialGroupStore == nil {
		return nil
	}
	if record.CredentialGroupID > 0 {
		group, err := s.credentialGroupStore.GetByID(ctx, record.CredentialGroupID)
		if err != nil {
			return err
		}
		if group == nil {
			return fmt.Errorf("Codex 凭证分组不存在: %d", record.CredentialGroupID)
		}
		if group.ChannelID != record.ChannelID {
			return fmt.Errorf("凭证分组不属于当前渠道")
		}
		if !s.codexCredentialAvailable(ctx, record.ChannelID, record.ApiKey, group.ApiKey) {
			return fmt.Errorf("Codex 凭证分组 '%s' 尚未配置 API Key，不能创建端点", group.Name)
		}
		return nil
	}
	group, err := s.credentialGroupStore.GetDefaultByChannelID(ctx, record.ChannelID)
	if err != nil {
		return err
	}
	if group == nil {
		return fmt.Errorf("Codex 渠道 %d 尚未配置凭证分组，不能创建端点", record.ChannelID)
	}
	if strings.TrimSpace(group.ApiKey) == "" {
		return fmt.Errorf("Codex 凭证分组 '%s' 尚未配置 API Key，不能创建端点", group.Name)
	}
	record.CredentialGroupID = group.ID
	return nil
}

func (s *CodexEndpointService) validateCredentialGroup(ctx context.Context, channelID int64, credentialGroupID int64) error {
	if s.credentialGroupStore == nil || credentialGroupID <= 0 {
		return nil
	}
	group, err := s.credentialGroupStore.GetByID(ctx, credentialGroupID)
	if err != nil {
		return err
	}
	if group == nil {
		return fmt.Errorf("Codex 凭证分组不存在: %d", credentialGroupID)
	}
	if group.ChannelID != channelID {
		return fmt.Errorf("凭证分组不属于当前渠道")
	}
	if !s.codexCredentialAvailable(ctx, channelID, "", group.ApiKey) {
		return fmt.Errorf("Codex 凭证分组 '%s' 尚未配置 API Key，不能使用该分组创建端点", group.Name)
	}
	return nil
}

func (s *CodexEndpointService) validateCredentialGroupWithOverride(ctx context.Context, channelID, credentialGroupID int64, endpointApiKey string) error {
	if s.credentialGroupStore == nil || credentialGroupID <= 0 {
		return nil
	}
	group, err := s.credentialGroupStore.GetByID(ctx, credentialGroupID)
	if err != nil {
		return err
	}
	if group == nil {
		return fmt.Errorf("Codex 凭证分组不存在: %d", credentialGroupID)
	}
	if group.ChannelID != channelID {
		return fmt.Errorf("凭证分组不属于当前渠道")
	}
	if !s.codexCredentialAvailable(ctx, channelID, endpointApiKey, group.ApiKey) {
		return fmt.Errorf("Codex 凭证分组 '%s' 尚未配置 API Key，不能使用该分组创建端点", group.Name)
	}
	return nil
}

func (s *CodexEndpointService) codexCredentialAvailable(ctx context.Context, channelID int64, endpointApiKey, groupApiKey string) bool {
	if strings.TrimSpace(endpointApiKey) != "" || strings.TrimSpace(groupApiKey) != "" {
		return true
	}
	if s.channelStore == nil {
		return false
	}
	channel, err := s.channelStore.GetByID(ctx, channelID)
	return err == nil && channel != nil && strings.TrimSpace(channel.ApiKey) != ""
}

func (s *CodexEndpointService) findEndpointURLConflict(ctx context.Context, channelID, credentialGroupID int64, baseURL string) (*store.CodexEndpointRecord, error) {
	conflict, err := s.endpointStore.GetByChannelCredentialGroupAndURL(ctx, channelID, credentialGroupID, baseURL)
	if err != nil || conflict != nil {
		return conflict, err
	}
	if !s.isDefaultCredentialGroup(ctx, channelID, credentialGroupID) {
		return nil, nil
	}
	return s.endpointStore.GetByChannelCredentialGroupAndURL(ctx, channelID, 0, baseURL)
}

func (s *CodexEndpointService) findEndpointNameConflict(ctx context.Context, channelID, credentialGroupID int64, name string) (*store.CodexEndpointRecord, error) {
	conflict, err := s.endpointStore.GetByChannelCredentialGroupAndName(ctx, channelID, credentialGroupID, name)
	if err != nil || conflict != nil {
		return conflict, err
	}
	if !s.isDefaultCredentialGroup(ctx, channelID, credentialGroupID) {
		return nil, nil
	}
	return s.endpointStore.GetByChannelCredentialGroupAndName(ctx, channelID, 0, name)
}

func (s *CodexEndpointService) isDefaultCredentialGroup(ctx context.Context, channelID, credentialGroupID int64) bool {
	if s.credentialGroupStore == nil || credentialGroupID <= 0 {
		return false
	}
	group, err := s.credentialGroupStore.GetDefaultByChannelID(ctx, channelID)
	return err == nil && group != nil && group.ID == credentialGroupID
}

func normalizeCodexModelMultipliers(rules []store.CodexModelMultiplierRecord) ([]store.CodexModelMultiplierRecord, error) {
	if len(rules) == 0 {
		return nil, nil
	}

	seen := make(map[string]struct{}, len(rules))
	normalized := make([]store.CodexModelMultiplierRecord, 0, len(rules))
	for _, rule := range rules {
		model := strings.TrimSpace(rule.Model)
		if model == "" {
			continue
		}
		if _, exists := seen[model]; exists {
			return nil, fmt.Errorf("模型附加倍率重复: %s", model)
		}
		seen[model] = struct{}{}
		normalized = append(normalized, store.CodexModelMultiplierRecord{
			Model:                   model,
			CostMultiplier:          defaultCodexMultiplier(rule.CostMultiplier),
			InputCostMultiplier:     defaultCodexMultiplier(rule.InputCostMultiplier),
			OutputCostMultiplier:    defaultCodexMultiplier(rule.OutputCostMultiplier),
			CacheReadCostMultiplier: defaultCodexMultiplier(rule.CacheReadCostMultiplier),
		})
	}
	return normalized, nil
}

func defaultCodexMultiplier(value float64) float64 {
	if value <= 0 {
		return 1.0
	}
	return value
}

func validateCodexEndpointName(name string) error {
	if name == "" {
		return fmt.Errorf("端点名不能为空")
	}
	if !codexEndpointNamePattern.MatchString(name) {
		return fmt.Errorf("端点名必须以字母或数字开头，仅允许字母、数字、空格、连字符或下划线，长度 1-64")
	}
	return nil
}

func validateBaseURL(baseURL string) error {
	if baseURL == "" {
		return fmt.Errorf("base_url 不能为空")
	}
	parsed, err := url.Parse(baseURL)
	if err != nil {
		return fmt.Errorf("base_url 格式无效: %w", err)
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return fmt.Errorf("base_url 必须以 http:// 或 https:// 开头")
	}
	if parsed.Host == "" {
		return fmt.Errorf("base_url 缺少 host")
	}
	return nil
}

func validateCodexEndpointTimeoutSeconds(timeoutSeconds int) error {
	if timeoutSeconds < 0 {
		return fmt.Errorf("timeout_seconds 不能为负数")
	}
	return nil
}
