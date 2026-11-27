package routing

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

func rejectLocalFixture(t *testing.T, r *ProfileRuntime, body string) DispatchPlan {
	t.Helper()
	request := httptest.NewRequest("POST", "/v1/responses", strings.NewReader(body))
	request, rejection := r.AdmitLocalRequest(request, []byte(body))
	if rejection != nil {
		t.Fatalf("first request blocked: %+v", rejection)
	}
	plan := r.Plan(request.Context(), 0, "test", false, TurnInput{}, false)
	plan.RequestID = "req-first"
	if rejection := plan.RejectLocal(plan.Error); rejection == nil {
		t.Fatal("pre-send failure was not retained")
	}
	plan.End(plan.Error)
	return plan
}

func TestLocalFailureRetainsFirstDeadlineAndDistinguishesRequests(t *testing.T) {
	const body = `{"model":"test","input":"private prompt","tools":[{"name":"one"}]}`
	now := time.Now()
	r := NewProfileRuntime(RuntimeConfig{Provider: "codex", LoadError: errors.New("SQL logic error: no such column: intent_id (1)")})
	r.now = func() time.Time { return now }
	plan := rejectLocalFixture(t, r, body)
	if plan.Error != "routing_config_unavailable" || !strings.Contains(plan.FailureDetail(), "no such column: intent_id") || plan.Trace().Provider != "codex" {
		t.Fatalf("configuration cause lost: %s / %s", plan.Error, plan.FailureDetail())
	}
	_, first := r.AdmitLocalRequest(httptest.NewRequest("POST", "/v1/responses", nil), []byte(body))
	deadline := first.RetryAt
	if plan.Trace().NextRecoveryAt != nil {
		t.Fatal("request cooldown manufactured a configuration recovery time")
	}
	for range 5 {
		now = now.Add(time.Second)
		request := httptest.NewRequest("POST", "/v1/responses", strings.NewReader(body))
		request.Header.Set("X-Stainless-Retry-Count", "9")
		_, blocked := r.AdmitLocalRequest(request, []byte(body))
		if blocked == nil || !blocked.Repeated || blocked.RequestID != "req-first" || !blocked.RetryAt.Equal(deadline) {
			t.Fatalf("retry bypassed or extended cooldown: %+v", blocked)
		}
	}
	for _, change := range []string{"body", "tools", "path", "query", "caller", "session", "auth", "model"} {
		t.Run(change, func(t *testing.T) {
			changed := body
			switch change {
			case "body":
				changed = strings.Replace(body, "private prompt", "new prompt", 1)
			case "tools":
				changed = strings.Replace(body, "one", "two", 1)
			case "model":
				changed = strings.Replace(body, "test", "another-model", 1)
			}
			request := httptest.NewRequest("POST", "/v1/responses", strings.NewReader(changed))
			switch change {
			case "path":
				request.URL.Path = "/responses/compact"
			case "query":
				request.URL.RawQuery = "mode=other"
			case "caller":
				request = request.WithContext(WithCallerScope(request.Context(), "another-caller"))
			case "session":
				request.Header.Set("Session_id", "another-session")
			case "auth":
				request.Header.Set("Authorization", "Bearer another-secret")
			}
			if _, blocked := r.AdmitLocalRequest(request, []byte(changed)); blocked != nil {
				t.Fatalf("distinct %s blocked", change)
			}
		})
	}
	canonical := ` {"tools": [{"name": "one"}], "input": "private prompt", "model": "test"} `
	if _, blocked := r.AdmitLocalRequest(httptest.NewRequest("POST", "/v1/responses", nil), []byte(canonical)); blocked == nil {
		t.Fatal("JSON formatting bypassed identical request gate")
	}
	data, _ := json.Marshal(plan.Trace())
	if strings.Contains(string(data), "private prompt") || strings.Contains(string(data), "another-secret") {
		t.Fatal("request identity leaked into public trace")
	}
	now = deadline
	if _, blocked := r.AdmitLocalRequest(httptest.NewRequest("POST", "/v1/responses", nil), []byte(body)); blocked != nil {
		t.Fatal("cooldown did not expire")
	}
}

