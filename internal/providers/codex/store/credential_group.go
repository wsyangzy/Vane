package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/wsyangzy/Vane/internal/providers/codex/proxy/subscription"
)

type CodexServiceTierMultiplierRecord struct {
	ServiceTier                 string  `json:"service_tier"`
	CostMultiplier              float64 `json:"cost_multiplier"`
	InputCostMultiplier         float64 `json:"input_cost_multiplier"`
	OutputCostMultiplier        float64 `json:"output_cost_multiplier"`
	CacheCreationCostMultiplier float64 `json:"cache_creation_cost_multiplier"`
	CacheReadCostMultiplier     float64 `json:"cache_read_cost_multiplier"`
}

type CodexBillingRuleRecord struct {
	Model                       string  `json:"model"`
	ServiceTier                 string  `json:"service_tier"`
	Context                     string  `json:"context"`
	Mode                        string  `json:"mode"`
	GroupMultiplierMode         string  `json:"group_multiplier_mode"`
	CostMultiplier              float64 `json:"cost_multiplier"`
	InputCostMultiplier         float64 `json:"input_cost_multiplier"`
	OutputCostMultiplier        float64 `json:"output_cost_multiplier"`
	CacheCreationCostMultiplier float64 `json:"cache_creation_cost_multiplier"`
	CacheReadCostMultiplier     float64 `json:"cache_read_cost_multiplier"`
	InputPrice                  float64 `json:"input_price"`
	OutputPrice                 float64 `json:"output_price"`
	CacheCreationPrice          float64 `json:"cache_creation_price"`
	CacheReadPrice              float64 `json:"cache_read_price"`
}

const (
	CodexModelPolicyInherit     = "inherit"
	CodexModelPolicyOverride    = "override"
	CodexModelPolicyPassthrough = "passthrough"
)

