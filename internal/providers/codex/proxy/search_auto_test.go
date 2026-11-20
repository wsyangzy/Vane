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
		body := nativeSearchBody
		if success {
			body = strings.Replace(body, `"external_web_access":false`, `"external_web_access":true`, 1)
		}
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest("POST", "/v1/alpha/search", strings.NewReader(body)))
		service.Close()
		if success && rec.Body.String() != nativeSearchResponse {
			t.Fatal("native tool output replaced")
		}
		if !success && rec.Code != 404 {
			t.Fatal("cached native error replaced", rec.Code)
		}
	}
}

func TestSearchOpenPageKeepsNativeFailureWithoutConfiguredExtractor(t *testing.T) {
	h, _, _ := newProfileCodexFixture(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		io.WriteString(w, `{"error":{"message":"native search unavailable"}}`)
	})
	service := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		t.Error("open_page called an unconfigured or unsupported service")
	}))
	defer service.Close()
	cfg := search.DefaultConfig()
	cfg.Endpoint = service.URL + "/tavily/search"
	brave := search.DefaultConfig()
	brave.Vendor, brave.Endpoint, brave.APIKey = "brave", service.URL+"/brave/search", "test-brave"
	cfg.Fallbacks = []search.Config{brave}
	h.searchConfig = func(context.Context) (search.Config, error) { return cfg, nil }
	const body = `{"model":"m","commands":{"open":[{"url":"https://example.org/page"}]}}`
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("POST", "/v1/alpha/search", strings.NewReader(body)))
	if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "native search unavailable") {
		t.Fatalf("native failure was lost: status=%d body=%s", rec.Code, rec.Body.String())
	}
}

func TestSearchOpenPageFallsBackFromNativeToTavilyToExa(t *testing.T) {
	h, _, _ := newProfileCodexFixture(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		io.WriteString(w, `{"error":{"message":"The /v1/alpha/search endpoint is not supported."}}`)
	})
	row := attachSearchTracker(t, h)
	service := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		if !strings.Contains(string(body), "https://example.org/page") || strings.Contains(string(body), "opaque-test-value") {
			t.Errorf("wrong extraction body: %s", body)
		}
		switch r.URL.Path {
		case "/tavily/extract":
			if r.Header.Get("Authorization") != "Bearer test-tavily" {
				t.Error("wrong Tavily credential")
			}
			io.WriteString(w, `{"results":[],"failed_results":[{"url":"https://example.org/page"}],"usage":{"credits":1}}`)
		case "/exa/contents":
			if r.Header.Get("X-API-Key") != "test-exa" || r.Header.Get("Authorization") != "" {
				t.Error("wrong Exa credential")
			}
			io.WriteString(w, `{"results":[{"url":"https://example.org/page","text":"extracted page"}]}`)
		default:
			t.Errorf("unexpected operation: %s", r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer service.Close()
	cfg := search.DefaultConfig()
	cfg.Endpoint, cfg.APIKey = service.URL+"/tavily/search", "test-tavily"
	exa := search.DefaultConfig()
	exa.Vendor, exa.Endpoint, exa.APIKey = "exa", service.URL+"/exa/search", "test-exa"
	cfg.Fallbacks = []search.Config{exa}
	h.searchConfig = func(context.Context) (search.Config, error) { return cfg, nil }
	const body = `{"model":"m","commands":{"open":[{"ref_id":"https://example.org/page"}]},"encrypted_context":"opaque-test-value"}`
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("POST", "/v1/alpha/search", strings.NewReader(body)))
	got := row()
	first := strings.Index(got.EndpointChain, "Tavily · 网页提取")
	second := strings.Index(got.EndpointChain, "Exa · 网页提取")
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), "extracted page") || got.Status != "completed" ||
		got.EndpointName != "Exa" || first < 0 || second <= first || !strings.Contains(got.EndpointChain, "未返回符合规则的网页正文") ||
		strings.Contains(got.EndpointChain, "https://example.org/page") {
		t.Fatalf("unexpected fallback: response=%s row=%+v", rec.Body.String(), got)
	}
}
