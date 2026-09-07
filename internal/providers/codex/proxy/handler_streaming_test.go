package codexproxy

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"ai-relay-forwarder/config"
	"ai-relay-forwarder/internal/tracking"
)

type contextCanceledAfterReadCloser struct {
	reader *strings.Reader
}

func (r *contextCanceledAfterReadCloser) Read(p []byte) (int, error) {
	n, err := r.reader.Read(p)
	if err == io.EOF {
		return n, context.Canceled
	}
	return n, err
}

func (r *contextCanceledAfterReadCloser) Close() error {
	return nil
}

func TestHandleStreamingReturnsCapacityErrorBeforeClientOutput(t *testing.T) {
	handler := &Handler{
		tokenParser: NewTokenParser(),
		logger:      newTestLogger(),
	}
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Header: http.Header{
			"Content-Type": []string{"text/event-stream"},
		},
		Body: io.NopCloser(strings.NewReader(
			"event: response.failed\n" +
				"data: {\"type\":\"response.failed\",\"response\":{\"status\":\"failed\",\"error\":{\"code\":\"server_error\",\"message\":\"Selected model is at capacity. Please try a different model.\"}}}\n\n",
		)),
	}
	recorder := httptest.NewRecorder()

	streamErr := handler.handleStreaming(recorder, resp, "req-capacity", time.Now(), "gpt-5.4", "primary", "轮询", "CCTQ", 1)
	if streamErr == nil {
		t.Fatal("expected capacity stream error")
	}
	if action := NewErrorClassifier().Classify(nil, streamErr); action != ActionFailover {
		t.Fatalf("capacity 错误应切换端点，got %v", action)
	}
	if recorder.Body.Len() != 0 {
		t.Fatalf("expected no client output before failover, got %q", recorder.Body.String())
	}
	if recorder.Code != http.StatusOK {
		t.Fatalf("expected recorder to remain unwritten default 200, got %d", recorder.Code)
	}
}

func TestRecordForwardingStoresCredentialGroupName(t *testing.T) {
	tracker, err := tracking.NewUsageTracker(&tracking.Config{
		Enabled:         true,
		DatabasePath:    filepath.Join(t.TempDir(), "usage.db"),
		BufferSize:      50,
		BatchSize:       5,
		FlushInterval:   10 * time.Millisecond,
		MaxRetry:        3,
		CleanupInterval: time.Hour,
		RetentionDays:   1,
		DefaultPricing: tracking.ModelPricing{
			Input:  1,
			Output: 1,
		},
	})
	if err != nil {
		t.Fatalf("create tracker: %v", err)
	}
	defer tracker.Close()

	handler := &Handler{
		tokenParser:  NewTokenParser(),
		logger:       newTestLogger(),
		usageTracker: tracker,
	}
	tracker.UpdateEndpointMultipliers(map[string]tracking.EndpointMultiplier{
		tracking.EndpointMultiplierKey("codex", "CCTQ", "国内"): {
			CostMultiplier:              9,
			BillingOutputTokenSource:    tracking.BillingOutputTokenSourceOutput,
			InputCostMultiplier:         9,
			OutputCostMultiplier:        9,
			CacheReadCostMultiplier:     9,
			CacheCreationCostMultiplier: 9,
		},
		tracking.EndpointMultiplierGroupKey("codex", "CCTQ", "轮询", "国内"): {
			CostMultiplier:           2.5,
			BillingOutputTokenSource: tracking.BillingOutputTokenSourceReasoning,
		},
	})
	requestID := "req-forwarding-group"
	req := httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(`{"model":"gpt-5.5","stream":true}`))
	handler.recordStart(requestID, req, true, "CCTQ", "gpt-5.5", modelRewriteResult{}, "", "")
	handler.recordForwarding(requestID, "国内", "CCTQ", "gpt-5.5", []chainNode{
		{Hop: 1, Channel: "CCTQ", CredentialGroup: "轮询", Endpoint: "国内"},
	})

	details, _, err := tracker.QueryRequestDetailsWithHotPool(context.Background(), &tracking.QueryOptions{Limit: 1})
	if err != nil {
		t.Fatalf("query request details: %v", err)
	}
	if len(details) != 1 {
		t.Fatalf("expected 1 request detail, got %d", len(details))
	}
	if details[0].GroupName != "轮询" {
		t.Fatalf("expected group name 轮询, got %q", details[0].GroupName)
	}
	active, ok := tracker.GetActiveRequest(requestID)
	if !ok {
		t.Fatalf("expected active request %s", requestID)
	}
	if active.CostMultiplier != 2.5 {
		t.Fatalf("expected group-scoped multiplier 2.5, got %.2f", active.CostMultiplier)
	}
	if active.BillingOutputTokenSource != tracking.BillingOutputTokenSourceReasoning {
		t.Fatalf("expected group-scoped billing output source %q, got %q", tracking.BillingOutputTokenSourceReasoning, active.BillingOutputTokenSource)
	}
}

