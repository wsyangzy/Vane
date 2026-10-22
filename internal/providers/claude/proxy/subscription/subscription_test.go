package subscription

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"github.com/wsyangzy/Vane/internal/routing"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type memoryStore struct {
	mu       sync.Mutex
	bindings []Binding
	pauses   map[scope]Pause
}

func (s *memoryStore) ListBindings(context.Context) ([]Binding, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]Binding(nil), s.bindings...), nil
}
func (s *memoryStore) ListPauses(context.Context) ([]Pause, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []Pause
	for _, p := range s.pauses {
		out = append(out, p)
	}
	return out, nil
}
func (s *memoryStore) PutPause(_ context.Context, p Pause) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.pauses[scope{p.GroupID, p.Version, p.Model}] = p
	return nil
}
func (s *memoryStore) DeletePause(_ context.Context, p Pause) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	key := scope{p.GroupID, p.Version, p.Model}
	if s.pauses[key].Revision == p.Revision {
		delete(s.pauses, key)
	}
	return nil
}
func fixture() (*Controller, *memoryStore, Binding) {
	digest := sha256.Sum256([]byte("local-key-sentinel"))
	b := Binding{GroupID: 1, Channel: "official", Version: 1, Enabled: true, KeyHash: hex.EncodeToString(digest[:])}
	s := &memoryStore{bindings: []Binding{b}, pauses: map[scope]Pause{}}
	return NewController(s), s, b
}

func TestClaudeManagedAccountInjectsOAuthAndAllowsBackupWhenDisabled(t *testing.T) {
	c, s, b := fixture()
	b.AccountID = "managed-account"
	s.bindings = []Binding{b}
	c.SetAccountResolver(func(context.Context, string) (string, string, error) { return "managed-oauth", "", nil }, func(context.Context, string) error { return nil })
	r := httptest.NewRequest("POST", "/v1/messages", nil)
	r.RemoteAddr = "127.0.0.1:1"
	r.Header.Set("X-Relay-Key", "local-key-sentinel")
	ctx, handled, e := c.Authenticate(r)
	if e != nil || !handled {
		t.Fatal(e)
	}
	dst := httptest.NewRequest("POST", OfficialOrigin+"/v1/messages", nil)
	dst.Header.Set("Anthropic-Beta", "existing-beta")
	dst.Header.Set("Cookie", "client-session")
	if e = AuthorizeOutbound(ctx, r, dst, ClientPassthrough, b.GroupID, nil); e != nil {
		t.Fatal(e)
	}
	if dst.Header.Get("Authorization") != "Bearer managed-oauth" || dst.Header.Get("Anthropic-Beta") != "existing-beta,oauth-2025-04-20" || dst.Header.Get("Cookie") != "" {
		t.Fatal("managed auth boundary failed")
	}
	c.SetAccountResolver(func(context.Context, string) (string, string, error) { return "", "", errors.New("disabled") }, func(context.Context, string) error { return errors.New("disabled") })
	ctx, _, e = c.Authenticate(r)
	if e != nil || !c.Valid(ctx, b) {
		t.Fatal("disabled account removed caller backup authorization")
	}
	if ok, _ := c.Eligible(ctx, b, "sonnet"); ok {
		t.Fatal("disabled account sent official request")
	}
}

