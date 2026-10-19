package subscriptionprobe

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestProbeRealHTTPPreservesBodyIdentityAndSeparatesCallerCredentials(t *testing.T) {
	registry := NewRegistry()
	callerA, keyA, _ := registry.Create("a")
	callerB, keyB, _ := registry.Create("b")
	const body = `{ "model":"m", "stream":false,"messages":[{"role":"user","content":"hi"}],"tools":[{"name":"t","input_schema":{"type":"object"}}] }`
	var mu sync.Mutex
	seen := map[string]string{}
	upstream := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		data, _ := io.ReadAll(r.Body)
		if r.Header.Get("X-Relay-Key") != "" || r.Header.Get("X-Api-Key") != "" {
			t.Error("local key leaked")
		}
		mu.Lock()
		seen[r.Header.Get("Authorization")] = string(data)
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"type":"message","stop_reason":"end_turn","content":[]}`)
	}))
	defer upstream.Close()
	mockURL, _ := url.Parse(upstream.URL)
	transport := roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.URL.Scheme+"://"+r.URL.Host != OfficialOrigin || r.Host != "api.anthropic.com" {
			t.Error("official origin was not checked")
		}
		clone := r.Clone(r.Context())
		clone.URL.Scheme = mockURL.Scheme
		clone.URL.Host = mockURL.Host
		clone.Host = mockURL.Host
		return upstream.Client().Transport.RoundTrip(clone)
	})
	probe := New(registry, transport, nil, "")
	var wg sync.WaitGroup
	for _, entry := range []struct {
		caller     Caller
		key, oauth string
	}{{callerA, keyA, "oauth-a-sentinel"}, {callerB, keyB, "oauth-b-sentinel"}} {
		wg.Add(1)
		go func() {
			defer wg.Done()
			req := httptest.NewRequest("POST", "/v1/messages", strings.NewReader(body))
			req.Header.Set("Authorization", "Bearer "+entry.oauth)
			req.Header.Set("X-Relay-Key", entry.key)
			req.Header.Set("User-Agent", "client-test")
			rec := httptest.NewRecorder()
			probe.ServeHTTP(rec, req)
			if rec.Code != 200 {
				t.Errorf("status=%d", rec.Code)
			}
		}()
	}
	wg.Wait()
	mu.Lock()
	defer mu.Unlock()
	if len(seen) != 2 || seen["Bearer oauth-a-sentinel"] != body || seen["Bearer oauth-b-sentinel"] != body {
		t.Fatal("body or caller credentials changed")
	}
	data, _ := json.Marshal(registry.List())
	if strings.Contains(string(data), keyA) || strings.Contains(string(data), "oauth-") {
		t.Fatal("caller listing leaked credentials")
	}
}

func TestProbeRejectsBadLocalAuthenticationAndDoesNotFollowRedirects(t *testing.T) {
	registry := NewRegistry()
	caller, key, _ := registry.Create("caller")
	var sends atomic.Int32
	probe := New(registry, roundTripFunc(func(*http.Request) (*http.Response, error) {
		sends.Add(1)
		return &http.Response{StatusCode: 302, Header: http.Header{"Location": []string{"https://third.invalid/steal"}}, Body: io.NopCloser(strings.NewReader("redirect"))}, nil
	}), nil, "")
	for _, headers := range []http.Header{
		{"Authorization": []string{"Bearer fake-oauth"}, "User-Agent": []string{"claude-cli"}},
		{"X-Relay-Key": []string{"wrong"}, "Authorization": []string{"Bearer oauth"}, "Origin": []string{"null"}},
		{"X-Relay-Key": []string{key, key}, "Authorization": []string{"Bearer oauth"}},
	} {
		req := httptest.NewRequest("POST", "/v1/messages", strings.NewReader(`{}`))
		req.Header = headers
		rec := httptest.NewRecorder()
		probe.ServeHTTP(rec, req)
		if rec.Code != 401 {
			t.Errorf("status=%d", rec.Code)
		}
	}
	if sends.Load() != 0 {
		t.Fatal("unauthorized request sent upstream")
	}
	req := httptest.NewRequest("POST", "/v1/messages", strings.NewReader(`{}`))
	req.Header.Set("X-Relay-Key", key)
	req.Header.Set("Authorization", "Bearer oauth")
	rec := httptest.NewRecorder()
	probe.ServeHTTP(rec, req)
	if rec.Code != 502 || rec.Header().Get("Location") != "" || sends.Load() != 1 {
		t.Fatal("redirect followed or exposed")
	}
	_ = registry.SetEnabled(caller.ID, false)
	rec = httptest.NewRecorder()
	probe.ServeHTTP(rec, req)
	if rec.Code != 401 {
		t.Fatal("disabled caller authorized")
	}
}

func TestBuildRequestOfficialAllowlistAndEveryHopRebuild(t *testing.T) {
	src := httptest.NewRequest("POST", "/v1/messages", strings.NewReader(`{}`))
	src.Header.Set("Authorization", "Bearer oauth-sentinel")
	src.Header.Set("X-Relay-Key", "relay-sentinel")
	src.Header.Set("Anthropic-Beta", "oauth-beta")
	src.Header.Set("User-Agent", "client")
	for _, origin := range []string{"http://api.anthropic.com", "https://api.anthropic.com.evil", "https://api.anthropic.com:443", "https://u@api.anthropic.com", "https://api.anthropic.com/v1", "https://api.anthropic.com?x=1"} {
		if _, err := BuildRequest(context.Background(), src, []byte(`{}`), true, origin, ""); err == nil {
			t.Errorf("accepted %s", origin)
		}
	}
	official, err := BuildRequest(context.Background(), src, []byte(`{}`), true, OfficialOrigin, "")
	if err != nil {
		t.Fatal(err)
	}
	third, err := BuildRequest(context.Background(), src, []byte(`{}`), false, "https://third.invalid", "group-key")
	if err != nil {
		t.Fatal(err)
	}
	if official.Header.Get("Authorization") != "Bearer oauth-sentinel" || third.Header.Get("Authorization") != "Bearer group-key" || third.Header.Get("X-Relay-Key") != "" || src.Header.Get("Authorization") != "Bearer oauth-sentinel" {
		t.Fatal("hop authentication contaminated")
	}
	src.Header["x-api-key"] = []string{"conflict"}
	if _, err := BuildRequest(context.Background(), src, []byte(`{}`), true, OfficialOrigin, ""); err == nil {
		t.Fatal("lowercase conflicting credential accepted")
	}
	delete(src.Header, "x-api-key")
	for _, query := range []string{"beta=false", "beta=true&beta=true", "beta=true&target=other"} {
		src.URL.RawQuery = query
		if _, err := BuildRequest(context.Background(), src, nil, true, OfficialOrigin, ""); err == nil {
			t.Errorf("unexpected query accepted: %s", query)
		}
	}
	src.URL.RawQuery = "beta=true"
	observed, err := BuildRequest(context.Background(), src, nil, true, OfficialOrigin, "")
	if err != nil || observed.URL.RawQuery != "beta=true" {
		t.Fatal("observed client beta query lost")
	}
	src.URL.RawQuery = ""
	src.URL.Path = "/v1/messages/../other"
	if _, err := BuildRequest(context.Background(), src, nil, true, OfficialOrigin, ""); err == nil {
		t.Fatal("path escape accepted")
	}
}

func TestProbe401PrivacyAndModelContract(t *testing.T) {
	registry := NewRegistry()
	_, key, _ := registry.Create("caller")
	const original = `{ "model":"old", "messages":[{"role":"user","content":"private-sentinel"}],"tools":[{"input":{"model":"untouched"}}],"stream":false }`
	want := strings.Replace(strings.Replace(original, "private-sentinel", "[redacted]", 1), `"model":"old"`, `"model":"new"`, 1)
	probe := New(registry, roundTripFunc(func(r *http.Request) (*http.Response, error) {
		body, _ := io.ReadAll(r.Body)
		if string(body) != want {
			t.Errorf("unexpected transform: %s", body)
		}
		return &http.Response{StatusCode: 401, Header: http.Header{"Content-Type": []string{"application/json"}}, Body: io.NopCloser(strings.NewReader(`{"error":{"type":"authentication_error"}}`))}, nil
	}), func(_ context.Context, _ Caller, body []byte) ([]byte, error) {
		return []byte(strings.Replace(string(body), "private-sentinel", "[redacted]", 1)), nil
	}, "new")
	req := httptest.NewRequest("POST", "/v1/messages", strings.NewReader(original))
	req.Header.Set("Authorization", "Bearer expired-sentinel")
	req.Header.Set("X-Relay-Key", key)
	rec := httptest.NewRecorder()
	probe.ServeHTTP(rec, req)
	if rec.Code != 401 || rec.Body.String() != `{"error":{"type":"authentication_error"}}` {
		t.Fatal("401 masked")
	}
}

func TestRegistryRotationAndRebindingAreIndependent(t *testing.T) {
	r := NewRegistry()
	a, keyA, _ := r.Create("a")
	b, keyB, _ := r.Create("b")
	newKey, err := r.Rotate(a.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := r.authenticate(keyA); ok {
		t.Fatal("old key remained valid")
	}
	got, ok := r.authenticate(newKey)
	if !ok || got.BindingVersion != 1 {
		t.Fatal("key rotation changed binding")
	}
	_ = r.Rebind(a.ID)
	got, _ = r.authenticate(newKey)
	if got.BindingVersion != 2 {
		t.Fatal("rebind failed")
	}
	other, ok := r.authenticate(keyB)
	if !ok || other.ID != b.ID || other.BindingVersion != 1 {
		t.Fatal("caller B contaminated")
	}
	for _, address := range []string{"0.0.0.0:18081", "localhost:18081", "192.168.0.1:18081"} {
		if listener, err := ListenLocal(address); err == nil {
			_ = listener.Close()
			t.Errorf("accepted nonexplicit loopback %s", address)
		}
	}
}

func TestProbeCountTokensStreamAndToolResultBytes(t *testing.T) {
	registry := NewRegistry()
	_, key, _ := registry.Create("caller")
	for _, tc := range []struct{ path, body, response, contentType string }{
		{"/v1/messages/count_tokens", `{"model":"m","messages":[{"role":"user","content":"hi"}]}`, `{"input_tokens":7}`, "application/json"},
		{"/v1/messages", `{"model":"m","stream":true,"messages":[{"role":"user","content":"hi"}],"tools":[{"name":"t","input_schema":{"type":"object"}}]}`, "data: {\"type\":\"message_stop\"}\n\n", "text/event-stream"},
		{"/v1/messages", `{"model":"m","stream":false,"messages":[{"role":"assistant","content":[{"type":"tool_use","id":"t1","name":"t","input":{"signature":"tool-data","model":"keep"}}]},{"role":"user","content":[{"type":"tool_result","tool_use_id":"t1","content":"result"}]}]}`, `{"type":"message","content":[],"stop_reason":"end_turn"}`, "application/json"},
	} {
		t.Run(tc.path+tc.contentType, func(t *testing.T) {
			var sends int
			probe := New(registry, roundTripFunc(func(r *http.Request) (*http.Response, error) {
				sends++
				body, _ := io.ReadAll(r.Body)
				if r.URL.Path != tc.path || string(body) != tc.body || r.Header.Get("Anthropic-Beta") != "test-beta" {
					t.Error("client protocol changed")
				}
				return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": []string{tc.contentType}}, Body: io.NopCloser(strings.NewReader(tc.response))}, nil
			}), nil, "")
			req := httptest.NewRequest("POST", tc.path, strings.NewReader(tc.body))
			req.Header.Set("X-Relay-Key", key)
			req.Header.Set("Authorization", "Bearer sentinel")
			req.Header.Set("Anthropic-Beta", "test-beta")
			rec := httptest.NewRecorder()
			probe.ServeHTTP(rec, req)
			if rec.Code != 200 || rec.Body.String() != tc.response || sends != 1 {
				t.Fatalf("response changed: %d %q sends=%d", rec.Code, rec.Body.String(), sends)
			}
			if tc.contentType == "text/event-stream" && !rec.Flushed {
				t.Fatal("stream was buffered")
			}
		})
	}
}

func TestProbeCancellationStopsTransportWithoutReplay(t *testing.T) {
	registry := NewRegistry()
	_, key, _ := registry.Create("caller")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var sends atomic.Int32
	finished := make(chan struct{})
	probe := New(registry, roundTripFunc(func(r *http.Request) (*http.Response, error) {
		sends.Add(1)
		cancel()
		<-r.Context().Done()
		close(finished)
		return nil, r.Context().Err()
	}), nil, "")
	req := httptest.NewRequest("POST", "/v1/messages", strings.NewReader(`{"model":"m"}`)).WithContext(ctx)
	req.Header.Set("X-Relay-Key", key)
	req.Header.Set("Authorization", "Bearer sentinel")
	probe.ServeHTTP(httptest.NewRecorder(), req)
	select {
	case <-finished:
	case <-time.After(time.Second):
		t.Fatal("upstream context not canceled")
	}
	if sends.Load() != 1 {
		t.Fatal("cancellation replayed upstream")
	}
}