func TestLocalFailureRecoveryAndUnchangedRefresh(t *testing.T) {
	const body = `{"model":"test","input":"hello"}`
	cfg := runtimeTestConfig()
	now := time.Now()
	until := now.Add(2 * time.Minute)
	blocked := true
	cfg.PrepareCandidate = func(c DispatchCandidate, model, override string) DispatchCandidate {
		c = CopyCandidateForModel(c, model, override)
		c.Health.CoolingDown, c.Health.Until = blocked, until
		return c
	}
	r := NewProfileRuntime(cfg)
	r.now = func() time.Time { return now }
	_ = rejectLocalFixture(t, r, body)
	_, first := r.AdmitLocalRequest(httptest.NewRequest("POST", "/v1/responses", nil), []byte(body))
	if first == nil || !first.RetryAt.Equal(until) {
		t.Fatal("health recovery deadline lost")
	}
	r.Replace(cfg)
	if _, gate := r.AdmitLocalRequest(httptest.NewRequest("POST", "/v1/responses", nil), []byte(body)); gate == nil {
		t.Fatal("unchanged refresh reset cooldown")
	}
	cfg.Candidates[0].Load.DecayedRequests = 12
	cfg.Candidates[0].Quota.Windows = []QuotaSnapshot{{ExpiresAt: now.Add(time.Minute)}}
	r.Replace(cfg)
	if _, gate := r.AdmitLocalRequest(httptest.NewRequest("POST", "/v1/responses", nil), []byte(body)); gate == nil {
		t.Fatal("periodic quota/load refresh reset cooldown")
	}
	blocked = false
	if _, gate := r.AdmitLocalRequest(httptest.NewRequest("POST", "/v1/responses", nil), []byte(body)); gate != nil {
		t.Fatal("manual recovery remained blocked")
	}
	blocked = true
	_ = rejectLocalFixture(t, r, body)
	cfg.Profiles[0].Revision++
	r.Replace(cfg)
	if _, gate := r.AdmitLocalRequest(httptest.NewRequest("POST", "/v1/responses", nil), []byte(body)); gate != nil {
		t.Fatal("configuration change remained blocked")
	}
}

func TestLocalRejectionOnlyTimedGatesAreRetryable(t *testing.T) {
	for _, condition := range []string{"cooldown", "budget", "configuration", "missing-profile", "unknown-recovery", "manual-repair", "disabled", "model-unsupported"} {
		t.Run(condition, func(t *testing.T) {
			cfg := runtimeTestConfig()
			until := time.Now().Add(2 * time.Minute)
			cfg.PrepareCandidate = func(c DispatchCandidate, model, override string) DispatchCandidate {
				c = CopyCandidateForModel(c, model, override)
				c.Health = HealthState{CoolingDown: true, Until: until}
				if condition == "unknown-recovery" {
					c.Health.Until = time.Time{}
				}
				if condition == "manual-repair" {
					c.Health.RepairRequired = true
				}
				if condition == "disabled" {
					c.Enabled = false
				}
				return c
			}
			if condition == "configuration" {
				cfg.LoadError = errors.New("no such column: intent_id")
			}
			if condition == "missing-profile" {
				cfg.Profiles = nil
			}
			if condition == "model-unsupported" {
				cfg.Supports = func(DispatchCandidate, string) bool { return false }
			}
			r := NewProfileRuntime(cfg)
			raw := []byte(`{"model":"test","input":"hello"}`)
			request, gate := r.AdmitLocalRequest(httptest.NewRequest("POST", "/v1/responses", nil), raw)
			if gate != nil {
				t.Fatal("first request already blocked")
			}
			plan := r.Plan(request.Context(), 0, "test", false, TurnInput{}, false)
			reason := plan.Error
			if condition == "budget" {
				reason = StopRecoveryOutsideBudget
			}
			first := plan.RejectLocal(reason)
			if first == nil {
				t.Fatal("missing local rejection")
			}
			_, repeated := r.AdmitLocalRequest(httptest.NewRequest("POST", "/v1/responses", nil), raw)
			if repeated == nil || !repeated.Repeated {
				t.Fatal("identical request did not stay gated")
			}
			wantStatus := http.StatusBadRequest
			if condition == "cooldown" || condition == "budget" {
				wantStatus = http.StatusServiceUnavailable
			}
			for _, gate := range []*LocalRejection{first, repeated} {
				recorder := httptest.NewRecorder()
				gate.Write(recorder)
				seconds, err := strconv.Atoi(recorder.Header().Get("Retry-After"))
				if recorder.Code != wantStatus || err != nil || seconds <= 0 || !strings.Contains(recorder.Body.String(), reason) {
					t.Fatalf("status=%d headers=%v body=%s", recorder.Code, recorder.Header(), recorder.Body.String())
				}
				if wantStatus == http.StatusServiceUnavailable && !gate.RetryAt.Equal(until) {
					t.Fatal("timed gate lost its actual recovery deadline")
				}
			}
			if !first.RetryAt.Equal(repeated.RetryAt) {
				t.Fatal("SDK retry extended the failure window")
			}
		})
	}
}

