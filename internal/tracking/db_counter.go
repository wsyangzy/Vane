package tracking

import (
	"context"
	"database/sql"
	"sync"
)

// DBRequestCounter 维护 request_logs 表的内存计数
//
// 设计目标：用于避免高频 count 查询触发 SQLITE_BUSY，
// 在无复杂过滤的场景下（仅按 provider）直接返回内存中维护的计数，
// 完全避免触发 DB 查询。
//
// 支持的维度：
//   - total: 总记录数
//   - by provider: {"claude": N, "codex": N, ...}
//
// 当查询带有 model/channel/endpoint/status/时间范围 等过滤条件时，
// 内存计数器无法提供准确结果，调用方应回退到 DB 查询路径
// （由现有的缓存降级机制兜底）。
type DBRequestCounter struct {
	mu          sync.RWMutex
	initialized bool
	total       int64
	byProvider  map[string]int64
}

// NewDBRequestCounter 创建一个未初始化的计数器
func NewDBRequestCounter() *DBRequestCounter {
	return &DBRequestCounter{
		byProvider: make(map[string]int64),
	}
}

// Init 从数据库初始化计数器（应用启动时调用）
// 初始化失败应不阻塞启动，调用方应在外部记录日志并接受降级（保持未初始化状态）。
func (c *DBRequestCounter) Init(ctx context.Context, db *sql.DB) error {
	if db == nil {
		return nil
	}

	rows, err := db.QueryContext(ctx, "SELECT COALESCE(provider, 'claude') as provider, COUNT(*) FROM request_logs GROUP BY provider")
	if err != nil {
		return err
	}
	defer rows.Close()

	c.mu.Lock()
	defer c.mu.Unlock()

	var total int64
	byProvider := make(map[string]int64)
	for rows.Next() {
		var provider string
		var count int64
		if err := rows.Scan(&provider, &count); err != nil {
			return err
		}
		if provider == "" {
			provider = "claude"
		}
		byProvider[provider] = count
		total += count
	}
	if err := rows.Err(); err != nil {
		return err
	}

	c.total = total
	c.byProvider = byProvider
	c.initialized = true
	return nil
}

// Inc 记录单条新增（归档单条插入时调用）
func (c *DBRequestCounter) Inc(provider string) {
	if provider == "" {
		provider = "claude"
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.initialized {
		return
	}
	c.total++
	c.byProvider[provider]++
}

// IncBatch 批量增加（批量归档时调用，避免频繁加锁）
//
// providers 切片中的每个元素对应一条新插入的记录的 provider 标识；
// 空字符串会被视作默认 "claude"。
func (c *DBRequestCounter) IncBatch(providers []string) {
	if len(providers) == 0 {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.initialized {
		return
	}
	for _, p := range providers {
		if p == "" {
			p = "claude"
		}
		c.total++
		c.byProvider[p]++
	}
}

// Sub 按 provider 维度扣减计数（归档清理时调用）
//
// 若 count 大于当前计数，结果会被截断到 0，避免负数。
func (c *DBRequestCounter) Sub(provider string, count int64) {
	if count <= 0 {
		return
	}
	if provider == "" {
		provider = "claude"
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.initialized {
		return
	}
	c.total -= count
	if c.total < 0 {
		c.total = 0
	}
	c.byProvider[provider] -= count
	if c.byProvider[provider] < 0 {
		c.byProvider[provider] = 0
	}
}

// Get 获取计数：
//   - 未初始化 → (0, false)
//   - 过滤条件包含 model/channel/endpoint/group/status/时间范围 → (0, false)
//   - 无过滤 → (total, true)
//   - 仅 Provider 过滤 → (byProvider[Provider], true)
func (c *DBRequestCounter) Get(opts *QueryOptions) (int64, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	if !c.initialized {
		return 0, false
	}

	// 检查是否仅包含 provider 过滤（其他字段必须为零值/空）
	if opts != nil {
		if opts.ModelName != "" || opts.Channel != "" || opts.EndpointName != "" ||
			opts.GroupName != "" || opts.Status != "" ||
			opts.StartDate != nil || opts.EndDate != nil {
			return 0, false // 复杂过滤，无法使用内存计数
		}
	}

	if opts == nil || opts.Provider == "" {
		return c.total, true
	}

	return c.byProvider[opts.Provider], true
}

// Reset 重置计数器（仅用于测试或数据库重建后）
func (c *DBRequestCounter) Reset() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.total = 0
	c.byProvider = make(map[string]int64)
	c.initialized = false
}
