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
	if streamErr.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("expected status 503, got %d", streamErr.StatusCode)
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
		tracking.EndpointMultiplierKey("CCTQ", "国内"): {
			CostMultiplier:              9,
			BillingOutputTokenSource:    tracking.BillingOutputTokenSourceOutput,
			InputCostMultiplier:         9,
			OutputCostMultiplier:        9,
			CacheReadCostMultiplier:     9,
			CacheCreationCostMultiplier: 9,
		},
		tracking.EndpointMultiplierGroupKey("CCTQ", "轮询", "国内"): {
			CostMultiplier:           2.5,
			BillingOutputTokenSource: tracking.BillingOutputTokenSourceReasoning,
		},
	})
	requestID := "req-forwarding-group"
	req := httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(`{"model":"gpt-5.5","stream":true}`))
	handler.recordStart(requestID, req, true, "CCTQ", "gpt-5.5", modelRewriteResult{})
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

func TestTryEndpointCycleSkipsCredentialGroupAfterInsufficientQuota(t *testing.T) {
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

	result := handler.tryEndpointCycle(recorder, req, body, "req-rate-limit-group", time.Now(), false, "CCTQ", "gpt-5.5", &failedIDs, &endpointChain, "")
	if !result.completed {
		t.Fatalf("expected request to complete via backup endpoint, got incomplete result: %+v", result)
	}
	if recorder.Code != http.StatusOK {
		t.Fatalf("expected backup endpoint 200, got %d body=%q", recorder.Code, recorder.Body.String())
	}
	if primaryBHits.Load() != 0 {
		t.Fatalf("expected same credential group endpoint to be skipped, got %d hits", primaryBHits.Load())
	}
	if backupHits.Load() != 1 {
		t.Fatalf("expected backup endpoint to be hit once, got %d hits", backupHits.Load())
	}
	if len(endpointChain) != 2 {
		t.Fatalf("expected chain to include failed primary and backup, got %#v", endpointChain)
	}
	if endpointChain[0].CredentialGroup != "主分组" || endpointChain[1].CredentialGroup != "备用分组" {
		t.Fatalf("unexpected endpoint chain groups: %#v", endpointChain)
	}
	if endpointChain[0].StatusCode != http.StatusForbidden || !strings.Contains(endpointChain[0].Error, "Insufficient account balance") {
		t.Fatalf("expected failed chain node to include upstream error, got %#v", endpointChain[0])
	}
}

func TestTryEndpointCycleCachesCredentialGroupFailureWithinTurn(t *testing.T) {
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

	run := func(requestID, turnKey string) []chainNode {
		req := httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(string(body)))
		recorder := httptest.NewRecorder()
		var failedIDs []int64
		var endpointChain []chainNode

		result := handler.tryEndpointCycle(recorder, req, body, requestID, time.Now(), false, "CCTQ", "gpt-5.5", &failedIDs, &endpointChain, turnKey)
		if !result.completed {
			t.Fatalf("expected request %s to complete via backup endpoint, got incomplete result: %+v", requestID, result)
		}
		if recorder.Code != http.StatusOK {
			t.Fatalf("expected request %s backup endpoint 200, got %d body=%q", requestID, recorder.Code, recorder.Body.String())
		}
		return endpointChain
	}

	firstChain := run("req-turn-1-a", "turn-1")
	if primaryAHits.Load() != 1 || primaryBHits.Load() != 0 || backupHits.Load() != 1 {
		t.Fatalf("unexpected first request hits: primaryA=%d primaryB=%d backup=%d", primaryAHits.Load(), primaryBHits.Load(), backupHits.Load())
	}
	if len(firstChain) != 2 || firstChain[0].CredentialGroup != "主分组" || firstChain[1].CredentialGroup != "备用分组" {
		t.Fatalf("unexpected first request chain: %#v", firstChain)
	}

	secondChain := run("req-turn-1-b", "turn-1")
	if primaryAHits.Load() != 1 || primaryBHits.Load() != 0 || backupHits.Load() != 2 {
		t.Fatalf("same turn should skip cached failed group, hits: primaryA=%d primaryB=%d backup=%d", primaryAHits.Load(), primaryBHits.Load(), backupHits.Load())
	}
	if len(secondChain) != 1 || secondChain[0].CredentialGroup != "备用分组" {
		t.Fatalf("same turn should route directly to backup group, got chain: %#v", secondChain)
	}

	thirdChain := run("req-turn-2", "turn-2")
	if primaryAHits.Load() != 1 || primaryBHits.Load() != 1 || backupHits.Load() != 3 {
		t.Fatalf("new turn should retry primary group, hits: primaryA=%d primaryB=%d backup=%d", primaryAHits.Load(), primaryBHits.Load(), backupHits.Load())
	}
	if len(thirdChain) != 2 || thirdChain[0].CredentialGroup != "主分组" || thirdChain[1].CredentialGroup != "备用分组" {
		t.Fatalf("new turn should try primary group again before backup, got chain: %#v", thirdChain)
	}
}

func TestRequestTurnFailureCacheKeyUsesUserInputAcrossResponseChain(t *testing.T) {
	body := []byte(`{"previous_response_id":"resp-1","input":[{"role":"user","content":[{"type":"input_text","text":"修复这个问题"}]}]}`)
	req := httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(string(body)))

	key := requestTurnFailureCacheKey(req, body)
	if key == "" {
		t.Fatal("expected key from user input")
	}
	if same := requestTurnFailureCacheKey(req, body); same != key {
		t.Fatalf("expected stable key for same request, got %q and %q", key, same)
	}

	nextBody := []byte(`{"previous_response_id":"resp-2","input":[{"role":"user","content":[{"type":"input_text","text":"修复这个问题"}]}]}`)
	if next := requestTurnFailureCacheKey(req, nextBody); next != key {
		t.Fatalf("expected same user input to keep the turn cache across response chain, got %q and %q", key, next)
	}

	toolOnly := []byte(`{"previous_response_id":"resp-2","input":[{"type":"function_call_output","call_id":"call-1","output":"ok"}]}`)
	if key := requestTurnFailureCacheKey(req, toolOnly); key != "" {
		t.Fatalf("expected no new key for tool-only request, got %q", key)
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
	handler.recordStart(requestID, req, true, "anyrouter", "gpt-5.5", modelRewriteResult{})

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
