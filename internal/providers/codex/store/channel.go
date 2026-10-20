package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/wsyangzy/Vane/internal/routing"
)

// CodexChannelRecord 表示 Codex 渠道配置（共用配置：API Key、模型、推理强度、1M 上下文等）。
// 同一时刻全局最多一行 IsActive=true。
type CodexChannelRecord struct {
	ID int64 `json:"id"`

	Name            string            `json:"name"`
	ProviderType    string            `json:"provider_type"`
	ApiKey          string            `json:"api_key"`
	Model           string            `json:"model"`
	ModelMapping    map[string]string `json:"model_mapping"`
	ReasoningEffort string            `json:"model_reasoning_effort"`
	Enable1MContext bool              `json:"enable_1m_context"`
	Website         string            `json:"website,omitempty"`
	IsActive        bool              `json:"is_active"`
	Priority        int               `json:"priority"`
	FailoverEnabled bool              `json:"failover_enabled"`
	// NonStreamRoute 渠道不接非流请求时，非流请求改道到的目标；nil 表示关闭。
	// 只经 SetNonStreamRoute 写入，UpdateByID 不改动该列。
	NonStreamRoute *routing.NonStreamRoute `json:"non_stream_route,omitempty"`
	// PrivacyLevel 渠道隐私级别：""=跟随全局 / third_party / trusted。
	// 只经 SetPrivacyLevel 写入，UpdateByID 不改动该列。
	PrivacyLevel string `json:"privacy_level,omitempty"`

	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// CodexChannelStore 定义渠道存储接口。
type CodexChannelStore interface {
	Create(ctx context.Context, record *CodexChannelRecord) (*CodexChannelRecord, error)
	GetByID(ctx context.Context, id int64) (*CodexChannelRecord, error)
	GetByName(ctx context.Context, name string) (*CodexChannelRecord, error)
	GetActive(ctx context.Context) (*CodexChannelRecord, error)
	List(ctx context.Context) ([]*CodexChannelRecord, error)
	UpdateByID(ctx context.Context, id int64, record *CodexChannelRecord) error
	DeleteByID(ctx context.Context, id int64) error
	SetActive(ctx context.Context, id int64) error
	SetFailoverEnabled(ctx context.Context, id int64, enabled bool) error

	WithTx(tx *sql.Tx) CodexChannelStore
}

// SQLiteCodexChannelStore 实现 CodexChannelStore 接口。
type SQLiteCodexChannelStore struct {
	db *sql.DB
	tx *sql.Tx
	mu sync.RWMutex
}

func NewSQLiteCodexChannelStore(db *sql.DB) *SQLiteCodexChannelStore {
	return &SQLiteCodexChannelStore{db: db}
}

// BeginTx starts a transaction for service-level atomic priority updates.
func (s *SQLiteCodexChannelStore) BeginTx(ctx context.Context) (*sql.Tx, error) {
	if s == nil || s.db == nil {
		return nil, fmt.Errorf("Codex 渠道数据库未初始化")
	}
	return s.db.BeginTx(ctx, nil)
}

func (s *SQLiteCodexChannelStore) WithTx(tx *sql.Tx) CodexChannelStore {
	return &SQLiteCodexChannelStore{db: s.db, tx: tx}
}

func (s *SQLiteCodexChannelStore) getQuerier() interface {
	QueryContext(ctx context.Context, query string, args ...interface{}) (*sql.Rows, error)
	QueryRowContext(ctx context.Context, query string, args ...interface{}) *sql.Row
	ExecContext(ctx context.Context, query string, args ...interface{}) (sql.Result, error)
} {
	if s.tx != nil {
		return s.tx
	}
	return s.db
}

const codexChannelColumns = `id, name, provider_type, api_key, model, model_reasoning_effort,
		COALESCE(model_mapping, '{}'), COALESCE(enable_1m_context, 0), COALESCE(website, ''), COALESCE(is_active, 0), COALESCE(priority, 0), COALESCE(failover_enabled, 1),
		COALESCE(non_stream_route, ''), COALESCE(privacy_level, ''), created_at, updated_at`

func (s *SQLiteCodexChannelStore) Create(ctx context.Context, record *CodexChannelRecord) (*CodexChannelRecord, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if record == nil {
		return nil, fmt.Errorf("record 不能为空")
	}
	if record.Name == "" {
		return nil, fmt.Errorf("渠道名不能为空")
	}
	modelMappingJSON, err := codexModelMappingToJSON(record.ModelMapping)
	if err != nil {
		return nil, err
	}

	query := `
		INSERT INTO codex_channels (
			name, provider_type, api_key, model, model_mapping, model_reasoning_effort,
			enable_1m_context, website, is_active, priority, failover_enabled
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`
	result, err := s.getQuerier().ExecContext(
		ctx,
		query,
		record.Name,
		record.ProviderType,
		record.ApiKey,
		record.Model,
		modelMappingJSON,
		record.ReasoningEffort,
		boolToInt(record.Enable1MContext),
		nullIfEmpty(record.Website),
		boolToInt(record.IsActive),
		record.Priority,
		boolToInt(record.FailoverEnabled),
	)
	if err != nil {
		return nil, fmt.Errorf("创建 Codex 渠道失败: %w", err)
	}

	id, err := result.LastInsertId()
	if err != nil {
		return nil, fmt.Errorf("获取插入 ID 失败: %w", err)
	}

	queryByID := "SELECT " + codexChannelColumns + " FROM codex_channels WHERE id = ?"
	return s.scanRecord(s.getQuerier().QueryRowContext(ctx, queryByID, id))
}

func (s *SQLiteCodexChannelStore) GetByID(ctx context.Context, id int64) (*CodexChannelRecord, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	query := "SELECT " + codexChannelColumns + " FROM codex_channels WHERE id = ?"
	return s.scanRecord(s.getQuerier().QueryRowContext(ctx, query, id))
}

func (s *SQLiteCodexChannelStore) GetByName(ctx context.Context, name string) (*CodexChannelRecord, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	query := "SELECT " + codexChannelColumns + " FROM codex_channels WHERE name = ?"
	return s.scanRecord(s.getQuerier().QueryRowContext(ctx, query, name))
}

func (s *SQLiteCodexChannelStore) GetActive(ctx context.Context) (*CodexChannelRecord, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	query := "SELECT " + codexChannelColumns + " FROM codex_channels WHERE is_active = 1 LIMIT 1"
	return s.scanRecord(s.getQuerier().QueryRowContext(ctx, query))
}

func (s *SQLiteCodexChannelStore) List(ctx context.Context) ([]*CodexChannelRecord, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	query := "SELECT " + codexChannelColumns + " FROM codex_channels ORDER BY is_active DESC, priority ASC, created_at ASC, name ASC"

	rows, err := s.getQuerier().QueryContext(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("获取 Codex 渠道列表失败: %w", err)
	}
	defer rows.Close()

	result := make([]*CodexChannelRecord, 0)
	for rows.Next() {
		record, err := s.scanRow(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, record)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("读取 Codex 渠道列表失败: %w", err)
	}

	return result, nil
}

func (s *SQLiteCodexChannelStore) UpdateByID(ctx context.Context, id int64, record *CodexChannelRecord) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if record == nil {
		return fmt.Errorf("record 不能为空")
	}
	if record.Name == "" {
		return fmt.Errorf("渠道名不能为空")
	}
	modelMappingJSON, err := codexModelMappingToJSON(record.ModelMapping)
	if err != nil {
		return err
	}

	query := `
		UPDATE codex_channels
		SET name = ?, provider_type = ?, api_key = ?, model = ?, model_mapping = ?, model_reasoning_effort = ?,
		    enable_1m_context = ?, website = ?, priority = ?, failover_enabled = ?
		WHERE id = ?
	`
	result, err := s.getQuerier().ExecContext(
		ctx,
		query,
		record.Name,
		record.ProviderType,
		record.ApiKey,
		record.Model,
		modelMappingJSON,
		record.ReasoningEffort,
		boolToInt(record.Enable1MContext),
		nullIfEmpty(record.Website),
		record.Priority,
		boolToInt(record.FailoverEnabled),
		id,
	)
	if err != nil {
		return fmt.Errorf("更新 Codex 渠道失败: %w", err)
	}

	affected, _ := result.RowsAffected()
	if affected == 0 {
		return fmt.Errorf("Codex 渠道不存在: %d", id)
	}
	return nil
}

