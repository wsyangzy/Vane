package store

import (
	"context"
	"database/sql"
	"fmt"
	"sync"
	"time"
)

// CodexProviderRecord 表示数据库中的 Codex 提供商配置。
type CodexProviderRecord struct {
	ID int64 `json:"id"`

	Name             string `json:"name"`
	ProviderType     string `json:"provider_type"`
	BaseURL          string `json:"base_url"`
	ApiKey           string `json:"api_key"`
	Model            string `json:"model"`
	ReasoningEffort  string `json:"model_reasoning_effort"`
	Website          string `json:"website,omitempty"`
	IsActive         bool   `json:"is_active"`

	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// CodexProviderStore 定义 Codex 提供商存储接口。
type CodexProviderStore interface {
	Create(ctx context.Context, record *CodexProviderRecord) (*CodexProviderRecord, error)
	GetByID(ctx context.Context, id int64) (*CodexProviderRecord, error)
	GetByName(ctx context.Context, name string) (*CodexProviderRecord, error)
	GetActive(ctx context.Context) (*CodexProviderRecord, error)
	List(ctx context.Context) ([]*CodexProviderRecord, error)
	UpdateByID(ctx context.Context, id int64, record *CodexProviderRecord) error
	DeleteByID(ctx context.Context, id int64) error
	SetActive(ctx context.Context, id int64) error

	WithTx(tx *sql.Tx) CodexProviderStore
}

// SQLiteCodexProviderStore 实现 CodexProviderStore 接口。
type SQLiteCodexProviderStore struct {
	db *sql.DB
	tx *sql.Tx
	mu sync.RWMutex
}

func NewSQLiteCodexProviderStore(db *sql.DB) *SQLiteCodexProviderStore {
	return &SQLiteCodexProviderStore{db: db}
}

func (s *SQLiteCodexProviderStore) WithTx(tx *sql.Tx) CodexProviderStore {
	return &SQLiteCodexProviderStore{db: s.db, tx: tx}
}

func (s *SQLiteCodexProviderStore) getQuerier() interface {
	QueryContext(ctx context.Context, query string, args ...interface{}) (*sql.Rows, error)
	QueryRowContext(ctx context.Context, query string, args ...interface{}) *sql.Row
	ExecContext(ctx context.Context, query string, args ...interface{}) (sql.Result, error)
} {
	if s.tx != nil {
		return s.tx
	}
	return s.db
}

func (s *SQLiteCodexProviderStore) Create(ctx context.Context, record *CodexProviderRecord) (*CodexProviderRecord, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if record == nil {
		return nil, fmt.Errorf("record 不能为空")
	}
	if record.Name == "" {
		return nil, fmt.Errorf("提供商标识不能为空")
	}

	query := `
		INSERT INTO codex_providers (
			name, provider_type, base_url, api_key, model, model_reasoning_effort, website, is_active
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?)
	`

	result, err := s.getQuerier().ExecContext(
		ctx,
		query,
		record.Name,
		record.ProviderType,
		record.BaseURL,
		record.ApiKey,
		record.Model,
		record.ReasoningEffort,
		nullIfEmpty(record.Website),
		boolToInt(record.IsActive),
	)
	if err != nil {
		return nil, fmt.Errorf("创建 Codex 提供商失败: %w", err)
	}

	id, err := result.LastInsertId()
	if err != nil {
		return nil, fmt.Errorf("获取插入 ID 失败: %w", err)
	}

	queryByID := `
		SELECT id, name, provider_type, base_url, api_key, model, model_reasoning_effort, COALESCE(website, ''), COALESCE(is_active, 0), created_at, updated_at
		FROM codex_providers
		WHERE id = ?
	`

	return s.scanRecord(s.getQuerier().QueryRowContext(ctx, queryByID, id))
}

func (s *SQLiteCodexProviderStore) GetByID(ctx context.Context, id int64) (*CodexProviderRecord, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	query := `
		SELECT id, name, provider_type, base_url, api_key, model, model_reasoning_effort, COALESCE(website, ''), COALESCE(is_active, 0), created_at, updated_at
		FROM codex_providers
		WHERE id = ?
	`
	return s.scanRecord(s.getQuerier().QueryRowContext(ctx, query, id))
}

func (s *SQLiteCodexProviderStore) GetByName(ctx context.Context, name string) (*CodexProviderRecord, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	query := `
		SELECT id, name, provider_type, base_url, api_key, model, model_reasoning_effort, COALESCE(website, ''), COALESCE(is_active, 0), created_at, updated_at
		FROM codex_providers
		WHERE name = ?
	`
	return s.scanRecord(s.getQuerier().QueryRowContext(ctx, query, name))
}

func (s *SQLiteCodexProviderStore) GetActive(ctx context.Context) (*CodexProviderRecord, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	query := `
		SELECT id, name, provider_type, base_url, api_key, model, model_reasoning_effort, COALESCE(website, ''), COALESCE(is_active, 0), created_at, updated_at
		FROM codex_providers
		WHERE is_active = 1
		LIMIT 1
	`
	return s.scanRecord(s.getQuerier().QueryRowContext(ctx, query))
}

func (s *SQLiteCodexProviderStore) List(ctx context.Context) ([]*CodexProviderRecord, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	query := `
		SELECT id, name, provider_type, base_url, api_key, model, model_reasoning_effort, COALESCE(website, ''), COALESCE(is_active, 0), created_at, updated_at
		FROM codex_providers
		ORDER BY is_active DESC, created_at DESC, name ASC
	`

	rows, err := s.getQuerier().QueryContext(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("获取 Codex 提供商列表失败: %w", err)
	}
	defer rows.Close()

	result := make([]*CodexProviderRecord, 0)
	for rows.Next() {
		record, err := s.scanRow(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, record)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("读取 Codex 提供商列表失败: %w", err)
	}

	return result, nil
}

func (s *SQLiteCodexProviderStore) UpdateByID(ctx context.Context, id int64, record *CodexProviderRecord) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if record == nil {
		return fmt.Errorf("record 不能为空")
	}
	if record.Name == "" {
		return fmt.Errorf("提供商标识不能为空")
	}

	query := `
		UPDATE codex_providers
		SET name = ?, provider_type = ?, base_url = ?, api_key = ?, model = ?, model_reasoning_effort = ?, website = ?
		WHERE id = ?
	`
	result, err := s.getQuerier().ExecContext(
		ctx,
		query,
		record.Name,
		record.ProviderType,
		record.BaseURL,
		record.ApiKey,
		record.Model,
		record.ReasoningEffort,
		nullIfEmpty(record.Website),
		id,
	)
	if err != nil {
		return fmt.Errorf("更新 Codex 提供商失败: %w", err)
	}

	affected, _ := result.RowsAffected()
	if affected == 0 {
		return fmt.Errorf("Codex 提供商不存在: %d", id)
	}
	return nil
}

