// Package store 提供数据存储层实现
// 模型路由规则存储 - 新增 (2026-09-28)
package store

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/wsyangzy/Vane/internal/platform/sqlite"
	"github.com/wsyangzy/Vane/internal/routing"
)

// ModelRouteRecord 模型路由规则。Claude 与 Codex 共用一张表，provider 维度隔离：
// 唯一键是 (provider, name)，两侧允许同名规则且互不影响。
// 规则挂在来源渠道上（SourceChannelID），只在该渠道为激活渠道时生效；0 为旧版全局规则。
type ModelRouteRecord struct {
	ID              int64            `json:"id"`
	Provider        string           `json:"provider"`
	SourceChannelID int64            `json:"source_channel_id"`
	Name            string           `json:"name"`
	Enabled         bool             `json:"enabled"`
	Priority        int              `json:"priority"`
	MatchModel      string           `json:"match_model"`
	Mode            string           `json:"mode"`
	Targets         []routing.Target `json:"targets"`
	CreatedAt       time.Time        `json:"created_at"`
	UpdatedAt       time.Time        `json:"updated_at"`
}

// ModelRouteStore 定义模型路由规则存储接口
type ModelRouteStore interface {
	List(ctx context.Context, provider string) ([]*ModelRouteRecord, error)
	ListBySource(ctx context.Context, provider string, sourceChannelID int64) ([]*ModelRouteRecord, error)
	ListEnabled(ctx context.Context, provider string) ([]*ModelRouteRecord, error)
	GetByID(ctx context.Context, id int64) (*ModelRouteRecord, error)
	Create(ctx context.Context, record *ModelRouteRecord) (*ModelRouteRecord, error)
	Update(ctx context.Context, record *ModelRouteRecord) error
	Delete(ctx context.Context, id int64) error
	DeleteBySource(ctx context.Context, provider string, sourceChannelID int64) error
	SetEnabled(ctx context.Context, id int64, enabled bool) error
}

// SQLiteModelRouteStore 实现 ModelRouteStore 接口
type SQLiteModelRouteStore struct {
	db *sql.DB
	mu sync.RWMutex
}

// NewSQLiteModelRouteStore 创建模型路由规则存储
func NewSQLiteModelRouteStore(db *sql.DB) *SQLiteModelRouteStore {
	return &SQLiteModelRouteStore{db: db}
}

const modelRouteColumns = `id, provider, COALESCE(source_channel_id, 0), name, enabled, priority, match_model, mode, COALESCE(targets, '[]'), created_at, updated_at`

// List 列出 provider 下全部规则，按优先级与 ID 排序
func (s *SQLiteModelRouteStore) List(ctx context.Context, provider string) ([]*ModelRouteRecord, error) {
	return s.list(ctx, `SELECT `+modelRouteColumns+` FROM model_routes WHERE provider = ? ORDER BY priority ASC, id ASC`, normalizeRouteProvider(provider))
}

// ListBySource 列出挂在某个来源渠道上的规则
func (s *SQLiteModelRouteStore) ListBySource(ctx context.Context, provider string, sourceChannelID int64) ([]*ModelRouteRecord, error) {
	return s.list(ctx, `SELECT `+modelRouteColumns+` FROM model_routes WHERE provider = ? AND source_channel_id = ? ORDER BY priority ASC, id ASC`,
		normalizeRouteProvider(provider), sourceChannelID)
}

// ListEnabled 列出 provider 下已启用的规则
func (s *SQLiteModelRouteStore) ListEnabled(ctx context.Context, provider string) ([]*ModelRouteRecord, error) {
	return s.list(ctx, `SELECT `+modelRouteColumns+` FROM model_routes WHERE provider = ? AND enabled = 1 ORDER BY priority ASC, id ASC`, normalizeRouteProvider(provider))
}

func (s *SQLiteModelRouteStore) list(ctx context.Context, query string, args ...any) ([]*ModelRouteRecord, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	rows, err := sqlite.QueryRowsWithBusyRetry(ctx, func() (*sql.Rows, error) {
		return s.db.QueryContext(ctx, query, args...)
	})
	if err != nil {
		return nil, fmt.Errorf("查询模型路由规则失败: %w", err)
	}
	defer rows.Close()

	var records []*ModelRouteRecord
	for rows.Next() {
		record, err := scanModelRoute(rows)
		if err != nil {
			return nil, err
		}
		records = append(records, record)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("读取模型路由规则失败: %w", err)
	}
	return records, nil
}

// GetByID 按 ID 获取规则，不存在时返回 nil
func (s *SQLiteModelRouteStore) GetByID(ctx context.Context, id int64) (*ModelRouteRecord, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	record, err := scanModelRoute(s.db.QueryRowContext(ctx, `SELECT `+modelRouteColumns+` FROM model_routes WHERE id = ?`, id))
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}
	return record, nil
}

