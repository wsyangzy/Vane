package main

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"ai-relay-forwarder/config"
	appcache "ai-relay-forwarder/internal/cache"
	"ai-relay-forwarder/internal/endpoint"
	"ai-relay-forwarder/internal/store"
	"ai-relay-forwarder/internal/tracking"
)

type endpointLookupStub struct {
	endpoints []*endpoint.Endpoint
}

func (s endpointLookupStub) GetEndpoints() []*endpoint.Endpoint {
	return s.endpoints
}

type endpointRecordLookupStub struct {
	records []*store.EndpointRecord
}

func (s endpointRecordLookupStub) List(context.Context) ([]*store.EndpointRecord, error) {
	return s.records, nil
}

type credentialGroupLookupStub struct {
	byID     map[int64]*store.ClaudeCredentialGroupRecord
	defaults map[string]*store.ClaudeCredentialGroupRecord
}

func (s credentialGroupLookupStub) GetByID(_ context.Context, id int64) (*store.ClaudeCredentialGroupRecord, error) {
	return s.byID[id], nil
}

func (s credentialGroupLookupStub) GetDefaultByChannel(_ context.Context, channel string) (*store.ClaudeCredentialGroupRecord, error) {
	return s.defaults[channel], nil
}

func TestParseEndpointChain(t *testing.T) {
	tests := []struct {
		name  string
		chain string
		want  []chainNode
	}{
		{
			name:  "empty",
			chain: "",
			want:  nil,
		},
		{
			name:  "structured json fills missing hops",
			chain: `[{"channel":"alpha","endpoint":"ep-a"},{"hop":4,"channel":"beta","endpoint":"ep-b"}]`,
			want: []chainNode{
				{Hop: 1, Channel: "alpha", Endpoint: "ep-a"},
				{Hop: 4, Channel: "beta", Endpoint: "ep-b"},
			},
		},
		{
			name:  "string array",
			chain: `["alpha::ep-a","beta::ep-b"]`,
			want: []chainNode{
				{Hop: 1, Channel: "alpha", Endpoint: "ep-a"},
				{Hop: 2, Channel: "beta", Endpoint: "ep-b"},
			},
		},
		{
			name:  "legacy delimiter",
			chain: "alpha::ep-a -> ep-b -> beta::ep-c",
			want: []chainNode{
				{Hop: 1, Channel: "alpha", Endpoint: "ep-a"},
				{Hop: 2, Endpoint: "ep-b"},
				{Hop: 3, Channel: "beta", Endpoint: "ep-c"},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := parseEndpointChain(tt.chain)
			if !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("parseEndpointChain() = %#v, want %#v", got, tt.want)
			}
		})
	}
}

func TestEnrichEndpointChainCredentialGroups(t *testing.T) {
	nodes := []chainNode{
		{Hop: 1, Channel: "公益站", Endpoint: "Ark API"},
		{Hop: 2, Channel: "foxcode", CredentialGroup: "已有分组", Endpoint: "aws"},
		{Hop: 3, Channel: "Right Code", Endpoint: "AWSQ逆向渠道"},
	}
	lookup := map[string]string{
		endpointChainLookupKey("公益站", "Ark API"):         "默认分组",
		endpointChainLookupKey("foxcode", "aws"):         "不应覆盖",
		endpointChainLookupKey("Right Code", "AWSQ逆向渠道"): "AWSQ逆向渠道",
	}

	enrichEndpointChainCredentialGroups("claude", nodes, lookup)

	want := []chainNode{
		{Hop: 1, Channel: "公益站", CredentialGroup: "默认分组", Endpoint: "Ark API"},
		{Hop: 2, Channel: "foxcode", CredentialGroup: "已有分组", Endpoint: "aws"},
		{Hop: 3, Channel: "Right Code", CredentialGroup: "AWSQ逆向渠道", Endpoint: "AWSQ逆向渠道"},
	}
	if !reflect.DeepEqual(nodes, want) {
		t.Fatalf("enrichEndpointChainCredentialGroups() = %#v, want %#v", nodes, want)
	}
}

func TestEndpointChainCredentialGroupLookupIndexesEndpointNameAndURL(t *testing.T) {
	lookup := endpointChainCredentialGroupLookup(
		context.Background(),
		endpointLookupStub{
			endpoints: []*endpoint.Endpoint{
				{
					Config: config.EndpointConfig{
						Channel:             "公益站",
						Name:                "Ark API",
						URL:                 "https://ark.example/v1",
						CredentialGroupName: "默认分组",
					},
				},
			},
		},
		nil,
		nil,
	)

	nodes := []chainNode{
		{Hop: 1, Channel: "公益站", Endpoint: "Ark API"},
		{Hop: 2, Channel: "公益站", Endpoint: "https://ark.example/v1"},
	}
	enrichEndpointChainCredentialGroups("claude", nodes, lookup)

	for _, node := range nodes {
		if node.CredentialGroup != "默认分组" {
			t.Fatalf("credential group for endpoint %q = %q, want 默认分组", node.Endpoint, node.CredentialGroup)
		}
	}
}

