package codexproxy

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/wsyangzy/Vane/internal/tracking"
)

func TestTokenParserPreservesCacheWriteTTLsForResponsesAndChat(t *testing.T) {
	usage := `"input_tokens":12000,"output_tokens":100,"input_tokens_details":{"cached_tokens":4000,"cache_write_tokens":7000,"cache_write_tokens_details":{"ttl_30m_tokens":"3000","ttl_12h_tokens":4000}}`
	for _, body := range []string{
		`{"model":"test","usage":{` + usage + `}}`,
		`{"response":{"model":"test","usage":{` + usage + `}}}`,
		`{"usage":{"prompt_tokens":12000,"completion_tokens":100,"prompt_tokens_details":{"cached_tokens":4000,"cache_write_tokens_details":{"ttl_30m_tokens":3000,"ttl_12h_tokens":"4000"}}}}`,
	} {
		got := NewTokenParser().ParseNonStreaming([]byte(body))
		require.NotNil(t, got)
		require.Equal(t, int64(7000), got.CacheWriteTokens)
		require.Equal(t, int64(3000), got.CacheWrite30mTokens)
		require.Equal(t, int64(4000), got.CacheWrite12hTokens)
		u := buildTokenUsage(got, tracking.BillingOutputTokenSourceOutput)
		require.Equal(t, got.CacheWrite12hTokens, u.CacheWrite12hTokens)
	}
	line := []byte(`data: {"type":"response.completed","response":{"service_tier":"ultrafast","usage":{` + usage + `}}}`)
	got, _ := NewTokenParser().ParseStreamingLine(line)
	require.NotNil(t, got)
	require.Equal(t, "ultrafast", got.ServiceTier)
	require.Equal(t, int64(4000), got.CacheWrite12hTokens)
	invalid := NewTokenParser().ParseNonStreaming([]byte(`{"usage":{"input_tokens":10,"input_tokens_details":{"cache_write_tokens":7,"cache_write_tokens_details":{"ttl_30m_tokens":-1,"ttl_12h_tokens":7}}}}`))
	require.Equal(t, int64(-1), invalid.CacheWrite30mTokens, "invalid split must reach billing validation and fall back to 30m")
}

func TestCodexTierPolicySurvivesCompletionAndArchive(t *testing.T) {
	for _, tc := range []struct {
		name, source string
		rules        bool
	}{
		{"legacy response", "response", false},
		{"legacy request", "request", false},
		{"rules ignore old response preference", "response", true},
		{"rules without manual preference", "", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dbPath := filepath.Join(t.TempDir(), "usage.db")
			tracker, err := tracking.NewUsageTracker(&tracking.Config{Enabled: true, DatabasePath: dbPath, BufferSize: 50, BatchSize: 1, FlushInterval: 10 * time.Millisecond, MaxRetry: 1, CleanupInterval: time.Hour, RetentionDays: 1})
			require.NoError(t, err)
			t.Cleanup(func() { tracker.Close() })
			tracker.UpdatePricing(map[string]tracking.ModelPricing{
				tracking.ModelPricingKey("codex", "test", ""):     {CodexCachePricing: true, Input: 1, CacheRead: .1, CacheWritePrice30m: 1.25, CacheWritePrice12h: 2.5},
				tracking.ModelPricingKey("codex", "test", "fast"): {CodexCachePricing: true, Input: 2, CacheRead: .2, CacheWritePrice30m: 2.5, CacheWritePrice12h: 5},
			})
			m := tracking.DefaultEndpointMultiplier()
			m.ServiceTierSource, m.CacheWriteTTLPricing = tc.source, true
			if tc.rules {
				m.BillingRules = []tracking.BillingRule{{Mode: tracking.BillingRuleModeMultiplier, GroupMultiplierMode: tracking.BillingGroupMultiplierInherit, CostMultiplier: 1}}
			}
			tracker.UpdateEndpointMultipliers(map[string]tracking.EndpointMultiplier{tracking.EndpointMultiplierGroupKey("codex", "c", "g", "e"): m})
			h := &Handler{usageTracker: tracker, tokenParser: NewTokenParser(), logger: newTestLogger()}
			req := httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(`{"model":"test","service_tier":"priority"}`))
			h.recordStart("tier-test", req, false, "c", "test", modelRewriteResult{}, "", "priority")
			h.recordForwarding("tier-test", "e", "c", "test", []chainNode{{Hop: 1, Channel: "c", CredentialGroup: "g", Endpoint: "e"}})
			active, ok := tracker.GetActiveRequest("tier-test")
			require.True(t, ok)
			wantTier := "standard"
			if tc.rules {
				wantTier = "priority"
			}
			require.Equal(t, wantTier, tracker.ActiveRequestToDetail(active).ServiceTier)
			result := &TokenResult{InputTokens: 12000, CacheReadTokens: 4000, CacheWriteTokens: 7000, CacheWrite30mTokens: 3000, CacheWrite12hTokens: 4000, ServiceTier: "default"}
			want, priced := tracker.CalculateAttemptCost("codex", "c", "g", "e", "test", "default", buildTokenUsage(result, tracking.BillingOutputTokenSourceOutput), "priority")
			require.True(t, priced)
			h.recordSuccess("tier-test", time.Now(), http.StatusOK, result, "test", "e", "g", "c")
			db, err := sql.Open("sqlite", dbPath)
			require.NoError(t, err)
			defer db.Close()
			var tier, raw string
			var cost float64
			require.Eventually(t, func() bool {
				return db.QueryRowContext(context.Background(), `SELECT service_tier, total_cost_usd, billing_rule_snapshot FROM request_logs WHERE request_id = 'tier-test'`).Scan(&tier, &cost, &raw) == nil
			}, 5*time.Second, 20*time.Millisecond)
			var snapshot tracking.BillingDecisionSnapshot
			require.NoError(t, json.Unmarshal([]byte(raw), &snapshot))
			require.Equal(t, "priority", snapshot.RequestedServiceTier)
			require.Equal(t, "default", snapshot.ResponseServiceTier)
			origin := tracking.ServiceTierSourceStandard
			if tc.rules {
				origin = "request"
				require.Equal(t, tracking.ServiceTierSourceRules, active.ServiceTierSource)
				require.Equal(t, tracking.ServiceTierSourceRules, snapshot.ServiceTierPolicy)
			}
			require.Equal(t, origin, snapshot.ServiceTierSource)
			require.Equal(t, "reported_ttl", snapshot.CacheWritePricing)
			require.Equal(t, int64(4000), snapshot.CacheWrite12hTokens)
			require.InDelta(t, want, cost, 1e-12, "request archive and wallet settlement must select identical prices")
			if origin == "request" {
				require.Equal(t, "priority", tier)
				require.Equal(t, "fast", snapshot.PricingServiceTier)
				require.InDelta(t, .0303, cost, 1e-12)
			} else {
				require.Equal(t, "standard", tier)
				require.Equal(t, "", snapshot.PricingServiceTier)
				require.InDelta(t, .01515, cost, 1e-12)
			}
		})
	}
}
