// Package store 提供数据存储层实现
// 渠道存储 - 新增
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

// ChannelRecord 表示数据库中的渠道记录
type ChannelRecord struct {
	ID int64 `json:"id"`

	Name     string `json:"name"`
	Website  string `json:"website,omitempty"`
	Priority int    `json:"priority"`
	// FailoverEnabled 表示该渠道是否参与“渠道间故障转移”（暂停/恢复按钮持久化）。
	FailoverEnabled bool              `json:"failover_enabled"`
	ModelMapping    map[string]string `json:"model_mapping"`
	// NonStreamRoute 渠道不接非流请求时，非流请求改道到的目标；nil 表示关闭。
	// 只经 SetNonStreamRoute 写入，Update 不改动该列。
	NonStreamRoute *routing.NonStreamRoute `json:"non_stream_route,omitempty"`
	// PrivacyLevel 渠道隐私级别：""=跟随全局 / third_party / trusted。
	// 只经 SetPrivacyLevel 写入，Update 不改动该列。
	PrivacyLevel string `json:"privacy_level,omitempty"`

	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// ChannelStore 定义渠道存储接口
type ChannelStore interface {
	Create(ctx context.Context, record *ChannelRecord) (*ChannelRecord, error)
	Get(ctx context.Context, name string) (*ChannelRecord, error)
	GetByPriority(ctx context.Context, priority int) (*ChannelRecord, error) // 用于优先级冲突交换
	GetMaxPriority(ctx context.Context) (int, error)                         // 用于自动递增
	List(ctx context.Context) ([]*ChannelRecord, error)
	Update(ctx context.Context, record *ChannelRecord) error
	Delete(ctx context.Context, name string) error

	WithTx(tx *sql.Tx) ChannelStore
}

// SQLiteChannelStore 实现 ChannelStore 接口
type SQLiteChannelStore struct {
	db *sql.DB
	mu sync.RWMutex
	tx *sql.Tx
}

func NewSQLiteChannelStore(db *sql.DB) *SQLiteChannelStore {
	return &SQLiteChannelStore{db: db}
}

// BeginTx starts a transaction for service-level atomic priority updates.
func (s *SQLiteChannelStore) BeginTx(ctx context.Context) (*sql.Tx, error) {
	if s == nil || s.db == nil {
		return nil, fmt.Errorf("渠道数据库未初始化")
	}
	return s.db.BeginTx(ctx, nil)
}

func (s *SQLiteChannelStore) WithTx(tx *sql.Tx) ChannelStore {
	return &SQLiteChannelStore{db: s.db, tx: tx}
}

func (s *SQLiteChannelStore) execContext(ctx context.Context, query string, args ...any) (sql.Result, error) {
	if s.tx != nil {
		return s.tx.ExecContext(ctx, query, args...)
	}
	return s.db.ExecContext(ctx, query, args...)
}

func (s *SQLiteChannelStore) queryRowContext(ctx context.Context, query string, args ...any) *sql.Row {
	if s.tx != nil {
		return s.tx.QueryRowContext(ctx, query, args...)
	}
	return s.db.QueryRowContext(ctx, query, args...)
}

func (s *SQLiteChannelStore) queryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error) {
	if s.tx != nil {
		return s.tx.QueryContext(ctx, query, args...)
	}
	return s.db.QueryContext(ctx, query, args...)
}

func (s *SQLiteChannelStore) Create(ctx context.Context, record *ChannelRecord) (*ChannelRecord, error) {
	if record == nil {
		return nil, fmt.Errorf("record 不能为空")
	}
	if record.Name == "" {
		return nil, fmt.Errorf("渠道名称不能为空")
	}
	if record.Priority <= 0 {
		record.Priority = 1
	}
	modelMappingJSON, err := channelModelMappingToJSON(record.ModelMapping)
	if err != nil {
		return nil, err
	}

	s.mu.Lock()
	query := `
		INSERT INTO channels (name, website, priority, failover_enabled, model_mapping)
		VALUES (?, ?, ?, ?, ?)
	`
	res, err := s.execContext(ctx, query, record.Name, nullIfEmpty(record.Website), record.Priority, boolToInt(record.FailoverEnabled), modelMappingJSON)
	if err != nil {
		s.mu.Unlock()
		return nil, fmt.Errorf("创建渠道失败: %w", err)
	}

	_, _ = res.LastInsertId()
	s.mu.Unlock()

	created, err := s.Get(ctx, record.Name)
	if err != nil {
		return nil, err
	}
	return created, nil
}

func (s *SQLiteChannelStore) Get(ctx context.Context, name string) (*ChannelRecord, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	record, err := scanChannelRecord(s.queryRowContext(ctx, `SELECT `+channelColumns+` FROM channels WHERE name = ?`, name))
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("获取渠道失败: %w", err)
	}
	return record, nil
}

// GetByPriority 根据优先级获取渠道（用于优先级冲突交换）
func (s *SQLiteChannelStore) GetByPriority(ctx context.Context, priority int) (*ChannelRecord, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	record, err := scanChannelRecord(s.queryRowContext(ctx, `SELECT `+channelColumns+` FROM channels WHERE priority = ? LIMIT 1`, priority))
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("根据优先级获取渠道失败: %w", err)
	}
	return record, nil
}

// channelColumns 渠道表的查询列，顺序与 scanChannelRecord 一致
const channelColumns = `id, name, COALESCE(website, ''), COALESCE(priority, 1), COALESCE(failover_enabled, 1),
	COALESCE(model_mapping, '{}'), COALESCE(non_stream_route, ''), COALESCE(privacy_level, ''), created_at, updated_at`

