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
					wantReason := map[string]string{"disabled": "route_unavailable", "budget": "retry_budget_exhausted", "cancel": "client_cancelled"}[scenario]
					if sends != 0 || trace.StopReason != wantReason || scenario == "disabled" && trace.WaitMS != 0 {
						t.Fatalf("recovery boundary failed: sends=%d trace=%+v body=%s", sends, trace, rec.Body.String())
					}
				}
			})
		}
	}
}