// Create 创建规则
func (s *SQLiteModelRouteStore) Create(ctx context.Context, record *ModelRouteRecord) (*ModelRouteRecord, error) {
	if record == nil {
		return nil, fmt.Errorf("record 不能为空")
	}
	targetsJSON, err := routing.EncodeTargets(record.Targets)
	if err != nil {
		return nil, err
	}

	s.mu.Lock()
	result, err := s.db.ExecContext(ctx, `
		INSERT INTO model_routes (provider, source_channel_id, name, enabled, priority, match_model, mode, targets)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		normalizeRouteProvider(record.Provider), record.SourceChannelID, record.Name, sqlite.BoolToInt(record.Enabled), record.Priority,
		record.MatchModel, record.Mode, targetsJSON,
	)
	s.mu.Unlock()
	if err != nil {
		if isUniqueConstraintError(err) {
			return nil, fmt.Errorf("规则名称 '%s' 已存在", record.Name)
		}
		return nil, fmt.Errorf("创建模型路由规则失败: %w", err)
	}
	id, err := result.LastInsertId()
	if err != nil {
		return nil, fmt.Errorf("获取插入 ID 失败: %w", err)
	}
	return s.GetByID(ctx, id)
}

// Update 更新规则的全部可编辑字段（provider 不可修改）
func (s *SQLiteModelRouteStore) Update(ctx context.Context, record *ModelRouteRecord) error {
	if record == nil {
		return fmt.Errorf("record 不能为空")
	}
	targetsJSON, err := routing.EncodeTargets(record.Targets)
	if err != nil {
		return err
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	result, err := s.db.ExecContext(ctx, `
		UPDATE model_routes
		SET name = ?, enabled = ?, priority = ?, match_model = ?, mode = ?, targets = ?
		WHERE id = ?`,
		record.Name, sqlite.BoolToInt(record.Enabled), record.Priority, record.MatchModel, record.Mode, targetsJSON, record.ID,
	)
	if err != nil {
		if isUniqueConstraintError(err) {
			return fmt.Errorf("规则名称 '%s' 已存在", record.Name)
		}
		return fmt.Errorf("更新模型路由规则失败: %w", err)
	}
	if affected, _ := result.RowsAffected(); affected == 0 {
		return fmt.Errorf("模型路由规则不存在: %d", record.ID)
	}
	return nil
}

// Delete 删除规则
func (s *SQLiteModelRouteStore) Delete(ctx context.Context, id int64) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	result, err := s.db.ExecContext(ctx, `DELETE FROM model_routes WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("删除模型路由规则失败: %w", err)
	}
	if affected, _ := result.RowsAffected(); affected == 0 {
		return fmt.Errorf("模型路由规则不存在: %d", id)
	}
	return nil
}

// DeleteBySource 删除挂在某个来源渠道上的全部规则（渠道被删除时调用）
func (s *SQLiteModelRouteStore) DeleteBySource(ctx context.Context, provider string, sourceChannelID int64) error {
	if sourceChannelID <= 0 {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, err := s.db.ExecContext(ctx, `DELETE FROM model_routes WHERE provider = ? AND source_channel_id = ?`,
		normalizeRouteProvider(provider), sourceChannelID); err != nil {
		return fmt.Errorf("删除渠道模型路由规则失败: %w", err)
	}
	return nil
}

// SetEnabled 启用或停用规则
func (s *SQLiteModelRouteStore) SetEnabled(ctx context.Context, id int64, enabled bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	result, err := s.db.ExecContext(ctx, `UPDATE model_routes SET enabled = ? WHERE id = ?`, sqlite.BoolToInt(enabled), id)
	if err != nil {
		return fmt.Errorf("更新模型路由规则状态失败: %w", err)
	}
	if affected, _ := result.RowsAffected(); affected == 0 {
		return fmt.Errorf("模型路由规则不存在: %d", id)
	}
	return nil
}

func scanModelRoute(scanner interface{ Scan(dest ...any) error }) (*ModelRouteRecord, error) {
	var record ModelRouteRecord
	var enabled int
	var targetsJSON, createdAt, updatedAt string
	if err := scanner.Scan(
		&record.ID, &record.Provider, &record.SourceChannelID, &record.Name, &enabled, &record.Priority,
		&record.MatchModel, &record.Mode, &targetsJSON, &createdAt, &updatedAt,
	); err != nil {
		if err == sql.ErrNoRows {
			return nil, err
		}
		return nil, fmt.Errorf("读取模型路由规则失败: %w", err)
	}
	targets, err := routing.ParseTargets(targetsJSON)
	if err != nil {
		return nil, err
	}
	record.Enabled = enabled != 0
	record.Targets = targets
	record.CreatedAt = sqlite.ParseDateTime(createdAt)
	record.UpdatedAt = sqlite.ParseDateTime(updatedAt)
	return &record, nil
}

func normalizeRouteProvider(provider string) string {
	provider = strings.ToLower(strings.TrimSpace(provider))
	if provider == "" {
		return "claude"
	}
	return provider
}

func isUniqueConstraintError(err error) bool {
	return err != nil && strings.Contains(strings.ToLower(err.Error()), "unique constraint")
}