func TestExtractCodexRequestMetadata(t *testing.T) {
	body := []byte(`{"reasoning":{"effort":"xhigh","summary":"concise"},"service_tier":"Priority"}`)
	if got := extractReasoning(body); got != `{"effort":"xhigh","summary":"concise"}` {
		t.Fatalf("unexpected reasoning metadata: %q", got)
	}
	if got := extractServiceTier(body); got != "priority" {
		t.Fatalf("unexpected service tier: %q", got)
	}
	if got := extractReasoning([]byte(`{"reasoning":null}`)); got != "" {
		t.Fatalf("null reasoning must be omitted, got %q", got)
	}
}

func TestRecordSuccessPersistsActualHTTPStatus(t *testing.T) {
	tracker, err := tracking.NewUsageTracker(&tracking.Config{
		Enabled:         true,
		DatabasePath:    filepath.Join(t.TempDir(), "usage.db"),
		BufferSize:      10,
		BatchSize:       5,
		FlushInterval:   time.Hour,
		MaxRetry:        1,
		CleanupInterval: time.Hour,
		RetentionDays:   1,
	})
	if err != nil {
		t.Fatalf("create tracker: %v", err)
	}
	defer tracker.Close()

	handler := &Handler{usageTracker: tracker, logger: newTestLogger()}
	requestID := "req-success-http-status"
	tracker.RecordRequestStart(requestID, "127.0.0.1", "test-agent", http.MethodPost, "/v1/responses", false)
	handler.recordSuccess(requestID, time.Now(), http.StatusCreated, nil, "gpt-5.5", "primary", "default", "CCTQ")

	details, _, err := tracker.QueryRequestDetailsWithHotPool(context.Background(), &tracking.QueryOptions{Limit: 1})
	if err != nil {
		t.Fatalf("query request details: %v", err)
	}
	if len(details) != 1 || details[0].HTTPStatusCode == nil || *details[0].HTTPStatusCode != http.StatusCreated {
		t.Fatalf("expected HTTP status %d, got %#v", http.StatusCreated, details)
	}
}

func TestForwardWithRetryReturnsActualAttemptCount(t *testing.T) {
	var hits atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if hits.Add(1) == 1 {
			http.Error(w, "retry", http.StatusRequestTimeout)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer upstream.Close()

	handler := &Handler{
		forwarder:  NewForwarder(time.Second, newTestLogger()),
		streamFwd:  NewStreamingForwarder(newTestLogger()),
		classifier: NewErrorClassifier(),
		logger:     newTestLogger(),
		retryCfg: config.RetryConfig{
			MaxAttempts: 2,
		},
	}
	endpoint := &CodexEndpoint{Name: "primary", BaseURL: upstream.URL, APIKey: "test-key"}
	body := []byte(`{"model":"gpt-5.5","stream":false}`)
	req := httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(string(body)))

	resp, err, attempts := handler.forwardWithRetry(req, body, endpoint, false, "req-attempt-count")
	if err != nil {
		t.Fatalf("forwardWithRetry failed: %v", err)
	}
	defer resp.Body.Close()
	if attempts != 2 || hits.Load() != 2 {
		t.Fatalf("attempts = %d, upstream hits = %d, want 2", attempts, hits.Load())
	}
}

