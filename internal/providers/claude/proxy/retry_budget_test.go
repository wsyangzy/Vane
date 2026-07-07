package proxy

import (
	"testing"
	"time"
)

func TestRetryBudget_InitOnce(t *testing.T) {
	rlm := NewRequestLifecycleManager(nil, nil, "test-req-1", nil)

	rlm.InitRetryBudget(10, 60*time.Second)
	// 第二次调用不应覆盖
	rlm.InitRetryBudget(99, 999*time.Second)

	if rlm.budgetMaxAttempts != 10 {
		t.Errorf("expected maxAttempts=10, got %d", rlm.budgetMaxAttempts)
	}
	if rlm.budgetMaxDuration != 60*time.Second {
		t.Errorf("expected maxDuration=60s, got %v", rlm.budgetMaxDuration)
	}
}

func TestRetryBudget_NotExhaustedInitially(t *testing.T) {
	rlm := NewRequestLifecycleManager(nil, nil, "test-req-2", nil)
	rlm.InitRetryBudget(5, 30*time.Second)

	exhausted, reason := rlm.CheckRetryBudget()
	if exhausted {
		t.Errorf("budget should not be exhausted initially, reason: %s", reason)
	}
}

func TestRetryBudget_ExhaustedByAttempts(t *testing.T) {
	rlm := NewRequestLifecycleManager(nil, nil, "test-req-3", nil)
	rlm.InitRetryBudget(3, 60*time.Second)

	// 模拟 3 次尝试
	rlm.IncrementAttempt() // 1
	rlm.IncrementAttempt() // 2
	rlm.IncrementAttempt() // 3

	exhausted, reason := rlm.CheckRetryBudget()
	if !exhausted {
		t.Error("budget should be exhausted after 3 attempts with limit=3")
	}
	if reason == "" {
		t.Error("reason should not be empty")
	}
}

func TestRetryBudget_ExhaustedByDuration(t *testing.T) {
	rlm := NewRequestLifecycleManager(nil, nil, "test-req-4", nil)
	// 设置极短的时长预算
	rlm.InitRetryBudget(100, 1*time.Millisecond)

	// 等待超过预算时长
	time.Sleep(5 * time.Millisecond)

	exhausted, reason := rlm.CheckRetryBudget()
	if !exhausted {
		t.Error("budget should be exhausted after duration exceeded")
	}
	if reason == "" {
		t.Error("reason should not be empty")
	}
}
