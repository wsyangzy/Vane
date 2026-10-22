package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/wsyangzy/Vane/internal/providers/claude/proxy/subscription"
)

const (
	ClaudeModelPolicyInherit     = "inherit"
	ClaudeModelPolicyOverride    = "override"
	ClaudeModelPolicyPassthrough = "passthrough"
)

// ClaudeCredentialGroupRecord 表示 Claude 渠道下可复用的凭证分组。
// 分组承载上游 Token/API Key 与基础成本倍率；端点可保留自己的覆盖值。
type ClaudeCredentialGroupRecord struct {
	ID           int64                 `json:"id"`
	Channel      string                `json:"channel"`
	Name         string                `json:"name"`
	Token        string                `json:"token,omitempty"`
	ApiKey       string                `json:"api_key,omitempty"`
	AuthMode     string                `json:"auth_mode"`
	Subscription *subscription.Binding `json:"-"`

	Enabled           bool              `json:"enabled"`
	IsDefault         bool              `json:"is_default"`
	DailyCostLimitUSD float64           `json:"daily_cost_limit_usd"`
	ModelPolicy       string            `json:"model_policy"`
	ModelMapping      map[string]string `json:"model_mapping"`
	SupportedModels   []string          `json:"supported_models"`

	CostMultiplier                         float64                         `json:"cost_multiplier"`
	InputCostMultiplier                    float64                         `json:"input_cost_multiplier"`
	OutputCostMultiplier                   float64                         `json:"output_cost_multiplier"`
	CacheCreationCostMultiplier            float64                         `json:"cache_creation_cost_multiplier"`
	CacheCreationCostMultiplier1h          float64                         `json:"cache_creation_cost_multiplier_1h"`
	CacheReadCostMultiplier                float64                         `json:"cache_read_cost_multiplier"`
	LongContextCostMultiplier              float64                         `json:"long_context_cost_multiplier"`
	LongContextInputCostMultiplier         float64                         `json:"long_context_input_cost_multiplier"`
	LongContextOutputCostMultiplier        float64                         `json:"long_context_output_cost_multiplier"`
	LongContextCacheCreationCostMultiplier float64                         `json:"long_context_cache_creation_cost_multiplier"`
	LongContextCacheReadCostMultiplier     float64                         `json:"long_context_cache_read_cost_multiplier"`
	DisableLongContextPricing              bool                            `json:"disable_long_context_pricing"`
	DisableServiceTierPricing              bool                            `json:"disable_service_tier_pricing"`
	ServiceTierCostMultiplier              float64                         `json:"service_tier_cost_multiplier"`
	ServiceTierInputCostMultiplier         float64                         `json:"service_tier_input_cost_multiplier"`
	ServiceTierOutputCostMultiplier        float64                         `json:"service_tier_output_cost_multiplier"`
	ServiceTierCacheReadCostMultiplier     float64                         `json:"service_tier_cache_read_cost_multiplier"`
	ModelMultipliers                       []EndpointModelMultiplierRecord `json:"model_multipliers"`

	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// ClaudeCredentialGroupStore 定义 Claude 凭证分组存储接口。
type ClaudeCredentialGroupStore interface {
	Create(ctx context.Context, record *ClaudeCredentialGroupRecord) (*ClaudeCredentialGroupRecord, error)
	GetByID(ctx context.Context, id int64) (*ClaudeCredentialGroupRecord, error)
	GetDefaultByChannel(ctx context.Context, channel string) (*ClaudeCredentialGroupRecord, error)
	GetByChannelAndName(ctx context.Context, channel, name string) (*ClaudeCredentialGroupRecord, error)
	ListByChannel(ctx context.Context, channel string) ([]*ClaudeCredentialGroupRecord, error)
	Update(ctx context.Context, record *ClaudeCredentialGroupRecord) error
	DeleteByID(ctx context.Context, id int64) error
	DeleteByChannel(ctx context.Context, channel string) error
	CountEndpoints(ctx context.Context, id int64) (int, error)
	SetDefault(ctx context.Context, id int64) error

	WithTx(tx *sql.Tx) ClaudeCredentialGroupStore
}

// SQLiteClaudeCredentialGroupStore 实现 ClaudeCredentialGroupStore。
type SQLiteClaudeCredentialGroupStore struct {
	db *sql.DB
	tx *sql.Tx
	mu sync.RWMutex
}

func NewSQLiteClaudeCredentialGroupStore(db *sql.DB) *SQLiteClaudeCredentialGroupStore {
	return &SQLiteClaudeCredentialGroupStore{db: db}
}

func (s *SQLiteClaudeCredentialGroupStore) WithTx(tx *sql.Tx) ClaudeCredentialGroupStore {
	return &SQLiteClaudeCredentialGroupStore{db: s.db, tx: tx}
}

func (s *SQLiteClaudeCredentialGroupStore) getQuerier() interface {
	QueryContext(ctx context.Context, query string, args ...interface{}) (*sql.Rows, error)
	QueryRowContext(ctx context.Context, query string, args ...interface{}) *sql.Row
	ExecContext(ctx context.Context, query string, args ...interface{}) (sql.Result, error)
} {
	if s.tx != nil {
		return s.tx
	}
	return s.db
}

const claudeCredentialGroupColumns = `id, channel, name, COALESCE(token, ''), COALESCE(api_key, ''), COALESCE(auth_mode, 'group_credential'), COALESCE(subscription_config, '{}'), COALESCE(is_default, 0), enabled,
	COALESCE(daily_cost_limit_usd, 0),
	COALESCE(model_policy, 'inherit'), COALESCE(model_mapping, '{}'), COALESCE(supported_models, '[]'),
	COALESCE(cost_multiplier, 1.0), COALESCE(input_cost_multiplier, 1.0),
	COALESCE(output_cost_multiplier, 1.0), COALESCE(cache_creation_cost_multiplier, 1.0),
	COALESCE(cache_creation_cost_multiplier_1h, 1.0), COALESCE(cache_read_cost_multiplier, 1.0),
	COALESCE(long_context_cost_multiplier, 1.0),
	COALESCE(long_context_input_cost_multiplier, 1.0), COALESCE(long_context_output_cost_multiplier, 1.0),
	COALESCE(long_context_cache_creation_cost_multiplier, 1.0), COALESCE(long_context_cache_read_cost_multiplier, 1.0),
	COALESCE(disable_long_context_pricing, 0),
	COALESCE(disable_service_tier_pricing, 1),
	COALESCE(service_tier_cost_multiplier, 1.0),
	COALESCE(service_tier_input_cost_multiplier, 1.0),
	COALESCE(service_tier_output_cost_multiplier, 1.0),
	COALESCE(service_tier_cache_read_cost_multiplier, 1.0),
	COALESCE(model_multipliers, '[]'),
	COALESCE(created_at, ''), COALESCE(updated_at, '')`

func (s *SQLiteClaudeCredentialGroupStore) Create(ctx context.Context, record *ClaudeCredentialGroupRecord) (*ClaudeCredentialGroupRecord, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if record == nil {
		return nil, fmt.Errorf("record 不能为空")
	}
	if record.Channel == "" {
		return nil, fmt.Errorf("渠道不能为空")
	}
	if record.Name == "" {
		return nil, fmt.Errorf("凭证分组名不能为空")
	}

	if record.IsDefault {
		if _, err := s.getQuerier().ExecContext(ctx, `UPDATE claude_credential_groups SET is_default = 0 WHERE channel = ?`, record.Channel); err != nil {
			return nil, fmt.Errorf("清空默认凭证分组失败: %w", err)
		}
	}

	result, err := s.getQuerier().ExecContext(ctx, `
		INSERT INTO claude_credential_groups (
			channel, name, token, api_key, is_default, daily_cost_limit_usd, cost_multiplier,
			input_cost_multiplier, output_cost_multiplier, model_policy, model_mapping, supported_models,
			cache_creation_cost_multiplier, cache_creation_cost_multiplier_1h,
			cache_read_cost_multiplier, long_context_cost_multiplier,
			long_context_input_cost_multiplier, long_context_output_cost_multiplier,
			long_context_cache_creation_cost_multiplier, long_context_cache_read_cost_multiplier,
			disable_long_context_pricing, disable_service_tier_pricing,
			service_tier_cost_multiplier, service_tier_input_cost_multiplier, service_tier_output_cost_multiplier, service_tier_cache_read_cost_multiplier,
			model_multipliers, auth_mode, subscription_config
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, record.Channel, record.Name, record.Token, record.ApiKey, boolToInt(record.IsDefault), record.DailyCostLimitUSD,
		defaultClaudeGroupStoreMultiplier(record.CostMultiplier),
		defaultClaudeGroupStoreMultiplier(record.InputCostMultiplier),
		defaultClaudeGroupStoreMultiplier(record.OutputCostMultiplier),
		defaultClaudeModelPolicy(record.ModelPolicy), encodeClaudeModelMapping(record.ModelMapping), encodeClaudeSupportedModels(record.SupportedModels),
		defaultClaudeGroupStoreMultiplier(record.CacheCreationCostMultiplier),
		defaultClaudeGroupStoreMultiplier(record.CacheCreationCostMultiplier1h),
		defaultClaudeGroupStoreMultiplier(record.CacheReadCostMultiplier),
		defaultClaudeGroupStoreMultiplier(record.LongContextCostMultiplier),
		defaultClaudeGroupStoreMultiplier(record.LongContextInputCostMultiplier),
		defaultClaudeGroupStoreMultiplier(record.LongContextOutputCostMultiplier),
		defaultClaudeGroupStoreMultiplier(record.LongContextCacheCreationCostMultiplier),
		defaultClaudeGroupStoreMultiplier(record.LongContextCacheReadCostMultiplier),
		boolToInt(record.DisableLongContextPricing),
		boolToInt(record.DisableServiceTierPricing),
		record.ServiceTierCostMultiplier,
		record.ServiceTierInputCostMultiplier,
		record.ServiceTierOutputCostMultiplier,
		record.ServiceTierCacheReadCostMultiplier,
		encodeEndpointModelMultipliers(record.ModelMultipliers), defaultAuthMode(record.AuthMode), encodeSubscription(record.Subscription))
	if err != nil {
		return nil, fmt.Errorf("创建 Claude 凭证分组失败: %w", err)
	}

	id, err := result.LastInsertId()
	if err != nil {
		return nil, fmt.Errorf("获取插入 ID 失败: %w", err)
	}
	query := "SELECT " + claudeCredentialGroupColumns + " FROM claude_credential_groups WHERE id = ?"
	return scanClaudeCredentialGroupRow(s.getQuerier().QueryRowContext(ctx, query, id))
}

func (s *SQLiteClaudeCredentialGroupStore) GetByID(ctx context.Context, id int64) (*ClaudeCredentialGroupRecord, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	query := "SELECT " + claudeCredentialGroupColumns + " FROM claude_credential_groups WHERE id = ?"
	return scanClaudeCredentialGroupRow(s.getQuerier().QueryRowContext(ctx, query, id))
}

func (s *SQLiteClaudeCredentialGroupStore) GetDefaultByChannel(ctx context.Context, channel string) (*ClaudeCredentialGroupRecord, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	query := "SELECT " + claudeCredentialGroupColumns + " FROM claude_credential_groups WHERE channel = ? AND is_default = 1 LIMIT 1"
	return scanClaudeCredentialGroupRow(s.getQuerier().QueryRowContext(ctx, query, channel))
}

func (s *SQLiteClaudeCredentialGroupStore) GetByChannelAndName(ctx context.Context, channel, name string) (*ClaudeCredentialGroupRecord, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	query := "SELECT " + claudeCredentialGroupColumns + " FROM claude_credential_groups WHERE channel = ? AND name = ?"
	return scanClaudeCredentialGroupRow(s.getQuerier().QueryRowContext(ctx, query, channel, name))
}

func (s *SQLiteClaudeCredentialGroupStore) ListByChannel(ctx context.Context, channel string) ([]*ClaudeCredentialGroupRecord, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	query := "SELECT " + claudeCredentialGroupColumns + " FROM claude_credential_groups WHERE channel = ? ORDER BY is_default DESC, created_at ASC, id ASC"
	rows, err := s.getQuerier().QueryContext(ctx, query, channel)
	if err != nil {
		return nil, fmt.Errorf("获取 Claude 凭证分组列表失败: %w", err)
	}
	defer rows.Close()

	result := make([]*ClaudeCredentialGroupRecord, 0)
	for rows.Next() {
		record, err := scanClaudeCredentialGroupRow(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, record)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("读取 Claude 凭证分组列表失败: %w", err)
	}
	return result, nil
}

func (s *SQLiteClaudeCredentialGroupStore) Update(ctx context.Context, record *ClaudeCredentialGroupRecord) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.tx == nil {
		tx, err := s.db.BeginTx(ctx, nil)
		if err != nil {
			return err
		}
		defer tx.Rollback()
		if err := s.WithTx(tx).Update(ctx, record); err != nil {
			return err
		}
		return tx.Commit()
	}

	if record == nil {
		return fmt.Errorf("record 不能为空")
	}
	if record.ID <= 0 {
		return fmt.Errorf("凭证分组 ID 无效")
	}
	if record.Channel == "" {
		return fmt.Errorf("渠道不能为空")
	}
	if record.Name == "" {
		return fmt.Errorf("凭证分组名不能为空")
	}

	previous, err := scanClaudeCredentialGroupRow(s.getQuerier().QueryRowContext(ctx, "SELECT "+claudeCredentialGroupColumns+" FROM claude_credential_groups WHERE id=?", record.ID))
	if err != nil {
		return err
	}
	if record.IsDefault {
		if _, err := s.getQuerier().ExecContext(ctx, `UPDATE claude_credential_groups SET is_default = 0 WHERE channel = ? AND id <> ?`, record.Channel, record.ID); err != nil {
			return fmt.Errorf("清空默认凭证分组失败: %w", err)
		}
	}

	result, err := s.getQuerier().ExecContext(ctx, `
		UPDATE claude_credential_groups
		SET name = ?, token = ?, api_key = ?, is_default = ?, daily_cost_limit_usd = ?, cost_multiplier = ?,
			input_cost_multiplier = ?, output_cost_multiplier = ?, model_policy = ?, model_mapping = ?, supported_models = ?,
			cache_creation_cost_multiplier = ?, cache_creation_cost_multiplier_1h = ?,
			cache_read_cost_multiplier = ?, long_context_cost_multiplier = ?,
			long_context_input_cost_multiplier = ?, long_context_output_cost_multiplier = ?,
			long_context_cache_creation_cost_multiplier = ?, long_context_cache_read_cost_multiplier = ?,
			disable_long_context_pricing = ?, disable_service_tier_pricing = ?,
			service_tier_cost_multiplier = ?, service_tier_input_cost_multiplier = ?, service_tier_output_cost_multiplier = ?, service_tier_cache_read_cost_multiplier = ?,
			model_multipliers = ?, auth_mode = ?, subscription_config = ?
		WHERE id = ?
	`, record.Name, record.Token, record.ApiKey, boolToInt(record.IsDefault), record.DailyCostLimitUSD,
		defaultClaudeGroupStoreMultiplier(record.CostMultiplier),
		defaultClaudeGroupStoreMultiplier(record.InputCostMultiplier),
		defaultClaudeGroupStoreMultiplier(record.OutputCostMultiplier),
		defaultClaudeModelPolicy(record.ModelPolicy), encodeClaudeModelMapping(record.ModelMapping), encodeClaudeSupportedModels(record.SupportedModels),
		defaultClaudeGroupStoreMultiplier(record.CacheCreationCostMultiplier),
		defaultClaudeGroupStoreMultiplier(record.CacheCreationCostMultiplier1h),
		defaultClaudeGroupStoreMultiplier(record.CacheReadCostMultiplier),
		defaultClaudeGroupStoreMultiplier(record.LongContextCostMultiplier),
		defaultClaudeGroupStoreMultiplier(record.LongContextInputCostMultiplier),
		defaultClaudeGroupStoreMultiplier(record.LongContextOutputCostMultiplier),
		defaultClaudeGroupStoreMultiplier(record.LongContextCacheCreationCostMultiplier),
		defaultClaudeGroupStoreMultiplier(record.LongContextCacheReadCostMultiplier),
		boolToInt(record.DisableLongContextPricing),
		boolToInt(record.DisableServiceTierPricing),
		record.ServiceTierCostMultiplier,
		record.ServiceTierInputCostMultiplier,
		record.ServiceTierOutputCostMultiplier,
		record.ServiceTierCacheReadCostMultiplier,
		encodeEndpointModelMultipliers(record.ModelMultipliers),
		defaultAuthMode(record.AuthMode), encodeSubscription(record.Subscription),
		record.ID)
	if err != nil {
		return fmt.Errorf("更新 Claude 凭证分组失败: %w", err)
	}
	affected, _ := result.RowsAffected()
	if affected == 0 {
		return fmt.Errorf("Claude 凭证分组不存在: %d", record.ID)
	}
	if err := s.reviseSubscriptionPauses(ctx, previous, record); err != nil {
		return err
	}
	return nil
}

func (s *SQLiteClaudeCredentialGroupStore) DeleteByID(ctx context.Context, id int64) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	result, err := s.getQuerier().ExecContext(ctx, `DELETE FROM claude_credential_groups WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("删除 Claude 凭证分组失败: %w", err)
	}
	affected, _ := result.RowsAffected()
	if affected == 0 {
		return fmt.Errorf("Claude 凭证分组不存在: %d", id)
	}
	return nil
}

func (s *SQLiteClaudeCredentialGroupStore) DeleteByChannel(ctx context.Context, channel string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, err := s.getQuerier().ExecContext(ctx, `DELETE FROM claude_credential_groups WHERE channel = ?`, channel); err != nil {
		return fmt.Errorf("删除渠道 Claude 凭证分组失败: %w", err)
	}
	return nil
}

func (s *SQLiteClaudeCredentialGroupStore) CountEndpoints(ctx context.Context, id int64) (int, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var count int
	if err := s.getQuerier().QueryRowContext(ctx, `SELECT COUNT(*) FROM endpoints WHERE credential_group_id = ?`, id).Scan(&count); err != nil {
		return 0, fmt.Errorf("统计凭证分组端点引用失败: %w", err)
	}
	return count, nil
}

func (s *SQLiteClaudeCredentialGroupStore) SetDefault(ctx context.Context, id int64) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	var channel string
	if err := s.getQuerier().QueryRowContext(ctx, `SELECT channel FROM claude_credential_groups WHERE id = ?`, id).Scan(&channel); err != nil {
		if err == sql.ErrNoRows {
			return fmt.Errorf("Claude 凭证分组不存在: %d", id)
		}
		return fmt.Errorf("查询 Claude 凭证分组 channel 失败: %w", err)
	}
	if _, err := s.getQuerier().ExecContext(ctx, `UPDATE claude_credential_groups SET is_default = 0 WHERE channel = ?`, channel); err != nil {
		return fmt.Errorf("清空默认凭证分组失败: %w", err)
	}
	if _, err := s.getQuerier().ExecContext(ctx, `UPDATE claude_credential_groups SET is_default = 1 WHERE id = ?`, id); err != nil {
		return fmt.Errorf("设置默认凭证分组失败: %w", err)
	}
	return nil
}