func TestTryEndpointCyclePersistsRetryCountAcrossEndpointsAndLoops(t *testing.T) {
	var primaryHits atomic.Int32
	primary := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if primaryHits.Add(1) <= 2 {
			http.Error(w, "retry", http.StatusRequestTimeout)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"model":"gpt-5.5","usage":{"input_tokens":1,"output_tokens":2}}`))
	}))
	defer primary.Close()
	backup := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "busy", http.StatusTooManyRequests)
	}))
	defer backup.Close()

	tracker, err := tracking.NewUsageTracker(&tracking.Config{
		Enabled: true, DatabasePath: filepath.Join(t.TempDir(), "usage.db"), BufferSize: 10, BatchSize: 5,
		FlushInterval: time.Hour, MaxRetry: 1, CleanupInterval: time.Hour, RetentionDays: 1,
	})
	if err != nil {
		t.Fatalf("create tracker: %v", err)
	}
	defer tracker.Close()
	manager := &EndpointManager{
		endpoints: []*CodexEndpoint{
			{ID: 1, Name: "primary", BaseURL: primary.URL, APIKey: "key", IsActive: true, FailoverEnabled: true, Priority: 0},
			{ID: 2, Name: "backup", BaseURL: backup.URL, APIKey: "key", FailoverEnabled: true, Priority: 1},
		},
		unhealthyMap: make(map[int64]bool), failureDetails: make(map[int64]EndpointFailureDetail), logger: newTestLogger(),
	}
	handler := &Handler{
		manager: manager, forwarder: NewForwarder(time.Second, newTestLogger()), streamFwd: NewStreamingForwarder(newTestLogger()),
		tokenParser: NewTokenParser(), classifier: NewErrorClassifier(), logger: newTestLogger(), usageTracker: tracker,
		retryCfg: config.RetryConfig{MaxAttempts: 2},
	}
	requestID := "req-retry-across-loops"
	body := []byte(`{"model":"gpt-5.5","stream":false}`)
	req := httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(string(body)))
	handler.recordStart(requestID, req, false, "CCTQ", "gpt-5.5", modelRewriteResult{}, "", "")
	var failedIDs []int64
	var endpointChain []chainNode
	attemptCount := 0

	first := handler.tryEndpointCycle(httptest.NewRecorder(), req, body, requestID, time.Now(), false, "CCTQ", "gpt-5.5", &failedIDs, &endpointChain, &attemptCount)
	if first.completed {
		t.Fatalf("expected first loop to exhaust endpoints, got %+v", first)
	}
	manager.ResetUnhealthy()
	failedIDs = failedIDs[:0]
	second := handler.tryEndpointCycle(httptest.NewRecorder(), req, body, requestID, time.Now(), false, "CCTQ", "gpt-5.5", &failedIDs, &endpointChain, &attemptCount)
	if !second.completed {
		t.Fatalf("expected second loop to succeed, got %+v", second)
	}

	details, _, err := tracker.QueryRequestDetailsWithHotPool(context.Background(), &tracking.QueryOptions{Limit: 1})
	if err != nil {
		t.Fatalf("query request details: %v", err)
	}
	if attemptCount != 4 || len(details) != 1 || details[0].RetryCount != 3 {
		t.Fatalf("attempts=%d details=%#v, want 4 attempts and 3 retries", attemptCount, details)
	}
}

func TestTryEndpointCycleTriesAllEndpointsAfterQuotaResponse(t *testing.T) {
	var primaryBHits atomic.Int32
	var backupHits atomic.Int32

	primaryA := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, `{"error":{"code":"insufficient_user_quota","message":"Insufficient account balance for the selected group. Please top up and try again, or switch to a group with available quota."}}`, http.StatusForbidden)
	}))
	defer primaryA.Close()
	primaryB := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		primaryBHits.Add(1)
		http.Error(w, `{"error":{"code":"insufficient_user_quota","message":"same group insufficient quota"}}`, http.StatusForbidden)
	}))
	defer primaryB.Close()
	backup := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		backupHits.Add(1)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"model":"gpt-5.5","usage":{"input_tokens":1,"output_tokens":2,"total_tokens":3}}`))
	}))
	defer backup.Close()

	manager := &EndpointManager{
		endpoints: []*CodexEndpoint{
			{
				ID:                   1,
				CredentialGroupID:    10,
				CredentialGroupName:  "主分组",
				CredentialGroupOrder: 0,
				Name:                 "国内-a",
				BaseURL:              primaryA.URL,
				APIKey:               "primary-key",
				IsActive:             true,
				FailoverEnabled:      true,
				SkipHealthCheck:      true,
				Priority:             0,
			},
			{
				ID:                   2,
				CredentialGroupID:    10,
				CredentialGroupName:  "主分组",
				CredentialGroupOrder: 0,
				Name:                 "国内-b",
				BaseURL:              primaryB.URL,
				APIKey:               "primary-key",
				FailoverEnabled:      true,
				SkipHealthCheck:      true,
				Priority:             1,
			},
			{
				ID:                   3,
				CredentialGroupID:    20,
				CredentialGroupName:  "备用分组",
				CredentialGroupOrder: 1,
				Name:                 "国内-backup",
				BaseURL:              backup.URL,
				APIKey:               "backup-key",
				FailoverEnabled:      true,
				Priority:             0,
			},
		},
		unhealthyMap:   make(map[int64]bool),
		failureDetails: make(map[int64]EndpointFailureDetail),
		logger:         newTestLogger(),
	}
	handler := &Handler{
		manager:       manager,
		forwarder:     NewForwarder(time.Second, newTestLogger()),
		streamFwd:     NewStreamingForwarder(newTestLogger()),
		tokenParser:   NewTokenParser(),
		classifier:    NewErrorClassifier(),
		logger:        newTestLogger(),
		usageTracker:  nil,
		suspensionMgr: nil,
	}
	body := []byte(`{"model":"gpt-5.5","stream":false}`)
	req := httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(string(body)))
	recorder := httptest.NewRecorder()
	var failedIDs []int64
	var endpointChain []chainNode

	result := handler.tryEndpointCycle(recorder, req, body, "req-rate-limit-group", time.Now(), false, "CCTQ", "gpt-5.5", &failedIDs, &endpointChain, new(int))
	if !result.completed {
		t.Fatalf("expected request to complete via backup endpoint, got incomplete result: %+v", result)
	}
	if recorder.Code != http.StatusOK {
		t.Fatalf("expected backup endpoint 200, got %d body=%q", recorder.Code, recorder.Body.String())
	}
	if primaryBHits.Load() != 1 {
		t.Fatalf("expected all endpoints in the credential group to be tried, got %d hits", primaryBHits.Load())
	}
	if backupHits.Load() != 1 {
		t.Fatalf("expected backup endpoint to be hit once, got %d hits", backupHits.Load())
	}
	if len(endpointChain) != 3 {
		t.Fatalf("expected chain to include both failed primary endpoints and backup, got %#v", endpointChain)
	}
	if endpointChain[0].CredentialGroup != "主分组" || endpointChain[1].CredentialGroup != "主分组" || endpointChain[2].CredentialGroup != "备用分组" {
		t.Fatalf("unexpected endpoint chain groups: %#v", endpointChain)
	}
	if endpointChain[0].StatusCode != http.StatusForbidden || !strings.Contains(endpointChain[0].Error, "Insufficient account balance") {
		t.Fatalf("expected failed chain node to include upstream error, got %#v", endpointChain[0])
	}
}