func TestEndpointChainCredentialGroupLookupUsesStoredDefaultGroup(t *testing.T) {
	lookup := endpointChainCredentialGroupLookup(
		context.Background(),
		endpointLookupStub{},
		endpointRecordLookupStub{
			records: []*store.EndpointRecord{
				{Channel: "anyrouter", Name: "大陆网络优化", URL: "https://cn.example/v1"},
				{Channel: "anyrouter", Name: "主站直连", URL: "https://direct.example/v1"},
			},
		},
		credentialGroupLookupStub{
			defaults: map[string]*store.ClaudeCredentialGroupRecord{
				"anyrouter": {Channel: "anyrouter", Name: "默认分组", IsDefault: true},
			},
		},
	)

	nodes := []chainNode{
		{Hop: 1, Channel: "anyrouter", Endpoint: "大陆网络优化"},
		{Hop: 2, Channel: "anyrouter", Endpoint: "主站直连"},
		{Hop: 3, Channel: "anyrouter", Endpoint: "https://direct.example/v1"},
	}
	enrichEndpointChainCredentialGroups("claude", nodes, lookup)

	for _, node := range nodes {
		if node.CredentialGroup != "默认分组" {
			t.Fatalf("credential group for endpoint %q = %q, want 默认分组", node.Endpoint, node.CredentialGroup)
		}
	}
}

func TestEndpointChainCredentialGroupLookupSkipsAmbiguousEndpoint(t *testing.T) {
	lookup := endpointChainCredentialGroupLookup(
		context.Background(),
		endpointLookupStub{
			endpoints: []*endpoint.Endpoint{
				{
					Config: config.EndpointConfig{
						Channel:             "公益站",
						Name:                "shared",
						CredentialGroupName: "默认分组",
					},
				},
				{
					Config: config.EndpointConfig{
						Channel:             "公益站",
						Name:                "shared",
						CredentialGroupName: "备用分组",
					},
				},
			},
		},
		nil,
		nil,
	)

	nodes := []chainNode{{Hop: 1, Channel: "公益站", Endpoint: "shared"}}
	enrichEndpointChainCredentialGroups("claude", nodes, lookup)

	if nodes[0].CredentialGroup != "" {
		t.Fatalf("ambiguous endpoint group = %q, want empty", nodes[0].CredentialGroup)
	}
}

func TestEnrichEndpointChainCredentialGroupsSkipsCodex(t *testing.T) {
	nodes := []chainNode{{Hop: 1, Channel: "Right Code", Endpoint: "AWSQ逆向渠道"}}
	lookup := map[string]string{
		endpointChainLookupKey("Right Code", "AWSQ逆向渠道"): "AWSQ逆向渠道",
	}

	enrichEndpointChainCredentialGroups("codex", nodes, lookup)

	if nodes[0].CredentialGroup != "" {
		t.Fatalf("codex chain group = %q, want empty", nodes[0].CredentialGroup)
	}
}

type endpointCostTrackerStub struct {
	calls int
	fail  bool
	costs []tracking.EndpointCostSummary
}

func (s *endpointCostTrackerStub) GetEndpointCostsForDate(context.Context, string) ([]tracking.EndpointCostSummary, error) {
	s.calls++
	if s.fail {
		return nil, errors.New("database busy")
	}
	return s.costs, nil
}

type endpointCostTestLogger struct{}

func (endpointCostTestLogger) Error(string, ...any) {}

func TestEndpointCostCacheUsesCallerTodaySelection(t *testing.T) {
	app := NewApp()
	app.endpointCostTodayCache.SetOptions(appcache.Options{Name: cacheNameEndpointCostToday, TTL: time.Minute, MaxEntries: 10, StaleOnError: true, Enabled: true})
	app.endpointCostHistoryCache.SetOptions(appcache.Options{Name: cacheNameEndpointCostHistory, TTL: time.Minute, MaxEntries: 10, StaleOnError: true, Enabled: true})

	tracker := &endpointCostTrackerStub{costs: []tracking.EndpointCostSummary{
		{EndpointName: "codex-main", Provider: "codex", InputTokens: 100, OutputTokens: 20, TotalCostUSD: 0.25},
	}}

	items := app.getEndpointCostsForDate(tracker, "1999-01-01", "codex", true, endpointCostTestLogger{})
	if len(items) != 1 || items[0].Name != "codex-main" {
		t.Fatalf("endpoint costs = %#v, want codex-main item", items)
	}

	todayStats := app.endpointCostTodayCache.Stats()
	historyStats := app.endpointCostHistoryCache.Stats()
	if todayStats.Misses != 1 || todayStats.Size != 1 {
		t.Fatalf("expected today cache miss+entry, got misses=%d size=%d", todayStats.Misses, todayStats.Size)
	}
	if historyStats.Misses != 0 || historyStats.Size != 0 {
		t.Fatalf("expected history cache untouched, got misses=%d size=%d", historyStats.Misses, historyStats.Size)
	}
}

