package handlers

import (
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/iotest"

	"ai-relay-forwarder/config"
	"ai-relay-forwarder/internal/providers/claude/proxy/endpoint"
	"ai-relay-forwarder/internal/providers/claude/proxy/subscription"
	"ai-relay-forwarder/internal/routing"
)

type subscriptionMemoryStore struct {
	binding subscription.Binding
	pauses  []subscription.Pause
}

func (s *subscriptionMemoryStore) ListBindings(context.Context) ([]subscription.Binding, error) {
	return []subscription.Binding{s.binding}, nil
}
func (s *subscriptionMemoryStore) ListPauses(context.Context) ([]subscription.Pause, error) {
	return append([]subscription.Pause(nil), s.pauses...), nil
}
func (s *subscriptionMemoryStore) PutPause(_ context.Context, p subscription.Pause) error {
	s.pauses = []subscription.Pause{p}
	return nil
}
func (s *subscriptionMemoryStore) DeletePause(context.Context, subscription.Pause) error {
	s.pauses = nil
	return nil
}

type subscriptionRoundTrip func(*http.Request) (*http.Response, error)

func TestClaudeSubscriptionSSEQuotaPersistsWithoutChangingHTTPStatusOrBytes(t *testing.T) {
	for _, tc := range []struct {
		code   string
		paused bool
	}{{"usage_limit_exceeded", true}, {"rate_limit_error", false}} {
		t.Run(tc.code, func(t *testing.T) {
			s := &subscriptionMemoryStore{binding: subscription.Binding{GroupID: 1, Version: 1, Enabled: true}}
			c := subscription.NewController(s)
			lease, e := c.Acquire(context.Background(), s.binding, "sonnet")
			if e != nil {
				t.Fatal(e)
			}
			body := `data: {"type":"error","error":{"type":"` + tc.code + `"}}` + "\n\n"
			observed := &subscriptionResponse{ReadCloser: io.NopCloser(strings.NewReader(body)), lease: lease, status: 200, headers: make(http.Header)}
			raw, e := io.ReadAll(observed)
			observed.Close()
			if e != nil || string(raw) != body || observed.status != 200 {
				t.Fatal("quota observation changed wire response")
			}
			if got := c.Status(context.Background(), s.binding); (len(got) > 0) != tc.paused {
				t.Fatalf("wrong protocol quota classification: %+v", got)
			}
		})
	}
}

func (f subscriptionRoundTrip) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestSubscriptionSendBoundaryPreservesBodyAndSeparatesCredentials(t *testing.T) {
	hash := sha256.Sum256([]byte("relay-local-sentinel"))
	store := &subscriptionMemoryStore{binding: subscription.Binding{GroupID: 1, Channel: "official", Version: 1, Enabled: true, KeyHash: hex.EncodeToString(hash[:]), BackupTargets: []routing.Target{{ChannelID: 2, GroupID: 2, UpstreamModel: "backup-model"}}}}
	c := subscription.NewController(store)
	cfg := &config.Config{}
	manager := endpoint.NewManager(cfg)
	manager.SetRouteConfig(&endpoint.RouteConfig{ChannelNames: map[int64]string{1: "official", 2: "backup"}})
	f := NewForwarder(cfg, manager)
	f.SetSubscriptionController(c)
	for _, path := range []string{"/v1/messages", "/v1/messages/count_tokens"} {
		for _, mode := range []string{subscription.ClientPassthrough, subscription.GroupCredential} {
			t.Run(path+"/"+mode, func(t *testing.T) {
				src := httptest.NewRequest("POST", path+"?beta=true", nil)
				src.RemoteAddr = "127.0.0.1:1"
				src.Header.Set("X-Relay-Key", "relay-local-sentinel")
				src.Header.Set("Authorization", "Bearer oauth-sentinel")
				src.Header.Set("Anthropic-Beta", "oauth-2025-04-20")
				src.Header.Set("User-Agent", "native-client")
				ctx, _, err := c.Authenticate(src)
				if err != nil {
					t.Fatal(err)
				}
				ep := &endpoint.Endpoint{Config: config.EndpointConfig{URL: subscription.OfficialOrigin, Channel: "official", CredentialGroupID: 1, AuthMode: mode}}
				body := `{ "model" : "original", "stream":false,"tools":[{"input_schema":{"model":"nested"}}],"messages":[]}`
				auth := "Bearer oauth-sentinel"
				if mode == subscription.GroupCredential {
					ep.Config.URL = "https://backup.invalid"
					ep.Config.Channel = "backup"
					ep.Config.CredentialGroupID = 2
					ep.Config.Token = "backup-secret"
					body = strings.Replace(body, "original", "backup-model", 1)
					auth = "Bearer backup-secret"
				}
				dst, _ := http.NewRequestWithContext(ctx, "POST", ep.Config.URL+path+"?beta=true", strings.NewReader(body))
				f.CopyHeaders(src, dst, ep)
				sends := 0
				client := &http.Client{Transport: subscriptionRoundTrip(func(r *http.Request) (*http.Response, error) {
					sends++
					data, _ := io.ReadAll(r.Body)
					if string(data) != body {
						t.Fatal("request body changed")
					}
					if r.Header.Get("Authorization") != auth || r.Header.Get("X-Relay-Key") != "" || r.Header.Get("X-Api-Key") != "" {
						t.Fatal("credential boundary violated")
					}
					if r.Header.Get("User-Agent") != "native-client" || r.Header.Get("Anthropic-Beta") == "" {
						t.Fatal("client identity removed")
					}
					return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": []string{"application/json"}}, Body: io.NopCloser(strings.NewReader(`{"type":"message","stop_reason":"end_turn","input_tokens":1}`))}, nil
				})}
				resp, err := f.sendClaude(ctx, src, dst, ep, []byte(body), client)
				if err != nil {
					t.Fatal(err)
				}
				io.Copy(io.Discard, resp.Body)
				resp.Body.Close()
				if sends != 1 {
					t.Fatalf("sends %d", sends)
				}
			})
		}
	}
}

