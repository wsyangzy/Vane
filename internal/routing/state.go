package routing

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

var stateVersionSequence atomic.Uint64

// StateVersion identifies a backend binding, never a token. Endpoint reloads
// retain it when destination and credentials are unchanged. Admission checks
// use it to detect a configuration change between planning and sending.
func StateVersion(slot *uint64) uint64 {
	if version := atomic.LoadUint64(slot); version != 0 {
		return version
	}
	version := stateVersionSequence.Add(1)
	if atomic.CompareAndSwapUint64(slot, 0, version) {
		return version
	}
	return atomic.LoadUint64(slot)
}

// StateTracker remembers only hashes and execution identities. Opaque content
// stays in the request/response and is never persisted or logged here.
type StateTracker struct {
	mu       sync.Mutex
	states   map[string]stateOwner
	turns    map[string]stateOwner
	sequence uint64
}
type stateOwner struct {
	candidate Candidate
	expires   time.Time
	observed  uint64
}

func NewStateTracker() *StateTracker {
	return &StateTracker{states: make(map[string]stateOwner), turns: make(map[string]stateOwner)}
}

type callerContextKey struct{}

func WithCallerScope(ctx context.Context, caller string) context.Context {
	return context.WithValue(ctx, callerContextKey{}, caller)
}
func CallerScope(ctx context.Context) string {
	v, _ := ctx.Value(callerContextKey{}).(string)
	return v
}

func stateKey(caller, kind, value string) string {
	sum := sha256.Sum256([]byte(caller + "\x00" + kind + "\x00" + value))
	return hex.EncodeToString(sum[:])
}

func StateKey(ctx context.Context, kind, value string) string {
	return stateKey(CallerScope(ctx), kind, value)
}

// Preference is a best-effort route hint, never an admission check. Clients may
// replay history from before startup, after expiry, or from another backend;
// only the upstream can validate its opaque protocol state.
func (s *StateTracker) Preference(keys []string, turn string) (Candidate, bool) {
	if s == nil {
		return Candidate{}, false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now()
	latest, found := s.turns[turn]
	found = found && now.Before(latest.expires)
	for _, key := range keys {
		owner, ok := s.states[key]
		if ok && now.Before(owner.expires) && (!found || owner.observed > latest.observed) {
			latest, found = owner, true
		}
	}
	return latest.candidate, found
}

type statePreferenceKey struct{}

func WithStatePreference(ctx context.Context, tracker *StateTracker, keys []string, turn string) context.Context {
	if candidate, ok := tracker.Preference(keys, turn); ok {
		return context.WithValue(ctx, statePreferenceKey{}, candidate)
	}
	return ctx
}

func (s *StateTracker) Observe(keys []string, turn string, c Candidate) {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now()
	for key, owner := range s.states {
		if !now.Before(owner.expires) {
			delete(s.states, key)
		}
	}
	for key, owner := range s.turns {
		if !now.Before(owner.expires) {
			delete(s.turns, key)
		}
	}
	s.sequence++
	owner := stateOwner{candidate: c, expires: now.Add(time.Hour), observed: s.sequence}
	for _, key := range keys {
		// Existing ownership is immutable until expiry; another backend cannot
		// claim somebody else's sealed value merely by echoing it.
		if _, exists := s.states[key]; !exists && len(s.states) < 4096 {
			s.states[key] = owner
		}
	}
	if turn != "" && len(s.turns) < 2048 {
		s.turns[turn] = owner
	}
}

func (s *StateTracker) Preferred(turn string, c Candidate) bool {
	owner, ok := s.Preference(nil, turn)
	return ok && owner == c
}

// CaptureResponse calls observe only after a complete read. The adapter must
// still verify protocol success before registering any state or turn owner.
func CaptureResponse(body io.ReadCloser, observe func([]byte)) io.ReadCloser {
	return &responseCapture{ReadCloser: body, observe: observe}
}

type responseCapture struct {
	io.ReadCloser
	buf      bytes.Buffer
	overflow bool
	done     bool
	observe  func([]byte)
}

func (c *responseCapture) Read(p []byte) (int, error) {
	n, err := c.ReadCloser.Read(p)
	if !c.overflow {
		if c.buf.Len()+n <= 1024*1024 {
			_, _ = c.buf.Write(p[:n])
		} else {
			c.overflow = true
			c.buf.Reset()
		}
	}
	if err == io.EOF && !c.done {
		c.done = true
		if !c.overflow {
			c.observe(c.buf.Bytes())
		}
		c.buf.Reset()
	}
	return n, err
}

// JSONFrames decodes JSON or SSE data fields. Adapters choose protocol fields;
// arbitrary tool parameters must not be interpreted as protocol state.
func JSONFrames(body []byte, visit func(map[string]any)) {
	var obj map[string]any
	if json.Unmarshal(body, &obj) == nil {
		visit(obj)
		return
	}
	for _, line := range bytes.Split(body, []byte{'\n'}) {
		line = bytes.TrimSpace(line)
		if !bytes.HasPrefix(line, []byte("data:")) {
			continue
		}
		obj = nil
		if json.Unmarshal(bytes.TrimSpace(line[5:]), &obj) == nil {
			visit(obj)
		}
	}
}

func ExplicitTurn(ctx context.Context, headers map[string][]string) string {
	for _, name := range []string{"X-Claude-Turn-Id", "X-Claude-Prompt-Id", "X-Codex-Turn-Id", "X-Codex-Prompt-Id"} {
		if values := headers[name]; len(values) == 1 && strings.TrimSpace(values[0]) != "" {
			return StateKey(ctx, "turn:"+name, values[0])
		}
	}
	return ""
}