func TestAuthenticateRejectsBrowserAmbiguityAndRemoteCallers(t *testing.T) {
	c, s, _ := fixture()
	request := func() *http.Request {
		r := httptest.NewRequest("POST", "/v1/messages?beta=true", strings.NewReader(`{"model":"sonnet"}`))
		r.RemoteAddr = "127.0.0.1:1234"
		r.Header.Set("X-Relay-Key", "local-key-sentinel")
		r.Header.Set("Authorization", "Bearer oauth-sentinel")
		return r
	}
	for _, tc := range []struct {
		name   string
		mutate func(*http.Request)
	}{
		{"valid", func(*http.Request) {}},
		{"origin-null", func(r *http.Request) { r.Header.Set("Origin", "null") }},
		{"localhost-subdomain", func(r *http.Request) { r.Header.Set("Origin", "http://evil.localhost") }},
		{"duplicate-key", func(r *http.Request) { r.Header["x-relay-key"] = []string{"local-key-sentinel"} }},
		{"duplicate-auth", func(r *http.Request) { r.Header.Add("Authorization", "Bearer other") }},
		{"remote", func(r *http.Request) { r.RemoteAddr = "192.0.2.1:123" }},
		{"apikey", func(r *http.Request) { r.Header.Set("x-api-key", "conflict") }},
		{"path", func(r *http.Request) { r.URL.Path = "/v1/models" }},
		{"query", func(r *http.Request) { r.URL.RawQuery = "redirect=evil" }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := request()
			tc.mutate(r)
			ctx, handled, err := c.Authenticate(r)
			if !handled {
				t.Fatal("not handled")
			}
			if tc.name == "valid" {
				if err != nil || FromContext(ctx) == nil {
					t.Fatal("valid rejected")
				}
			} else if err == nil {
				t.Fatal("invalid accepted")
			}
		})
	}
	s.bindings = append(s.bindings, s.bindings[0])
	if _, _, err := c.Authenticate(request()); err == nil {
		t.Fatal("ambiguous identity accepted")
	}
	r := request()
	r.Header.Del("X-Relay-Key")
	if _, handled, _ := c.Authenticate(r); handled {
		t.Fatal("legacy authentication intercepted")
	}
}

func TestOutboundDestinationAndHeaderBoundary(t *testing.T) {
	c, _, b := fixture()
	src := httptest.NewRequest("POST", "/v1/messages?beta=true", nil)
	src.RemoteAddr = "[::1]:42"
	src.Header.Set("X-Relay-Key", "local-key-sentinel")
	src.Header.Set("Authorization", "Bearer oauth-sentinel")
	ctx, _, err := c.Authenticate(src)
	if err != nil {
		t.Fatal(err)
	}
	for _, url := range []string{OfficialOrigin + "/v1/messages?beta=true", "http://api.anthropic.com/v1/messages?beta=true", "https://api.anthropic.com.evil/v1/messages?beta=true", OfficialOrigin + "/v1/models?beta=true", OfficialOrigin + "/v1/messages?bad=true", OfficialOrigin + ":443/v1/messages?beta=true", OfficialOrigin + "/v1/%6dessages?beta=true"} {
		dst, _ := http.NewRequest("POST", url, nil)
		dst.Host = dst.URL.Host
		dst.Header.Set("X-Relay-Key", "local-key-sentinel")
		err := AuthorizeOutbound(ctx, src, dst, ClientPassthrough, b.GroupID, nil)
		if url == OfficialOrigin+"/v1/messages?beta=true" {
			if err != nil || dst.Header.Get("Authorization") != "Bearer oauth-sentinel" {
				t.Fatal("official auth missing")
			}
		} else if err == nil {
			t.Fatalf("unsafe destination accepted: %s", url)
		}
		if dst.Header.Get("X-Relay-Key") != "" {
			t.Fatal("local key leaked")
		}
	}
	for _, name := range []string{"aUtHoRiZaTiOn", "x-api-key", "HOST", "connection", "X-Relay-Key"} {
		if ValidateEndpoint(OfficialOrigin, map[string]string{name: "bad"}) == nil {
			t.Fatalf("custom %s accepted", name)
		}
	}
}

