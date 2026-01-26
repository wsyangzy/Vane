package config

import (
	"fmt"
	"time"
)

// FailoverConfigV2 v6.2 故障转移配置（简化版）
// 移除了 recovery_strategy 三选一模式，改为更直观的布尔开关
type FailoverConfigV2 struct {
	Enabled         bool          `yaml:"enabled"`          // 启用渠道间故障转移，默认: true
	DefaultCooldown time.Duration `yaml:"default_cooldown"` // 默认冷却时间（已废弃，保留用于向后兼容解析）

	// LoopRetry 所有渠道失败后是否循环重试
	// - true: 按优先级从头开始重试，直到达到 MaxLoops 轮数
	// - false: 遍历所有渠道一轮后停止并提示用户
	// 默认: false
	LoopRetry bool `yaml:"loop_retry"`

	// MaxLoops 最大循环轮数（仅当 LoopRetry = true 时生效）
	// - 0: 无限制（不推荐）
	// - 1-10: 建议范围
	// 默认: 3
	MaxLoops int `yaml:"max_loops"`

	// SkipHealthCheck 跳过定时健康检测（仅当 LoopRetry = true 时生效）
	// - true: 不进行定时健康检测，端点状态仅通过实际请求更新
	// - false: 正常进行定时健康检测
	// 默认: false
	// v6.2.11: 新增，用于减少循环重试模式下的日志噪音
	SkipHealthCheck bool `yaml:"skip_health_check"`

	// SuspendTimeout 普通挂起等待超时
	// - 当请求进入挂起等待（端点耗尽/等待切换）时的最大等待时间
	// 默认: 120s
	SuspendTimeout time.Duration `yaml:"suspend_timeout,omitempty"`

	// LoopRetrySuspendTimeout 达到最大循环轮数后的手动等待超时
	// - 当达到最大循环轮数时，仅等待用户操作的最大等待时间
	// 默认: 120s
	LoopRetrySuspendTimeout time.Duration `yaml:"loop_retry_suspend_timeout,omitempty"`

	// ===== 以下字段已废弃，保留用于向后兼容 =====
	// v6.2.19: InterruptClientOnExhausted 已废弃，始终保持连接挂起
	InterruptClientOnExhausted *bool `yaml:"interrupt_client_on_exhausted,omitempty"` // DEPRECATED: v6.2.19 始终保持连接
	// v6.1.x: 这些字段将在解析配置时自动映射到新字段
	RecoveryStrategy        RecoveryStrategy `yaml:"recovery_strategy,omitempty"`         // DEPRECATED: 使用 loop_retry 替代
	AutoWaitTimeout         time.Duration    `yaml:"auto_wait_timeout,omitempty"`         // DEPRECATED: 已移除 auto_wait 策略
	AutoWaitMaxRequests     int              `yaml:"auto_wait_max_requests,omitempty"`    // DEPRECATED: 已移除 auto_wait 策略
	AllowRevisitAfterExhausted *bool         `yaml:"allow_revisit_after_exhausted,omitempty"` // DEPRECATED: 使用 loop_retry 替代
}

// RecoveryStrategy 端点耗尽后的恢复策略 (DEPRECATED in v6.2)
// 保留用于向后兼容配置解析
type RecoveryStrategy string

const (
	// RecoveryStrategyAutoWait 智能等待恢复（已废弃，映射为 loop_retry: false）
	RecoveryStrategyAutoWait RecoveryStrategy = "auto_wait"

	// RecoveryStrategyLoopRetry 循环重试（映射为 loop_retry: true）
	RecoveryStrategyLoopRetry RecoveryStrategy = "loop_retry"

	// RecoveryStrategyFailFast 快速失败（映射为 loop_retry: false）
	RecoveryStrategyFailFast RecoveryStrategy = "fail_fast"
)

// IsValid 验证策略值是否合法 (DEPRECATED)
func (rs RecoveryStrategy) IsValid() bool {
	switch rs {
	case RecoveryStrategyAutoWait, RecoveryStrategyLoopRetry, RecoveryStrategyFailFast, "":
		return true
	default:
		return false
	}
}

// String 返回策略的可读名称 (DEPRECATED)
func (rs RecoveryStrategy) String() string {
	switch rs {
	case RecoveryStrategyAutoWait:
		return "智能等待恢复（已废弃）"
	case RecoveryStrategyLoopRetry:
		return "循环重试"
	case RecoveryStrategyFailFast:
		return "快速失败"
	default:
		return "未知策略"
	}
}

// ShouldWaitForRecovery 是否应该等待恢复 (DEPRECATED: 始终返回 false)
func (rs RecoveryStrategy) ShouldWaitForRecovery() bool {
	return false // v6.2: auto_wait 已移除，不再支持等待恢复
}

// ShouldLoopRetry 是否应该循环重试 (DEPRECATED: 使用 FailoverConfigV2.LoopRetry 替代)
func (rs RecoveryStrategy) ShouldLoopRetry() bool {
	return rs == RecoveryStrategyLoopRetry
}

// RequestSuspendConfigV2 (DEPRECATED)
// v6.1: 请求挂起配置已废弃
// v6.2: 完全移除，保留此结构体仅为向后兼容解析
type RequestSuspendConfigV2 struct {
	Enabled              *bool         `yaml:"enabled,omitempty"`                // DEPRECATED
	Timeout              time.Duration `yaml:"timeout,omitempty"`                // DEPRECATED
	MaxSuspendedRequests int           `yaml:"max_suspended_requests,omitempty"` // DEPRECATED
	EOFRetryHint         bool          `yaml:"eof_retry_hint,omitempty"`         // DEPRECATED
}