func TestSubscriptionResponseRequiresCompleteProtocolAndRejectsRedirect(t *testing.T) {
	hash := sha256.Sum256([]byte("key"))
	store := &subscriptionMemoryStore{binding: subscription.Binding{GroupID: 1, Version: 1, Enabled: true, KeyHash: hex.EncodeToString(hash[:])}}
	c := subscription.NewController(store)
	f := NewForwarder(&config.Config{}, endpoint.NewManager(&config.Config{}))
	f.SetSubscriptionController(c)
	src := httptest.NewRequest("POST", "/v1/messages", nil)
	src.RemoteAddr = "127.0.0.1:1"
	src.Header.Set("X-Relay-Key", "key")
	src.Header.Set("Authorization", "Bearer secret")
	ctx, _, _ := c.Authenticate(src)
	ep := &endpoint.Endpoint{Config: config.EndpointConfig{URL: subscription.OfficialOrigin, CredentialGroupID: 1, AuthMode: subscription.ClientPassthrough}}
	start := "event: message_start\ndata: {\"type\":\"message_start\",\"message\":{\"type\":\"message\"}}\n\n"
	stop := "event: message_stop\ndata: {\"type\":\"message_stop\"}\n\n"
	delta := "data: {\"type\":\"content_block_delta\",\"delta\":{\"type\":\"text_delta\",\"text\":\"" + strings.Repeat("x", 1024) + "\"}}\n\n"
	for _, tc := range []struct {
		name, body string
		status     int
		recover    bool
	}{
		{"sse-complete", start + stop, 200, true},
		{"sse-long-complete", start + strings.Repeat(delta, 2048) + stop, 200, true},
		{"sse-crlf-multiline", strings.ReplaceAll("data: {\"type\":\n"+"data: \"message_start\"}\n\n"+stop, "\n", "\r\n"), 200, true},
		{"sse-error", start + "data: {\"type\":\"error\",\"error\":{}}\n\n", 200, false},
		{"sse-late-error", start + stop + "data: {\"type\":\"error\",\"error\":{}}\n\n", 200, false},
		{"sse-malformed", start + "data: {broken}\n\n" + stop, 200, false},
		{"sse-no-start", stop, 200, false},
		{"sse-incomplete", start, 200, false},
		{"sse-unfinished-event", start + strings.TrimSuffix(stop, "\n"), 200, false},
		{"sse-oversized-event", start + "data: {\"type\":\"content_block_delta\",\"text\":\"" + strings.Repeat("x", subscriptionObservationLimit) + "\"}\n\n" + stop, 200, false},
		{"json-complete", `{"type":"message","stop_reason":"end_turn"}`, 200, true},
		{"json-incomplete", `{"type":"message","stop_reason":null}`, 200, false},
		{"json-error", `{"type":"message","stop_reason":"end_turn","error":{"type":"server_error"}}`, 200, false},
		{"json-oversized", `{"type":"message","stop_reason":"end_turn","text":"` + strings.Repeat("x", subscriptionObservationLimit) + `"}`, 200, false},
		{"401", `{"error":{"type":"authentication_error","message":"Bearer secret"}}`, 401, false},
		{"redirect", "", 302, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			store.pauses = []subscription.Pause{{GroupID: 1, Version: 1, Model: "*", Revision: 1}}
			dst, _ := http.NewRequestWithContext(ctx, "POST", subscription.OfficialOrigin+"/v1/messages", strings.NewReader(`{"model":"x"}`))
			f.CopyHeaders(src, dst, ep)
			client := &http.Client{Transport: subscriptionRoundTrip(func(*http.Request) (*http.Response, error) {
				var reader io.Reader = strings.NewReader(tc.body)
				if tc.name == "sse-crlf-multiline" {
					reader = iotest.OneByteReader(reader)
				}
				return &http.Response{StatusCode: tc.status, Header: http.Header{"Location": []string{"https://evil.invalid"}}, Body: io.NopCloser(reader)}, nil
			})}
			resp, err := f.sendClaude(ctx, src, dst, ep, []byte(`{"model":"x"}`), client)
			if tc.status == 302 {
				if err == nil {
					t.Fatal("redirect accepted")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			data, _ := io.ReadAll(resp.Body)
			if strings.Contains(string(data), "Bearer secret") {
				t.Fatal("credential echo escaped error boundary")
			}
			if tc.status == 200 && string(data) != tc.body {
				t.Fatal("response observer changed wire bytes")
			}
			resp.Body.Close()
			if (len(store.pauses) == 0) != tc.recover {
				t.Fatalf("recovery incorrectly inferred: %+v", store.pauses)
			}
		})
	}
}

func TestSubscriptionCompressedResponseRecoveryUsesDecodedProtocol(t *testing.T) {
	for _, path := range []string{"/v1/messages", "/v1/messages/count_tokens"} {
		for _, truncated := range []bool{false, true} {
			name := path + "/complete"
			if truncated {
				name = path + "/truncated"
			}
			t.Run(name, func(t *testing.T) {
				hash := sha256.Sum256([]byte("key"))
				store := &subscriptionMemoryStore{binding: subscription.Binding{GroupID: 1, Version: 1, Enabled: true, KeyHash: hex.EncodeToString(hash[:])}, pauses: []subscription.Pause{{GroupID: 1, Version: 1, Model: "*", Revision: 1}}}
				c := subscription.NewController(store)
				f := NewForwarder(&config.Config{}, endpoint.NewManager(&config.Config{}))
				f.SetSubscriptionController(c)
				src := httptest.NewRequest("POST", path, nil)
				src.RemoteAddr = "127.0.0.1:1"
				src.Header.Set("X-Relay-Key", "key")
				src.Header.Set("Authorization", "Bearer secret")
				ctx, _, err := c.Authenticate(src)
				if err != nil {
					t.Fatal(err)
				}
				body := "data: {\"type\":\"message_start\"}\n\n" + strings.Repeat("data: {\"type\":\"content_block_delta\",\"text\":\""+strings.Repeat("x", 1024)+"\"}\n\n", 2048) + "data: {\"type\":\"message_stop\"}\n\n"
				if path == "/v1/messages/count_tokens" {
					body = `{"input_tokens":123}`
				}
				var compressed bytes.Buffer
				writer := gzip.NewWriter(&compressed)
				if _, err := io.WriteString(writer, body); err != nil {
					t.Fatal(err)
				}
				if err := writer.Close(); err != nil {
					t.Fatal(err)
				}
				wire := compressed.Bytes()
				if truncated {
					wire = wire[:len(wire)-8]
				}
				ep := &endpoint.Endpoint{Config: config.EndpointConfig{URL: subscription.OfficialOrigin, CredentialGroupID: 1, AuthMode: subscription.ClientPassthrough}}
				dst, _ := http.NewRequestWithContext(ctx, "POST", subscription.OfficialOrigin+path, strings.NewReader(`{"model":"x"}`))
				f.CopyHeaders(src, dst, ep)
				client := &http.Client{Transport: subscriptionRoundTrip(func(*http.Request) (*http.Response, error) {
					return &http.Response{StatusCode: 200, Header: http.Header{"Content-Encoding": []string{"gzip"}}, Body: io.NopCloser(bytes.NewReader(wire))}, nil
				})}
				resp, err := f.sendClaude(ctx, src, dst, ep, []byte(`{"model":"x"}`), client)
				if err != nil {
					t.Fatal(err)
				}
				data, readErr := io.ReadAll(resp.Body)
				resp.Body.Close()
				if string(data) != body || resp.Header.Get("Content-Encoding") != "" {
					t.Fatal("decoded response changed or retained stale encoding")
				}
				if (readErr != nil) != truncated || (len(store.pauses) != 0) != truncated {
					t.Fatalf("compressed EOF recovery: truncated=%v readErr=%v pauses=%+v", truncated, readErr, store.pauses)
				}
			})
		}
	}
}