func TestQuotaWindowsAndTimeUnits(t *testing.T) {
	now := time.Unix(1800000000, 0)
	b := Binding{GroupID: 1, Version: 2}
	for _, tc := range []struct {
		raw          string
		retry, valid bool
	}{{"1800000060", false, true}, {"1800000060000", false, false}, {"1799999999", false, false}, {"NaN", false, false}, {"Inf", false, false}, {"60", true, true}, {"-1", true, false}, {now.Add(time.Hour).UTC().Format(http.TimeFormat), true, true}, {now.Add(40 * 24 * time.Hour).UTC().Format(http.TimeFormat), false, false}} {
		_, ok := ResetTime(tc.raw, now, tc.retry)
		if ok != tc.valid {
			t.Fatalf("time %q: %v", tc.raw, ok)
		}
	}
	h := http.Header{}
	h.Set("Anthropic-Ratelimit-Unified-5h-Utilization", "1")
	h.Set("Anthropic-Ratelimit-Unified-5h-Reset", fmt.Sprint(now.Add(time.Hour).Unix()))
	h.Set("Anthropic-Ratelimit-Unified-7d-Utilization", "0.2")
	h.Set("Anthropic-Ratelimit-Unified-7d-Reset", fmt.Sprint(now.Add(7*24*time.Hour).Unix()))
	pauses := QuotaPauses(429, h, nil, b, "claude-sonnet-4", now)
	if len(pauses) != 1 || !pauses[0].ResumeNotBefore.Equal(now.Add(time.Hour)) || pauses[0].Model != "*" {
		t.Fatalf("unexhausted weekly extended pause: %+v", pauses)
	}
	h = http.Header{}
	h.Set("Anthropic-Ratelimit-Unified-7d-opus-Status", "rejected")
	h.Set("Retry-After", "60")
	if len(QuotaPauses(429, h, nil, b, "claude-sonnet-4", now)) != 0 {
		t.Fatal("opus pause affected sonnet")
	}
	if p := QuotaPauses(429, h, nil, b, "claude-opus-4", now); len(p) != 1 || p[0].Model != "family:opus" {
		t.Fatal("model scope lost")
	}
	if len(QuotaPauses(401, h, nil, b, "claude-opus-4", now)) != 0 || len(QuotaPauses(429, nil, []byte(`{"error":{"type":"rate_limit_error"}}`), b, "sonnet", now)) != 0 {
		t.Fatal("ordinary errors became quota")
	}
	p := QuotaPauses(429, nil, []byte(`{"error":{"code":"quota_exceeded"}}`), b, "sonnet", now)
	if len(p) != 1 || p[0].ResetKnown || !p[0].ResumeNotBefore.Equal(now.Add(time.Minute)) {
		t.Fatal("unknown reset fabricated")
	}
}

func TestRecoverySingleAdmissionCancellationAndVersionIsolation(t *testing.T) {
	c, s, b := fixture()
	ctx := context.Background()
	now := time.Unix(1800000000, 0)
	c.now = func() time.Time { return now }
	p := Pause{GroupID: b.GroupID, Version: b.Version, Model: "*", ResumeNotBefore: now.Add(time.Minute), Revision: 1}
	s.PutPause(ctx, p)
	if _, err := c.Acquire(ctx, b, "sonnet"); err == nil {
		t.Fatal("early admission")
	}
	now = now.Add(time.Minute)
	var admitted atomic.Int32
	var lease *Lease
	var mu sync.Mutex
	var wg sync.WaitGroup
	for i := 0; i < 32; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if l, err := c.Acquire(ctx, b, "sonnet"); err == nil {
				admitted.Add(1)
				mu.Lock()
				lease = l
				mu.Unlock()
			}
		}()
	}
	wg.Wait()
	if admitted.Load() != 1 {
		t.Fatalf("admitted %d probes", admitted.Load())
	}
	lease.Finish(200, nil, []byte(`{"error":{}}`), false)
	if status := c.Status(ctx, b); len(status) != 1 || status[0].State != "paused" || status[0].ResetKnown {
		t.Fatal("incomplete/error declared recovery")
	}
	now = now.Add(30 * time.Second)
	lease, err := c.Acquire(ctx, b, "sonnet")
	if err != nil {
		t.Fatal(err)
	}
	lease.Finish(200, nil, nil, true)
	if len(c.Status(ctx, b)) != 0 {
		t.Fatal("complete success did not recover")
	}
	// A response acquired before a newly observed quota cannot clear that quota.
	older, _ := c.Acquire(ctx, b, "sonnet")
	newer, _ := c.Acquire(ctx, b, "sonnet")
	newer.Finish(429, http.Header{"Retry-After": []string{"60"}}, []byte(`{"error":{"code":"quota_exceeded"}}`), false)
	older.Finish(200, nil, nil, true)
	if len(c.Status(ctx, b)) != 1 {
		t.Fatal("older success erased new quota")
	}
	// Restart loads the persisted pause, without a timer or credential request.
	restarted := NewController(s)
	restarted.now = c.now
	if ok, _ := restarted.Eligible(ctx, b, "sonnet"); ok {
		t.Fatal("restart lost pause")
	}
	s.mu.Lock()
	s.bindings[0].Version++
	s.mu.Unlock()
	if c.Valid(ctx, b) {
		t.Fatal("rebound old request still valid")
	}
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	if _, err := c.Acquire(cancelled, b, "sonnet"); err == nil {
		t.Fatal("cancel acquired")
	}
}