func (s *SQLiteCodexChannelStore) DeleteByID(ctx context.Context, id int64) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	result, err := s.getQuerier().ExecContext(ctx, `DELETE FROM codex_channels WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("删除 Codex 渠道失败: %w", err)
	}

	affected, _ := result.RowsAffected()
	if affected == 0 {
		return fmt.Errorf("Codex 渠道不存在: %d", id)
	}
	return nil
}

// SetActive 通过事务内先清空再置位确保全局单活。
// 注意：部分唯一索引 idx_codex_channels_single_active 强制约束，必须先清空再设置。
func (s *SQLiteCodexChannelStore) SetActive(ctx context.Context, id int64) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.tx != nil {
		return setActiveCodexChannel(ctx, s.tx, id)
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("开启 Codex 渠道激活事务失败: %w", err)
	}
	if err := setActiveCodexChannel(ctx, tx, id); err != nil {
		_ = tx.Rollback()
		return err
	}
	if err := tx.Commit(); err != nil {
		_ = tx.Rollback()
		return fmt.Errorf("提交 Codex 渠道激活事务失败: %w", err)
	}
	return nil
}

func setActiveCodexChannel(ctx context.Context, executor interface {
	ExecContext(context.Context, string, ...interface{}) (sql.Result, error)
}, id int64) error {
	if _, err := executor.ExecContext(ctx, `UPDATE codex_channels SET is_active = 0`); err != nil {
		return fmt.Errorf("清空激活渠道失败: %w", err)
	}
	result, err := executor.ExecContext(ctx, `UPDATE codex_channels SET is_active = 1 WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("设置激活渠道失败: %w", err)
	}
	affected, _ := result.RowsAffected()
	if affected == 0 {
		return fmt.Errorf("Codex 渠道不存在: %d", id)
	}
	return nil
}