func scanChannelRecord(scanner interface{ Scan(dest ...any) error }) (*ChannelRecord, error) {
	var record ChannelRecord
	var createdAt, updatedAt string
	var failoverEnabled int
	var modelMappingJSON, nonStreamRouteJSON string

	if err := scanner.Scan(
		&record.ID,
		&record.Name,
		&record.Website,
		&record.Priority,
		&failoverEnabled,
		&modelMappingJSON,
		&nonStreamRouteJSON,
		&record.PrivacyLevel,
		&createdAt,
		&updatedAt,
	); err != nil {
		return nil, err
	}

	record.CreatedAt = parseSQLiteDateTime(createdAt)
	record.UpdatedAt = parseSQLiteDateTime(updatedAt)
	record.FailoverEnabled = failoverEnabled != 0
	modelMapping, err := channelModelMappingFromJSON(modelMappingJSON)
	if err != nil {
		return nil, err
	}
	record.ModelMapping = modelMapping
	// 非流改道配置损坏时按关闭处理：它只是改道增强，不能让整张渠道表读不出来
	record.NonStreamRoute, _ = routing.ParseNonStreamRoute(nonStreamRouteJSON)
	return &record, nil
}

// GetMaxPriority 获取所有渠道的最大优先级（用于自动递增）
func (s *SQLiteChannelStore) GetMaxPriority(ctx context.Context) (int, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var maxPriority sql.NullInt64
	err := s.queryRowContext(ctx, `SELECT MAX(priority) FROM channels`).Scan(&maxPriority)
	if err != nil {
		return 0, fmt.Errorf("获取渠道最大优先级失败: %w", err)
	}

	if !maxPriority.Valid {
		return 0, nil // 没有渠道时返回 0
	}
	return int(maxPriority.Int64), nil
}

func (s *SQLiteChannelStore) List(ctx context.Context) ([]*ChannelRecord, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	query := `SELECT ` + channelColumns + `
		FROM channels
		ORDER BY COALESCE(priority, 1) ASC, created_at DESC, name ASC
	`
	rows, err := s.queryContext(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("列出渠道失败: %w", err)
	}
	defer rows.Close()

	var result []*ChannelRecord
	for rows.Next() {
		record, err := scanChannelRecord(rows)
		if err != nil {
			return nil, fmt.Errorf("读取渠道失败: %w", err)
		}
		result = append(result, record)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("读取渠道失败: %w", err)
	}
	return result, nil
}

func (s *SQLiteChannelStore) Update(ctx context.Context, record *ChannelRecord) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if record == nil {
		return fmt.Errorf("record 不能为空")
	}
	if record.Name == "" {
		return fmt.Errorf("渠道名称不能为空")
	}
	if record.Priority <= 0 {
		record.Priority = 1
	}
	modelMappingJSON, err := channelModelMappingToJSON(record.ModelMapping)
	if err != nil {
		return err
	}

	query := `UPDATE channels SET website = ?, priority = ?, failover_enabled = ?, model_mapping = ? WHERE name = ?`
	res, err := s.execContext(ctx, query, nullIfEmpty(record.Website), record.Priority, boolToInt(record.FailoverEnabled), modelMappingJSON, record.Name)
	if err != nil {
		return fmt.Errorf("更新渠道失败: %w", err)
	}
	affected, _ := res.RowsAffected()
	if affected == 0 {
		return fmt.Errorf("渠道不存在: %s", record.Name)
	}
	return nil
}

func (s *SQLiteChannelStore) Delete(ctx context.Context, name string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	res, err := s.execContext(ctx, `DELETE FROM channels WHERE name = ?`, name)
	if err != nil {
		return fmt.Errorf("删除渠道失败: %w", err)
	}
	affected, _ := res.RowsAffected()
	if affected == 0 {
		return fmt.Errorf("渠道不存在: %s", name)
	}
	return nil
}

// SetPrivacyLevel 写入渠道隐私级别；空串表示跟随全局（落库为 NULL）。取值由调用方校验。
func (s *SQLiteChannelStore) SetPrivacyLevel(ctx context.Context, name, level string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	res, err := s.execContext(ctx, `UPDATE channels SET privacy_level = ? WHERE name = ?`, nullIfEmpty(level), name)
	if err != nil {
		return fmt.Errorf("更新渠道隐私级别失败: %w", err)
	}
	if affected, _ := res.RowsAffected(); affected == 0 {
		return fmt.Errorf("渠道不存在: %s", name)
	}
	return nil
}

// SetNonStreamRoute 写入渠道的非流改道配置；route 为 nil 或没有目标时落库为 NULL（关闭）。
func (s *SQLiteChannelStore) SetNonStreamRoute(ctx context.Context, name string, route *routing.NonStreamRoute) error {
	encoded, err := routing.EncodeNonStreamRoute(route)
	if err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	res, err := s.execContext(ctx, `UPDATE channels SET non_stream_route = ? WHERE name = ?`, nullIfEmpty(encoded), name)
	if err != nil {
		return fmt.Errorf("更新渠道非流改道失败: %w", err)
	}
	affected, _ := res.RowsAffected()
	if affected == 0 {
		return fmt.Errorf("渠道不存在: %s", name)
	}
	return nil
}

func nullIfEmpty(s string) any {
	if s == "" {
		return nil
	}
	return s
}

func channelModelMappingToJSON(modelMapping map[string]string) (string, error) {
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
		return "", fmt.Errorf("序列化渠道模型映射失败: %w", err)
	}
	return string(data), nil
}

func channelModelMappingFromJSON(raw string) (map[string]string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return map[string]string{}, nil
	}

	var parsed map[string]string
	if err := json.Unmarshal([]byte(raw), &parsed); err != nil {
		return nil, fmt.Errorf("解析渠道模型映射失败: %w", err)
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
