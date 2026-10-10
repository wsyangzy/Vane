package main

import (
	"context"
	"database/sql"
	"errors"
	"log/slog"
	"reflect"
	"testing"
	"time"

	"ai-relay-forwarder/config"
	appcache "ai-relay-forwarder/internal/cache"
	"ai-relay-forwarder/internal/providers/claude/proxy/endpoint"
	claudestore "ai-relay-forwarder/internal/providers/claude/store"
	codexstore "ai-relay-forwarder/internal/providers/codex/store"
	"ai-relay-forwarder/internal/tracking"
)

type endpointLookupStub struct {
	endpoints []*endpoint.Endpoint
}

func (s endpointLookupStub) GetEndpoints() []*endpoint.Endpoint {
	return s.endpoints
}

type endpointRecordLookupStub struct {
	records []*claudestore.EndpointRecord
}

func (s endpointRecordLookupStub) List(context.Context) ([]*claudestore.EndpointRecord, error) {
	return s.records, nil
}

type credentialGroupLookupStub struct {
	byID     map[int64]*claudestore.ClaudeCredentialGroupRecord
	defaults map[string]*claudestore.ClaudeCredentialGroupRecord
}

func (s credentialGroupLookupStub) GetByID(_ context.Context, id int64) (*claudestore.ClaudeCredentialGroupRecord, error) {
	return s.byID[id], nil
}

