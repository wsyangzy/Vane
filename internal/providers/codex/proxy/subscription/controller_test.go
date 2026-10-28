package subscription

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"github.com/wsyangzy/Vane/internal/routing"
	"net/http"
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
	b := Binding{ChannelID: 1, GroupID: 1, Channel: "official", Version: 1, Enabled: true, KeyHash: hex.EncodeToString(digest[:])}
	s := &memoryStore{bindings: []Binding{b}, pauses: map[scope]Pause{}}
	return NewController(s), s, b
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
	runtime := routing.NewPoolRuntime(routing.PoolConfig{Pools: []routing.Pool{{ID: 1, Provider: "codex", Revision: 1, KeyHash: hex.EncodeToString(digest[:])}}, Accounts: []routing.AccountState{{ChannelID: 1, GroupID: 1, Enabled: true, Managed: true}, {ChannelID: 1, GroupID: 2, Enabled: true, Managed: true}, {ChannelID: 1, GroupID: 3, Enabled: true, Managed: true}}}, nil, nil)
	c.SetPoolRuntime(runtime)
	caller := routing.PoolCaller{Provider: "codex", PoolID: 1, Revision: 1}
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
	if _, err := c.SelectPoolAccount(ctx, 1, 2); err == nil {
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