// ClearActive clears a known active channel without deleting it. Deletion uses
// this to make an empty runtime state observable before the record is removed.
func (s *SQLiteCodexChannelStore) ClearActive(ctx context.Context, id int64) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	result, err := s.getQuerier().ExecContext(ctx, `UPDATE codex_channels SET is_active = 0 WHERE id = ? AND is_active = 1`, id)
	if err != nil {
		return fmt.Errorf("清空 Codex 激活渠道失败: %w", err)
	}
	affected, _ := result.RowsAffected()
	if affected == 0 {
		return fmt.Errorf("Codex 渠道不存在或未激活: %d", id)
	}
	return nil
}

func (s *SQLiteCodexChannelStore) SetFailoverEnabled(ctx context.Context, id int64, enabled bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	result, err := s.getQuerier().ExecContext(ctx, `UPDATE codex_channels SET failover_enabled = ? WHERE id = ?`, boolToInt(enabled), id)
	if err != nil {
		return fmt.Errorf("更新 Codex 渠道故障转移状态失败: %w", err)
	}
	affected, _ := result.RowsAffected()
	if affected == 0 {
		return fmt.Errorf("Codex 渠道不存在: %d", id)
	}
	return nil
}

// SetPrivacyLevel 写入渠道隐私级别；空串表示跟随全局（落库为 NULL）。取值由调用方校验。
func (s *SQLiteCodexChannelStore) SetPrivacyLevel(ctx context.Context, id int64, level string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	result, err := s.getQuerier().ExecContext(ctx, `UPDATE codex_channels SET privacy_level = ? WHERE id = ?`, nullIfEmpty(level), id)
	if err != nil {
		return fmt.Errorf("更新 Codex 渠道隐私级别失败: %w", err)
	}
	if affected, _ := result.RowsAffected(); affected == 0 {
		return fmt.Errorf("Codex 渠道不存在: %d", id)
	}
	return nil
}

