package logging

import (
	"sync"
)

// RingBuffer 环形缓冲区，用于存储最近的N条日志
// 特点：
// - 固定大小，自动覆盖旧数据
// - 线程安全
// - O(1) 写入和读取
type RingBuffer struct {
	logs     []LogEntry   // 日志数组
	size     int          // 缓冲区大小
	position int          // 当前写入位置
	count    int          // 当前已存储的日志数量
	mu       sync.RWMutex // 读写锁
}

// NewRingBuffer 创建指定大小的环形缓冲区
func NewRingBuffer(size int) *RingBuffer {
	if size <= 0 {
		size = 1000 // 默认1000条
	}
	return &RingBuffer{
		logs: make([]LogEntry, size),
		size: size,
	}
}

// Add 添加一条日志（覆盖最旧的）
func (rb *RingBuffer) Add(entry LogEntry) {
	rb.mu.Lock()
	defer rb.mu.Unlock()

	rb.logs[rb.position] = entry
	rb.position = (rb.position + 1) % rb.size

	if rb.count < rb.size {
		rb.count++
	}
}

// GetRecent 获取最近的N条日志（按时间顺序）
func (rb *RingBuffer) GetRecent(limit int) []LogEntry {
	rb.mu.RLock()
	defer rb.mu.RUnlock()

	if limit <= 0 || limit > rb.count {
		limit = rb.count
	}

	result := make([]LogEntry, limit)

	// 计算起始位置（从最旧的有效日志开始）
	start := rb.position - limit
	if start < 0 {
		start += rb.size
	}

	// 读取日志（循环读取）
	for i := 0; i < limit; i++ {
		idx := (start + i) % rb.size
		result[i] = rb.logs[idx]
	}

	return result
}

// GetAll 获取所有日志
func (rb *RingBuffer) GetAll() []LogEntry {
	return rb.GetRecent(rb.count)
}

// Count 返回当前存储的日志数量
func (rb *RingBuffer) Count() int {
	rb.mu.RLock()
	defer rb.mu.RUnlock()
	return rb.count
}

// Clear 清空缓冲区
func (rb *RingBuffer) Clear() {
	rb.mu.Lock()
	defer rb.mu.Unlock()
	rb.position = 0
	rb.count = 0
}
