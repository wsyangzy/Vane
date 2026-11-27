package codexproxy

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/wsyangzy/Vane/internal/providers/codex/proxy/subscription"
	"github.com/wsyangzy/Vane/internal/routing"
	"github.com/wsyangzy/Vane/internal/tracking"
)

func TestImageProfilePreservesModelBodyAndActualProvider(t *testing.T) {
	for _, edit := range []bool{false, true} {
		name, path, contentType := "generation", "/v1/images/generations", "application/json"
		body := []byte(`{ "model" : "image-model", "prompt":"example", "size":"1024x1024", "n":1 }`)
		if edit {
			name, path = "edit", "/images/edits"
			var form bytes.Buffer
			writer := multipart.NewWriter(&form)
			_ = writer.WriteField("model", "image-model")
			_ = writer.WriteField("prompt", "example")
			part, _ := writer.CreateFormFile("image", "input.png")
			_, _ = part.Write([]byte("binary-image-data"))
			_ = writer.Close()
			body, contentType = form.Bytes(), writer.FormDataContentType()
		}
		t.Run(name, func(t *testing.T) {
			sends := 0
			response := `{"created":123,"data":[{"b64_json":"aW1hZ2U="}],"usage":{"input_tokens":3,"output_tokens":7,"total_tokens":10}}`
			h, runtime, _ := newProfileCodexFixture(t, func(w http.ResponseWriter, r *http.Request) {
				sends++
				got, _ := io.ReadAll(r.Body)
				if !bytes.Equal(got, body) || r.Header.Get("Content-Type") != contentType || r.Header.Get("X-Client-Identity") != "image-client" || !strings.HasPrefix(r.URL.Path, "/second/v1/images/") {
					t.Errorf("image request changed or selected subscription: path=%s body_equal=%t", r.URL.Path, bytes.Equal(got, body))
				}
				w.Header().Set("Content-Type", "application/json")
				_, _ = io.WriteString(w, response)
			})
			h.manager.endpointsByChannel[1][0].AuthMode = subscription.ClientPassthrough
			h.manager.channelsByID[2].Model = "text-only-model"
			h.manager.endpointsByChannel[2][0].SupportedModels = []string{"image-model"}
			profile := routing.DefaultProfile("codex")
			profile.ID, profile.IsDefault = 1, true
			profile.Members = []routing.RoutingMember{{ChannelID: 1, GroupID: 11, Enabled: true}, {ChannelID: 2, GroupID: 22, Enabled: true}}
			profile.Rules = []routing.RoutingRule{{ID: 1, Enabled: true, Kind: "non_stream", Mode: "strict", Targets: []routing.RouteTarget{{ChannelID: 1, UpstreamModel: "wrong-text-model"}}}}

			runtime.Replace(routing.RuntimeConfig{Provider: "codex", Profiles: []routing.RoutingProfile{profile}, Candidates: []routing.DispatchCandidate{
				{Provider: "codex", ChannelID: 1, GroupID: 11, EndpointID: "1", Protocol: "responses", Enabled: true},
				{Provider: "codex", ChannelID: 2, GroupID: 22, EndpointID: "2", Protocol: "responses", Enabled: true},
			}, PrepareCandidate: h.manager.PrepareDispatchCandidate, Supports: h.manager.SupportsDispatchCandidate})
			tracker, err := tracking.NewUsageTracker(&tracking.Config{Enabled: true, DatabasePath: filepath.Join(t.TempDir(), "usage.db"), BufferSize: 50, BatchSize: 5, FlushInterval: time.Hour, CleanupInterval: time.Hour, MaxRetry: 1})
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { tracker.Close() })
			h.usageTracker = tracker
			req := httptest.NewRequest("POST", path, bytes.NewReader(body))
			req.Header.Set("Content-Type", contentType)
			req.Header.Set("X-Client-Identity", "image-client")
			recorder := httptest.NewRecorder()
			h.ServeHTTP(recorder, req)
			if recorder.Code != 200 || recorder.Body.String() != response || sends != 1 {
				t.Fatalf("image generation failed: status=%d sends=%d body=%s", recorder.Code, sends, recorder.Body.String())
			}
			rows, _, err := tracker.QueryRequestDetailsWithHotPool(context.Background(), &tracking.QueryOptions{Limit: 5})
			if err != nil || len(rows) != 1 {
				t.Fatalf("missing tracking record: rows=%d err=%v", len(rows), err)
			}
			row := rows[0]
			var trace routing.DispatchTrace
			_ = json.Unmarshal([]byte(row.RouteSnapshot), &trace)
			if row.Provider != "codex" || row.ModelName != "image-model" || row.Status != "completed" || row.ModelRewriteSource != "" || trace.Attempts != 1 || !trace.Successful || trace.Decision.Kind != "default" {
				t.Fatalf("incorrect image metadata: provider=%s model=%s status=%s trace=%s", row.Provider, row.ModelName, row.Status, row.RouteSnapshot)
			}
			for _, event := range trace.Candidates {
				if event.Candidate.Protocol != "images" || event.Candidate.Model != "image-model" {
					t.Fatalf("text model or protocol leaked into image trace: %+v", event)
				}
			}
		})
	}
}

