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

func TestTryEndpointCycleSkipsCredentialGroupAfterRateLimit(t *testing.T) {
	var primaryBHits atomic.Int32
	var backupHits atomic.Int32

	primaryA := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, `{"error":{"type":"rate_limit_error","message":"limited"}}`, http.StatusTooManyRequests)
	}))
	defer primaryA.Close()
	primaryB := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		primaryBHits.Add(1)
		http.Error(w, `{"error":{"type":"rate_limit_error","message":"same group limited"}}`, http.StatusTooManyRequests)
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
				IsActive:             true,
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

	result := handler.tryEndpointCycle(recorder, req, body, "req-rate-limit-group", time.Now(), false, "CCTQ", "gpt-5.5", &failedIDs, &endpointChain)
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
