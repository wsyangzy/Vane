package subscription

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
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
