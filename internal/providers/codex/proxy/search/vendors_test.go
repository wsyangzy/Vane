package search

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestSearchVendorsSendOwnProtocolAndNormalizeResults(t *testing.T) {
	for _, vendor := range []string{"tavily", "brave", "exa", "searxng"} {
		t.Run(vendor, func(t *testing.T) {
			calls := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				raw, _ := io.ReadAll(r.Body)
				if strings.Contains(string(raw), "opaque-private") || r.Header.Get("X-Client-Identity") != "" {
					t.Error("native context or client identity leaked to search service")
				}
				switch vendor {
				case "brave":
					if r.Method != "GET" || r.Header.Get("X-Subscription-Token") != "search-key-sentinel" || !strings.Contains(r.URL.Query().Get("q"), "site:example.org") || r.URL.Query().Get("freshness") == "" {
						t.Errorf("Brave wire request: %s %v", r.URL, r.Header)
					}
					io.WriteString(w, `{"web":{"results":[{"title":"Brave","url":"https://example.org/page","description":"web snippet"},{"url":"https://blocked.example.org/"}]}}`)
				case "searxng":
					if r.Method != "GET" || r.URL.Query().Get("format") != "json" || r.URL.Query().Get("time_range") != "month" || r.Header.Get("Authorization") != "" {
						t.Errorf("SearXNG wire request: %s %v", r.URL, r.Header)
					}
					io.WriteString(w, `{"results":[{"title":"SearXNG","url":"https://example.org/page","content":"web snippet"}]}`)
				case "exa":
					var body map[string]any
					_ = json.Unmarshal(raw, &body)
					if r.Method != "POST" || r.Header.Get("X-API-Key") != "search-key-sentinel" || body["startPublishedDate"] == nil || body["numResults"] != float64(5) || body["contents"] == nil {
						t.Errorf("Exa wire request: %s %v", raw, r.Header)
					}
					io.WriteString(w, `{"results":[{"title":"Exa","url":"https://example.org/page","text":"web snippet"}]}`)
				case "tavily":
					var body map[string]any
					_ = json.Unmarshal(raw, &body)
					if r.Method != "POST" || r.Header.Get("Authorization") != "Bearer search-key-sentinel" || body["start_date"] == nil || body["max_results"] != float64(5) {
						t.Errorf("Tavily wire request: %s %v", raw, r.Header)
					}
					io.WriteString(w, `{"results":[{"title":"Tavily","url":"https://example.org/page","content":"web snippet"}]}`)
				}
			}))
			defer server.Close()
			cfg := testConfig(server.URL)
			cfg.Vendor = vendor
			if vendor == "searxng" {
				cfg.APIKey = ""
			}
			req := parseTestRequest(t, `{"model":"search","commands":{"search_query":[{"q":"news","recency_days":30}]},"settings":{"external_web_access":true,"filters":{"allowed_domains":["example.org"],"blocked_domains":["blocked.example.org"]}},"encrypted_context":"opaque-private"}`)
			out := Run(context.Background(), server.Client(), cfg, req, "vendors")
			if out.Failure != nil || calls != 1 || len(out.Reply.Results) != 1 || out.Reply.Results[0].URL != "https://example.org/page" || !strings.Contains(out.Reply.Output, "web snippet") {
				t.Fatalf("normalization failed: %+v", out)
			}
		})
	}
}

func TestSearchVendorImageProtocolsAndCapabilityFailures(t *testing.T) {
	for _, vendor := range []string{"brave", "searxng", "exa"} {
		t.Run(vendor, func(t *testing.T) {
			calls := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				if vendor == "brave" {
					if r.URL.Path != "/res/v1/images/search" {
						t.Errorf("wrong images resource: %s", r.URL.Path)
					}
					io.WriteString(w, `{"results":[{"title":"image","url":"https://example.org/page","properties":{"url":"https://images.example.org/photo.png"}}]}`)
				} else {
					if r.URL.Query().Get("categories") != "images" {
						t.Error("missing images category")
					}
					io.WriteString(w, `{"results":[{"title":"image","url":"https://example.org/page","img_src":"https://images.example.org/photo.png"}]}`)
				}
			}))
			defer server.Close()
			cfg := testConfig(server.URL + "/res/v1/web/search")
			cfg.Vendor = vendor
			req := parseTestRequest(t, `{"model":"search","commands":{"image_query":[{"q":"photo"}]},"settings":{"external_web_access":true}}`)
			out := Run(context.Background(), server.Client(), cfg, req, "images")
			if vendor == "exa" {
				if calls != 0 || out.Failure == nil || out.Failure.Reason != "search_command_unsupported" || out.Failure.Status != 0 {
					t.Fatalf("Exa capability failure faked upstream: %+v", out)
				}
			} else if calls != 1 || out.Failure != nil || len(out.Reply.Results) != 1 || out.Reply.Results[0].URL != "https://images.example.org/photo.png" {
				t.Fatalf("image conversion: %+v", out)
			}
		})
	}
}

func TestSearchVendorUnsupportedRecencyDoesNotSend(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { t.Error("unsupported query was sent") }))
	defer server.Close()
	for _, vendor := range []string{"brave", "searxng"} {
		cfg := testConfig(server.URL)
		cfg.Vendor = vendor
		req := parseTestRequest(t, `{"model":"search","commands":{"image_query":[{"q":"photo","recency_days":7}]},"settings":{"external_web_access":true}}`)
		out := Run(context.Background(), server.Client(), cfg, req, "unsupported")
		if out.Failure == nil || out.Failure.Reason != "search_command_unsupported" || out.Failure.Status != 0 || len(out.Attempts) != 1 || out.Attempts[0].Sent {
			t.Fatalf("capability mismatch: %+v", out)
		}
	}
}