func TestLocalFailureRuleSettingsInvalidateButCooldownPolicyPreservesDeadline(t *testing.T) {
	const body = `{"model":"test","input":"hello"}`
	r := NewProfileRuntime(RuntimeConfig{Provider: "codex"})
	pool := NewPoolRuntime(PoolConfig{}, nil, nil)
	pool.SetProfileRuntime(r)
	pool.SetCooldownPolicy(CooldownPolicy{Initial: time.Minute})
	plan := rejectLocalFixture(t, r, body)
	_, first := r.AdmitLocalRequest(httptest.NewRequest("POST", "/v1/responses", nil), []byte(body))
	if first == nil || first.RetryAt.Sub(plan.Trace().CreatedAt) < 59*time.Second {
		t.Fatal("local failure ignored configured cooldown")
	}
	pool.SetCooldownPolicy(CooldownPolicy{Initial: time.Second})
	_, next := r.AdmitLocalRequest(httptest.NewRequest("POST", "/v1/responses", nil), []byte(body))
	if next == nil || !next.RetryAt.Equal(first.RetryAt) {
		t.Fatal("policy update rewrote existing deadline")
	}
	r.SetRuleDefaults(RuleDefaults{FallbackToDefault: true})
	if _, gate := r.AdmitLocalRequest(httptest.NewRequest("POST", "/v1/responses", nil), []byte(body)); gate != nil {
		t.Fatal("changed routing policy did not release old failure")
	}
}

func TestLocalFailureNeverCachesSentOrCancelledRequests(t *testing.T) {
	r := NewProfileRuntime(runtimeTestConfig())
	for _, outcome := range []string{"sent", "cancelled", "preview", "successful"} {
		t.Run(outcome, func(t *testing.T) {
			request, _ := r.AdmitLocalRequest(httptest.NewRequest("POST", "/v1/responses", nil), []byte(outcome))
			plan := r.Plan(request.Context(), 0, "test", false, TurnInput{}, outcome == "preview")
			reason := "route_unavailable"
			switch outcome {
			case "sent":
				if err := plan.Budget.Begin(context.Background()); err != nil {
					t.Fatal(err)
				}
			case "cancelled":
				reason = "client_cancelled"
			case "successful":
				plan.state.successful = true
			}
			if plan.RejectLocal(reason) != nil {
				t.Fatal("non-local outcome entered failure gate")
			}
		})
	}
}

func TestLocalFailureBoundsCacheAndConcurrentLookups(t *testing.T) {
	r := NewProfileRuntime(RuntimeConfig{Provider: "codex"})
	for i := 0; i < maxLocalFailures+20; i++ {
		body, _ := json.Marshal(i)
		_ = rejectLocalFixture(t, r, string(body))
	}
	if len(r.localFailures) != maxLocalFailures {
		t.Fatal("failure cache not bounded")
	}
	_ = rejectLocalFixture(t, r, `{"input":"concurrent"}`)
	var wg sync.WaitGroup
	for range 20 {
		wg.Go(func() {
			if _, gate := r.AdmitLocalRequest(httptest.NewRequest("POST", "/v1/responses", nil), []byte(`{"input":"concurrent"}`)); gate == nil {
				t.Error("concurrent retry bypassed cooldown")
			}
		})
	}
	wg.Wait()
}

func TestConfigurationFailureDoesNotExposeUnrecognizedErrorValues(t *testing.T) {
	err := errors.New("database error with Authorization: Bearer private-secret and private prompt")
	detail := configurationFailureDetail(err)
	if strings.Contains(detail, "private") || !strings.Contains(detail, "应用日志") {
		t.Fatal("unsafe configuration diagnostic")
	}
}