func TestEndpointCostCacheReturnsStaleValueOnLoaderError(t *testing.T) {
	app := NewApp()
	app.endpointCostTodayCache.SetOptions(appcache.Options{Name: cacheNameEndpointCostToday, TTL: 5 * time.Millisecond, MaxEntries: 10, StaleOnError: true, Enabled: true})

	tracker := &endpointCostTrackerStub{costs: []tracking.EndpointCostSummary{
		{EndpointName: "claude-main", Provider: "claude", InputTokens: 42, OutputTokens: 8, TotalCostUSD: 0.12},
	}}

	first := app.getEndpointCostsForDate(tracker, "2026-06-27", "claude", true, endpointCostTestLogger{})
	if len(first) != 1 || first[0].Name != "claude-main" {
		t.Fatalf("first endpoint costs = %#v, want claude-main item", first)
	}

	time.Sleep(10 * time.Millisecond)
	tracker.fail = true
	tracker.costs = nil

	second := app.getEndpointCostsForDate(tracker, "2026-06-27", "claude", true, endpointCostTestLogger{})
	if len(second) != 1 || second[0].Name != "claude-main" {
		t.Fatalf("second endpoint costs = %#v, want stale claude-main item", second)
	}
	if tracker.calls != 2 {
		t.Fatalf("expected loader to be retried after TTL expiry, got %d calls", tracker.calls)
	}
}

func TestQueryTokenUsageForRangeFiltersProvider(t *testing.T) {
	tracker := newAppUsageTestTracker(t)
	defer tracker.Close()

	recordTokenUsageForProvider(t, tracker, "claude-usage", "claude", 100, 10, 0, 5)
	recordTokenUsageForProvider(t, tracker, "codex-usage", "codex", 200, 20, 0, 10)

	got, err := queryTokenUsageForRange(tracker, time.Now().Add(-time.Hour), time.Now().Add(time.Hour), "codex")
	if err != nil {
		t.Fatalf("queryTokenUsageForRange failed: %v", err)
	}

	if got.InputTokens != 190 || got.OutputTokens != 20 || got.CacheReadTokens != 10 || got.TotalTokens != 220 {
		t.Fatalf("codex token usage = %#v, want input=190 output=20 cache_read=10 total=220", got)
	}
}

func newAppUsageTestTracker(t *testing.T) *tracking.UsageTracker {
	t.Helper()

	tracker, err := tracking.NewUsageTracker(&tracking.Config{
		Enabled:         true,
		DatabasePath:    ":memory:",
		BufferSize:      100,
		BatchSize:       10,
		FlushInterval:   time.Hour,
		MaxRetry:        1,
		CleanupInterval: 24 * time.Hour,
		RetentionDays:   30,
		ModelPricing: map[string]tracking.ModelPricing{
			"test-model": {Input: 1, Output: 2, CacheCreation: 1, CacheRead: 0.1},
		},
		HotPool: &tracking.HotPoolSettings{
			Enabled:          true,
			MaxAge:           time.Hour,
			MaxSize:          100,
			CleanupInterval:  time.Hour,
			ArchiveOnCleanup: true,
		},
	})
	if err != nil {
		t.Fatalf("NewUsageTracker failed: %v", err)
	}
	return tracker
}

func recordTokenUsageForProvider(t *testing.T, tracker *tracking.UsageTracker, requestID, provider string, input, output, cacheCreation, cacheRead int64) {
	t.Helper()

	tracker.RecordRequestStart(requestID, "127.0.0.1", "test-agent", "POST", "/v1/messages", false)
	status := "processing"
	model := "test-model"
	tracker.RecordRequestUpdate(requestID, tracking.UpdateOptions{
		Provider:  &provider,
		Status:    &status,
		ModelName: &model,
	})
	tracker.RecordRequestSuccess(requestID, model, &tracking.TokenUsage{
		InputTokens:         input,
		OutputTokens:        output,
		CacheCreationTokens: cacheCreation,
		CacheReadTokens:     cacheRead,
	}, 100*time.Millisecond)
	if err := tracker.ForceFlush(); err != nil {
		t.Fatalf("ForceFlush failed: %v", err)
	}
}