func TestImageUnsupportedEndpointDoesNotCoolTextRequests(t *testing.T) {
	sends := 0
	h, _, trace := newProfileCodexFixture(t, func(w http.ResponseWriter, r *http.Request) {
		sends++
		if strings.HasPrefix(r.URL.Path, "/first") {
			w.WriteHeader(404)
			_, _ = io.WriteString(w, `{"error":{"message":"Invalid URL (POST /v1/images/generations)","type":"invalid_request_error"}}`)
			return
		}
		_, _ = io.WriteString(w, `{"created":123,"data":[{"url":"https://images.invalid/output.png"}]}`)
	})
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("POST", "/v1/images/generations", strings.NewReader(`{"model":"image-model","prompt":"example"}`)))
	if rec.Code != 200 || sends != 2 || !trace.Successful {
		t.Fatalf("image failover failed: sends=%d status=%d trace=%+v", sends, rec.Code, trace)
	}
	if allowed, reason := h.manager.PoolRuntime().HealthAllowed(1, 11, "1", "text-model"); !allowed {
		t.Fatalf("image path failure poisoned text health: %s", reason)
	}
}

func TestImageProtocolRejectsNonImageSuccessBodies(t *testing.T) {
	for _, body := range []string{`{"status":"completed","output":[]}`, `{"data":[]}`, `{"data":[{"id":"model"}]}`, `{"error":{"message":"denied"},"data":[{"url":"x"}]}`} {
		if codexProfileProtocolSuccess("/images/generations", []byte(body)) {
			t.Fatalf("non-image response accepted: %s", body)
		}
	}
}

func TestImageLegacyFailurePreservesResponseAndTextHealth(t *testing.T) {
	raw := `{"error":{"message":"Invalid URL (POST /v1/images/generations)","type":"invalid_request_error"}}`
	body := `{"model":"image-model","prompt":"example"}`
	sends := 0
	h, _, _ := newProfileCodexFixture(t, func(w http.ResponseWriter, r *http.Request) {
		sends++
		got, _ := io.ReadAll(r.Body)
		if string(got) != body {
			t.Errorf("legacy image model was rewritten: %s", got)
		}
		w.WriteHeader(404)
		_, _ = io.WriteString(w, raw)
	})
	h.manager.poolRuntime.Store(nil)
	h.manager.activeChannel.Model = "text-only-model"
	ep := h.manager.endpointsByChannel[1][0]
	ep.FailoverEnabled = true
	h.manager.endpoints = []*CodexEndpoint{ep}
	h.failoverCfg.Enabled = false
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("POST", "/v1/images/generations", strings.NewReader(body)))
	if rec.Code != 404 || rec.Body.String() != raw || sends != 1 || h.manager.unhealthyMap[ep.ID] {
		t.Fatalf("legacy image failure changed response or poisoned text health: status=%d sends=%d unhealthy=%t body=%s", rec.Code, sends, h.manager.unhealthyMap[ep.ID], rec.Body.String())
	}
}