func TestTryEndpointCycleKeepsSameCredentialGroupAfterTransient429(t *testing.T) {
	var sameGroupHits atomic.Int32
	var backupHits atomic.Int32

	primary := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, `{"error":{"code":"rate_limit_exceeded","message":"Concurrency limit exceeded for account, please retry later"}}`, http.StatusTooManyRequests)
	}))
	defer primary.Close()
	sameGroup := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		sameGroupHits.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"model":"gpt-5.5","usage":{"input_tokens":1,"output_tokens":2,"total_tokens":3}}`))
	}))
	defer sameGroup.Close()
	backup := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		backupHits.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"model":"gpt-5.5","usage":{"input_tokens":1,"output_tokens":2,"total_tokens":3}}`))
	}))
	defer backup.Close()

	manager := &EndpointManager{
		endpoints: []*CodexEndpoint{
			{
				ID:                   1,
				CredentialGroupID:    10,
				CredentialGroupName:  "订阅",
				CredentialGroupOrder: 0,
				Name:                 "大陆优化-a",
				BaseURL:              primary.URL,
				APIKey:               "primary-key",
				IsActive:             true,
				FailoverEnabled:      true,
				SkipHealthCheck:      true,
				Priority:             0,
			},
			{
				ID:                   2,
				CredentialGroupID:    10,
				CredentialGroupName:  "订阅",
				CredentialGroupOrder: 0,
				Name:                 "大陆优化-b",
				BaseURL:              sameGroup.URL,
				APIKey:               "primary-key",
				FailoverEnabled:      true,
				SkipHealthCheck:      true,
				Priority:             1,
			},
			{
				ID:                   3,
				CredentialGroupID:    20,
				CredentialGroupName:  "备用",
				CredentialGroupOrder: 1,
				Name:                 "海外备用",
				BaseURL:              backup.URL,
				APIKey:               "backup-key",
				FailoverEnabled:      true,
				SkipHealthCheck:      true,
				Priority:             0,
			},
		},
		unhealthyMap:   make(map[int64]bool),
		failureDetails: make(map[int64]EndpointFailureDetail),
		logger:         newTestLogger(),
	}
	handler := &Handler{
		manager:      manager,
		forwarder:    NewForwarder(time.Second, newTestLogger()),
		streamFwd:    NewStreamingForwarder(newTestLogger()),
		tokenParser:  NewTokenParser(),
		classifier:   NewErrorClassifier(),
		logger:       newTestLogger(),
		usageTracker: nil,
	}
	body := []byte(`{"model":"gpt-5.5","stream":false}`)
	req := httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(string(body)))
	recorder := httptest.NewRecorder()
	var failedIDs []int64
	var endpointChain []chainNode

	result := handler.tryEndpointCycle(recorder, req, body, "req-transient-429", time.Now(), false, "合租巴士", "gpt-5.5", &failedIDs, &endpointChain, new(int))
	if !result.completed {
		t.Fatalf("expected request to complete via same-group endpoint, got incomplete result: %+v", result)
	}
	if recorder.Code != http.StatusOK {
		t.Fatalf("expected same-group endpoint 200, got %d body=%q", recorder.Code, recorder.Body.String())
	}
	if sameGroupHits.Load() != 1 {
		t.Fatalf("expected next endpoint in the same credential group to be hit once, got %d", sameGroupHits.Load())
	}
	if backupHits.Load() != 0 {
		t.Fatalf("transient 429 should not skip to the backup credential group, got %d backup hits", backupHits.Load())
	}
	if len(endpointChain) != 2 || endpointChain[0].CredentialGroup != "订阅" || endpointChain[1].CredentialGroup != "订阅" {
		t.Fatalf("expected endpoint-to-endpoint chain inside 订阅, got %#v", endpointChain)
	}
	if endpointChain[0].StatusCode != http.StatusTooManyRequests || !strings.Contains(endpointChain[0].Error, "Concurrency limit exceeded") {
		t.Fatalf("expected first chain node to retain provider 429 detail, got %#v", endpointChain[0])
	}
}