func (s *SQLiteCodexProviderStore) DeleteByID(ctx context.Context, id int64) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	result, err := s.getQuerier().ExecContext(ctx, `DELETE FROM codex_providers WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("删除 Codex 提供商失败: %w", err)
	}

	affected, _ := result.RowsAffected()
	if affected == 0 {
		return fmt.Errorf("Codex 提供商不存在: %d", id)
	}
	return nil
}

func (s *SQLiteCodexProviderStore) SetActive(ctx context.Context, id int64) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, err := s.getQuerier().ExecContext(ctx, `UPDATE codex_providers SET is_active = 0`); err != nil {
		return fmt.Errorf("清空激活提供商失败: %w", err)
	}

	result, err := s.getQuerier().ExecContext(ctx, `UPDATE codex_providers SET is_active = 1 WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("设置激活提供商失败: %w", err)
	}

	affected, _ := result.RowsAffected()
	if affected == 0 {
		return fmt.Errorf("Codex 提供商不存在: %d", id)
	}
	return nil
}

func (s *SQLiteCodexProviderStore) scanRecord(row *sql.Row) (*CodexProviderRecord, error) {
	var (
		record    CodexProviderRecord
		website   string
		isActive  int
		createdAt string
		updatedAt string
	)

	err := row.Scan(
		&record.ID,
		&record.Name,
		&record.ProviderType,
		&record.BaseURL,
		&record.ApiKey,
		&record.Model,
		&record.ReasoningEffort,
		&website,
		&isActive,
		&createdAt,
		&updatedAt,
	)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("读取 Codex 提供商失败: %w", err)
	}

	record.Website = website
	record.IsActive = isActive != 0
	record.CreatedAt = parseSQLiteDateTime(createdAt)
	record.UpdatedAt = parseSQLiteDateTime(updatedAt)
	return &record, nil
}

func (s *SQLiteCodexProviderStore) scanRow(scanner interface {
	Scan(dest ...interface{}) error
}) (*CodexProviderRecord, error) {
	var (
		record    CodexProviderRecord
		website   string
		isActive  int
		createdAt string
		updatedAt string
	)

	if err := scanner.Scan(
		&record.ID,
		&record.Name,
		&record.ProviderType,
		&record.BaseURL,
		&record.ApiKey,
		&record.Model,
		&record.ReasoningEffort,
		&website,
		&isActive,
		&createdAt,
		&updatedAt,
	); err != nil {
		return nil, fmt.Errorf("读取 Codex 提供商失败: %w", err)
	}

	record.Website = website
	record.IsActive = isActive != 0
	record.CreatedAt = parseSQLiteDateTime(createdAt)
	record.UpdatedAt = parseSQLiteDateTime(updatedAt)
	return &record, nil
}
