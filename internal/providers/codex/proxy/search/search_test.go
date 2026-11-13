package search

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func testConfig(endpoint string) Config {
	cfg := DefaultConfig()
	cfg.Mode, cfg.APIKey, cfg.Endpoint = "service", "search-key-sentinel", endpoint
	return cfg
}

func parseTestRequest(t *testing.T, body string) Request {
	t.Helper()
	req, err := Parse([]byte(body))
	if err != nil {
		t.Fatal(err)
	}
	return req
}

func TestRunTranslatesQueriesFiltersAndCitations(t *testing.T) {
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		var payload map[string]any
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Error(err)
		}
		encoded, _ := json.Marshal(payload)
		if r.Method != "POST" || r.Header.Get("Authorization") != "Bearer search-key-sentinel" || payload["query"] != "test query" || payload["start_date"] != time.Now().UTC().AddDate(0, 0, -7).Format("2006-01-02") || strings.Contains(string(encoded), "opaque-sentinel") {
			t.Errorf("wrong search service request: %s", encoded)
		}
		if got := payload["include_domains"].([]any); len(got) != 1 || got[0] != "docs.example.org" {
			t.Errorf("domain intersection lost: %v", got)
		}
		fmt.Fprint(w, `{"results":[{"title":"Docs","url":"https://docs.example.org/guide","content":"A useful snippet"},{"title":"blocked","url":"https://private.docs.example.org/","content":"private"},{"url":"https://docs.example.org.evil.test/"},{"url":"javascript:alert(1)"}]}`)
	}))
	defer server.Close()
	req := parseTestRequest(t, `{"model":"original-model","commands":{"search_query":[{"q":"test query","domains":["example.org"],"recency":7}],"open":[{"ref_id":"opaque-sentinel"}]},"settings":{"external_web_access":true,"filters":{"allowed_domains":["docs.example.org"],"blocked_domains":["private.docs.example.org"]}},"encrypted_context":"opaque-sentinel"}`)
	out := Run(context.Background(), server.Client(), testConfig(server.URL), req, "abc")
	if out.Failure != nil || calls != 1 || len(out.Reply.Results) != 1 || out.Reply.EncryptedOutput != nil {
		t.Fatalf("unexpected result: %+v calls=%d", out, calls)
	}
	if out.Reply.Results[0].RefID != "turnabcsearch0" || !strings.Contains(out.Reply.Output, "\ue200cite\ue202turnabcsearch0\ue201") || !strings.Contains(out.Reply.Output, "does not support these commands: open") {
		t.Fatalf("missing citations or capability notice: %s", out.Reply.Output)
	}
}

func TestRunImageQueryReturnsImagesInsteadOfWebResults(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var payload map[string]any
		_ = json.NewDecoder(r.Body).Decode(&payload)
		if payload["include_images"] != true || payload["include_image_descriptions"] != true {
			t.Error("image options missing")
		}
		fmt.Fprint(w, `{"results":[{"url":"https://example.org/not-an-image"}],"images":[{"url":"https://images.example.org/a.png","description":"A picture"},"https://images.example.org/b.png"]}`)
	}))
	defer server.Close()
	out := Run(context.Background(), server.Client(), testConfig(server.URL), parseTestRequest(t, `{"model":"m","commands":{"image_query":[{"q":"picture"}]}}`), "img")
	if out.Failure != nil || len(out.Reply.Results) != 2 || !strings.HasSuffix(out.Reply.Results[0].URL, ".png") || strings.Contains(out.Reply.Output, "not-an-image") {
		t.Fatalf("image query lost semantics: %+v", out)
	}
}

func TestRunRejectsUnsupportedRequestsBeforeSending(t *testing.T) {
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { calls++ }))
	defer server.Close()
	for _, tc := range []struct{ body, reason string }{
		{`{"model":"m","commands":{"open":[{"ref_id":"ref"}]}}`, "search_command_unsupported"},
		{`{"model":"m","commands":{"search_query":[{"q":"test"}]},"settings":{"external_web_access":false}}`, "search_cached_unsupported"},
		{`{"model":"m","commands":{"search_query":[{"q":""}]}}`, "search_request_invalid"},
		{`{"model":"m","commands":{"search_query":[{"q":"test","domains":["one.test"]}]},"settings":{"filters":{"allowed_domains":["two.test"]}}}`, "search_request_invalid"},
		{`{"model":"m","commands":{"search_query":{"q":"test"}}}`, "search_request_invalid"},
	} {
		out := Run(context.Background(), server.Client(), testConfig(server.URL), parseTestRequest(t, tc.body), "t")
		if out.Failure == nil || out.Failure.Reason != tc.reason || out.Failure.Status != 0 || out.Reply.Output == "" {
			t.Errorf("incorrect local failure: %+v", out)
		}
	}
	cfg := testConfig(server.URL)
	cfg.APIKey = ""
	out := Run(context.Background(), server.Client(), cfg, parseTestRequest(t, `{"model":"m","commands":{"search_query":[{"q":"test"}]}}`), "t")
	if calls != 0 || out.Failure == nil || out.Failure.Reason != "search_not_configured" {
		t.Fatalf("invalid request reached search service: calls=%d outcome=%+v", calls, out)
	}
}

func TestRunPreservesUpstreamFailuresAndRejectsMalformedSuccess(t *testing.T) {
	for _, tc := range []struct {
		status       int
		body, reason string
	}{
		{429, `{"detail":"rate limited"}`, "search_upstream_error"},
		{401, `search-key-sentinel denied`, "search_upstream_error"},
		{200, `{"output":"wrong protocol"}`, "response_read_error"},
		{200, `{"error":"denied","results":[]}`, "response_read_error"},
		{200, strings.Repeat("x", (2<<20)+1), "response_read_error"},
	} {
		t.Run(fmt.Sprint(tc.status, "-", tc.reason, "-", len(tc.body)), func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(tc.status); fmt.Fprint(w, tc.body) }))
			defer server.Close()
			out := Run(context.Background(), server.Client(), testConfig(server.URL), parseTestRequest(t, `{"model":"m","commands":{"search_query":[{"q":"test"}]}}`), "t")
			if out.Failure == nil || out.Failure.Status != tc.status || out.Failure.Reason != tc.reason || len(out.Attempts) != 1 || !out.Attempts[0].Sent || strings.Contains(out.Reply.Output, "search-key-sentinel") {
				t.Fatalf("incorrect external failure: %+v", out)
			}
			if tc.status == 429 && out.Failure.Detail != tc.body {
				t.Fatalf("upstream error replaced: %s", out.Failure.Detail)
			}
		})
	}
}