func TestTryEndpointCycleKeepsRepeatedEndpointAttemptsAcrossLoops(t *testing.T) {
	var hits atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if hits.Add(1) == 1 {
			http.Error(w, `{"error":{"code":"rate_limit_exceeded","message":"Concurrency limit exceeded"}}`, http.StatusTooManyRequests)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"model":"gpt-5.5","usage":{"input_tokens":1,"output_tokens":2,"total_tokens":3}}`))
	}))
	defer upstream.Close()

	manager := &EndpointManager{
		endpoints: []*CodexEndpoint{
			{
				ID:                  1,
				CredentialGroupID:   10,
				CredentialGroupName: "订阅",
				Name:                "大陆优化",
				BaseURL:             upstream.URL,
				APIKey:              "test-key",
				IsActive:            true,
				FailoverEnabled:     true,
			},
		},
		unhealthyMap:   make(map[int64]bool),
		failureDetails: make(map[int64]EndpointFailureDetail),
		logger:         newTestLogger(),
	}
	handler := &Handler{
		manager:      manager,
		forwarder:    NewForwarder(time.Second, newTestLogger()),
		streamFwd:    NewStreamingForwarder(newTestLogger()),
		tokenParser:  NewTokenParser(),
		classifier:   NewErrorClassifier(),
		logger:       newTestLogger(),
		usageTracker: nil,
	}
	body := []byte(`{"model":"gpt-5.5","stream":false}`)
	var failedIDs []int64
	var endpointChain []chainNode

	first := handler.tryEndpointCycle(
		httptest.NewRecorder(),
		httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(string(body))),
		body, "req-loop-attempt", time.Now(), false, "合租巴士", "gpt-5.5", &failedIDs, &endpointChain, new(int),
	)
	if first.completed {
		t.Fatalf("expected first 429 attempt to exhaust the loop, got %+v", first)
	}

	manager.ResetUnhealthy()
	failedIDs = failedIDs[:0]
	recorder := httptest.NewRecorder()
	second := handler.tryEndpointCycle(
		recorder,
		httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(string(body))),
		body, "req-loop-attempt", time.Now(), false, "合租巴士", "gpt-5.5", &failedIDs, &endpointChain, new(int),
	)
	if !second.completed || recorder.Code != http.StatusOK {
		t.Fatalf("expected second loop attempt to succeed, result=%+v status=%d body=%q", second, recorder.Code, recorder.Body.String())
	}
	if len(endpointChain) != 2 {
		t.Fatalf("expected both real attempts in the chain, got %#v", endpointChain)
	}
	if endpointChain[0].StatusCode != http.StatusTooManyRequests || endpointChain[0].Error == "" {
		t.Fatalf("expected first attempt to keep its 429 error, got %#v", endpointChain[0])
	}
	if endpointChain[1].StatusCode != 0 || endpointChain[1].Error != "" {
		t.Fatalf("expected successful final attempt without stale error, got %#v", endpointChain[1])
	}
}

func TestServeHTTPRecoversFromInitiallyUnhealthyEndpointsBeforeExhaustion(t *testing.T) {
	for _, tc := range []struct {
		name string
		cfg  config.FailoverConfig
	}{
		{name: "loop retry disabled", cfg: config.FailoverConfig{}},
		{name: "single configured loop", cfg: config.FailoverConfig{LoopRetry: true, MaxLoops: 1}},
		{name: "multiple configured loops", cfg: config.FailoverConfig{LoopRetry: true, MaxLoops: 2}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var hits atomic.Int32
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				hits.Add(1)
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(`{"model":"gpt-5.5","usage":{"input_tokens":1,"output_tokens":2,"total_tokens":3}}`))
			}))
			defer upstream.Close()

			manager := &EndpointManager{
				endpoints: []*CodexEndpoint{
					{
						ID:                  1,
						CredentialGroupID:   10,
						CredentialGroupName: "订阅",
						Name:                "大陆优化",
						BaseURL:             upstream.URL,
						APIKey:              "test-key",
						IsActive:            true,
						FailoverEnabled:     true,
					},
				},
				unhealthyMap: map[int64]bool{
					1: true,
				},
				failureDetails: map[int64]EndpointFailureDetail{
					1: {StatusCode: http.StatusTooManyRequests, Reason: "previous request failed"},
				},
				logger: newTestLogger(),
			}
			handler := NewHandler(
				manager,
				time.Second,
				config.RetryConfig{},
				tc.cfg,
				nil,
				newTestLogger(),
				nil,
			)
			req := httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(`{"model":"gpt-5.5","stream":false}`))
			recorder := httptest.NewRecorder()

			handler.ServeHTTP(recorder, req)

			if recorder.Code != http.StatusOK {
				t.Fatalf("expected the recovered endpoint to be selected, got %d body=%q", recorder.Code, recorder.Body.String())
			}
			if hits.Load() != 1 {
				t.Fatalf("expected recovered endpoint to be tried once, got %d hits", hits.Load())
			}
			if !manager.IsHealthy(1) {
				t.Fatal("expected successful endpoint to remain healthy after reset")
			}
		})
	}
}

func TestTryEndpointCycleDoesNotCacheCredentialGroupFailureWithinTurn(t *testing.T) {
	var primaryAHits atomic.Int32
	var primaryBHits atomic.Int32
	var backupHits atomic.Int32

	primaryA := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		primaryAHits.Add(1)
		http.Error(w, `{"error":{"code":"insufficient_user_quota","message":"Insufficient account balance for the selected group."}}`, http.StatusForbidden)
	}))
	defer primaryA.Close()
	primaryB := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		primaryBHits.Add(1)
		http.Error(w, `{"error":{"code":"insufficient_user_quota","message":"same group insufficient quota"}}`, http.StatusForbidden)
	}))
	defer primaryB.Close()
	backup := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		backupHits.Add(1)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"model":"gpt-5.5","usage":{"input_tokens":1,"output_tokens":2,"total_tokens":3}}`))
	}))
	defer backup.Close()

	manager := &EndpointManager{
		endpoints: []*CodexEndpoint{
			{
				ID:                   1,
				CredentialGroupID:    10,
				CredentialGroupName:  "主分组",
				CredentialGroupOrder: 0,
				Name:                 "国内-a",
				BaseURL:              primaryA.URL,
				APIKey:               "primary-key",
				IsActive:             true,
				FailoverEnabled:      true,
				Priority:             0,
			},
			{
				ID:                   2,
				CredentialGroupID:    10,
				CredentialGroupName:  "主分组",
				CredentialGroupOrder: 0,
				Name:                 "国内-b",
				BaseURL:              primaryB.URL,
				APIKey:               "primary-key",
				FailoverEnabled:      true,
				Priority:             1,
			},
			{
				ID:                   3,
				CredentialGroupID:    20,
				CredentialGroupName:  "备用分组",
				CredentialGroupOrder: 1,
				Name:                 "国内-backup",
				BaseURL:              backup.URL,
				APIKey:               "backup-key",
				FailoverEnabled:      true,
				Priority:             0,
			},
		},
		unhealthyMap:   make(map[int64]bool),
		failureDetails: make(map[int64]EndpointFailureDetail),
		logger:         newTestLogger(),
	}
	handler := &Handler{
		manager:       manager,
		forwarder:     NewForwarder(time.Second, newTestLogger()),
		streamFwd:     NewStreamingForwarder(newTestLogger()),
		tokenParser:   NewTokenParser(),
		classifier:    NewErrorClassifier(),
		logger:        newTestLogger(),
		usageTracker:  nil,
		suspensionMgr: nil,
	}
	body := []byte(`{"model":"gpt-5.5","stream":false}`)

	run := func(requestID string) []chainNode {
		req := httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(string(body)))
		recorder := httptest.NewRecorder()
		var failedIDs []int64
		var endpointChain []chainNode

		result := handler.tryEndpointCycle(recorder, req, body, requestID, time.Now(), false, "CCTQ", "gpt-5.5", &failedIDs, &endpointChain, new(int))
		if !result.completed {
			t.Fatalf("expected request %s to complete via backup endpoint, got incomplete result: %+v", requestID, result)
		}
		if recorder.Code != http.StatusOK {
			t.Fatalf("expected request %s backup endpoint 200, got %d body=%q", requestID, recorder.Code, recorder.Body.String())
		}
		return endpointChain
	}

	firstChain := run("req-turn-1-a")
	if primaryAHits.Load() != 1 || primaryBHits.Load() != 1 || backupHits.Load() != 1 {
		t.Fatalf("unexpected first request hits: primaryA=%d primaryB=%d backup=%d", primaryAHits.Load(), primaryBHits.Load(), backupHits.Load())
	}
	if len(firstChain) != 3 || firstChain[0].CredentialGroup != "主分组" || firstChain[1].CredentialGroup != "主分组" || firstChain[2].CredentialGroup != "备用分组" {
		t.Fatalf("unexpected first request chain: %#v", firstChain)
	}

	manager.ResetUnhealthy()
	secondChain := run("req-turn-1-b")
	if primaryAHits.Load() != 2 || primaryBHits.Load() != 2 || backupHits.Load() != 2 {
		t.Fatalf("same turn must retry every endpoint after health state reset, hits: primaryA=%d primaryB=%d backup=%d", primaryAHits.Load(), primaryBHits.Load(), backupHits.Load())
	}
	if len(secondChain) != 3 || secondChain[0].CredentialGroup != "主分组" || secondChain[1].CredentialGroup != "主分组" || secondChain[2].CredentialGroup != "备用分组" {
		t.Fatalf("same turn must not skip the failed credential group, got chain: %#v", secondChain)
	}
}

