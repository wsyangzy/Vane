package codexproxy

import (
	"compress/gzip"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestCodexCatalogForwardingUsesSelectedKeyAndKeepsOrdinaryModelLists(t *testing.T) {
	t.Setenv("CODEX_HOME", t.TempDir())
	const native = `{"models":[{"slug":"known","display_name":"Known","base_instructions":"Keep original instructions","context_window":272000}]}`
	if err := os.WriteFile(filepath.Join(os.Getenv("CODEX_HOME"), "models_cache.json"), []byte(native), 0600); err != nil {
		t.Fatal(err)
	}
	const public = `{"object":"list","data":[{"id":"known"},{"id":"custom"}]}`
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer upstream-key" || r.Header.Get("X-Relay-Key") != "" || r.Header.Get("X-Openai-Actor-Authorization") != "local-image-extension" {
			t.Error("catalog authentication or extension headers changed")
		}
		if r.URL.Path != "/v1/models" || r.URL.Query().Get("keep") != "value" {
			t.Errorf("wrong catalog URL: %s", r.URL)
		}
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("ETag", "upstream-tag")
		if r.URL.Query().Get("compressed") == "true" {
			w.Header().Set("Content-Encoding", "gzip")
			encoded := gzip.NewWriter(w)
			io.WriteString(encoded, public)
			encoded.Close()
			return
		}
		io.WriteString(w, public)
	}))
	defer server.Close()
	for _, query := range []string{"keep=value", "keep=value&client_version=0.166.0", "keep=value&client_version=0.166.0&compressed=true"} {
		t.Run(query, func(t *testing.T) {
			r := httptest.NewRequest("GET", "/v1/models?"+query, nil)
			r.Header.Set("Authorization", "Bearer local-token")
			r.Header.Set("X-Relay-Key", "local-token")
			r.Header.Set("X-Openai-Actor-Authorization", "local-image-extension")
			resp, err := NewForwarder(time.Second, newTestLogger()).Forward(r, nil, &CodexEndpoint{BaseURL: server.URL + "/v1", APIKey: "upstream-key"})
			if err != nil {
				t.Fatal(err)
			}
			raw, err := io.ReadAll(resp.Body)
			resp.Body.Close()
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(query, "client_version") {
				if string(raw) != public || resp.Header.Get("ETag") != "upstream-tag" {
					t.Fatalf("ordinary OpenAI model list changed: %s", raw)
				}
				return
			}
			var catalog struct {
				Models []map[string]any `json:"models"`
			}
			if json.Unmarshal(raw, &catalog) != nil || len(catalog.Models) != 2 || catalog.Models[0]["base_instructions"] != "Keep original instructions" || catalog.Models[1]["slug"] != "custom" {
				t.Fatalf("client catalog invalid: %s", raw)
			}
			if resp.ContentLength != int64(len(raw)) || resp.Header.Get("ETag") != "" || resp.Header.Get("Content-Encoding") != "" {
				t.Fatal("transformed response retained upstream entity headers")
			}
		})
	}
}

func TestCodexModelCatalogThroughUnifiedProfile(t *testing.T) {
	t.Setenv("CODEX_HOME", t.TempDir())
	h, _, _ := newProfileCodexFixture(t, func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/first") {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		if r.URL.Query().Get("client_version") != "0.166.0" || r.Header.Get("Authorization") != "Bearer key-2" {
			t.Error("catalog lost selected credentials or client version")
		}
		io.WriteString(w, `{"object":"list","data":[{"id":"provider-model"}]}`)
	})
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/v1/models?client_version=0.166.0", nil))
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"slug":"provider-model"`) {
		t.Fatalf("model discovery failed through unified routing: %d %s", rec.Code, rec.Body.String())
	}
}

func TestCodexCatalogDoesNotConvertErrorsOrAcceptInvalidSuccess(t *testing.T) {
	for _, status := range []int{http.StatusOK, http.StatusForbidden} {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(status)
			io.WriteString(w, `{"error":{"message":"upstream denied"}}`)
		}))
		r := httptest.NewRequest("GET", "/v1/models?client_version=0.166.0", nil)
		resp, err := NewForwarder(time.Second, newTestLogger()).Forward(r, nil, &CodexEndpoint{BaseURL: server.URL, APIKey: "selected"})
		if status == http.StatusOK {
			if err == nil {
				t.Error("invalid success was accepted as a catalog")
			}
		} else if err != nil {
			t.Error(err)
		} else {
			raw, _ := io.ReadAll(resp.Body)
			resp.Body.Close()
			if resp.StatusCode != status || string(raw) != `{"error":{"message":"upstream denied"}}` {
				t.Error("upstream denial was changed")
			}
		}
		server.Close()
	}
}
