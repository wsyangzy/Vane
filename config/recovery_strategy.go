package config

import (
	"fmt"
	"time"
)

// RecoveryStrategy 端点耗尽后的恢复策略
type RecoveryStrategy string

const (
	// RecoveryStrategyAutoWait 智能等待恢复（推荐）
	// 行为：挂起请求，等待端点自动恢复信号（通过其他请求成功、冷却期到期或管理员手动恢复）
	// 优点：避免循环重试，减少无效请求
	// 缺点：可能等待较长时间（最多 auto_wait_timeout）
	RecoveryStrategyAutoWait RecoveryStrategy = "auto_wait"

	// RecoveryStrategyLoopRetry 循环重试
	// 行为：回到最高优先级渠道继续尝试（跳过冷却中的端点）
	// 优点：持续尝试，不会超时
	// 缺点：可能在渠道间循环重试，浪费资源
	// 注意：仅适用于测试环境或渠道数量极少的场景
	RecoveryStrategyLoopRetry RecoveryStrategy = "loop_retry"

	// RecoveryStrategyFailFast 快速失败
	// 行为：立即返回错误，不等待恢复，不循环重试
	// 优点：明确错误信号，快速反馈
	// 缺点：可能错过即将恢复的端点
	RecoveryStrategyFailFast RecoveryStrategy = "fail_fast"
)

// IsValid 验证策略值是否合法
func (rs RecoveryStrategy) IsValid() bool {
	switch rs {
	case RecoveryStrategyAutoWait, RecoveryStrategyLoopRetry, RecoveryStrategyFailFast:
		return true
	default:
		return false
	}
}

// String 返回策略的可读名称
func (rs RecoveryStrategy) String() string {
	switch rs {
	case RecoveryStrategyAutoWait:
		return "智能等待恢复"
	case RecoveryStrategyLoopRetry:
		return "循环重试"
	case RecoveryStrategyFailFast:
		return "快速失败"
	default:
		return "未知策略"
	}
}

// ShouldWaitForRecovery 是否应该等待恢复（替代原来的 suspend_enabled 判断）
func (rs RecoveryStrategy) ShouldWaitForRecovery() bool {
	return rs == RecoveryStrategyAutoWait
}

// ShouldLoopRetry 是否应该循环重试（替代原来的 allow_revisit_after_exhausted 判断）
func (rs RecoveryStrategy) ShouldLoopRetry() bool {
	return rs == RecoveryStrategyLoopRetry
}

// FailoverConfigV2 v6.1 故障转移配置（简化版）
// 替代原有的 FailoverConfig + RequestSuspendConfig 双配置结构
type FailoverConfigV2 struct {
	Enabled         bool          `yaml:"enabled"`          // 启用故障转移，默认: true
	DefaultCooldown time.Duration `yaml:"default_cooldown"` // 默认冷却时间，默认: 10m

	// RecoveryStrategy 恢复策略（新增，统一控制）
	// 可选值: "auto_wait" | "loop_retry" | "fail_fast"
	// 默认: "auto_wait"（向后兼容：若未配置，根据旧配置推断）
	RecoveryStrategy RecoveryStrategy `yaml:"recovery_strategy"`

	// AutoWaitTimeout 智能等待超时时间（仅当 recovery_strategy = "auto_wait" 时生效）
	// 默认: 300s
	AutoWaitTimeout time.Duration `yaml:"auto_wait_timeout"`

	// AutoWaitMaxRequests 最大挂起请求数（仅当 recovery_strategy = "auto_wait" 时生效）
	// 默认: 100
	AutoWaitMaxRequests int `yaml:"auto_wait_max_requests"`

	// ===== 以下字段已废弃，保留用于向后兼容 =====
	// v6.0.x: 这些字段将在解析配置时自动映射到新字段
	AllowRevisitAfterExhausted *bool `yaml:"allow_revisit_after_exhausted,omitempty"` // DEPRECATED: 使用 recovery_strategy 替代
}

// RequestSuspendConfigV2 (DEPRECATED)
// v6.1: 请求挂起配置已合并到 FailoverConfigV2.RecoveryStrategy
// 保留此结构体仅为向后兼容，不再使用
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
	fc.DefaultCooldown = oldFailover.DefaultCooldown

	// 策略迁移逻辑（根据旧配置组合推断）
	if oldSuspend != nil && oldSuspend.Enabled {
		// 旧配置：启用了请求挂起 → 新策略：智能等待恢复
		fc.RecoveryStrategy = RecoveryStrategyAutoWait
		fc.AutoWaitTimeout = oldSuspend.Timeout
		fc.AutoWaitMaxRequests = oldSuspend.MaxSuspendedRequests
	} else if oldFailover.AllowRevisitAfterExhausted {
		// 旧配置：允许回访 → 新策略：循环重试
		fc.RecoveryStrategy = RecoveryStrategyLoopRetry
	} else {
		// 旧配置：两者都未启用 → 新策略：快速失败
		fc.RecoveryStrategy = RecoveryStrategyFailFast
	}
}

// SetDefaults 设置默认值
func (fc *FailoverConfigV2) SetDefaults() {
	if fc.DefaultCooldown == 0 {
		fc.DefaultCooldown = 10 * time.Minute
	}

	if fc.RecoveryStrategy == "" {
		fc.RecoveryStrategy = RecoveryStrategyAutoWait // 默认策略
	}

	if fc.AutoWaitTimeout == 0 {
		fc.AutoWaitTimeout = 300 * time.Second // 默认5分钟
	}

	if fc.AutoWaitMaxRequests == 0 {
		fc.AutoWaitMaxRequests = 100 // 默认最多100个挂起请求
	}
}

// Validate 验证配置合法性
func (fc *FailoverConfigV2) Validate() error {
	if !fc.RecoveryStrategy.IsValid() {
		return fmt.Errorf("invalid recovery_strategy '%s', must be one of: auto_wait, loop_retry, fail_fast", fc.RecoveryStrategy)
	}

	if fc.RecoveryStrategy == RecoveryStrategyAutoWait {
		if fc.AutoWaitTimeout <= 0 {
			return fmt.Errorf("auto_wait_timeout must be greater than 0 when recovery_strategy is 'auto_wait'")
		}
		if fc.AutoWaitMaxRequests <= 0 {
			return fmt.Errorf("auto_wait_max_requests must be greater than 0 when recovery_strategy is 'auto_wait'")
		}
		if fc.AutoWaitMaxRequests > 10000 {
			return fmt.Errorf("auto_wait_max_requests cannot exceed 10000 for performance reasons")
		}
	}

	return nil
}