// CodexCredentialGroupRecord 表示 Codex 渠道下可复用的凭证分组。
// 端点引用分组后，可共享 API Key 与默认成本倍率。
type CodexCredentialGroupRecord struct {
	ServiceTierSource    string                `json:"service_tier_source"`
	CacheWriteTTLPricing bool                  `json:"cache_write_ttl_pricing"`
	AuthMode             string                `json:"auth_mode"`
	Subscription         *subscription.Binding `json:"subscription,omitempty"`
	ID                   int64                 `json:"id"`
	ChannelID            int64                 `json:"channel_id"`
	Name                 string                `json:"name"`
	ApiKey               string                `json:"api_key,omitempty"`
	Enabled              bool                  `json:"enabled"`
	IsDefault            bool                  `json:"is_default"`
	DailyCostLimitUSD    float64               `json:"daily_cost_limit_usd"`
	ModelPolicy          string                `json:"model_policy"`
	ModelMapping         map[string]string     `json:"model_mapping"`
	SupportedModels      []string              `json:"supported_models"`

	CostMultiplier                         float64                            `json:"cost_multiplier"`
	InputCostMultiplier                    float64                            `json:"input_cost_multiplier"`
	OutputCostMultiplier                   float64                            `json:"output_cost_multiplier"`
	CacheCreationCostMultiplier            float64                            `json:"cache_creation_cost_multiplier"`
	CacheReadCostMultiplier                float64                            `json:"cache_read_cost_multiplier"`
	LongContextCostMultiplier              float64                            `json:"long_context_cost_multiplier"`
	LongContextInputCostMultiplier         float64                            `json:"long_context_input_cost_multiplier"`
	LongContextOutputCostMultiplier        float64                            `json:"long_context_output_cost_multiplier"`
	LongContextCacheCreationCostMultiplier float64                            `json:"long_context_cache_creation_cost_multiplier"`
	LongContextCacheReadCostMultiplier     float64                            `json:"long_context_cache_read_cost_multiplier"`
	DisableLongContextPricing              bool                               `json:"disable_long_context_pricing"`
	DisableServiceTierPricing              bool                               `json:"disable_service_tier_pricing"`
	ServiceTierMultipliers                 []CodexServiceTierMultiplierRecord `json:"service_tier_multipliers"`
	BillingOutputTokenSource               string                             `json:"billing_output_token_source"`
	ModelMultipliers                       []CodexModelMultiplierRecord       `json:"model_multipliers"`
	BillingRules                           []CodexBillingRuleRecord           `json:"billing_rules"`

	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// CodexCredentialGroupStore 定义 Codex 凭证分组存储接口。
type CodexCredentialGroupStore interface {
	Create(ctx context.Context, record *CodexCredentialGroupRecord) (*CodexCredentialGroupRecord, error)
	GetByID(ctx context.Context, id int64) (*CodexCredentialGroupRecord, error)
	GetDefaultByChannelID(ctx context.Context, channelID int64) (*CodexCredentialGroupRecord, error)
	GetByChannelAndName(ctx context.Context, channelID int64, name string) (*CodexCredentialGroupRecord, error)
	ListByChannelID(ctx context.Context, channelID int64) ([]*CodexCredentialGroupRecord, error)
	Update(ctx context.Context, record *CodexCredentialGroupRecord) error
	DeleteByID(ctx context.Context, id int64) error
	CountEndpoints(ctx context.Context, id int64) (int, error)
	SetDefault(ctx context.Context, id int64) error

	WithTx(tx *sql.Tx) CodexCredentialGroupStore
}

// SQLiteCodexCredentialGroupStore 实现 CodexCredentialGroupStore。
type SQLiteCodexCredentialGroupStore struct {
	db *sql.DB
	tx *sql.Tx
	mu sync.RWMutex
}

func NewSQLiteCodexCredentialGroupStore(db *sql.DB) *SQLiteCodexCredentialGroupStore {
	return &SQLiteCodexCredentialGroupStore{db: db}
}

func (s *SQLiteCodexCredentialGroupStore) WithTx(tx *sql.Tx) CodexCredentialGroupStore {
	return &SQLiteCodexCredentialGroupStore{db: s.db, tx: tx}
}

func (s *SQLiteCodexCredentialGroupStore) getQuerier() interface {
	QueryContext(ctx context.Context, query string, args ...interface{}) (*sql.Rows, error)
	QueryRowContext(ctx context.Context, query string, args ...interface{}) *sql.Row
	ExecContext(ctx context.Context, query string, args ...interface{}) (sql.Result, error)
} {
	if s.tx != nil {
		return s.tx
	}
	return s.db
}

const codexCredentialGroupColumns = `id, channel_id, name, api_key, COALESCE(is_default, 0), enabled,
	COALESCE(daily_cost_limit_usd, 0),
	COALESCE(model_policy, 'inherit'), COALESCE(model_mapping, '{}'), COALESCE(supported_models, '[]'),
	COALESCE(cost_multiplier, 1.0), COALESCE(input_cost_multiplier, 1.0),
	COALESCE(output_cost_multiplier, 1.0), COALESCE(cache_creation_cost_multiplier, 1.0),
	COALESCE(cache_read_cost_multiplier, 1.0), COALESCE(long_context_cost_multiplier, 1.0),
	COALESCE(long_context_input_cost_multiplier, 1.0), COALESCE(long_context_output_cost_multiplier, 1.0),
	COALESCE(long_context_cache_creation_cost_multiplier, 1.0), COALESCE(long_context_cache_read_cost_multiplier, 1.0),
	COALESCE(disable_long_context_pricing, 0),
	COALESCE(disable_service_tier_pricing, 1), COALESCE(service_tier_multipliers, '[]'),
	COALESCE(billing_output_token_source, 'output_tokens'), COALESCE(model_multipliers, '[]'),
	COALESCE(service_tier_source, 'response'), COALESCE(cache_write_ttl_pricing, 0),
	COALESCE(billing_rules, '[]'), COALESCE(auth_mode,'group_credential'), COALESCE(subscription_config,'{}'),
	COALESCE(created_at, ''), COALESCE(updated_at, '')`

func (s *SQLiteCodexCredentialGroupStore) Create(ctx context.Context, record *CodexCredentialGroupRecord) (*CodexCredentialGroupRecord, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if record == nil {
		return nil, fmt.Errorf("record 不能为空")
	}
	if record.ChannelID <= 0 {
		return nil, fmt.Errorf("channel_id 无效")
	}
	if record.Name == "" {
		return nil, fmt.Errorf("凭证分组名不能为空")
	}

	if record.IsDefault {
		if _, err := s.getQuerier().ExecContext(ctx, `UPDATE codex_credential_groups SET is_default = 0 WHERE channel_id = ?`, record.ChannelID); err != nil {
			return nil, fmt.Errorf("清空默认凭证分组失败: %w", err)
		}
	}

	result, err := s.getQuerier().ExecContext(ctx, `
		INSERT INTO codex_credential_groups (
			channel_id, name, api_key, is_default, daily_cost_limit_usd, model_policy, model_mapping, supported_models, cost_multiplier,
			input_cost_multiplier, output_cost_multiplier, cache_creation_cost_multiplier,
			cache_read_cost_multiplier, long_context_cost_multiplier,
			long_context_input_cost_multiplier, long_context_output_cost_multiplier,
			long_context_cache_creation_cost_multiplier, long_context_cache_read_cost_multiplier,
			disable_long_context_pricing,
			disable_service_tier_pricing, service_tier_multipliers,
			billing_output_token_source, model_multipliers, service_tier_source, cache_write_ttl_pricing, billing_rules, auth_mode, subscription_config
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, record.ChannelID, record.Name, record.ApiKey, boolToInt(record.IsDefault), record.DailyCostLimitUSD,
		defaultCodexModelPolicy(record.ModelPolicy), encodeCodexModelMapping(record.ModelMapping), encodeCodexSupportedModels(record.SupportedModels),
		defaultCodexStoreMultiplier(record.CostMultiplier),
		defaultCodexStoreMultiplier(record.InputCostMultiplier),
		defaultCodexStoreMultiplier(record.OutputCostMultiplier),
		defaultCodexStoreMultiplier(record.CacheCreationCostMultiplier),
		defaultCodexStoreMultiplier(record.CacheReadCostMultiplier),
		defaultCodexStoreMultiplier(record.LongContextCostMultiplier),
		defaultCodexStoreMultiplier(record.LongContextInputCostMultiplier),
		defaultCodexStoreMultiplier(record.LongContextOutputCostMultiplier),
		defaultCodexStoreMultiplier(record.LongContextCacheCreationCostMultiplier),
		defaultCodexStoreMultiplier(record.LongContextCacheReadCostMultiplier),
		boolToInt(record.DisableLongContextPricing),
		boolToInt(record.DisableServiceTierPricing),
		encodeCodexServiceTierMultipliers(record.ServiceTierMultipliers),
		defaultBillingOutputTokenSource(record.BillingOutputTokenSource),
		encodeCodexModelMultipliers(record.ModelMultipliers),
		record.ServiceTierSource, boolToInt(record.CacheWriteTTLPricing),
		encodeCodexBillingRules(record.BillingRules), defaultAuthMode(record.AuthMode), encodeSubscription(record.Subscription))
	if err != nil {
		return nil, fmt.Errorf("创建 Codex 凭证分组失败: %w", err)
	}

	id, err := result.LastInsertId()
	if err != nil {
		return nil, fmt.Errorf("获取插入 ID 失败: %w", err)
	}
	queryByID := "SELECT " + codexCredentialGroupColumns + " FROM codex_credential_groups WHERE id = ?"
	return scanCodexCredentialGroupRow(s.getQuerier().QueryRowContext(ctx, queryByID, id))
}

func (s *SQLiteCodexCredentialGroupStore) GetByID(ctx context.Context, id int64) (*CodexCredentialGroupRecord, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	query := "SELECT " + codexCredentialGroupColumns + " FROM codex_credential_groups WHERE id = ?"
	return scanCodexCredentialGroupRow(s.getQuerier().QueryRowContext(ctx, query, id))
}

func (s *SQLiteCodexCredentialGroupStore) GetDefaultByChannelID(ctx context.Context, channelID int64) (*CodexCredentialGroupRecord, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	query := "SELECT " + codexCredentialGroupColumns + " FROM codex_credential_groups WHERE channel_id = ? AND is_default = 1 LIMIT 1"
	return scanCodexCredentialGroupRow(s.getQuerier().QueryRowContext(ctx, query, channelID))
}

func (s *SQLiteCodexCredentialGroupStore) GetByChannelAndName(ctx context.Context, channelID int64, name string) (*CodexCredentialGroupRecord, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	query := "SELECT " + codexCredentialGroupColumns + " FROM codex_credential_groups WHERE channel_id = ? AND name = ?"
	return scanCodexCredentialGroupRow(s.getQuerier().QueryRowContext(ctx, query, channelID, name))
}

func (s *SQLiteCodexCredentialGroupStore) ListByChannelID(ctx context.Context, channelID int64) ([]*CodexCredentialGroupRecord, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	query := "SELECT " + codexCredentialGroupColumns + " FROM codex_credential_groups WHERE channel_id = ? ORDER BY is_default DESC, created_at ASC, id ASC"
	rows, err := s.getQuerier().QueryContext(ctx, query, channelID)
	if err != nil {
		return nil, fmt.Errorf("获取 Codex 凭证分组列表失败: %w", err)
	}
	defer rows.Close()

	result := make([]*CodexCredentialGroupRecord, 0)
	for rows.Next() {
		record, err := scanCodexCredentialGroupRow(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, record)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("读取 Codex 凭证分组列表失败: %w", err)
	}
	return result, nil
}

func (s *SQLiteCodexCredentialGroupStore) Update(ctx context.Context, record *CodexCredentialGroupRecord) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.tx == nil {
		tx, err := s.db.BeginTx(ctx, nil)
		if err != nil {
			return err
		}
		defer tx.Rollback()
		if err = s.WithTx(tx).Update(ctx, record); err != nil {
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
	if record.ChannelID <= 0 {
		return fmt.Errorf("channel_id 无效")
	}
	if record.Name == "" {
		return fmt.Errorf("凭证分组名不能为空")
	}

	previous, err := scanCodexCredentialGroupRow(s.getQuerier().QueryRowContext(ctx, "SELECT "+codexCredentialGroupColumns+" FROM codex_credential_groups WHERE id=?", record.ID))
	if err != nil {
		return err
	}
	if record.IsDefault {
		if _, err := s.getQuerier().ExecContext(ctx, `UPDATE codex_credential_groups SET is_default = 0 WHERE channel_id = ? AND id <> ?`, record.ChannelID, record.ID); err != nil {
			return fmt.Errorf("清空默认凭证分组失败: %w", err)
		}
	}

	result, err := s.getQuerier().ExecContext(ctx, `
		UPDATE codex_credential_groups
		SET name = ?, api_key = ?, is_default = ?, daily_cost_limit_usd = ?, model_policy = ?, model_mapping = ?, supported_models = ?, cost_multiplier = ?,
			input_cost_multiplier = ?, output_cost_multiplier = ?,
			cache_creation_cost_multiplier = ?, cache_read_cost_multiplier = ?,
			long_context_cost_multiplier = ?, long_context_input_cost_multiplier = ?,
			long_context_output_cost_multiplier = ?, long_context_cache_creation_cost_multiplier = ?,
			long_context_cache_read_cost_multiplier = ?, disable_long_context_pricing = ?,
			disable_service_tier_pricing = ?, service_tier_multipliers = ?,
			billing_output_token_source = ?, model_multipliers = ?, service_tier_source = ?, cache_write_ttl_pricing = ?, billing_rules = ?, auth_mode = ?, subscription_config = ?
		WHERE id = ?
	`, record.Name, record.ApiKey, boolToInt(record.IsDefault), record.DailyCostLimitUSD,
		defaultCodexModelPolicy(record.ModelPolicy), encodeCodexModelMapping(record.ModelMapping), encodeCodexSupportedModels(record.SupportedModels),
		defaultCodexStoreMultiplier(record.CostMultiplier),
		defaultCodexStoreMultiplier(record.InputCostMultiplier),
		defaultCodexStoreMultiplier(record.OutputCostMultiplier),
		defaultCodexStoreMultiplier(record.CacheCreationCostMultiplier),
		defaultCodexStoreMultiplier(record.CacheReadCostMultiplier),
		defaultCodexStoreMultiplier(record.LongContextCostMultiplier),
		defaultCodexStoreMultiplier(record.LongContextInputCostMultiplier),
		defaultCodexStoreMultiplier(record.LongContextOutputCostMultiplier),
		defaultCodexStoreMultiplier(record.LongContextCacheCreationCostMultiplier),
		defaultCodexStoreMultiplier(record.LongContextCacheReadCostMultiplier),
		boolToInt(record.DisableLongContextPricing),
		boolToInt(record.DisableServiceTierPricing),
		encodeCodexServiceTierMultipliers(record.ServiceTierMultipliers),
		defaultBillingOutputTokenSource(record.BillingOutputTokenSource),
		encodeCodexModelMultipliers(record.ModelMultipliers),
		record.ServiceTierSource, boolToInt(record.CacheWriteTTLPricing),
		encodeCodexBillingRules(record.BillingRules), defaultAuthMode(record.AuthMode), encodeSubscription(record.Subscription),
		record.ID)
	if err != nil {
		return fmt.Errorf("更新 Codex 凭证分组失败: %w", err)
	}
	affected, _ := result.RowsAffected()
	if affected == 0 {
		return fmt.Errorf("Codex 凭证分组不存在: %d", record.ID)
	}
	return s.reviseSubscriptionPauses(ctx, previous, record)
}

func (s *SQLiteCodexCredentialGroupStore) DeleteByID(ctx context.Context, id int64) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.tx != nil {
		return deleteInactiveCodexCredentialGroup(ctx, s.tx, id)
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("开启 Codex 凭证分组删除事务失败: %w", err)
	}
	defer tx.Rollback()
	if err := deleteInactiveCodexCredentialGroup(ctx, tx, id); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("提交 Codex 凭证分组删除事务失败: %w", err)
	}
	return nil
}

// Check the live state and delete both resources in one transaction, so an
// endpoint cannot become eligible between the guard and the deletion.
func deleteInactiveCodexCredentialGroup(ctx context.Context, tx *sql.Tx, id int64) error {
	var isDefault bool
	if err := tx.QueryRowContext(ctx, `SELECT COALESCE(is_default, 0) FROM codex_credential_groups WHERE id = ?`, id).Scan(&isDefault); err != nil {
		if err == sql.ErrNoRows {
			return fmt.Errorf("Codex 凭证分组不存在: %d", id)
		}
		return fmt.Errorf("读取待删除 Codex 凭证分组失败: %w", err)
	}
	if isDefault {
		return fmt.Errorf("默认凭证分组不能删除")
	}

	var participating, active int
	if err := tx.QueryRowContext(ctx, `
		SELECT COUNT(CASE WHEN COALESCE(failover_enabled, 1) != 0 THEN 1 END),
		       COUNT(CASE WHEN COALESCE(is_active, 0) != 0 THEN 1 END)
		FROM codex_endpoints WHERE credential_group_id = ?`, id).Scan(&participating, &active); err != nil {
		return fmt.Errorf("检查 Codex 凭证分组端点状态失败: %w", err)
	}
	if active > 0 {
		return fmt.Errorf("凭证分组仍有 %d 个激活端点，请先切换激活端点后再删除", active)
	}
	if participating > 0 {
		return fmt.Errorf("凭证分组仍有 %d 个端点参与故障转移，请先全部取消参与后再删除", participating)
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM codex_endpoints WHERE credential_group_id = ?`, id); err != nil {
		return fmt.Errorf("删除 Codex 凭证分组内停用端点失败: %w", err)
	}
	result, err := tx.ExecContext(ctx, `DELETE FROM codex_credential_groups WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("删除 Codex 凭证分组失败: %w", err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("确认 Codex 凭证分组删除结果失败: %w", err)
	}
	if affected == 0 {
		return fmt.Errorf("Codex 凭证分组不存在: %d", id)
	}
	return nil
}

func (s *SQLiteCodexCredentialGroupStore) CountEndpoints(ctx context.Context, id int64) (int, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var count int
	if err := s.getQuerier().QueryRowContext(ctx, `SELECT COUNT(*) FROM codex_endpoints WHERE credential_group_id = ?`, id).Scan(&count); err != nil {
		return 0, fmt.Errorf("统计凭证分组端点引用失败: %w", err)
	}
	return count, nil
}

func (s *SQLiteCodexCredentialGroupStore) SetDefault(ctx context.Context, id int64) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	var channelID int64
	if err := s.getQuerier().QueryRowContext(ctx, `SELECT channel_id FROM codex_credential_groups WHERE id = ?`, id).Scan(&channelID); err != nil {
		if err == sql.ErrNoRows {
			return fmt.Errorf("Codex 凭证分组不存在: %d", id)
		}
		return fmt.Errorf("查询 Codex 凭证分组 channel_id 失败: %w", err)
	}
	if _, err := s.getQuerier().ExecContext(ctx, `UPDATE codex_credential_groups SET is_default = 0 WHERE channel_id = ?`, channelID); err != nil {
		return fmt.Errorf("清空默认凭证分组失败: %w", err)
	}
	if _, err := s.getQuerier().ExecContext(ctx, `UPDATE codex_credential_groups SET is_default = 1 WHERE id = ?`, id); err != nil {
		return fmt.Errorf("设置默认凭证分组失败: %w", err)
	}
	return nil
}

func scanCodexCredentialGroupRow(scanner interface {
	Scan(dest ...interface{}) error
}) (*CodexCredentialGroupRecord, error) {
	var (
		record                             CodexCredentialGroupRecord
		isDefault                          int
		dailyCostLimitUSD                  float64
		modelPolicy                        string
		modelMappingJSON                   string
		supportedModelsJSON                string
		costMultiplier                     float64
		inputCostMultiplier                float64
		outputCostMultiplier               float64
		cacheCreationCostMultiplier        float64
		cacheReadCostMultiplier            float64
		longContextCostMultiplier          float64
		longContextInputMultiplier         float64
		longContextOutputMultiplier        float64
		longContextCacheCreationMultiplier float64
		longContextCacheReadMultiplier     float64
		disableLongContextPricing          int
		disableServiceTierPricing          int
		serviceTierMultipliersJSON         string
		billingOutputTokenSource           string
		modelMultipliersJSON               string
		billingRulesJSON                   string
		subscriptionJSON                   string
		createdAt                          string
		updatedAt                          string
	)
	if err := scanner.Scan(
		&record.ID,
		&record.ChannelID,
		&record.Name,
		&record.ApiKey,
		&isDefault,
		&record.Enabled, &dailyCostLimitUSD,
		&modelPolicy,
		&modelMappingJSON,
		&supportedModelsJSON,
		&costMultiplier,
		&inputCostMultiplier,
		&outputCostMultiplier,
		&cacheCreationCostMultiplier,
		&cacheReadCostMultiplier,
		&longContextCostMultiplier,
		&longContextInputMultiplier,
		&longContextOutputMultiplier,
		&longContextCacheCreationMultiplier,
		&longContextCacheReadMultiplier,
		&disableLongContextPricing,
		&disableServiceTierPricing,
		&serviceTierMultipliersJSON,
		&billingOutputTokenSource,
		&modelMultipliersJSON,
		&record.ServiceTierSource, &record.CacheWriteTTLPricing, &billingRulesJSON, &record.AuthMode, &subscriptionJSON,
		&createdAt,
		&updatedAt,
	); err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("读取 Codex 凭证分组失败: %w", err)
	}
	record.Subscription = decodeSubscription(subscriptionJSON)
	record.IsDefault = isDefault != 0
	record.DailyCostLimitUSD = dailyCostLimitUSD
	record.ModelPolicy = defaultCodexModelPolicy(modelPolicy)
	record.ModelMapping = decodeCodexModelMapping(modelMappingJSON)
	record.SupportedModels = decodeCodexSupportedModels(supportedModelsJSON)
	record.CostMultiplier = defaultCodexStoreMultiplier(costMultiplier)
	record.InputCostMultiplier = defaultCodexStoreMultiplier(inputCostMultiplier)
	record.OutputCostMultiplier = defaultCodexStoreMultiplier(outputCostMultiplier)
	record.CacheCreationCostMultiplier = defaultCodexStoreMultiplier(cacheCreationCostMultiplier)
	record.CacheReadCostMultiplier = defaultCodexStoreMultiplier(cacheReadCostMultiplier)
	record.LongContextCostMultiplier = defaultCodexStoreMultiplier(longContextCostMultiplier)
	record.LongContextInputCostMultiplier = defaultCodexStoreMultiplier(longContextInputMultiplier)
	record.LongContextOutputCostMultiplier = defaultCodexStoreMultiplier(longContextOutputMultiplier)
	record.LongContextCacheCreationCostMultiplier = defaultCodexStoreMultiplier(longContextCacheCreationMultiplier)
	record.LongContextCacheReadCostMultiplier = defaultCodexStoreMultiplier(longContextCacheReadMultiplier)
	record.DisableLongContextPricing = disableLongContextPricing != 0
	record.DisableServiceTierPricing = disableServiceTierPricing != 0
	record.ServiceTierMultipliers = decodeCodexServiceTierMultipliers(serviceTierMultipliersJSON)
	record.BillingOutputTokenSource = defaultBillingOutputTokenSource(billingOutputTokenSource)
	record.ModelMultipliers = decodeCodexModelMultipliers(modelMultipliersJSON)
	record.BillingRules = decodeCodexBillingRules(billingRulesJSON)
	record.CreatedAt = parseSQLiteDateTime(createdAt)
	record.UpdatedAt = parseSQLiteDateTime(updatedAt)
	return &record, nil
}

func defaultCodexStoreMultiplier(value float64) float64 {
	if value <= 0 {
		return 1.0
	}
	return value
}

func defaultCodexModelPolicy(policy string) string {
	switch policy {
	case CodexModelPolicyOverride, CodexModelPolicyPassthrough:
		return policy
	default:
		return CodexModelPolicyInherit
	}
}

func encodeCodexModelMapping(mapping map[string]string) string {
	if len(mapping) == 0 {
		return "{}"
	}
	normalized := make(map[string]string, len(mapping))
	for source, target := range mapping {
		source = strings.TrimSpace(source)
		target = strings.TrimSpace(target)
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

func decodeCodexModelMapping(raw string) map[string]string {
	var mapping map[string]string
	if err := json.Unmarshal([]byte(raw), &mapping); err != nil || mapping == nil {
		return map[string]string{}
	}
	normalized := make(map[string]string, len(mapping))
	for source, target := range mapping {
		source = strings.TrimSpace(source)
		target = strings.TrimSpace(target)
		if source != "" && target != "" {
			normalized[source] = target
		}
	}
	return normalized
}

func encodeCodexSupportedModels(models []string) string {
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
		if _, ok := seen[model]; ok {
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

func decodeCodexSupportedModels(raw string) []string {
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
		if _, ok := seen[model]; ok {
			continue
		}
		seen[model] = struct{}{}
		result = append(result, model)
	}
	return result
}

func encodeCodexServiceTierMultipliers(multipliers []CodexServiceTierMultiplierRecord) string {
	if len(multipliers) == 0 {
		return "[]"
	}
	data, err := json.Marshal(multipliers)
	if err != nil {
		return "[]"
	}
	return string(data)
}

func decodeCodexServiceTierMultipliers(raw string) []CodexServiceTierMultiplierRecord {
	if raw == "" {
		return nil
	}
	var multipliers []CodexServiceTierMultiplierRecord
	if err := json.Unmarshal([]byte(raw), &multipliers); err != nil {
		return nil
	}
	return multipliers
}

func encodeCodexBillingRules(rules []CodexBillingRuleRecord) string {
	if len(rules) == 0 {
		return "[]"
	}
	data, err := json.Marshal(rules)
	if err != nil {
		return "[]"
	}
	return string(data)
}

func decodeCodexBillingRules(raw string) []CodexBillingRuleRecord {
	if raw == "" {
		return nil
	}
	var rules []CodexBillingRuleRecord
	if err := json.Unmarshal([]byte(raw), &rules); err != nil {
		return nil
	}
	return rules
}

func defaultAuthMode(mode string) string {
	if mode == "" {
		return subscription.GroupCredential
	}
	return mode
}
