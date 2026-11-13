package codexproxy

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/wsyangzy/Vane/internal/config"
	"github.com/wsyangzy/Vane/internal/providers/codex/proxy/search"
)

func TestSearchAutomaticFallbackPreservesNativeFailureAndFinalServiceSuccess(t *testing.T) {
	for _, legacy := range []bool{false, true} {
		for _, status := range []int{200, 404, 503} {
			t.Run(strings.Join([]string{map[bool]string{true: "legacy", false: "profile"}[legacy], http.StatusText(status)}, "/"), func(t *testing.T) {
				nativeCalls, serviceCalls := 0, 0
				h, _, _ := newProfileCodexFixture(t, func(w http.ResponseWriter, r *http.Request) {
					nativeCalls++
					w.WriteHeader(status)
					if status == 200 {
						io.WriteString(w, `{"models":[]}`)
					} else {
						io.WriteString(w, `{"error":{"message":"native search unavailable"}}`)
					}
				})
				h.UpdateConfig(config.RetryConfig{MaxAttempts: 1, MaxTotalAttempts: 2, DisableRecoveryWait: true}, config.FailoverConfig{Enabled: true})
				if legacy {
					h.manager.poolRuntime.Store(nil)
					ep := h.manager.endpointsByChannel[1][0]
					ep.FailoverEnabled = true
					h.manager.endpoints = []*CodexEndpoint{ep}
					h.failoverCfg.Enabled = false
				}
				service := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					serviceCalls++
					if r.Header.Get("Authorization") != "Bearer external-key" {
						t.Error("wrong search credential")
					}
					io.WriteString(w, `{"results":[{"title":"fallback","url":"https://example.org/page","content":"external answer"}]}`)
				}))
				defer service.Close()
				cfg := search.DefaultConfig()
				cfg.Endpoint, cfg.APIKey = service.URL, "external-key"
				h.searchConfig = func(context.Context) (search.Config, error) { return cfg, nil }
				row := attachSearchTracker(t, h)
				body := strings.Replace(nativeSearchBody, `"external_web_access":false`, `"external_web_access":true`, 1)
				rec := httptest.NewRecorder()
				h.ServeHTTP(rec, httptest.NewRequest("POST", "/v1/alpha/search", strings.NewReader(body)))
				got := row()
				if rec.Code != 200 || nativeCalls < 1 || serviceCalls != 1 || !strings.Contains(rec.Body.String(), "external answer") || got.Status != "completed" {
					t.Fatalf("automatic fallback failed: native=%d service=%d response=%s row=%+v", nativeCalls, serviceCalls, rec.Body.String(), got)
				}
				if !strings.Contains(got.EndpointChain, "搜索服务") || !strings.Contains(got.EndpointChain, "first") {
					t.Fatalf("native history lost: %s", got.EndpointChain)
				}
			})
		}
	}
}

func TestSearchAutomaticModeDoesNotLeakCachedQueriesOrOverrideNativeSuccess(t *testing.T) {
	for _, success := range []bool{false, true} {
		h, _, _ := newProfileCodexFixture(t, func(w http.ResponseWriter, _ *http.Request) {
			if success {
				io.WriteString(w, nativeSearchResponse)
			} else {
				w.WriteHeader(404)
				io.WriteString(w, `{"error":"native unavailable"}`)
			}
		})
		h.UpdateConfig(config.RetryConfig{MaxAttempts: 1, MaxTotalAttempts: 2, DisableRecoveryWait: true}, config.FailoverConfig{Enabled: true})
		service := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
			t.Error("native success or cached request fell back to live service")
		}))
		cfg := search.DefaultConfig()
		cfg.Endpoint, cfg.APIKey = service.URL, "key"
		h.searchConfig = func(context.Context) (search.Config, error) { return cfg, nil }
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest("POST", "/v1/alpha/search", strings.NewReader(nativeSearchBody)))
		service.Close()
		if success && rec.Body.String() != nativeSearchResponse {
			t.Fatal("native tool output replaced")
		}
		if !success && rec.Code != 404 {
			t.Fatal("cached native error replaced", rec.Code)
		}
	}
}
