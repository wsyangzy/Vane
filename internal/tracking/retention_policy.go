package tracking

import (
	"fmt"
	"time"

	_ "modernc.org/sqlite"
)

type retentionPolicy struct {
	retentionDays   int
	cleanupInterval time.Duration
}

func (ut *UsageTracker) currentRetentionPolicy() retentionPolicy {
	ut.retentionMu.RLock()
	defer ut.retentionMu.RUnlock()
	if ut.config == nil {
		return retentionPolicy{}
	}
	return retentionPolicy{
		retentionDays:   ut.config.RetentionDays,
		cleanupInterval: ut.config.CleanupInterval,
	}
}

// UpdateRetentionPolicy applies cleanup settings without restarting the tracker.
func (ut *UsageTracker) UpdateRetentionPolicy(retentionDays int) error {
	return ut.applyRetentionPolicy(retentionDays, true)
}

// ConfigureRetentionPolicy loads cleanup settings without starting maintenance immediately.
func (ut *UsageTracker) ConfigureRetentionPolicy(retentionDays int) error {
	return ut.applyRetentionPolicy(retentionDays, false)
}

func (ut *UsageTracker) applyRetentionPolicy(retentionDays int, runNow bool) error {
	if ut == nil || ut.config == nil {
		return fmt.Errorf("使用追踪器未初始化")
	}
	if retentionDays < 0 {
		return fmt.Errorf("数据保留天数不能小于 0")
	}
	ut.retentionMu.Lock()
	ut.config.RetentionDays = retentionDays
	ut.retentionMu.Unlock()

	if !runNow {
		return nil
	}
	select {
	case ut.retentionChanged <- struct{}{}:
	default:
	}
	return nil
}
