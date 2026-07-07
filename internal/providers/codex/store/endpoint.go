package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"sync"
	"time"
)

type CodexModelMultiplierRecord struct {
	Model                   string  `json:"model"`
	CostMultiplier          float64 `json:"costMultiplier"`
	InputCostMultiplier     float64 `json:"inputCostMultiplier"`
	OutputCostMultiplier    float64 `json:"outputCostMultiplier"`
	CacheReadCostMultiplier float64 `json:"cacheReadCostMultiplier"`
}

// CodexEndpointRecord 表示 Codex 渠道下的某个端点。
// name + base_url 在同一凭证分组内分别唯一。
type CodexEndpointRecord struct {
	ID                int64   `json:"id"`
	ChannelID         int64   `json:"channel_id"`
	CredentialGroupID int64   `json:"credential_group_id"`
	ApiKey            string  `json:"api_key,omitempty"`
	Name              string  `json:"name"`
	BaseURL           string  `json:"base_url"`
	IsActive          bool    `json:"is_active"`
	FailoverEnabled   bool    `json:"failover_enabled"`
	SkipHealthCheck   bool    `json:"skip_health_check"`
	Priority          int     `json:"priority"`
	TimeoutSeconds    int     `json:"timeout_seconds"`
	CostMultiplier    float64 `json:"cost_multiplier"` // 总成本倍率，默认 1.0

	// 细分成本倍率（默认 1.0，与 Claude 端点对齐）
	InputCostMultiplier      float64                      `json:"input_cost_multiplier"`
	OutputCostMultiplier     float64                      `json:"output_cost_multiplier"`
	CacheReadCostMultiplier  float64                      `json:"cache_read_cost_multiplier"`
	BillingOutputTokenSource string                       `json:"billing_output_token_source"`
	ModelMultipliers         []CodexModelMultiplierRecord `json:"model_multipliers"`

	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// CodexEndpointStore 定义端点存储接口。
type CodexEndpointStore interface {
	Create(ctx context.Context, record *CodexEndpointRecord) (*CodexEndpointRecord, error)
	GetByID(ctx context.Context, id int64) (*CodexEndpointRecord, error)
	GetByChannelAndURL(ctx context.Context, channelID int64, baseURL string) (*CodexEndpointRecord, error)
	GetByChannelAndName(ctx context.Context, channelID int64, name string) (*CodexEndpointRecord, error)
	GetByChannelCredentialGroupAndURL(ctx context.Context, channelID, credentialGroupID int64, baseURL string) (*CodexEndpointRecord, error)
	GetByChannelCredentialGroupAndName(ctx context.Context, channelID, credentialGroupID int64, name string) (*CodexEndpointRecord, error)
	GetByChannelCredentialGroupAndPriority(ctx context.Context, channelID, credentialGroupID int64, priority int) (*CodexEndpointRecord, error)
	GetByChannelAndPriority(ctx context.Context, channelID int64, priority int) (*CodexEndpointRecord, error)
	GetMaxPriorityByChannelCredentialGroup(ctx context.Context, channelID, credentialGroupID int64) (int, error)
	GetMaxPriorityByChannel(ctx context.Context, channelID int64) (int, error)
	GetActiveByChannelID(ctx context.Context, channelID int64) (*CodexEndpointRecord, error)
	ListByChannelID(ctx context.Context, channelID int64) ([]*CodexEndpointRecord, error)
	Update(ctx context.Context, id int64, name, baseURL string, credentialGroupID int64, apiKey string, failoverEnabled bool, skipHealthCheck bool, priority, timeoutSeconds int, costMultiplier, inputCostMultiplier, outputCostMultiplier, cacheReadCostMultiplier float64, billingOutputTokenSource string, modelMultipliers []CodexModelMultiplierRecord) error
	DeleteByID(ctx context.Context, id int64) error
	SetActive(ctx context.Context, id int64) error

	WithTx(tx *sql.Tx) CodexEndpointStore
}

// SQLiteCodexEndpointStore 实现 CodexEndpointStore 接口。
type SQLiteCodexEndpointStore struct {
	db *sql.DB
	tx *sql.Tx
	mu sync.RWMutex
}

func NewSQLiteCodexEndpointStore(db *sql.DB) *SQLiteCodexEndpointStore {
	return &SQLiteCodexEndpointStore{db: db}
}

func (s *SQLiteCodexEndpointStore) WithTx(tx *sql.Tx) CodexEndpointStore {
	return &SQLiteCodexEndpointStore{db: s.db, tx: tx}
}

func (s *SQLiteCodexEndpointStore) getQuerier() interface {
	QueryContext(ctx context.Context, query string, args ...interface{}) (*sql.Rows, error)
	QueryRowContext(ctx context.Context, query string, args ...interface{}) *sql.Row
	ExecContext(ctx context.Context, query string, args ...interface{}) (sql.Result, error)
} {
	if s.tx != nil {
		return s.tx
	}
	return s.db
}

const codexEndpointColumns = `id, channel_id, COALESCE(credential_group_id, 0), COALESCE(api_key, ''), name, base_url, COALESCE(is_active, 0), COALESCE(failover_enabled, 1), COALESCE(skip_health_check, 0), COALESCE(priority, 0), COALESCE(timeout_seconds, 0), COALESCE(cost_multiplier, 1.0), COALESCE(input_cost_multiplier, 1.0), COALESCE(output_cost_multiplier, 1.0), COALESCE(cache_read_cost_multiplier, 1.0), COALESCE(billing_output_token_source, 'output_tokens'), COALESCE(model_multipliers, '[]'), COALESCE(created_at, ''), COALESCE(updated_at, '')`

func (s *SQLiteCodexEndpointStore) Create(ctx context.Context, record *CodexEndpointRecord) (*CodexEndpointRecord, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if record == nil {
		return nil, fmt.Errorf("record 不能为空")
	}
	if record.ChannelID <= 0 {
		return nil, fmt.Errorf("channel_id 无效")
	}
	if record.Name == "" {
		return nil, fmt.Errorf("name 不能为空")
	}
	if record.BaseURL == "" {
		return nil, fmt.Errorf("base_url 不能为空")
	}

	query := `INSERT INTO codex_endpoints (channel_id, credential_group_id, api_key, name, base_url, is_active, failover_enabled, skip_health_check, priority, timeout_seconds, cost_multiplier, input_cost_multiplier, output_cost_multiplier, cache_read_cost_multiplier, billing_output_token_source, model_multipliers) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`
	result, err := s.getQuerier().ExecContext(ctx, query, record.ChannelID, nullInt64IfZero(record.CredentialGroupID), record.ApiKey, record.Name, record.BaseURL, boolToInt(record.IsActive), boolToInt(record.FailoverEnabled), boolToInt(record.SkipHealthCheck), record.Priority, record.TimeoutSeconds, record.CostMultiplier, record.InputCostMultiplier, record.OutputCostMultiplier, record.CacheReadCostMultiplier, defaultBillingOutputTokenSource(record.BillingOutputTokenSource), encodeCodexModelMultipliers(record.ModelMultipliers))
	if err != nil {
		return nil, fmt.Errorf("创建 Codex 端点失败: %w", err)
	}

	id, err := result.LastInsertId()
	if err != nil {
		return nil, fmt.Errorf("获取插入 ID 失败: %w", err)
	}

	queryByID := "SELECT " + codexEndpointColumns + " FROM codex_endpoints WHERE id = ?"
	return s.scanRecord(s.getQuerier().QueryRowContext(ctx, queryByID, id))
}

func (s *SQLiteCodexEndpointStore) GetByID(ctx context.Context, id int64) (*CodexEndpointRecord, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	query := "SELECT " + codexEndpointColumns + " FROM codex_endpoints WHERE id = ?"
	return s.scanRecord(s.getQuerier().QueryRowContext(ctx, query, id))
}

func (s *SQLiteCodexEndpointStore) GetByChannelAndURL(ctx context.Context, channelID int64, baseURL string) (*CodexEndpointRecord, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	query := "SELECT " + codexEndpointColumns + " FROM codex_endpoints WHERE channel_id = ? AND base_url = ?"
	return s.scanRecord(s.getQuerier().QueryRowContext(ctx, query, channelID, baseURL))
}

func (s *SQLiteCodexEndpointStore) GetByChannelAndName(ctx context.Context, channelID int64, name string) (*CodexEndpointRecord, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	query := "SELECT " + codexEndpointColumns + " FROM codex_endpoints WHERE channel_id = ? AND name = ?"
	return s.scanRecord(s.getQuerier().QueryRowContext(ctx, query, channelID, name))
}

func (s *SQLiteCodexEndpointStore) GetByChannelCredentialGroupAndURL(ctx context.Context, channelID, credentialGroupID int64, baseURL string) (*CodexEndpointRecord, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	query := "SELECT " + codexEndpointColumns + " FROM codex_endpoints WHERE channel_id = ? AND COALESCE(credential_group_id, 0) = ? AND base_url = ?"
	return s.scanRecord(s.getQuerier().QueryRowContext(ctx, query, channelID, positiveInt64OrZero(credentialGroupID), baseURL))
}

func (s *SQLiteCodexEndpointStore) GetByChannelCredentialGroupAndName(ctx context.Context, channelID, credentialGroupID int64, name string) (*CodexEndpointRecord, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	query := "SELECT " + codexEndpointColumns + " FROM codex_endpoints WHERE channel_id = ? AND COALESCE(credential_group_id, 0) = ? AND name = ?"
	return s.scanRecord(s.getQuerier().QueryRowContext(ctx, query, channelID, positiveInt64OrZero(credentialGroupID), name))
}

func (s *SQLiteCodexEndpointStore) GetByChannelCredentialGroupAndPriority(ctx context.Context, channelID, credentialGroupID int64, priority int) (*CodexEndpointRecord, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	query := "SELECT " + codexEndpointColumns + " FROM codex_endpoints WHERE channel_id = ? AND COALESCE(credential_group_id, 0) = ? AND priority = ?"
	return s.scanRecord(s.getQuerier().QueryRowContext(ctx, query, channelID, positiveInt64OrZero(credentialGroupID), priority))
}

func (s *SQLiteCodexEndpointStore) GetByChannelAndPriority(ctx context.Context, channelID int64, priority int) (*CodexEndpointRecord, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	query := "SELECT " + codexEndpointColumns + " FROM codex_endpoints WHERE channel_id = ? AND priority = ?"
	return s.scanRecord(s.getQuerier().QueryRowContext(ctx, query, channelID, priority))
}

func (s *SQLiteCodexEndpointStore) GetMaxPriorityByChannelCredentialGroup(ctx context.Context, channelID, credentialGroupID int64) (int, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var maxPriority sql.NullInt64
	err := s.getQuerier().QueryRowContext(ctx, `SELECT MAX(priority) FROM codex_endpoints WHERE channel_id = ? AND COALESCE(credential_group_id, 0) = ?`, channelID, positiveInt64OrZero(credentialGroupID)).Scan(&maxPriority)
	if err != nil {
		return 0, fmt.Errorf("获取 Codex 凭证分组最大优先级失败: %w", err)
	}
	if !maxPriority.Valid {
		return -1, nil
	}
	return int(maxPriority.Int64), nil
}

func (s *SQLiteCodexEndpointStore) GetMaxPriorityByChannel(ctx context.Context, channelID int64) (int, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var maxPriority sql.NullInt64
	err := s.getQuerier().QueryRowContext(ctx, `SELECT MAX(priority) FROM codex_endpoints WHERE channel_id = ?`, channelID).Scan(&maxPriority)
	if err != nil {
		return 0, fmt.Errorf("获取 Codex 渠道最大优先级失败: %w", err)
	}
	if !maxPriority.Valid {
		return -1, nil
	}
	return int(maxPriority.Int64), nil
}

func (s *SQLiteCodexEndpointStore) GetActiveByChannelID(ctx context.Context, channelID int64) (*CodexEndpointRecord, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	query := "SELECT " + codexEndpointColumns + " FROM codex_endpoints WHERE channel_id = ? AND is_active = 1 LIMIT 1"
	return s.scanRecord(s.getQuerier().QueryRowContext(ctx, query, channelID))
}

func (s *SQLiteCodexEndpointStore) ListByChannelID(ctx context.Context, channelID int64) ([]*CodexEndpointRecord, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	query := "SELECT " + codexEndpointColumns + " FROM codex_endpoints WHERE channel_id = ? ORDER BY created_at ASC, id ASC"
	rows, err := s.getQuerier().QueryContext(ctx, query, channelID)
	if err != nil {
		return nil, fmt.Errorf("获取 Codex 端点列表失败: %w", err)
	}
	defer rows.Close()

	result := make([]*CodexEndpointRecord, 0)
	for rows.Next() {
		record, err := s.scanRow(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, record)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("读取 Codex 端点列表失败: %w", err)
	}

	return result, nil
}

func (s *SQLiteCodexEndpointStore) Update(ctx context.Context, id int64, name, baseURL string, credentialGroupID int64, apiKey string, failoverEnabled bool, skipHealthCheck bool, priority, timeoutSeconds int, costMultiplier, inputCostMultiplier, outputCostMultiplier, cacheReadCostMultiplier float64, billingOutputTokenSource string, modelMultipliers []CodexModelMultiplierRecord) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if name == "" {
		return fmt.Errorf("name 不能为空")
	}
	if baseURL == "" {
		return fmt.Errorf("base_url 不能为空")
	}

	result, err := s.getQuerier().ExecContext(ctx, `UPDATE codex_endpoints SET name = ?, base_url = ?, credential_group_id = ?, api_key = ?, failover_enabled = ?, skip_health_check = ?, priority = ?, timeout_seconds = ?, cost_multiplier = ?, input_cost_multiplier = ?, output_cost_multiplier = ?, cache_read_cost_multiplier = ?, billing_output_token_source = ?, model_multipliers = ? WHERE id = ?`, name, baseURL, nullInt64IfZero(credentialGroupID), apiKey, boolToInt(failoverEnabled), boolToInt(skipHealthCheck), priority, timeoutSeconds, costMultiplier, inputCostMultiplier, outputCostMultiplier, cacheReadCostMultiplier, defaultBillingOutputTokenSource(billingOutputTokenSource), encodeCodexModelMultipliers(modelMultipliers), id)
	if err != nil {
		return fmt.Errorf("更新 Codex 端点失败: %w", err)
	}

	affected, _ := result.RowsAffected()
	if affected == 0 {
		return fmt.Errorf("Codex 端点不存在: %d", id)
	}
	return nil
}