// SetNonStreamRoute 写入渠道的非流改道配置；route 为 nil 或没有目标时落库为 NULL（关闭）。
func (s *SQLiteCodexChannelStore) SetNonStreamRoute(ctx context.Context, id int64, route *routing.NonStreamRoute) error {
	encoded, err := routing.EncodeNonStreamRoute(route)
	if err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	result, err := s.getQuerier().ExecContext(ctx, `UPDATE codex_channels SET non_stream_route = ? WHERE id = ?`, nullIfEmpty(encoded), id)
	if err != nil {
		return fmt.Errorf("更新 Codex 渠道非流改道失败: %w", err)
	}
	affected, _ := result.RowsAffected()
	if affected == 0 {
		return fmt.Errorf("Codex 渠道不存在: %d", id)
	}
	return nil
}

func (s *SQLiteCodexChannelStore) scanRecord(row *sql.Row) (*CodexChannelRecord, error) {
	return scanCodexChannelRow(row)
}

func (s *SQLiteCodexChannelStore) scanRow(scanner interface {
	Scan(dest ...interface{}) error
}) (*CodexChannelRecord, error) {
	return scanCodexChannelRow(scanner)
}

func scanCodexChannelRow(scanner interface {
	Scan(dest ...interface{}) error
}) (*CodexChannelRecord, error) {
	var (
		record             CodexChannelRecord
		enable1M           int
		modelMappingJSON   string
		website            string
		isActive           int
		failoverEnabled    int
		nonStreamRouteJSON string
		createdAt          string
		updatedAt          string
	)

	err := scanner.Scan(
		&record.ID,
		&record.Name,
		&record.ProviderType,
		&record.ApiKey,
		&record.Model,
		&record.ReasoningEffort,
		&modelMappingJSON,
		&enable1M,
		&website,
		&isActive,
		&record.Priority,
		&failoverEnabled,
		&nonStreamRouteJSON,
		&record.PrivacyLevel,
		&createdAt,
		&updatedAt,
	)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("读取 Codex 渠道失败: %w", err)
	}

	modelMapping, err := codexModelMappingFromJSON(modelMappingJSON)
	if err != nil {
		return nil, err
	}
	record.ModelMapping = modelMapping
	// 非流改道配置损坏时按关闭处理：它只是改道增强，不能让整张渠道表读不出来
	record.NonStreamRoute, _ = routing.ParseNonStreamRoute(nonStreamRouteJSON)
	record.Enable1MContext = enable1M != 0
	record.Website = website
	record.IsActive = isActive != 0
	record.FailoverEnabled = failoverEnabled != 0
	record.CreatedAt = parseSQLiteDateTime(createdAt)
	record.UpdatedAt = parseSQLiteDateTime(updatedAt)
	return &record, nil
}

func codexModelMappingToJSON(modelMapping map[string]string) (string, error) {
	if len(modelMapping) == 0 {
		return "{}", nil
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
	if len(normalized) == 0 {
		return "{}", nil
	}

	data, err := json.Marshal(normalized)
	if err != nil {
		return "", fmt.Errorf("序列化 Codex 模型映射失败: %w", err)
	}
	return string(data), nil
}

func codexModelMappingFromJSON(raw string) (map[string]string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return map[string]string{}, nil
	}

	var parsed map[string]string
	if err := json.Unmarshal([]byte(raw), &parsed); err != nil {
		return nil, fmt.Errorf("解析 Codex 模型映射失败: %w", err)
	}
	if parsed == nil {
		return map[string]string{}, nil
	}

	normalized := make(map[string]string, len(parsed))
	for source, target := range parsed {
		source = strings.TrimSpace(source)
		target = strings.TrimSpace(target)
		if source == "" || target == "" {
			continue
		}
		normalized[source] = target
	}
	return normalized, nil
}