func TestPoolAccountsResolveIndependentlyAndShareLegacyAliasQuota(t *testing.T) {
	c, s, b := fixture()
	ctx := context.Background()
	b.AccountID = "managed-one"
	s.bindings[0] = b
	second := b
	second.GroupID = 2
	second.AccountID = "managed-two"
	alias := b
	alias.GroupID = 3
	s.bindings = append(s.bindings, second, alias)
	var resolutions []string
	c.SetAccountResolver(func(_ context.Context, id string) (string, string, error) {
		resolutions = append(resolutions, id)
		return "token-" + id, "workspace", nil
	}, func(context.Context, string) error { return nil })
	digest := sha256.Sum256([]byte("pool-test-key"))
	runtime := routing.NewPoolRuntime(routing.PoolConfig{Pools: []routing.Pool{{ID: 1, Provider: "claude", Revision: 1, KeyHash: hex.EncodeToString(digest[:])}}, Accounts: []routing.AccountState{{ChannelID: 1, GroupID: 1, Enabled: true, Managed: true}, {ChannelID: 1, GroupID: 2, Enabled: true, Managed: true}, {ChannelID: 1, GroupID: 3, Enabled: true, Managed: true}}}, nil, nil)
	c.SetPoolRuntime(runtime)
	caller, err := runtime.Authenticate("claude", "pool-test-key")
	if err != nil {
		t.Fatal(err)
	}
	poolCtx := routing.WithPoolCaller(ctx, caller)
	for _, group := range []int64{1, 2} {
		selected, err := c.SelectPoolAccount(poolCtx, 1, group)
		if err != nil {
			t.Fatal(err)
		}
		if FromContext(selected).Binding.GroupID != group {
			t.Fatal("pool reused another member's binding")
		}
	}
	if len(resolutions) != 2 || resolutions[0] == resolutions[1] {
		t.Fatal("credentials were not resolved for each selected account")
	}
	if _, err = c.SelectPoolAccount(ctx, 1, 2); err == nil {
		t.Fatal("API caller borrowed managed account")
	}
	now := time.Now()
	c.now = func() time.Time { return now }
	s.PutPause(ctx, Pause{GroupID: alias.GroupID, Version: alias.Version, Model: "*", Revision: 1, ResumeNotBefore: now.Add(time.Minute)})
	if allowed, _ := c.Eligible(ctx, b, "test-model"); allowed {
		t.Fatal("legacy alias quota was ignored")
	}
	if allowed, _ := c.Eligible(ctx, second, "test-model"); !allowed {
		t.Fatal("quota leaked to a different account")
	}
	now = now.Add(time.Minute)
	lease, err := c.Acquire(ctx, b, "test-model")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = c.Acquire(ctx, alias, "test-model"); err == nil {
		t.Fatal("two groups probed the same account")
	}
	lease.Finish(200, nil, nil, true)
	if len(c.Status(ctx, alias)) != 0 {
		t.Fatal("alias pause not cleared after protocol success")
	}
}
