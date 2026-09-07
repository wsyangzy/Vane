package proxy

import (
	"testing"
	"time"

	"ai-relay-forwarder/config"
	"ai-relay-forwarder/internal/providers/claude/proxy/endpoint"
)

// TestHasResumableEndpointSkipsStreamOnly 锁定挂起唤醒的判据：
// 非流式请求不会被路由到 stream_only 端点，唤醒判断必须同样排除它们，
// 否则请求被唤醒后在 regular.go 的过滤中拿到空列表，白跑一轮渠道转移。
func TestHasResumableEndpointSkipsStreamOnly(t *testing.T) {
	newSM := func(endpoints []config.EndpointConfig) *SuspensionManager {
		cfg := &config.Config{
			Strategy:  config.StrategyConfig{Type: "priority"},
			Health:    config.HealthConfig{Timeout: 5 * time.Second},
			Endpoints: endpoints,
		}
		mgr := endpoint.NewManager(cfg)
		if err := mgr.ManualActivateGroupWithForce("A", true); err != nil {
			t.Fatalf("ManualActivateGroupWithForce(A) error: %v", err)
		}
		return NewSuspensionManager(cfg, mgr, endpoint.NewGroupManager(cfg))
	}

	streamOnly := config.EndpointConfig{
		Name: "stream-only", URL: "http://example.invalid/a1", Channel: "A",
		Priority: 1, SkipHealthCheck: true, StreamOnly: true, Timeout: time.Second,
	}
	regular := config.EndpointConfig{
		Name: "regular", URL: "http://example.invalid/a2", Channel: "A",
		Priority: 2, SkipHealthCheck: true, Timeout: time.Second,
	}

	t.Run("仅剩 stream_only 时不唤醒非流请求", func(t *testing.T) {
		sm := newSM([]config.EndpointConfig{streamOnly})
		if sm.hasResumableEndpoint(true) {
			t.Error("非流请求不应因 stream_only 端点健康而被唤醒")
		}
		if !sm.hasResumableEndpoint(false) {
			t.Error("流式请求应能被 stream_only 端点唤醒")
		}
	})

	t.Run("存在非流端点时两者都唤醒", func(t *testing.T) {
		sm := newSM([]config.EndpointConfig{streamOnly, regular})
		if !sm.hasResumableEndpoint(true) {
			t.Error("存在非 stream_only 端点时应唤醒非流请求")
		}
		if !sm.hasResumableEndpoint(false) {
			t.Error("存在健康端点时应唤醒流式请求")
		}
	})
}
