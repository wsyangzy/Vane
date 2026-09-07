package proxy

import (
	"context"
	"errors"
	"testing"

	"ai-relay-forwarder/internal/tracking"
)

// TestClientCancelDetection 测试客户端取消检测功能
func TestClientCancelDetection(t *testing.T) {
	// 创建错误恢复管理器
	tracker := &tracking.UsageTracker{} // Mock tracker
	errorRecovery := NewErrorRecoveryManager(tracker)

	// 测试案例
	tests := []struct {
		name         string
		err          error
		expectedType ErrorType
		shouldRetry  bool
	}{
		{
			name:         "Context Canceled",
			err:          context.Canceled,
			expectedType: ErrorTypeClientCancel,
			shouldRetry:  false,
		},
		{
			name:         "Custom Cancel Error",
			err:          errors.New("context canceled"),
			expectedType: ErrorTypeClientCancel,
			shouldRetry:  false,
		},
		{
			name:         "Network Error",
			err:          errors.New("connection refused"),
			expectedType: ErrorTypeNetwork,
			shouldRetry:  true,
		},
		{
			// context.DeadlineExceeded 被保守处理为响应超时
			// 原因：无法确定请求是否已被服务器处理，不重试避免重复计费
			name:         "Timeout Error",
			err:          context.DeadlineExceeded,
			expectedType: ErrorTypeResponseTimeout,
			shouldRetry:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// 分类错误
			errorCtx := errorRecovery.ClassifyError(tt.err, "test-req", "test-endpoint", "test-group", 0)

			// 验证错误类型
			if errorCtx.ErrorType != tt.expectedType {
				t.Errorf("Expected error type %v, got %v", tt.expectedType, errorCtx.ErrorType)
			}

			// 验证重试判断
			shouldRetry := errorRecovery.ShouldRetry(errorCtx)
			if shouldRetry != tt.shouldRetry {
				t.Errorf("Expected shouldRetry %v, got %v", tt.shouldRetry, shouldRetry)
			}

			// 特别验证客户端取消错误不可重试
			if tt.expectedType == ErrorTypeClientCancel && shouldRetry {
				t.Error("Client cancel error should never be retryable")
			}
		})
	}
}

// TestLifecycleManagerClientCancel 测试生命周期管理器处理客户端取消
func TestLifecycleManagerClientCancel(t *testing.T) {
	// 创建生命周期管理器
	lifecycle := NewRequestLifecycleManager(nil, nil, "test-req-123", nil)
	lifecycle.SetEndpoint("test-endpoint", "test-group", "")

	// 模拟客户端取消错误
	cancelErr := context.Canceled

	// 处理错误
	lifecycle.HandleError(cancelErr)

	// 验证状态
	if lifecycle.GetLastStatus() != "cancelled" {
		t.Errorf("Expected status 'cancelled', got '%s'", lifecycle.GetLastStatus())
	}

	// 验证不应该重试 - 直接使用错误恢复管理器判断
	errorCtx := lifecycle.errorRecovery.ClassifyError(cancelErr, "test-req-123", "test-endpoint", "test-group", 0)
	if lifecycle.errorRecovery.ShouldRetry(errorCtx) {
		t.Error("Client cancelled request should not be retryable")
	}

	// 验证错误记录
	if lifecycle.GetLastError() != cancelErr {
		t.Error("Last error should be the cancel error")
	}
}

// TestStreamProcessingClientCancel 测试流式处理中的客户端取消
func TestStreamProcessingClientCancel(t *testing.T) {
	// 创建上下文并取消
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // 立即取消

	// 创建错误恢复管理器
	errorRecovery := NewErrorRecoveryManager(nil)

	// 模拟流式处理错误（context canceled）
	streamErr := ctx.Err() // 这会是 context.Canceled

	// 分类错误
	errorCtx := errorRecovery.ClassifyError(streamErr, "test-stream", "test-endpoint", "test-group", 1)

	// 验证被正确分类为客户端取消
	if errorCtx.ErrorType != ErrorTypeClientCancel {
		t.Errorf("Stream cancel error should be classified as ClientCancel, got %v", errorCtx.ErrorType)
	}

	// 验证不应该重试
	if errorRecovery.ShouldRetry(errorCtx) {
		t.Error("Stream cancel error should not be retryable")
	}

	// 验证重试延迟为0（不可重试）
	if errorCtx.RetryableAfter != 0 {
		t.Errorf("Cancel error should have RetryableAfter = 0, got %v", errorCtx.RetryableAfter)
	}
}