func TestHandleStreamingRecordsSuccessWhenContextCanceledAfterCompletion(t *testing.T) {
	tracker, err := tracking.NewUsageTracker(&tracking.Config{
		Enabled:         true,
		DatabasePath:    filepath.Join(t.TempDir(), "usage.db"),
		BufferSize:      50,
		BatchSize:       5,
		FlushInterval:   10 * time.Millisecond,
		MaxRetry:        3,
		CleanupInterval: time.Hour,
		RetentionDays:   1,
		DefaultPricing: tracking.ModelPricing{
			Input:  1,
			Output: 1,
		},
	})
	if err != nil {
		t.Fatalf("create tracker: %v", err)
	}
	defer tracker.Close()

	handler := &Handler{
		tokenParser:  NewTokenParser(),
		logger:       newTestLogger(),
		usageTracker: tracker,
	}
	requestID := "req-stream-completed-cancel"
	startTime := time.Now().Add(-time.Second)
	req := httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(`{"model":"gpt-5.5","stream":true}`))
	handler.recordStart(requestID, req, true, "anyrouter", "gpt-5.5", modelRewriteResult{}, "", "")

	resp := &http.Response{
		StatusCode: http.StatusOK,
		Header: http.Header{
			"Content-Type": []string{"text/event-stream"},
		},
		Body: &contextCanceledAfterReadCloser{reader: strings.NewReader(
			"event: response.output_text.delta\n" +
				"data: {\"type\":\"response.output_text.delta\",\"delta\":\"hello\"}\n\n" +
				"event: response.completed\n" +
				"data: {\"type\":\"response.completed\",\"response\":{\"model\":\"gpt-5.5\",\"usage\":{\"input_tokens\":12,\"output_tokens\":3,\"total_tokens\":15}}}\n\n",
		)},
	}
	recorder := httptest.NewRecorder()

	streamErr := handler.handleStreaming(recorder, resp, requestID, startTime, "gpt-5.5", "jp-node", "默认分组", "anyrouter", 1)
	if streamErr != nil {
		t.Fatalf("unexpected stream error: %v", streamErr)
	}

	details, _, err := tracker.QueryRequestDetailsWithHotPool(context.Background(), &tracking.QueryOptions{Limit: 1})
	if err != nil {
		t.Fatalf("query request details: %v", err)
	}
	if len(details) != 1 {
		t.Fatalf("expected 1 request detail, got %d", len(details))
	}
	detail := details[0]
	if detail.Status != "completed" {
		t.Fatalf("expected completed status, got %q with failure_reason=%q", detail.Status, detail.FailureReason)
	}
	if detail.FailureReason != "" {
		t.Fatalf("expected empty failure_reason, got %q", detail.FailureReason)
	}
	if detail.InputTokens != 12 || detail.OutputTokens != 3 {
		t.Fatalf("expected parsed tokens 12/3, got %d/%d", detail.InputTokens, detail.OutputTokens)
	}
}
