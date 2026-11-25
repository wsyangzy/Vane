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
					wantReason := map[string]string{"disabled": "route_unavailable", "budget": routing.StopRecoveryOutsideBudget}[scenario]
					if sends != 0 || trace.StopReason != wantReason || scenario == "disabled" && trace.WaitMS != 0 {
						t.Fatalf("recovery boundary failed: sends=%d trace=%+v body=%s", sends, trace, rec.Body.String())
					}
					if scenario == "budget" && (trace.Attempts != 0 || trace.WaitMS != 0 || trace.NextRecoveryAt == nil || trace.MaxDurationMS != 5) {
						t.Fatalf("recovery stop lost scheduling evidence: %+v", trace)
					}
				}
			})
		}
	}
}

func TestClaudeProfileWaitsForInFlightRecoveryProbe(t *testing.T) {
	for _, streaming := range []bool{false, true} {
		for _, scenario := range []string{"recovered", "disabled", "deadline"} {
			t.Run(fmt.Sprintf("%s/stream=%t", scenario, streaming), func(t *testing.T) {
				sends := 0
				raw := fmt.Sprintf(`{"model":"test","stream":%t,"messages":[{"role":"user","content":"hello"}],"max_tokens":16}`, streaming)
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					sends++
					body, _ := io.ReadAll(r.Body)
					if string(body) != raw {
						t.Errorf("recovery changed the request body: %s", body)
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
				if scenario == "deadline" {
					cfg.Retry.MaxTotalDuration = 30 * time.Millisecond
				}
				h, manager := profileClaudeHandler(t, cfg, 2, nil)
				pool := manager.PoolRuntime()
				runtime := pool.ProfileRuntime()
				profile, _ := runtime.Profile(1)
				candidates := runtime.ExpandTargets(profile, []routing.RouteTarget{{ProfileID: 1}}, "test")
				candidate := candidates[0]
				if err := pool.Fail(candidate.ChannelID, candidate.GroupID, candidate.EndpointID, candidate.Identity().Model, routing.Failure{Scope: routing.ScopeEndpoint, Reason: "network_error"}, time.Time{}); err != nil {
					t.Fatal(err)
				}
				if err := pool.RequestRetry(candidate.ChannelID, candidate.GroupID, candidate.EndpointID, candidate.Identity().Model); err != nil {
					t.Fatal(err)
				}
				release, err := pool.AcquireHealth(candidate.ChannelID, candidate.GroupID, candidate.EndpointID, candidate.Identity().Model)
				if err != nil {
					t.Fatal(err)
				}
				defer release(false)
				var trace routing.DispatchTrace
				runtime.Replace(routing.RuntimeConfig{Provider: "claude", Profiles: []routing.RoutingProfile{profile}, Candidates: candidates, PrepareCandidate: func(candidate routing.DispatchCandidate, model, override string) routing.DispatchCandidate {
					candidate = routing.CopyCandidateForModel(candidate, model, override)
					if allowed, reason := pool.HealthAllowed(candidate.ChannelID, candidate.GroupID, candidate.EndpointID, candidate.Identity().Model); !allowed {
						candidate.Health = routing.HealthState{CoolingDown: true, Reason: reason}
					}
					return candidate
				}, OnTrace: func(value routing.DispatchTrace) { trace = value }})
				if scenario == "recovered" {
					timer := time.AfterFunc(60*time.Millisecond, func() { release(true) })
					defer timer.Stop()
				}
				rec := httptest.NewRecorder()
				h.ServeHTTP(rec, httptest.NewRequest("POST", "/v1/messages", strings.NewReader(raw)))
				if scenario == "recovered" {
					if rec.Code != http.StatusOK || sends != 1 || trace.Attempts != 1 || !trace.Successful || trace.WaitMS <= 0 {
						t.Fatalf("probe completion did not resume the request: status=%d sends=%d attempts=%d success=%t wait=%d stop=%s", rec.Code, sends, trace.Attempts, trace.Successful, trace.WaitMS, trace.StopReason)
					}
				} else {
					wantReason := map[string]string{"disabled": "route_unavailable", "deadline": routing.StopRetryDeadlineExceeded}[scenario]
					if sends != 0 || trace.Attempts != 0 || trace.StopReason != wantReason || scenario == "disabled" && trace.WaitMS != 0 || scenario == "deadline" && trace.WaitMS <= 0 {
						t.Fatalf("probe wait ignored its boundary: sends=%d attempts=%d wait=%d stop=%s", sends, trace.Attempts, trace.WaitMS, trace.StopReason)
					}
				}
				if trace.NextRecoveryAt != nil {
					t.Fatal("an in-flight probe fabricated a known recovery time")
				}
			})
		}
	}
}