func (s credentialGroupLookupStub) GetDefaultByChannel(_ context.Context, channel string) (*claudestore.ClaudeCredentialGroupRecord, error) {
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

func TestRequestDetailToRecordMapsNetworkFields(t *testing.T) {
	record := requestDetailToRecord(tracking.RequestDetail{
		RequestID:         "req-network-fields",
		ClientIP:          "192.0.2.10:1234",
		UserAgent:         "Codex CLI/1.0",
		Method:            "PATCH",
		Path:              "/v1/responses",
		FailureReason:     "server_error",
		LastFailureReason: `{"error":{"code":"invalid_encrypted_content"}}`,
	})

	if record.ClientIP != "192.0.2.10:1234" || record.UserAgent != "Codex CLI/1.0" || record.Method != "PATCH" || record.Path != "/v1/responses" {
		t.Fatalf("network fields were not mapped: %#v", record)
	}
	if record.FailureReason != "server_error" || record.LastFailureReason != `{"error":{"code":"invalid_encrypted_content"}}` {
		t.Fatalf("failure fields were not mapped separately: %#v", record)
	}
}

func TestRequestDetailToRecordUsesEndpointChainAsHistoricalRetryFloor(t *testing.T) {
	record := requestDetailToRecord(tracking.RequestDetail{
		RequestID:     "req-historical-retry",
		Status:        "completed",
		RetryCount:    0,
		EndpointChain: `[{"hop":1,"channel":"CCTQ","credential_group":"primary","endpoint":"A"},{"hop":2,"channel":"CCTQ","credential_group":"backup","endpoint":"B"}]`,
	})

	if record.RetryCount != 1 {
		t.Fatalf("retry count = %d, want historical floor 1", record.RetryCount)
	}
	if record.HTTPStatus != 0 {
		t.Fatalf("completed historical HTTP status = %d, want hidden status", record.HTTPStatus)
	}
}

func TestRequestDetailToRecordShowsOnlyProviderHTTPStatus(t *testing.T) {
	provider := 200
	if got := requestDetailToRecord(tracking.RequestDetail{
		Status:               "failed",
		HTTPStatusCode:       &provider,
		HTTPStatusIsUpstream: true,
		FailureReason:        "server_error",
		LastFailureReason:    `{"error":"upstream"}`,
	}); got.HTTPStatus != 200 || !got.HTTPStatusIsUpstream {
		t.Fatalf("provider HTTP result = %#v, want status 200 with upstream origin", got)
	}

	for _, tc := range []struct {
		name   string
		status string
		reason string
		code   int
	}{
		{name: "client cancellation", status: "cancelled", reason: "client_cancelled", code: 499},
		{name: "network fallback", status: "failed", reason: "network_error", code: 502},
		{name: "endpoint fallback", status: "failed", reason: "endpoint_exhausted", code: 503},
	} {
		t.Run(tc.name, func(t *testing.T) {
			code := tc.code
			got := requestDetailToRecord(tracking.RequestDetail{
				Status:         tc.status,
				HTTPStatusCode: &code,
				FailureReason:  tc.reason,
			})
			if got.HTTPStatus != 0 || got.HTTPStatusIsUpstream {
				t.Fatalf("relay result = %#v, want hidden status", got)
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
			records: []*claudestore.EndpointRecord{
				{Channel: "anyrouter", Name: "大陆网络优化", URL: "https://cn.example/v1"},
				{Channel: "anyrouter", Name: "主站直连", URL: "https://direct.example/v1"},
			},
		},
		credentialGroupLookupStub{
			defaults: map[string]*claudestore.ClaudeCredentialGroupRecord{
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

func TestEndpointCostCacheBuildsStructuredItems(t *testing.T) {
	app := NewApp()
	app.endpointCostTodayCache.SetOptions(appcache.Options{Name: cacheNameEndpointCostToday, TTL: time.Minute, MaxEntries: 10, StaleOnError: true, Enabled: true})

	tracker := &endpointCostTrackerStub{costs: []tracking.EndpointCostSummary{
		{Channel: "Codex 正价", EndpointName: "codex-main", GroupName: "国内", Provider: "codex", InputTokens: 100, OutputTokens: 20, TotalCostUSD: 0.25},
	}}

	day := time.Date(1999, 1, 1, 0, 0, 0, 0, time.Local)
	items := app.getEndpointCostsForDate(tracker, day, "codex", endpointCostTestLogger{})
	if len(items) != 1 || items[0].Name != "codex-main (国内)" || items[0].Channel != "Codex 正价" || items[0].Endpoint != "codex-main" || items[0].Group != "国内" {
		t.Fatalf("endpoint costs = %#v, want structured codex-main item", items)
	}

	todayStats := app.endpointCostTodayCache.Stats()
	if todayStats.Misses != 1 || todayStats.Size != 1 {
		t.Fatalf("expected today cache miss+entry, got misses=%d size=%d", todayStats.Misses, todayStats.Size)
	}
}

// 写入只清依赖受影响日期的条目：昨天的写入不能清掉今天的成本缓存，今天的写入必须清掉。
func TestEndpointCostCacheInvalidatedOnlyBySameDayWrites(t *testing.T) {
	app := NewApp()
	app.endpointCostTodayCache.SetOptions(appcache.Options{Name: cacheNameEndpointCostToday, TTL: time.Hour, MaxEntries: 10, Enabled: true})
	tracker := &endpointCostTrackerStub{costs: []tracking.EndpointCostSummary{{EndpointName: "e", Provider: "claude"}}}

	day := time.Date(2026, 10, 8, 0, 0, 0, 0, time.Local)
	app.getEndpointCostsForDate(tracker, day, "claude", endpointCostTestLogger{})

	yesterday := day.Add(-time.Hour)
	app.invalidateUsageQueryCaches(appcache.Change{Providers: []string{"claude"}, Start: yesterday, End: yesterday})
	app.getEndpointCostsForDate(tracker, day, "claude", endpointCostTestLogger{})
	if tracker.calls != 1 {
		t.Fatalf("write on another day must not invalidate, loader calls = %d", tracker.calls)
	}

	noon := day.Add(12 * time.Hour)
	app.invalidateUsageQueryCaches(appcache.Change{Providers: []string{"codex"}, Start: noon, End: noon})
	app.getEndpointCostsForDate(tracker, day, "claude", endpointCostTestLogger{})
	if tracker.calls != 2 {
		t.Fatalf("same-day write must invalidate regardless of provider, loader calls = %d", tracker.calls)
	}
}

func TestEndpointCostCacheReturnsStaleValueOnLoaderError(t *testing.T) {
	app := NewApp()
	app.endpointCostTodayCache.SetOptions(appcache.Options{Name: cacheNameEndpointCostToday, TTL: 5 * time.Millisecond, MaxEntries: 10, StaleOnError: true, Enabled: true})

	tracker := &endpointCostTrackerStub{costs: []tracking.EndpointCostSummary{
		{EndpointName: "claude-main", Provider: "claude", InputTokens: 42, OutputTokens: 8, TotalCostUSD: 0.12},
	}}

	day := time.Date(2026, 6, 27, 0, 0, 0, 0, time.Local)
	first := app.getEndpointCostsForDate(tracker, day, "claude", endpointCostTestLogger{})
	if len(first) != 1 || first[0].Name != "claude-main" {
		t.Fatalf("first endpoint costs = %#v, want claude-main item", first)
	}

	time.Sleep(10 * time.Millisecond)
	tracker.fail = true
	tracker.costs = nil

	second := app.getEndpointCostsForDate(tracker, day, "claude", endpointCostTestLogger{})
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

func TestQueryTokenUsageHeatmapMatchesRequestFilters(t *testing.T) {
	tracker := newAppUsageTestTracker(t)
	defer tracker.Close()

	db := tracker.GetDB()
	mustInsertHeatmapUsage(t, db, heatmapUsageRow{
		requestID: "match-day-1", startTime: "2026-07-01 10:00:00",
		status: "timeout", provider: "codex", model: "gpt-5", channel: "合租巴士", endpoint: "大陆优化", group: "正价",
		input: 200, output: 20, cacheRead: 10,
	})
	mustInsertHeatmapUsage(t, db, heatmapUsageRow{
		requestID: "status-miss", startTime: "2026-07-01 11:00:00",
		status: "completed", provider: "codex", model: "gpt-5", channel: "合租巴士", endpoint: "大陆优化", group: "正价",
		input: 900, output: 90,
	})
	mustInsertHeatmapUsage(t, db, heatmapUsageRow{
		requestID: "provider-miss", startTime: "2026-07-01 12:00:00",
		status: "timeout", provider: "claude", model: "gpt-5", channel: "合租巴士", endpoint: "大陆优化", group: "正价",
		input: 900, output: 90,
	})
	mustInsertHeatmapUsage(t, db, heatmapUsageRow{
		requestID: "match-day-3", startTime: "2026-07-03 11:00:00",
		status: "failed", provider: "codex", model: "gpt-5", channel: "合租巴士", endpoint: "大陆优化", group: "正价",
		input: 300, output: 30, cacheRead: 15,
	})

	start := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)
	end := time.Date(2026, 7, 3, 23, 59, 0, 0, time.UTC)
	got, err := queryTokenUsageHeatmap(tracker, &tracking.QueryOptions{
		StartDate:    &start,
		EndDate:      &end,
		Status:       "failed",
		Provider:     "codex",
		ModelName:    "gpt-5",
		Channel:      "合租巴士",
		EndpointName: "大陆优化",
		GroupName:    "正价",
	})
	if err != nil {
		t.Fatalf("queryTokenUsageHeatmap failed: %v", err)
	}

	if len(got) != 72 {
		t.Fatalf("heatmap len = %d, want 72 hourly buckets: %#v", len(got), got)
	}
	if got[10].Date != "2026-07-01 10" || got[10].InputTokens != 190 || got[10].OutputTokens != 20 || got[10].TotalTokens != 220 {
		t.Fatalf("hour 2026-07-01 10 = %#v, want codex-only tokens", got[10])
	}
	if got[10].Bucket != "hour" {
		t.Fatalf("bucket = %q, want hour", got[10].Bucket)
	}
	if got[34].Date != "2026-07-02 10" || got[34].TotalTokens != 0 {
		t.Fatalf("empty hour = %#v, want empty filled hour", got[34])
	}
	if got[59].Date != "2026-07-03 11" || got[59].InputTokens != 285 || got[59].OutputTokens != 30 || got[59].TotalTokens != 330 {
		t.Fatalf("hour 2026-07-03 11 = %#v, want codex-only tokens", got[59])
	}
	if got[10].Intensity != 220.0/330.0 || got[59].Intensity != 1 {
		t.Fatalf("intensity = %.4f %.4f, want normalized by peak total", got[10].Intensity, got[59].Intensity)
	}
}

func TestQueryTokenUsageHeatmapUsesHourlyBucketsForSingleDay(t *testing.T) {
	tracker := newAppUsageTestTracker(t)
	defer tracker.Close()

	db := tracker.GetDB()
	mustInsertHeatmapUsage(t, db, heatmapUsageRow{
		requestID: "hour-10-a", startTime: "2026-07-01 10:05:00",
		status: "completed", provider: "codex", model: "gpt-5",
		input: 100, output: 10, cacheRead: 5,
	})
	mustInsertHeatmapUsage(t, db, heatmapUsageRow{
		requestID: "hour-10-b", startTime: "2026-07-01 10:55:00",
		status: "completed", provider: "codex", model: "gpt-5",
		input: 50, output: 5,
	})
	mustInsertHeatmapUsage(t, db, heatmapUsageRow{
		requestID: "next-day-miss", startTime: "2026-07-02 10:00:00",
		status: "completed", provider: "codex", model: "gpt-5",
		input: 500, output: 50,
	})

	start := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)
	end := time.Date(2026, 7, 1, 23, 59, 0, 0, time.UTC)
	got, err := queryTokenUsageHeatmap(tracker, &tracking.QueryOptions{
		StartDate: &start,
		EndDate:   &end,
		Provider:  "codex",
	})
	if err != nil {
		t.Fatalf("queryTokenUsageHeatmap failed: %v", err)
	}

	if len(got) != 24 {
		t.Fatalf("heatmap len = %d, want 24: %#v", len(got), got)
	}
	if got[10].Date != "2026-07-01 10" || got[10].Bucket != "hour" {
		t.Fatalf("hour bucket = %#v, want 2026-07-01 10/hour", got[10])
	}
	if got[10].InputTokens != 145 || got[10].OutputTokens != 15 || got[10].TotalTokens != 165 || got[10].Intensity != 1 {
		t.Fatalf("hour 10 = %#v, want summed codex tokens", got[10])
	}
	if got[9].TotalTokens != 0 || got[11].TotalTokens != 0 {
		t.Fatalf("neighbor hours = %#v %#v, want empty filled hours", got[9], got[11])
	}
}

func TestQueryTokenUsageHeatmapUsesDailyBucketsForThirtyDayRanges(t *testing.T) {
	tracker := newAppUsageTestTracker(t)
	defer tracker.Close()

	db := tracker.GetDB()
	mustInsertHeatmapUsage(t, db, heatmapUsageRow{
		requestID: "thirty-day-range-day", startTime: "2026-07-20 10:05:00",
		status: "completed", provider: "codex", model: "gpt-5",
		input: 100, output: 10,
	})

	start := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)
	end := time.Date(2026, 7, 30, 23, 59, 0, 0, time.UTC)
	got, err := queryTokenUsageHeatmap(tracker, &tracking.QueryOptions{
		StartDate: &start,
		EndDate:   &end,
		Provider:  "codex",
	})
	if err != nil {
		t.Fatalf("queryTokenUsageHeatmap failed: %v", err)
	}

	if len(got) != 30 {
		t.Fatalf("heatmap len = %d, want 30 daily buckets: %#v", len(got), got)
	}
	if got[19].Date != "2026-07-20" || got[19].Bucket != "day" || got[19].TotalTokens != 110 {
		t.Fatalf("day bucket = %#v, want 2026-07-20/day tokens", got[19])
	}
}

func TestBuildRequestQueryOptionsParsesTimeRange(t *testing.T) {
	opts := buildRequestQueryOptions(RequestQueryParams{
		StartDate: "2026-07-01T08:30",
		EndDate:   "2026-07-03T23:59",
		Status:    "failed",
		Provider:  "codex",
	}, "Asia/Shanghai", 0, 0)

	if got := opts.StartDate.Format("2006-01-02 15:04:05"); got != "2026-07-01 08:30:00" {
		t.Fatalf("start = %s, want 2026-07-01 08:30:00", got)
	}
	if got := opts.EndDate.Format("2006-01-02 15:04:05"); got != "2026-07-03 23:59:00" {
		t.Fatalf("end = %s, want 2026-07-03 23:59:00", got)
	}
	if opts.Status != "failed" || opts.Provider != "codex" {
		t.Fatalf("filters = status %q provider %q, want failed/codex", opts.Status, opts.Provider)
	}
}

func TestSyncEndpointMultipliersKeepsCodexCredentialGroupDimension(t *testing.T) {
	tracker := newAppUsageTestTracker(t)
	defer tracker.Close()

	ctx := context.Background()
	db := tracker.GetDB()
	channelStore := codexstore.NewSQLiteCodexChannelStore(db)
	groupStore := codexstore.NewSQLiteCodexCredentialGroupStore(db)
	endpointStore := codexstore.NewSQLiteCodexEndpointStore(db)

	channel, err := channelStore.Create(ctx, &codexstore.CodexChannelRecord{
		Name:            "快跑API",
		ProviderType:    "openai-compatible",
		ApiKey:          "legacy-key",
		Model:           "gpt-5",
		ReasoningEffort: "xhigh",
	})
	if err != nil {
		t.Fatalf("create channel: %v", err)
	}
	subsidy, err := groupStore.Create(ctx, &codexstore.CodexCredentialGroupRecord{
		ChannelID:      channel.ID,
		Name:           "补贴",
		ApiKey:         "subsidy-key",
		IsDefault:      true,
		CostMultiplier: 0.4,
	})
	if err != nil {
		t.Fatalf("create subsidy group: %v", err)
	}
	fullPrice, err := groupStore.Create(ctx, &codexstore.CodexCredentialGroupRecord{
		ChannelID:      channel.ID,
		Name:           "正价",
		ApiKey:         "full-key",
		CostMultiplier: 1.5,
	})
	if err != nil {
		t.Fatalf("create full price group: %v", err)
	}
	for _, group := range []*codexstore.CodexCredentialGroupRecord{subsidy, fullPrice} {
		_, err := endpointStore.Create(ctx, &codexstore.CodexEndpointRecord{
			ChannelID:         channel.ID,
			CredentialGroupID: group.ID,
			Name:              "国内",
			BaseURL:           "https://example.test/v1",
			FailoverEnabled:   true,
			Priority:          0,
			CostMultiplier:    1,
		})
		if err != nil {
			t.Fatalf("create endpoint for group %s: %v", group.Name, err)
		}
	}

	app := &App{
		logger:                    slog.Default(),
		usageTracker:              tracker,
		codexChannelStore:         channelStore,
		codexEndpointStore:        endpointStore,
		codexCredentialGroupStore: groupStore,
	}
	app.syncEndpointMultipliersToTracker(ctx)

	subsidyMultiplier := tracker.GetEndpointMultiplierForEndpointWithGroup("codex", "快跑API", "补贴", "国内")
	if subsidyMultiplier.CostMultiplier != 0.4 {
		t.Fatalf("subsidy multiplier = %.2f, want 0.40", subsidyMultiplier.CostMultiplier)
	}
	fullPriceMultiplier := tracker.GetEndpointMultiplierForEndpointWithGroup("codex", "快跑API", "正价", "国内")
	if fullPriceMultiplier.CostMultiplier != 1.5 {
		t.Fatalf("full price multiplier = %.2f, want 1.50", fullPriceMultiplier.CostMultiplier)
	}
	legacyMultiplier := tracker.GetEndpointMultiplierForEndpoint("codex", "快跑API", "国内")
	if legacyMultiplier.CostMultiplier != 1.0 {
		t.Fatalf("duplicate-name legacy multiplier = %.2f, want default 1.00", legacyMultiplier.CostMultiplier)
	}
}

// Claude 与 Codex 允许各自配置同名的渠道 / 凭证分组 / 端点。倍率缓存是两者共用的
// 一张 map，若 key 不含 provider，后写入的 Codex 条目会覆盖 Claude 条目，导致
// Claude 请求按 Codex 的倍率计费（实测 anyrouter 渠道 685 条 Claude 请求被按 0.4 计价）。
func TestSyncEndpointMultipliersIsolatesProvidersOnIdenticalNames(t *testing.T) {
	tracker := newAppUsageTestTracker(t)
	defer tracker.Close()

	ctx := context.Background()
	db := tracker.GetDB()

	const (
		channelName  = "anyrouter"
		groupName    = "默认分组"
		endpointName = "主站直连"
	)

	claudeGroupStore := claudestore.NewSQLiteClaudeCredentialGroupStore(db)
	claudeEndpointStore := claudestore.NewSQLiteEndpointStore(db)
	claudeGroup, err := claudeGroupStore.Create(ctx, &claudestore.ClaudeCredentialGroupRecord{
		Channel:        channelName,
		Name:           groupName,
		Token:          "claude-token",
		IsDefault:      true,
		CostMultiplier: 1.0,
	})
	if err != nil {
		t.Fatalf("create claude group: %v", err)
	}
	if _, err := claudeEndpointStore.Create(ctx, &claudestore.EndpointRecord{
		Channel:           channelName,
		CredentialGroupID: claudeGroup.ID,
		Name:              endpointName,
		URL:               "https://claude.example.test",
		FailoverEnabled:   true,
		// 端点级倍率已停用：即使 DB 里残留非 1 的历史值，也不得参与运行时计费。
		CostMultiplier: 2.0,
	}); err != nil {
		t.Fatalf("create claude endpoint: %v", err)
	}

	codexChannelStore := codexstore.NewSQLiteCodexChannelStore(db)
	codexGroupStore := codexstore.NewSQLiteCodexCredentialGroupStore(db)
	codexEndpointStore := codexstore.NewSQLiteCodexEndpointStore(db)
	codexChannel, err := codexChannelStore.Create(ctx, &codexstore.CodexChannelRecord{
		Name:         channelName,
		ProviderType: "openai-compatible",
		ApiKey:       "codex-key",
		Model:        "gpt-5.6-sol",
	})
	if err != nil {
		t.Fatalf("create codex channel: %v", err)
	}
	codexGroup, err := codexGroupStore.Create(ctx, &codexstore.CodexCredentialGroupRecord{
		ChannelID:      codexChannel.ID,
		Name:           groupName,
		ApiKey:         "codex-group-key",
		IsDefault:      true,
		CostMultiplier: 0.4,
	})
	if err != nil {
		t.Fatalf("create codex group: %v", err)
	}
	if _, err := codexEndpointStore.Create(ctx, &codexstore.CodexEndpointRecord{
		ChannelID:         codexChannel.ID,
		CredentialGroupID: codexGroup.ID,
		Name:              endpointName,
		BaseURL:           "https://codex.example.test/v1",
		FailoverEnabled:   true,
		CostMultiplier:    1,
	}); err != nil {
		t.Fatalf("create codex endpoint: %v", err)
	}

	app := &App{
		logger:                     slog.Default(),
		usageTracker:               tracker,
		endpointStore:              claudeEndpointStore,
		claudeCredentialGroupStore: claudeGroupStore,
		codexChannelStore:          codexChannelStore,
		codexEndpointStore:         codexEndpointStore,
		codexCredentialGroupStore:  codexGroupStore,
	}
	app.syncEndpointMultipliersToTracker(ctx)

	claudeMultiplier := tracker.GetEndpointMultiplierForEndpointWithGroup("claude", channelName, groupName, endpointName)
	if claudeMultiplier.CostMultiplier != 1.0 {
		t.Fatalf("claude multiplier = %.2f, want 1.00 (端点倍率已停用，且不得取到 Codex 的 0.40)", claudeMultiplier.CostMultiplier)
	}

	codexMultiplier := tracker.GetEndpointMultiplierForEndpointWithGroup("codex", channelName, groupName, endpointName)
	if codexMultiplier.CostMultiplier != 0.4 {
		t.Fatalf("codex multiplier = %.2f, want 0.40", codexMultiplier.CostMultiplier)
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

type heatmapUsageRow struct {
	requestID     string
	startTime     string
	status        string
	provider      string
	model         string
	channel       string
	endpoint      string
	group         string
	input         int64
	output        int64
	cacheCreation int64
	cacheRead     int64
}

func mustInsertHeatmapUsage(t *testing.T, db interface {
	Exec(query string, args ...any) (sql.Result, error)
}, row heatmapUsageRow) {
	t.Helper()

	_, err := db.Exec(`INSERT INTO request_logs (
		request_id, start_time, status, provider, model_name, channel, endpoint_name, group_name,
		input_tokens, output_tokens, cache_creation_tokens, cache_read_tokens
	) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		row.requestID, row.startTime, row.status, row.provider, row.model, row.channel, row.endpoint, row.group,
		row.input, row.output, row.cacheCreation, row.cacheRead,
	)
	if err != nil {
		t.Fatalf("insert heatmap usage %s: %v", row.requestID, err)
	}
}
