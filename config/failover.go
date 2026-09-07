package config

import (
	"fmt"
	"time"
)

// FailoverConfig defines channel failover and request suspension behavior.
type FailoverConfig struct {
	// Enabled 渠道间故障转移总开关
	// - true:  当前渠道内所有端点均重试耗尽时，自动切换到备用渠道
	// - false: 仅在当前渠道内端点之间转移，不切换渠道
	// 默认: true
	Enabled bool

	// LoopRetry 所有渠道失败后是否循环重试
	// - true:  按优先级从头开始重试，直到达到 MaxLoops 轮数
	// - false: 遍历所有渠道一轮后停止
	// 默认: false
	LoopRetry bool

	// MaxLoops 最大循环轮数（仅当 LoopRetry = true 时生效）
	// - 0:    无限制（不推荐）
	// - 1-10: 建议范围
	// 默认: 3
	MaxLoops int

	// SkipHealthCheck 跳过定时健康检测
	// - true:  不进行定时健康检测，端点状态仅通过实际请求更新（减少日志噪音）
	// - false: 正常进行定时健康检测
	// 默认: false（仅当 Enabled + LoopRetry + SkipHealthCheck 全 true 时实际跳过）
	SkipHealthCheck bool

	// SuspendTimeout 普通挂起等待超时
	// 当请求进入挂起等待（端点耗尽/等待切换）时的最大等待时间
	// 默认: 120s
	SuspendTimeout time.Duration

	// LoopRetrySuspendTimeout 达到最大循环轮数后的手动等待超时
	// 当达到最大循环轮数时，仅等待用户操作的最大等待时间
	// 默认: 120s
	LoopRetrySuspendTimeout time.Duration

	// SuspendOnExhausted 渠道间转移关闭时是否仍挂起请求（原 RequestSuspend.Enabled）
	// 当 Enabled = false 时影响"端点耗尽后"行为：
	// - true:  挂起请求等待用户手动激活渠道
	// - false: 允许自动激活下一个可用渠道
	// 默认: false
	SuspendOnExhausted bool

	// MaxSuspendedRequests 最大挂起请求数（防止内存压力）
	// 默认: 100；上限: 10000
	MaxSuspendedRequests int

	// EOFRetryHint 流式响应 EOF 时是否返回可重试错误格式
	// 默认: false
	EOFRetryHint bool
}

// SetDefaults 设置默认值
func (fc *FailoverConfig) SetDefaults() {
	if fc.MaxLoops == 0 {
		fc.MaxLoops = 3
	}
	if fc.SuspendTimeout == 0 {
		fc.SuspendTimeout = 120 * time.Second
	}
	if fc.LoopRetrySuspendTimeout == 0 {
		fc.LoopRetrySuspendTimeout = fc.SuspendTimeout
	}
	if fc.MaxSuspendedRequests == 0 {
		fc.MaxSuspendedRequests = 100
	}
	// Enabled / LoopRetry / SkipHealthCheck / SuspendOnExhausted / EOFRetryHint
	// 默认为 Go 零值（false），由调用方按需显式开启
}

// Validate 验证配置合法性
func (fc *FailoverConfig) Validate() error {
	if fc.LoopRetry && fc.MaxLoops < 0 {
		return fmt.Errorf("max_loops cannot be negative when loop_retry is enabled")
	}
	if fc.MaxLoops > 100 {
		return fmt.Errorf("max_loops cannot exceed 100 for performance reasons")
	}
	if fc.SuspendTimeout < 0 {
		return fmt.Errorf("suspend_timeout cannot be negative")
	}
	if fc.LoopRetrySuspendTimeout < 0 {
		return fmt.Errorf("loop_retry_suspend_timeout cannot be negative")
	}
	if fc.MaxSuspendedRequests < 0 {
		return fmt.Errorf("max_suspended_requests cannot be negative")
	}
	if fc.MaxSuspendedRequests > 10000 {
		return fmt.Errorf("max_suspended_requests cannot exceed 10000 for performance reasons")
	}
	return nil
}

// ShouldLoopRetryOnExhausted 所有渠道耗尽后是否循环重试
func (fc *FailoverConfig) ShouldLoopRetryOnExhausted() bool {
	return fc.LoopRetry
}

// GetMaxLoops 获取最大循环轮数（<= 0 表示无限制）
func (fc *FailoverConfig) GetMaxLoops() int {
	if fc.MaxLoops <= 0 {
		return 0
	}
	return fc.MaxLoops
}

// GetSuspendTimeout 获取普通挂起等待超时
func (fc *FailoverConfig) GetSuspendTimeout() time.Duration {
	if fc == nil || fc.SuspendTimeout <= 0 {
		return 120 * time.Second
	}
	return fc.SuspendTimeout
}

// GetLoopRetrySuspendTimeout 获取循环终止的手动等待超时
func (fc *FailoverConfig) GetLoopRetrySuspendTimeout() time.Duration {
	if fc == nil {
		return 120 * time.Second
	}
	if fc.LoopRetrySuspendTimeout > 0 {
		return fc.LoopRetrySuspendTimeout
	}
	if fc.SuspendTimeout > 0 {
		return fc.SuspendTimeout
	}
	return 120 * time.Second
}

// ShouldSkipHealthCheck 是否应该跳过定时健康检测
// 仅当 Enabled + LoopRetry + SkipHealthCheck 三者都为 true 时跳过
func (fc *FailoverConfig) ShouldSkipHealthCheck() bool {
	return fc.Enabled && fc.LoopRetry && fc.SkipHealthCheck
}