// MigrateFromLegacy 从旧配置迁移到新配置
// 处理 v6.0.x 及之前版本的配置文件向后兼容
func (fc *FailoverConfigV2) MigrateFromLegacy(oldFailover *FailoverConfig, oldSuspend *RequestSuspendConfig) {
	// 基础配置迁移
	fc.Enabled = oldFailover.Enabled

	// 策略迁移逻辑
	if oldFailover.AllowRevisitAfterExhausted {
		// 旧配置：允许回访 → 新配置：循环重试
		fc.LoopRetry = true
	} else {
		// 其他情况 → 新配置：不循环（快速失败）
		fc.LoopRetry = false
	}

	if oldSuspend != nil && oldSuspend.Timeout > 0 {
		fc.SuspendTimeout = oldSuspend.Timeout
		if fc.LoopRetrySuspendTimeout == 0 {
			fc.LoopRetrySuspendTimeout = oldSuspend.Timeout
		}
	}
}

// MigrateFromV61 从 v6.1.x 配置迁移
// 处理 recovery_strategy 字段的向后兼容
func (fc *FailoverConfigV2) MigrateFromV61() {
	if fc.RecoveryStrategy != "" {
		switch fc.RecoveryStrategy {
		case RecoveryStrategyLoopRetry:
			fc.LoopRetry = true
		case RecoveryStrategyAutoWait, RecoveryStrategyFailFast:
			fc.LoopRetry = false
		}
		// 清空旧字段
		fc.RecoveryStrategy = ""
	}

	// 处理旧的 allow_revisit_after_exhausted 字段
	if fc.AllowRevisitAfterExhausted != nil && *fc.AllowRevisitAfterExhausted {
		fc.LoopRetry = true
		fc.AllowRevisitAfterExhausted = nil
	}

	if fc.AutoWaitTimeout > 0 {
		if fc.SuspendTimeout == 0 {
			fc.SuspendTimeout = fc.AutoWaitTimeout
		}
		if fc.LoopRetrySuspendTimeout == 0 {
			fc.LoopRetrySuspendTimeout = fc.AutoWaitTimeout
		}
	}
}

// SetDefaults 设置默认值
func (fc *FailoverConfigV2) SetDefaults() {
	// 从 v6.1 迁移
	fc.MigrateFromV61()

	// max_loops 默认值
	if fc.MaxLoops == 0 {
		fc.MaxLoops = 3 // 默认最多循环3轮
	}

	if fc.SuspendTimeout == 0 {
		if fc.AutoWaitTimeout > 0 {
			fc.SuspendTimeout = fc.AutoWaitTimeout
		} else {
			fc.SuspendTimeout = 120 * time.Second
		}
	}

	if fc.LoopRetrySuspendTimeout == 0 {
		if fc.SuspendTimeout > 0 {
			fc.LoopRetrySuspendTimeout = fc.SuspendTimeout
		} else if fc.AutoWaitTimeout > 0 {
			fc.LoopRetrySuspendTimeout = fc.AutoWaitTimeout
		} else {
			fc.LoopRetrySuspendTimeout = 120 * time.Second
		}
	}

	// 注意：loop_retry 默认为 false（Go 零值），表示快速失败
	// 注意：default_cooldown 不再使用，但保留用于向后兼容解析
}

// Validate 验证配置合法性
func (fc *FailoverConfigV2) Validate() error {
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

	return nil
}

// ShouldLoopRetryOnExhausted 所有渠道耗尽后是否循环重试
func (fc *FailoverConfigV2) ShouldLoopRetryOnExhausted() bool {
	return fc.LoopRetry
}

// GetMaxLoops 获取最大循环轮数
func (fc *FailoverConfigV2) GetMaxLoops() int {
	if fc.MaxLoops <= 0 {
		return 0 // 无限制
	}
	return fc.MaxLoops
}

// GetSuspendTimeout 获取普通挂起等待超时
func (fc *FailoverConfigV2) GetSuspendTimeout() time.Duration {
	if fc == nil {
		return 120 * time.Second
	}
	if fc.SuspendTimeout > 0 {
		return fc.SuspendTimeout
	}
	if fc.AutoWaitTimeout > 0 {
		return fc.AutoWaitTimeout
	}
	return 120 * time.Second
}

// GetLoopRetrySuspendTimeout 获取循环终止的手动等待超时
func (fc *FailoverConfigV2) GetLoopRetrySuspendTimeout() time.Duration {
	if fc == nil {
		return 120 * time.Second
	}
	if fc.LoopRetrySuspendTimeout > 0 {
		return fc.LoopRetrySuspendTimeout
	}
	if fc.SuspendTimeout > 0 {
		return fc.SuspendTimeout
	}
	if fc.AutoWaitTimeout > 0 {
		return fc.AutoWaitTimeout
	}
	return 120 * time.Second
}

// ShouldSkipHealthCheck 是否应该跳过定时健康检测
// 仅当启用渠道间转移、循环重试且 SkipHealthCheck = true 时跳过
// v6.2.17: 修复关闭渠道间转移后仍跳过健康检测的问题
func (fc *FailoverConfigV2) ShouldSkipHealthCheck() bool {
	return fc.Enabled && fc.LoopRetry && fc.SkipHealthCheck
}

// ShouldInterruptClientOnExhausted 所有渠道耗尽后是否中断客户端重试
// v6.2.19: 始终返回 false，保持连接挂起等待恢复
// 原因：简化配置（KISS），提供更好的用户体验
// - 客户端连接不会突然断开
// - 前端可以实时显示状态，用户可通过"重试"按钮控制恢复
// DEPRECATED: InterruptClientOnExhausted 配置项已废弃，始终保持连接
func (fc *FailoverConfigV2) ShouldInterruptClientOnExhausted() bool {
	return false // v6.2.19: 始终保持连接，不再支持 422 中断
}
