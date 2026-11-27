package codexproxy

import (
	"context"
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

func TestCodexGlobalRecoveryPolicyOverridesStoredProfile(t *testing.T) {
	for _, streaming := range []bool{false, true} {
		for _, scenario := range []string{"default", "disabled", "budget", "cancel"} {
			t.Run(fmt.Sprintf("%s/stream=%t", scenario, streaming), func(t *testing.T) {
				sends := 0
				var ready time.Time
				h, runtime, _ := newProfileCodexFixture(t, func(w http.ResponseWriter, r *http.Request) {
					sends++
					if time.Now().Before(ready) {
						t.Error("request sent during cooldown")
					}
					if streaming {
						w.Header().Set("Content-Type", "text/event-stream")
						io.WriteString(w, "data: {\"type\":\"response.completed\",\"response\":{\"status\":\"completed\",\"output\":[]}}\n\n")
					} else {
						io.WriteString(w, `{"status":"completed","output":[]}`)
					}
				})
				ready = time.Now().Add(60 * time.Millisecond)
				profile, _ := runtime.Profile(1)
				profile.Members = profile.Members[:1]
				profile.WaitForRecovery = scenario == "disabled"
				var trace routing.DispatchTrace
				runtime.Replace(routing.RuntimeConfig{Provider: "codex", Profiles: []routing.RoutingProfile{profile}, Candidates: []routing.DispatchCandidate{{Provider: "codex", ChannelID: 1, GroupID: 11, EndpointID: "1", Protocol: "responses", Enabled: true, Health: routing.HealthState{CoolingDown: true, Until: ready}}}, OnTrace: func(value routing.DispatchTrace) { trace = value }})
				retry := config.RetryConfig{MaxTotalAttempts: 2, MaxTotalDuration: time.Second, DisableRecoveryWait: scenario == "disabled"}
				if scenario == "budget" {
					retry.MaxTotalDuration = 5 * time.Millisecond
				}
				h.UpdateConfig(retry, config.FailoverConfig{Enabled: true})
				req := httptest.NewRequest("POST", "/v1/responses", strings.NewReader(fmt.Sprintf(`{"model":"test","stream":%t,"input":"hello"}`, streaming)))
				if scenario == "cancel" {
					ctx, cancel := context.WithCancel(req.Context())
					defer cancel()
					timer := time.AfterFunc(5*time.Millisecond, cancel)
					defer timer.Stop()
					req = req.WithContext(ctx)
				}
				rec := httptest.NewRecorder()
				h.ServeHTTP(rec, req)
				if scenario == "default" {
					if sends != 1 || !trace.Successful || trace.WaitMS <= 0 {
						t.Fatalf("global default did not recover: sends=%d trace=%+v body=%s", sends, trace, rec.Body.String())
					}
				} else {
					wantReason := map[string]string{"disabled": "route_unavailable", "budget": routing.StopRecoveryOutsideBudget, "cancel": "client_cancelled"}[scenario]
					if scenario != "cancel" && (rec.Code != http.StatusServiceUnavailable || rec.Header().Get("Retry-After") == "" || !strings.Contains(rec.Body.String(), wantReason)) {
						t.Fatalf("temporary gate must allow client retries: status=%d headers=%v body=%s", rec.Code, rec.Header(), rec.Body.String())
					}
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

func TestCodexProfileWaitsForInFlightRecoveryProbe(t *testing.T) {
	for _, streaming := range []bool{false, true} {
		for _, scenario := range []string{"recovered", "disabled", "deadline"} {
			t.Run(fmt.Sprintf("%s/stream=%t", scenario, streaming), func(t *testing.T) {
				sends := 0
				raw := fmt.Sprintf(`{"model":"test","stream":%t,"input":"hello"}`, streaming)
				h, runtime, _ := newProfileCodexFixture(t, func(w http.ResponseWriter, r *http.Request) {
					sends++
					body, _ := io.ReadAll(r.Body)
					if string(body) != raw {
						t.Errorf("recovery changed the request body: %s", body)
					}
					if streaming {
						w.Header().Set("Content-Type", "text/event-stream")
						io.WriteString(w, "data: {\"type\":\"response.completed\",\"response\":{\"status\":\"completed\",\"output\":[]}}\n\n")
					} else {
						io.WriteString(w, `{"status":"completed","output":[]}`)
					}
				})
				pool := h.manager.PoolRuntime()
				if err := pool.Fail(1, 11, "1", "test", routing.Failure{Scope: routing.ScopeEndpoint, Reason: "endpoint_unavailable"}, time.Time{}); err != nil {
					t.Fatal(err)
				}
				if err := pool.RequestRetry(1, 11, "1", "test"); err != nil {
					t.Fatal(err)
				}
				release, err := pool.AcquireHealth(1, 11, "1", "test")
				if err != nil {
					t.Fatal(err)
				}
				defer release(false)
				profile, _ := runtime.Profile(1)
				profile.Members = profile.Members[:1]
				var trace routing.DispatchTrace
				runtime.Replace(routing.RuntimeConfig{Provider: "codex", Profiles: []routing.RoutingProfile{profile}, Candidates: []routing.DispatchCandidate{{Provider: "codex", ChannelID: 1, GroupID: 11, EndpointID: "1", Protocol: "responses", Enabled: true}}, PrepareCandidate: func(candidate routing.DispatchCandidate, model, override string) routing.DispatchCandidate {
					candidate = h.manager.PrepareDispatchCandidate(candidate, model, override)
					if allowed, reason := pool.HealthAllowed(candidate.ChannelID, candidate.GroupID, candidate.EndpointID, candidate.Identity().Model); !allowed {
						candidate.Health = routing.HealthState{CoolingDown: true, Reason: reason}
					}
					return candidate
				}, OnTrace: func(value routing.DispatchTrace) { trace = value }})
				retry := config.RetryConfig{MaxTotalAttempts: 2, MaxTotalDuration: time.Second, DisableRecoveryWait: scenario == "disabled"}
				if scenario == "deadline" {
					retry.MaxTotalDuration = 30 * time.Millisecond
				}
				h.UpdateConfig(retry, config.FailoverConfig{Enabled: true})
				if scenario == "recovered" {
					timer := time.AfterFunc(60*time.Millisecond, func() { release(true) })
					defer timer.Stop()
				}
				rec := httptest.NewRecorder()
				h.ServeHTTP(rec, httptest.NewRequest("POST", "/v1/responses", strings.NewReader(raw)))
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
