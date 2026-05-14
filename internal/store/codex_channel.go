package store

import (
	"context"
	"database/sql"
	"fmt"
	"sync"
	"time"
)

// CodexChannelRecord 表示 Codex 渠道配置（共用配置：API Key、模型、推理强度、1M 上下文等）。
// 同一时刻全局最多一行 IsActive=true。
type CodexChannelRecord struct {
	ID int64 `json:"id"`

	Name            string `json:"name"`
	ProviderType    string `json:"provider_type"`
	ApiKey          string `json:"api_key"`
	Model           string `json:"model"`
	ReasoningEffort string `json:"model_reasoning_effort"`
	Enable1MContext bool   `json:"enable_1m_context"`
	Website         string `json:"website,omitempty"`
	IsActive        bool   `json:"is_active"`

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
		COALESCE(enable_1m_context, 0), COALESCE(website, ''), COALESCE(is_active, 0), created_at, updated_at`

func (s *SQLiteCodexChannelStore) Create(ctx context.Context, record *CodexChannelRecord) (*CodexChannelRecord, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if record == nil {
		return nil, fmt.Errorf("record 不能为空")
	}
	if record.Name == "" {
		return nil, fmt.Errorf("渠道名不能为空")
	}

	query := `
		INSERT INTO codex_channels (
			name, provider_type, api_key, model, model_reasoning_effort,
			enable_1m_context, website, is_active
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?)
	`
	result, err := s.getQuerier().ExecContext(
		ctx,
		query,
		record.Name,
		record.ProviderType,
		record.ApiKey,
		record.Model,
		record.ReasoningEffort,
		boolToInt(record.Enable1MContext),
		nullIfEmpty(record.Website),
		boolToInt(record.IsActive),
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

	query := "SELECT " + codexChannelColumns + " FROM codex_channels ORDER BY is_active DESC, created_at DESC, name ASC"

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

	query := `
		UPDATE codex_channels
		SET name = ?, provider_type = ?, api_key = ?, model = ?, model_reasoning_effort = ?,
		    enable_1m_context = ?, website = ?
		WHERE id = ?
	`
	result, err := s.getQuerier().ExecContext(
		ctx,
		query,
		record.Name,
		record.ProviderType,
		record.ApiKey,
		record.Model,
		record.ReasoningEffort,
		boolToInt(record.Enable1MContext),
		nullIfEmpty(record.Website),
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

// SetActive 通过先清空再置位的方式确保全局单活。
// 注意：部分唯一索引 idx_codex_channels_single_active 强制约束，必须先清空再设置。
func (s *SQLiteCodexChannelStore) SetActive(ctx context.Context, id int64) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, err := s.getQuerier().ExecContext(ctx, `UPDATE codex_channels SET is_active = 0`); err != nil {
		return fmt.Errorf("清空激活渠道失败: %w", err)
	}

	result, err := s.getQuerier().ExecContext(ctx, `UPDATE codex_channels SET is_active = 1 WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("设置激活渠道失败: %w", err)
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
		record    CodexChannelRecord
		enable1M  int
		website   string
		isActive  int
		createdAt string
		updatedAt string
	)

	err := scanner.Scan(
		&record.ID,
		&record.Name,
		&record.ProviderType,
		&record.ApiKey,
		&record.Model,
		&record.ReasoningEffort,
		&enable1M,
		&website,
		&isActive,
		&createdAt,
		&updatedAt,
	)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("读取 Codex 渠道失败: %w", err)
	}

	record.Enable1MContext = enable1M != 0
	record.Website = website
	record.IsActive = isActive != 0
	record.CreatedAt = parseSQLiteDateTime(createdAt)
	record.UpdatedAt = parseSQLiteDateTime(updatedAt)
	return &record, nil
}