func scanClaudeCredentialGroupRow(scanner interface {
	Scan(dest ...interface{}) error
}) (*ClaudeCredentialGroupRecord, error) {
	var (
		record                             ClaudeCredentialGroupRecord
		isDefault                          int
		dailyCostLimitUSD                  float64
		modelPolicy                        string
		modelMappingJSON                   string
		supportedModelsJSON                string
		costMultiplier                     float64
		inputCostMultiplier                float64
		outputCostMultiplier               float64
		cacheCreationMultiplier            float64
		cacheCreationMultiplier1           float64
		cacheReadMultiplier                float64
		longContextMultiplier              float64
		longContextInputMultiplier         float64
		longContextOutputMultiplier        float64
		longContextCacheCreationMultiplier float64
		longContextCacheReadMultiplier     float64
		disableLongContext                 int
		disableServiceTier                 int
		serviceTierCostMultiplier          float64
		serviceTierInputCostMultiplier     float64
		serviceTierOutputCostMultiplier    float64
		serviceTierCacheReadCostMultiplier float64
		modelMultipliersJSON               string
		createdAt                          string
		updatedAt                          string
		subscriptionJSON                   string
	)
	if err := scanner.Scan(
		&record.ID,
		&record.Channel,
		&record.Name,
		&record.Token,
		&record.ApiKey,
		&record.AuthMode,
		&subscriptionJSON,
		&isDefault,
		&record.Enabled, &dailyCostLimitUSD,
		&modelPolicy,
		&modelMappingJSON,
		&supportedModelsJSON,
		&costMultiplier,
		&inputCostMultiplier,
		&outputCostMultiplier,
		&cacheCreationMultiplier,
		&cacheCreationMultiplier1,
		&cacheReadMultiplier,
		&longContextMultiplier,
		&longContextInputMultiplier,
		&longContextOutputMultiplier,
		&longContextCacheCreationMultiplier,
		&longContextCacheReadMultiplier,
		&disableLongContext,
		&disableServiceTier,
		&serviceTierCostMultiplier,
		&serviceTierInputCostMultiplier,
		&serviceTierOutputCostMultiplier,
		&serviceTierCacheReadCostMultiplier,
		&modelMultipliersJSON,
		&createdAt,
		&updatedAt,
	); err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("读取 Claude 凭证分组失败: %w", err)
	}
	record.IsDefault = isDefault != 0
	record.Subscription = decodeSubscription(subscriptionJSON)
	if record.Subscription != nil {
		record.Subscription.GroupID = record.ID
		record.Subscription.Channel = record.Channel
	}
	record.DailyCostLimitUSD = dailyCostLimitUSD
	record.ModelPolicy = defaultClaudeModelPolicy(modelPolicy)
	record.ModelMapping = decodeClaudeModelMapping(modelMappingJSON)
	record.SupportedModels = decodeClaudeSupportedModels(supportedModelsJSON)
	record.CostMultiplier = defaultClaudeGroupStoreMultiplier(costMultiplier)
	record.InputCostMultiplier = defaultClaudeGroupStoreMultiplier(inputCostMultiplier)
	record.OutputCostMultiplier = defaultClaudeGroupStoreMultiplier(outputCostMultiplier)
	record.CacheCreationCostMultiplier = defaultClaudeGroupStoreMultiplier(cacheCreationMultiplier)
	record.CacheCreationCostMultiplier1h = defaultClaudeGroupStoreMultiplier(cacheCreationMultiplier1)
	record.CacheReadCostMultiplier = defaultClaudeGroupStoreMultiplier(cacheReadMultiplier)
	record.LongContextCostMultiplier = defaultClaudeGroupStoreMultiplier(longContextMultiplier)
	record.LongContextInputCostMultiplier = defaultClaudeGroupStoreMultiplier(longContextInputMultiplier)
	record.LongContextOutputCostMultiplier = defaultClaudeGroupStoreMultiplier(longContextOutputMultiplier)
	record.LongContextCacheCreationCostMultiplier = defaultClaudeGroupStoreMultiplier(longContextCacheCreationMultiplier)
	record.LongContextCacheReadCostMultiplier = defaultClaudeGroupStoreMultiplier(longContextCacheReadMultiplier)
	record.DisableLongContextPricing = disableLongContext != 0
	record.DisableServiceTierPricing = disableServiceTier != 0
	record.ServiceTierCostMultiplier = defaultClaudeGroupStoreMultiplier(serviceTierCostMultiplier)
	record.ServiceTierInputCostMultiplier = defaultClaudeGroupStoreMultiplier(serviceTierInputCostMultiplier)
	record.ServiceTierOutputCostMultiplier = defaultClaudeGroupStoreMultiplier(serviceTierOutputCostMultiplier)
	record.ServiceTierCacheReadCostMultiplier = defaultClaudeGroupStoreMultiplier(serviceTierCacheReadCostMultiplier)
	record.ModelMultipliers = decodeEndpointModelMultipliers(modelMultipliersJSON)
	record.CreatedAt = parseSQLiteDateTime(createdAt)
	record.UpdatedAt = parseSQLiteDateTime(updatedAt)
	return &record, nil
}

