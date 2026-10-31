package handlers

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/wsyangzy/Vane/internal/config"
	"github.com/wsyangzy/Vane/internal/providers/claude/proxy/endpoint"
	"github.com/wsyangzy/Vane/internal/providers/claude/proxy/subscription"
	"github.com/wsyangzy/Vane/internal/routing"
)

func TestCountTokensSettingsUseLocalEstimatesOrProfileUpstream(t *testing.T) {
	var sends atomic.Int32
	body := `{"model":"test","messages":[{"role":"user","content":"abcdefgh"}]}`
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sends.Add(1)
		if r.URL.Path != "/v1/messages/count_tokens" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"input_tokens":123}`))
	}))
	defer server.Close()
	cfg := config.NewDefaultConfig()
	cfg.Endpoints = []config.EndpointConfig{{Name: "test", Channel: "A", CredentialGroupID: 1, URL: server.URL, SkipHealthCheck: true, SupportsCountTokens: true}}
	manager := endpoint.NewManager(cfg)
	manager.SetRouteConfig(&endpoint.RouteConfig{ChannelNames: map[int64]string{1: "A"}})
	pool := routing.NewPoolRuntime(routing.PoolConfig{Accounts: []routing.AccountState{{ChannelID: 1, GroupID: 1, Enabled: true}}}, nil, nil)
	profile := routing.DefaultProfile("claude")
	profile.ID, profile.IsDefault = 1, true
	profile.Members = []routing.RoutingMember{{ChannelID: 1, GroupID: 1, Enabled: true}}
	pool.SetProfileRuntime(routing.NewProfileRuntime(routing.RuntimeConfig{
		Provider: "claude", Profiles: []routing.RoutingProfile{profile},
		Candidates: []routing.DispatchCandidate{{Provider: "claude", ChannelID: 1, GroupID: 1, EndpointID: endpoint.EndpointKey("A", "test"), Enabled: true, Protocol: "messages"}},
	}))
	manager.SetPoolRuntime(pool)
	h := NewCountTokensHandler(cfg, manager, NewForwarder(cfg, manager), nil)
	for _, poolCaller := range []bool{false, true} {
		t.Run(fmt.Sprintf("pool-caller=%t", poolCaller), func(t *testing.T) {
			for _, scenario := range []struct {
				enabled bool
				ratio   float64
				want    int
				sends   int32
			}{{false, 2, 54, 0}, {false, 4, 52, 0}, {true, 4, 123, 1}, {false, 4, 52, 0}} {
				cfg.TokenCounting.Enabled, cfg.TokenCounting.EstimationRatio = scenario.enabled, scenario.ratio
				request := httptest.NewRequest("POST", "/v1/messages/count_tokens", strings.NewReader(body))
				if poolCaller {
					request = request.WithContext(routing.WithPoolCaller(request.Context(), routing.PoolCaller{Provider: "claude", PoolID: 1, Revision: 1}))
				}
				recorder := httptest.NewRecorder()
				sends.Store(0)
				h.Handle(request.Context(), recorder, request, []byte(body), "settings-test", nil)
				var response CountTokensResponse
				if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil || recorder.Code != 200 || response.InputTokens != scenario.want || sends.Load() != scenario.sends {
					t.Fatalf("setting had no expected effect: enabled=%v ratio=%v response=%s sends=%d err=%v", scenario.enabled, scenario.ratio, recorder.Body.String(), sends.Load(), err)
				}
				if !scenario.enabled && recorder.Header().Get("X-Token-Estimation") != "true" {
					t.Fatal("disabled forwarding did not mark the local estimate")
				}
			}
		})
	}
}

func TestDisabledCountTokensBypassesSubscriptionRoutingAndPrivacyTransport(t *testing.T) {
	keyHash := sha256.Sum256([]byte("test-local-key"))
	store := &subscriptionMemoryStore{binding: subscription.Binding{GroupID: 1, Version: 1, Enabled: true, KeyHash: hex.EncodeToString(keyHash[:])}}
	controller := subscription.NewController(store)
	caller := httptest.NewRequest("POST", "/v1/messages/count_tokens", nil)
	caller.RemoteAddr = "127.0.0.1:1234"
	caller.Header.Set("X-Relay-Key", "test-local-key")
	caller.Header.Set("Authorization", "Bearer test-subscription-token")
	ctx, _, err := controller.Authenticate(caller)
	if err != nil {
		t.Fatal(err)
	}
	if subscription.FromContext(ctx) == nil {
		t.Fatal("fixture did not authenticate a subscription caller")
	}
	// With no manager or forwarder, any attempt to plan or send upstream fails.
	// Disabled forwarding must answer locally before touching either dependency.
	for _, cfg := range []*config.Config{nil, config.NewDefaultConfig()} {
		handler := NewCountTokensHandler(cfg, nil, nil, nil)
		for _, body := range []string{`{"messages":[{"role":"user","content":"abcdefgh"}]}`, `{"messages":`} {
			request := httptest.NewRequest("POST", "/v1/messages/count_tokens", strings.NewReader(body)).WithContext(ctx)
			recorder := httptest.NewRecorder()
			handler.Handle(ctx, recorder, request, []byte(body), "disabled-subscription", nil)
			if json.Valid([]byte(body)) {
				if recorder.Code != 200 || recorder.Header().Get("X-Token-Estimation") != "true" || recorder.Body.String() != `{"input_tokens":52}` {
					t.Fatalf("subscription request was not estimated locally: %d %s", recorder.Code, recorder.Body.String())
				}
			} else if recorder.Code != http.StatusBadRequest {
				t.Fatalf("invalid local count request should fail without forwarding: %d %s", recorder.Code, recorder.Body.String())
			}
		}
	}
}

func TestDefaultAndHeaderTimeoutSettingsApplyToNewClaudeClients(t *testing.T) {
	cfg := config.NewDefaultConfig()
	cfg.GlobalTimeout = 12 * time.Second
	f := NewForwarder(cfg, nil)
	old, err := f.NewClient(0)
	if err != nil || old.Timeout != 12*time.Second {
		t.Fatalf("default timeout was not applied: %v, %v", old, err)
	}
	cfg.GlobalTimeout = 24 * time.Second
	cfg.Streaming.DefaultResponseHeaderTimeout = 8 * time.Second
	next, err := f.NewClient(0)
	if err != nil || next.Timeout != 24*time.Second || old.Timeout != 12*time.Second {
		t.Fatal("settings update changed an existing client's timeout")
	}
	if next.Transport.(*http.Transport).ResponseHeaderTimeout != 8*time.Second {
		t.Fatal("non-streaming header timeout was not applied")
	}
	explicit, err := f.NewClient(time.Second)
	if err != nil || explicit.Timeout != time.Second {
		t.Fatal("endpoint timeout must take precedence over the default")
	}
}

func TestForwarder_ForwardRequestToEndpoint(t *testing.T) {
	// 创建测试服务器（返回 SSE Content-Type 以通过流式校验）
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// 验证请求头
		if r.Header.Get("Authorization") == "" {
			t.Errorf("Expected Authorization header to be set")
		}

		// 验证Host头
		expectedHost := r.Host
		if expectedHost == "" {
			t.Errorf("Expected Host header to be set")
		}

		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("test response"))
	}))
	defer server.Close()

	// 创建配置
	cfg := &config.Config{}

	// 创建端点配置
	endpointConfig := config.EndpointConfig{
		Name:     "test-endpoint",
		URL:      server.URL,
		Token:    "test-token",
		Timeout:  30 * time.Second,
		Priority: 1,
	}

	// 创建端点管理器
	endpointManager := endpoint.NewManager(cfg)
	ep := &endpoint.Endpoint{Config: endpointConfig}

	// 创建Forwarder
	forwarder := NewForwarder(cfg, endpointManager)

	// 创建测试请求
	bodyBytes := []byte(`{"message": "test"}`)
	req := httptest.NewRequest("POST", "/test", bytes.NewReader(bodyBytes))
	req.Header.Set("Content-Type", "application/json")

	// 执行转发
	ctx := context.Background()
	resp, err := forwarder.ForwardRequestToEndpoint(ctx, req, bodyBytes, ep)

	if err != nil {
		t.Fatalf("ForwardRequestToEndpoint failed: %v", err)
	}

	if resp == nil {
		t.Fatal("Expected response, got nil")
	}

	if resp.StatusCode != http.StatusOK {
		t.Errorf("Expected status 200, got %d", resp.StatusCode)
	}

	// 验证响应体
	defer resp.Body.Close()
	body := make([]byte, 1024)
	n, _ := resp.Body.Read(body)
	responseBody := string(body[:n])

	if responseBody != "test response" {
		t.Errorf("Expected 'test response', got '%s'", responseBody)
	}
}

func TestForwarder_CopyHeaders(t *testing.T) {
	// 创建配置
	cfg := &config.Config{}

	// 创建端点配置
	endpointConfig := config.EndpointConfig{
		Name:   "test-endpoint",
		URL:    "https://api.example.com",
		Token:  "test-token",
		ApiKey: "test-api-key",
		Headers: map[string]string{
			"X-Custom-Header": "custom-value",
		},
	}

	// 创建端点管理器
	endpointManager := endpoint.NewManager(cfg)
	ep := &endpoint.Endpoint{Config: endpointConfig}

	// 创建Forwarder
	forwarder := NewForwarder(cfg, endpointManager)

	// 创建源请求
	srcReq := httptest.NewRequest("POST", "/test", nil)
	srcReq.Header.Set("Content-Type", "application/json")
	srcReq.Header.Set("User-Agent", "Test-Client")
	srcReq.Header.Set("Authorization", "Bearer client-token") // 应该被覆盖
	srcReq.Header.Set("X-API-Key", "client-api-key")          // 应该被移除

	// 创建目标请求
	dstReq := httptest.NewRequest("POST", "https://api.example.com/test", nil)

	// 执行头部复制
	forwarder.CopyHeaders(srcReq, dstReq, ep)

	// 验证结果
	if dstReq.Header.Get("Content-Type") != "application/json" {
		t.Errorf("Expected Content-Type to be preserved")
	}

	if dstReq.Header.Get("User-Agent") != "Test-Client" {
		t.Errorf("Expected User-Agent to be preserved")
	}

	if dstReq.Header.Get("Authorization") != "Bearer test-token" {
		t.Errorf("Expected Authorization to be replaced with endpoint token")
	}

	if dstReq.Header.Get("X-Api-Key") != "test-api-key" {
		t.Errorf("Expected X-Api-Key to be set from endpoint config")
	}

	if dstReq.Header.Get("X-Custom-Header") != "custom-value" {
		t.Errorf("Expected custom header to be added")
	}

	if dstReq.Header.Get("Host") != "api.example.com" {
		t.Errorf("Expected Host header to be set correctly")
	}

	// 验证敏感头部被移除
	if dstReq.Header.Get("X-API-Key") == "client-api-key" {
		t.Errorf("Expected client X-API-Key to be removed")
	}
}
