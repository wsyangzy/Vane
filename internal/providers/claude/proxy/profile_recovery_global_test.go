package proxy

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/wsyangzy/Vane/internal/config"
	"github.com/wsyangzy/Vane/internal/routing"
)

func TestClaudeGlobalRecoveryPolicyOverridesStoredProfile(t *testing.T) {
	for _, streaming := range []bool{false, true} {
		for _, scenario := range []string{"default", "disabled", "budget"} {
			t.Run(fmt.Sprintf("%s/stream=%t", scenario, streaming), func(t *testing.T) {
				sends := 0
				var ready time.Time
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					sends++
					if time.Now().Before(ready) {
						t.Error("request sent during cooldown")
					}
					if streaming {
						w.Header().Set("Content-Type", "text/event-stream")
						io.WriteString(w, "event: message_start\ndata: {\"type\":\"message_start\",\"message\":{\"model\":\"test\",\"usage\":{\"input_tokens\":1}}}\n\nevent: message_delta\ndata: {\"type\":\"message_delta\",\"delta\":{\"stop_reason\":\"end_turn\"},\"usage\":{\"output_tokens\":1}}\n\nevent: message_stop\ndata: {\"type\":\"message_stop\"}\n\n")
					} else {
						w.Header().Set("Content-Type", "application/json")
						io.WriteString(w, `{"type":"message","stop_reason":"end_turn","content":[],"usage":{"input_tokens":1,"output_tokens":1}}`)
					}
				}))
				defer server.Close()
				cfg := &config.Config{Retry: config.RetryConfig{MaxAttempts: 2, MaxTotalAttempts: 2, MaxTotalDuration: time.Second, DisableRecoveryWait: scenario == "disabled"}, Endpoints: []config.EndpointConfig{{Name: "a", Channel: "A", CredentialGroupID: 11, URL: server.URL, SkipHealthCheck: true}}}
				if scenario == "budget" {
					cfg.Retry.MaxTotalDuration = 5 * time.Millisecond
				}
				h, manager := profileClaudeHandler(t, cfg, 2, nil)
				runtime := manager.PoolRuntime().ProfileRuntime()
				profile, _ := runtime.Profile(1)
				profile.WaitForRecovery = scenario == "disabled"
				candidates := runtime.ExpandTargets(profile, []routing.RouteTarget{{ProfileID: 1}}, "test")
				ready = time.Now().Add(60 * time.Millisecond)
				for i := range candidates {
					candidates[i].Health = routing.HealthState{CoolingDown: true, Until: ready}
				}
				var trace routing.DispatchTrace
				runtime.Replace(routing.RuntimeConfig{Provider: "claude", Profiles: []routing.RoutingProfile{profile}, Candidates: candidates, OnTrace: func(value routing.DispatchTrace) { trace = value }})
				rec := httptest.NewRecorder()
				h.ServeHTTP(rec, httptest.NewRequest("POST", "/v1/messages", strings.NewReader(fmt.Sprintf(`{"model":"test","stream":%t,"messages":[{"role":"user","content":"hello"}],"max_tokens":16}`, streaming))))
				if scenario == "default" {
					if sends != 1 || !trace.Successful || trace.WaitMS <= 0 {
						t.Fatalf("global default did not recover: sends=%d trace=%+v body=%s", sends, trace, rec.Body.String())
					}
				} else {
					wantReason := map[string]string{"disabled": "route_unavailable", "budget": "retry_budget_exhausted"}[scenario]
					if sends != 0 || trace.StopReason != wantReason || scenario == "disabled" && trace.WaitMS != 0 {
						t.Fatalf("recovery boundary failed: sends=%d trace=%+v body=%s", sends, trace, rec.Body.String())
					}
				}
			})
		}
	}
}