func defaultAuthMode(mode string) string {
	if mode == "" {
		return subscription.GroupCredential
	}
	return mode
}

func defaultClaudeGroupStoreMultiplier(value float64) float64 {
	if value <= 0 {
		return 1.0
	}
	return value
}

func defaultClaudeModelPolicy(policy string) string {
	switch strings.ToLower(strings.TrimSpace(policy)) {
	case ClaudeModelPolicyOverride:
		return ClaudeModelPolicyOverride
	case ClaudeModelPolicyPassthrough:
		return ClaudeModelPolicyPassthrough
	default:
		return ClaudeModelPolicyInherit
	}
}

func encodeClaudeModelMapping(mapping map[string]string) string {
	if len(mapping) == 0 {
		return "{}"
	}
	normalized := make(map[string]string, len(mapping))
	for source, target := range mapping {
		source, target = strings.TrimSpace(source), strings.TrimSpace(target)
		if source != "" && target != "" {
			normalized[source] = target
		}
	}
	if len(normalized) == 0 {
		return "{}"
	}
	data, err := json.Marshal(normalized)
	if err != nil {
		return "{}"
	}
	return string(data)
}

func decodeClaudeModelMapping(raw string) map[string]string {
	var mapping map[string]string
	if err := json.Unmarshal([]byte(raw), &mapping); err != nil || mapping == nil {
		return map[string]string{}
	}
	result := make(map[string]string, len(mapping))
	for source, target := range mapping {
		source, target = strings.TrimSpace(source), strings.TrimSpace(target)
		if source != "" && target != "" {
			result[source] = target
		}
	}
	return result
}

func encodeClaudeSupportedModels(models []string) string {
	if len(models) == 0 {
		return "[]"
	}
	seen := make(map[string]struct{}, len(models))
	normalized := make([]string, 0, len(models))
	for _, model := range models {
		model = strings.TrimSpace(model)
		if model == "" {
			continue
		}
		if _, exists := seen[model]; exists {
			continue
		}
		seen[model] = struct{}{}
		normalized = append(normalized, model)
	}
	if len(normalized) == 0 {
		return "[]"
	}
	data, err := json.Marshal(normalized)
	if err != nil {
		return "[]"
	}
	return string(data)
}

func decodeClaudeSupportedModels(raw string) []string {
	var models []string
	if err := json.Unmarshal([]byte(raw), &models); err != nil {
		return nil
	}
	seen := make(map[string]struct{}, len(models))
	result := make([]string, 0, len(models))
	for _, model := range models {
		model = strings.TrimSpace(model)
		if model == "" {
			continue
		}
		if _, exists := seen[model]; exists {
			continue
		}
		seen[model] = struct{}{}
		result = append(result, model)
	}
	return result
}