func (s *SQLiteCodexEndpointStore) DeleteByID(ctx context.Context, id int64) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	result, err := s.getQuerier().ExecContext(ctx, `DELETE FROM codex_endpoints WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("删除 Codex 端点失败: %w", err)
	}

	affected, _ := result.RowsAffected()
	if affected == 0 {
		return fmt.Errorf("Codex 端点不存在: %d", id)
	}
	return nil
}

// SetActive 在端点所属渠道内单活：先把同 channel_id 下其他端点置 0，再把目标置 1。
func (s *SQLiteCodexEndpointStore) SetActive(ctx context.Context, id int64) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	// 先查到 channel_id
	var channelID int64
	if err := s.getQuerier().QueryRowContext(ctx, `SELECT channel_id FROM codex_endpoints WHERE id = ?`, id).Scan(&channelID); err != nil {
		if err == sql.ErrNoRows {
			return fmt.Errorf("Codex 端点不存在: %d", id)
		}
		return fmt.Errorf("查询 Codex 端点 channel_id 失败: %w", err)
	}

	if _, err := s.getQuerier().ExecContext(ctx, `UPDATE codex_endpoints SET is_active = 0 WHERE channel_id = ?`, channelID); err != nil {
		return fmt.Errorf("清空渠道激活端点失败: %w", err)
	}

	if _, err := s.getQuerier().ExecContext(ctx, `UPDATE codex_endpoints SET is_active = 1 WHERE id = ?`, id); err != nil {
		return fmt.Errorf("设置激活端点失败: %w", err)
	}
	return nil
}

func (s *SQLiteCodexEndpointStore) scanRecord(row *sql.Row) (*CodexEndpointRecord, error) {
	return scanCodexEndpointRow(row)
}

func (s *SQLiteCodexEndpointStore) scanRow(scanner interface {
	Scan(dest ...interface{}) error
}) (*CodexEndpointRecord, error) {
	return scanCodexEndpointRow(scanner)
}

func scanCodexEndpointRow(scanner interface {
	Scan(dest ...interface{}) error
}) (*CodexEndpointRecord, error) {
	var (
		record                   CodexEndpointRecord
		isActive                 int
		failoverEnabled          int
		skipHealthCheck          int
		priority                 int
		timeoutSeconds           int
		costMultiplier           float64
		inputCostMultiplier      float64
		outputCostMultiplier     float64
		cacheReadCostMultiplier  float64
		billingOutputTokenSource string
		modelMultipliersRaw      string
		createdAt                string
		updatedAt                string
	)

	err := scanner.Scan(
		&record.ID,
		&record.ChannelID,
		&record.CredentialGroupID,
		&record.ApiKey,
		&record.Name,
		&record.BaseURL,
		&isActive,
		&failoverEnabled,
		&skipHealthCheck,
		&priority,
		&timeoutSeconds,
		&costMultiplier,
		&inputCostMultiplier,
		&outputCostMultiplier,
		&cacheReadCostMultiplier,
		&billingOutputTokenSource,
		&modelMultipliersRaw,
		&createdAt,
		&updatedAt,
	)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("读取 Codex 端点失败: %w", err)
	}

	record.IsActive = isActive != 0
	record.FailoverEnabled = failoverEnabled != 0
	record.SkipHealthCheck = skipHealthCheck != 0
	record.Priority = priority
	record.TimeoutSeconds = timeoutSeconds
	record.CostMultiplier = costMultiplier
	record.InputCostMultiplier = inputCostMultiplier
	record.OutputCostMultiplier = outputCostMultiplier
	record.CacheReadCostMultiplier = cacheReadCostMultiplier
	record.BillingOutputTokenSource = defaultBillingOutputTokenSource(billingOutputTokenSource)
	record.ModelMultipliers = decodeCodexModelMultipliers(modelMultipliersRaw)
	record.CreatedAt = parseSQLiteDateTime(createdAt)
	record.UpdatedAt = parseSQLiteDateTime(updatedAt)
	return &record, nil
}

func nullInt64IfZero(value int64) any {
	if value <= 0 {
		return nil
	}
	return value
}

func positiveInt64OrZero(value int64) int64 {
	if value <= 0 {
		return 0
	}
	return value
}

func defaultBillingOutputTokenSource(source string) string {
	switch source {
	case "reasoning_tokens", "visible_output_tokens":
		return source
	default:
		return "output_tokens"
	}
}

func encodeCodexModelMultipliers(rules []CodexModelMultiplierRecord) string {
	if len(rules) == 0 {
		return "[]"
	}
	data, err := json.Marshal(rules)
	if err != nil {
		return "[]"
	}
	return string(data)
}

func decodeCodexModelMultipliers(raw string) []CodexModelMultiplierRecord {
	if raw == "" {
		return nil
	}
	var rules []CodexModelMultiplierRecord
	if err := json.Unmarshal([]byte(raw), &rules); err != nil {
		return nil
	}
	return rules
}
