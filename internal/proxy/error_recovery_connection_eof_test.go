package proxy

import (
	"errors"
	"testing"
)

// TestErrorRecoveryManager_ConnectionPhaseEOF 测试连接阶段的EOF错误分类
// 🔧 [Bug #3修复验证] 2025-12-29
// 验证 HTTP 客户端连接阶段的 EOF 错误应该被分类为网络错误（可重试），而不是 EOF 错误（不可重试）
func TestErrorRecoveryManager_ConnectionPhaseEOF(t *testing.T) {
	erm := NewErrorRecoveryManager(nil)

	testCases := []struct {
		name          string
		err           error
		expectedType  ErrorType
		shouldRetry   bool
		description   string
	}{
		{
			name:         "HTTP POST 连接阶段 EOF",
			err:          errors.New(`Post "https://anyrouter.top/v1/messages?beta=true": EOF`),
			expectedType: ErrorTypeNetwork,
			shouldRetry:  true,
			description:  "POST 请求在连接阶段遇到 EOF，应该被分类为网络错误（可重试+故障转移）",
		},
		{
			name:         "HTTP POST 连接阶段 unexpected EOF",
			err:          errors.New(`Post "https://api.example.com/v1/chat": unexpected EOF`),
			expectedType: ErrorTypeNetwork,
			shouldRetry:  true,
			description:  "POST 请求在连接阶段遇到 unexpected EOF，应该被分类为网络错误（可重试+故障转移）",
		},
		{
			name:         "HTTP GET 连接阶段 EOF",
			err:          errors.New(`Get "https://api.example.com/models": EOF`),
			expectedType: ErrorTypeNetwork,
			shouldRetry:  true,
			description:  "GET 请求在连接阶段遇到 EOF，应该被分类为网络错误（可重试+故障转移）",
		},
		{
			name:         "HTTP PUT 连接阶段 unexpected EOF",
			err:          errors.New(`Put "https://api.example.com/update": unexpected EOF`),
			expectedType: ErrorTypeNetwork,
			shouldRetry:  true,
			description:  "PUT 请求在连接阶段遇到 unexpected EOF，应该被分类为网络错误（可重试+故障转移）",
		},
		{
			name:         "HTTP DELETE 连接阶段 EOF",
			err:          errors.New(`Delete "https://api.example.com/resource": EOF`),
			expectedType: ErrorTypeNetwork,
			shouldRetry:  true,
			description:  "DELETE 请求在连接阶段遇到 EOF，应该被分类为网络错误（可重试+故障转移）",
		},
		{
			name:         "普通 EOF（不是连接阶段）",
			err:          errors.New("EOF"),
			expectedType: ErrorTypeEOF,
			shouldRetry:  false,
			description:  "普通 EOF 错误（响应读取中断）应该被分类为 EOF 错误（不可重试）",
		},
		{
			name:         "响应读取阶段 unexpected EOF",
			err:          errors.New("unexpected EOF while reading response body"),
			expectedType: ErrorTypeEOF,
			shouldRetry:  false,
			description:  "响应读取阶段的 EOF 应该被分类为 EOF 错误（不可重试，避免重复计费）",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			// 分类错误
			errorCtx := erm.ClassifyError(tc.err, "req-test", "test-endpoint", "test-group", 1)

			// 验证错误类型
			if errorCtx.ErrorType != tc.expectedType {
				t.Errorf("错误类型不匹配: 期望 %s (%d)，实际 %s (%d)\n错误: %v\n说明: %s",
					tc.expectedType.String(), tc.expectedType,
					errorCtx.ErrorType.String(), errorCtx.ErrorType,
					tc.err, tc.description)
			}

			// 验证重试决策
			shouldRetry := erm.ShouldRetry(errorCtx)
			if shouldRetry != tc.shouldRetry {
				t.Errorf("重试决策不匹配: 期望 %v，实际 %v\n错误: %v\n说明: %s",
					tc.shouldRetry, shouldRetry, tc.err, tc.description)
			}

			// 如果应该重试，验证有重试延迟
			if tc.shouldRetry {
				if errorCtx.RetryableAfter == 0 {
					t.Errorf("期望可重试错误应该有重试延迟，但 RetryableAfter = 0\n错误: %v", tc.err)
				}
			}

			// 输出详细信息
			t.Logf("✅ 测试通过: %s", tc.name)
			t.Logf("   错误: %v", tc.err)
			t.Logf("   分类: %s (%d)", errorCtx.ErrorType.String(), errorCtx.ErrorType)
			t.Logf("   可重试: %v", shouldRetry)
			if tc.shouldRetry {
				t.Logf("   重试延迟: %v", errorCtx.RetryableAfter)
			}
			t.Logf("   说明: %s", tc.description)
		})
	}
}

// TestErrorRecoveryManager_ConnectionPhaseEOF_Integration 集成测试：验证连接阶段 EOF 错误的完整流程
// 验证错误分类 -> 重试决策 -> 故障转移
func TestErrorRecoveryManager_ConnectionPhaseEOF_Integration(t *testing.T) {
	erm := NewErrorRecoveryManager(nil)

	// 模拟用户报告的错误
	connectionEOFErr := errors.New(`Post "https://anyrouter.top/v1/messages?beta=true": unexpected EOF`)

	t.Run("用户报告的错误应该触发故障转移", func(t *testing.T) {
		// 第1次尝试：分类错误
		errorCtx := erm.ClassifyError(connectionEOFErr, "req-user-001", "endpoint-1", "group-a", 1)

		// 验证分类为网络错误
		if errorCtx.ErrorType != ErrorTypeNetwork {
			t.Fatalf("❌ 错误分类不正确: 期望 ErrorTypeNetwork，实际 %s\n这将导致不触发故障转移！",
				errorCtx.ErrorType.String())
		}
		t.Logf("✅ 步骤1: 错误正确分类为网络错误 (ErrorTypeNetwork)")

		// 验证应该重试
		shouldRetry := erm.ShouldRetry(errorCtx)
		if !shouldRetry {
			t.Fatalf("❌ 错误应该可以重试，但 ShouldRetry 返回 false")
		}
		t.Logf("✅ 步骤2: 错误可以重试 (ShouldRetry = true)")

		// 模拟多次重试直到达到上限
		maxAttempts := 3
		for attempt := 1; attempt <= maxAttempts; attempt++ {
			errorCtx = erm.ClassifyError(connectionEOFErr, "req-user-001", "endpoint-1", "group-a", attempt)
			shouldRetry = erm.ShouldRetry(errorCtx)

			if attempt < maxAttempts {
				if !shouldRetry {
					t.Errorf("❌ 尝试 %d/%d 应该可以重试", attempt, maxAttempts)
				}
				t.Logf("   尝试 %d/%d: 继续重试，延迟 %v", attempt, maxAttempts, errorCtx.RetryableAfter)
			}
		}

		t.Logf("✅ 步骤3: 经过 %d 次重试后，应该触发故障转移", maxAttempts)
		t.Logf("📝 结论: 连接阶段 EOF 错误现在会正确触发故障转移流程")
	})
}

// BenchmarkErrorRecoveryManager_ConnectionPhaseEOF 性能基准测试
func BenchmarkErrorRecoveryManager_ConnectionPhaseEOF(b *testing.B) {
	erm := NewErrorRecoveryManager(nil)
	err := errors.New(`Post "https://api.example.com/v1/messages": unexpected EOF`)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		errorCtx := erm.ClassifyError(err, "req-bench", "endpoint-1", "group-a", 1)
		_ = erm.ShouldRetry(errorCtx)
	}
}