// TestErrorTypePriority 测试错误类型优先级
func TestErrorTypePriority(t *testing.T) {
	errorRecovery := NewErrorRecoveryManager(nil)

	// 当前实现优先识别客户端取消
	// "connection timeout: context canceled" 命中 context canceled，会先归类为用户取消
	mixedErr := errors.New("connection timeout: context canceled")
	errorCtx := errorRecovery.ClassifyError(mixedErr, "test", "endpoint", "group", 0)

	if errorCtx.ErrorType != ErrorTypeClientCancel {
		t.Errorf("Mixed error containing context canceled should be classified as ClientCancel, got %v", errorCtx.ErrorType)
	}

	// 纯粹的 context canceled 才是用户取消
	pureCancel := context.Canceled
	errorCtx2 := errorRecovery.ClassifyError(pureCancel, "test", "endpoint", "group", 0)
	if errorCtx2.ErrorType != ErrorTypeClientCancel {
		t.Errorf("Pure context.Canceled should be classified as ClientCancel, got %v", errorCtx2.ErrorType)
	}
}

// TestShouldSwitchEndpoint 测试端点切换判断
// 核心原则：切换端点不会导致重复计费，所以几乎所有错误都应允许切换
func TestShouldSwitchEndpoint(t *testing.T) {
	errorRecovery := NewErrorRecoveryManager(nil)

	tests := []struct {
		name         string
		err          error
		shouldSwitch bool
		reason       string
	}{
		{
			name:         "ClientCancel - 不切换",
			err:          context.Canceled,
			shouldSwitch: false,
			reason:       "用户主动取消，无需切换",
		},
		{
			name:         "EOF - 允许切换",
			err:          errors.New("Post https://example.com: EOF"),
			shouldSwitch: true,
			reason:       "切换到新端点可能成功",
		},
		{
			name:         "ResponseTimeout - 允许切换",
			err:          context.DeadlineExceeded,
			shouldSwitch: true,
			reason:       "当前端点可能有问题",
		},
		{
			name:         "Network - 允许切换",
			err:          errors.New("connection refused"),
			shouldSwitch: true,
			reason:       "网络问题，换端点可能解决",
		},
		{
			name:         "Stream truncated - 允许切换",
			err:          errors.New("stream_error: unexpected EOF"),
			shouldSwitch: true,
			reason:       "流错误允许切换（切换不会重复计费）",
		},
		{
			name:         "HTTP 400 - 允许切换",
			err:          errors.New("endpoint returned error: 400"),
			shouldSwitch: true,
			reason:       "不同中转商可能有不同处理",
		},
		{
			name:         "HTTP 403 - 允许切换",
			err:          errors.New("HTTP 403: Forbidden"),
			shouldSwitch: true,
			reason:       "可能是特定端点权限问题",
		},
		{
			name:         "Server Error 502 - 允许切换",
			err:          errors.New("endpoint returned error: 502"),
			shouldSwitch: true,
			reason:       "服务器错误应尝试其他端点",
		},
		{
			name:         "Connection Timeout - 允许切换",
			err:          errors.New("http2: timeout awaiting response headers"),
			shouldSwitch: true,
			reason:       "连接阶段超时，未计费",
		},
		{
			name:         "Unknown Error - 允许切换",
			err:          errors.New("some unknown error"),
			shouldSwitch: true,
			reason:       "未知错误默认允许切换（安全策略）",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			errorCtx := errorRecovery.ClassifyError(tt.err, "test", "endpoint", "group", 0)
			shouldSwitch := errorRecovery.ShouldSwitchEndpoint(errorCtx)

			if shouldSwitch != tt.shouldSwitch {
				t.Errorf("ShouldSwitchEndpoint() = %v, want %v (reason: %s, errorType: %v)",
					shouldSwitch, tt.shouldSwitch, tt.reason, errorCtx.ErrorType)
			}
		})
	}
}

// BenchmarkErrorClassification 基准测试错误分类性能
func BenchmarkErrorClassification(b *testing.B) {
	errorRecovery := NewErrorRecoveryManager(nil)
	cancelErr := context.Canceled

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		errorCtx := errorRecovery.ClassifyError(cancelErr, "bench-req", "bench-endpoint", "bench-group", i)
		if errorCtx.ErrorType != ErrorTypeClientCancel {
			b.Error("Unexpected error classification")
		}
	}
}
