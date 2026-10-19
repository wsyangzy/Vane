// Package subscriptionprobe is an isolated T0 feasibility harness. It is not
// wired into the desktop listener, Wails APIs, or the application's database.
package subscriptionprobe

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"ai-relay-forwarder/internal/bodyrewrite"
	"ai-relay-forwarder/internal/routing"
)

const OfficialOrigin = "https://api.anthropic.com"
const maxBodyBytes = 32 << 20

type Caller struct {
	ID             string `json:"id"`
	Name           string `json:"name"`
	Enabled        bool   `json:"enabled"`
	BindingVersion uint64 `json:"binding_version"`
}

type entry struct {
	caller Caller
	digest [32]byte
}

// Registry contains no subscription tokens. Caller keys are hashed at entry;
// the full key is returned only by Create/Rotate and never by List.
type Registry struct {
	mu      sync.RWMutex
	callers map[string]entry
}

func NewRegistry() *Registry { return &Registry{callers: make(map[string]entry)} }

func randomID() (string, error) {
	var b [32]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(b[:]), nil
}
func (r *Registry) Create(name string) (Caller, string, error) {
	id, err := randomID()
	if err != nil {
		return Caller{}, "", err
	}
	key, err := randomID()
	if err != nil {
		return Caller{}, "", err
	}
	caller := Caller{ID: id, Name: name, Enabled: true, BindingVersion: 1}
	r.mu.Lock()
	r.callers[id] = entry{caller: caller, digest: sha256.Sum256([]byte(key))}
	r.mu.Unlock()
	return caller, key, nil
}
func (r *Registry) List() []Caller {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]Caller, 0, len(r.callers))
	for _, e := range r.callers {
		out = append(out, e.caller)
	}
	return out
}
func (r *Registry) SetEnabled(id string, enabled bool) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	e, ok := r.callers[id]
	if !ok {
		return errors.New("caller not found")
	}
	e.caller.Enabled = enabled
	r.callers[id] = e
	return nil
}
func (r *Registry) Rebind(id string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	e, ok := r.callers[id]
	if !ok {
		return errors.New("caller not found")
	}
	e.caller.BindingVersion++
	r.callers[id] = e
	return nil
}
func (r *Registry) Rotate(id string) (string, error) {
	key, err := randomID()
	if err != nil {
		return "", err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	e, ok := r.callers[id]
	if !ok {
		return "", errors.New("caller not found")
	}
	e.digest = sha256.Sum256([]byte(key))
	r.callers[id] = e
	return key, nil
}
func (r *Registry) authenticate(key string) (Caller, bool) {
	if key == "" {
		return Caller{}, false
	}
	digest := sha256.Sum256([]byte(key))
	r.mu.RLock()
	defer r.mu.RUnlock()
	for _, e := range r.callers {
		if e.caller.Enabled && subtle.ConstantTimeCompare(e.digest[:], digest[:]) == 1 {
			return e.caller, true
		}
	}
	return Caller{}, false
}

// Transform is the explicit boundary for the application's target privacy
// policy. Model replacement runs afterwards and preserves all other bytes.
type Transform func(context.Context, Caller, []byte) ([]byte, error)
type Probe struct {
	registry      *Registry
	transport     http.RoundTripper
	transform     Transform
	upstreamModel string
	observer      func(Evidence)
	maxAttempts   uint64
	attempts      atomic.Uint64
}

func New(registry *Registry, transport http.RoundTripper, transform Transform, upstreamModel string, options ...Option) *Probe {
	if transport == nil {
		transport = http.DefaultTransport
	}
	p := &Probe{registry: registry, transport: transport, transform: transform, upstreamModel: upstreamModel}
	for _, option := range options {
		option(p)
	}
	return p
}

func allowedPath(path string) bool {
	return path == "/v1/messages" || path == "/v1/messages/count_tokens"
}

func singleHeader(h http.Header, key string) (string, bool) {
	var values []string
	for name, items := range h {
		if strings.EqualFold(name, key) {
			values = append(values, items...)
		}
	}
	if len(values) != 1 || strings.TrimSpace(values[0]) == "" {
		return "", false
	}
	return strings.TrimSpace(values[0]), true
}

func stripHopHeaders(h http.Header) {
	for _, value := range h.Values("Connection") {
		for _, name := range strings.Split(value, ",") {
			h.Del(strings.TrimSpace(name))
		}
	}
	for _, name := range []string{"Connection", "Keep-Alive", "Proxy-Authenticate", "Proxy-Authorization", "Te", "Trailer", "Transfer-Encoding", "Upgrade"} {
		h.Del(name)
	}
}

// BuildRequest enforces the destination at the actual send boundary. Tests
// inject a RoundTripper rather than weakening the official origin allowlist.
func BuildRequest(ctx context.Context, src *http.Request, body []byte, official bool, target string, groupCredential string) (*http.Request, error) {
	u, err := url.Parse(target)
	if err != nil || u.Scheme != "https" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || u.Path != "" || u.RawPath != "" || u.Opaque != "" {
		return nil, errors.New("invalid upstream origin")
	}
	if official && u.String() != OfficialOrigin {
		return nil, errors.New("official credential destination rejected")
	}
	// Claude Code 2.1.293 adds the SDK's beta=true query to Messages.
	// Preserve that observed form without opening arbitrary query forwarding.
	if !allowedPath(src.URL.Path) || src.URL.RawPath != "" || (src.URL.RawQuery != "" && src.URL.RawQuery != "beta=true") {
		return nil, errors.New("unsupported probe path")
	}
	requestURL := target + src.URL.Path
	if src.URL.RawQuery != "" {
		requestURL += "?" + src.URL.RawQuery
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, requestURL, bytes.NewReader(body))
	if err != nil {
		return nil, errors.New("cannot build upstream request")
	}
	req.Header = src.Header.Clone()
	stripHopHeaders(req.Header)
	for name := range req.Header {
		if strings.EqualFold(name, "X-Relay-Key") || strings.EqualFold(name, "Authorization") || strings.EqualFold(name, "X-Api-Key") || strings.EqualFold(name, "Host") {
			delete(req.Header, name)
		}
	}
	if official {
		auth, ok := singleHeader(src.Header, "Authorization")
		conflict := false
		for name := range src.Header {
			if strings.EqualFold(name, "X-Api-Key") {
				conflict = true
			}
		}
		if !ok || !strings.HasPrefix(auth, "Bearer ") || strings.TrimSpace(strings.TrimPrefix(auth, "Bearer ")) == "" || conflict {
			return nil, errors.New("missing or conflicting subscription authentication")
		}
		req.Header.Set("Authorization", auth)
	} else if groupCredential != "" {
		req.Header.Set("Authorization", "Bearer "+groupCredential)
	}
	req.Host = u.Host
	return req, nil
}

func (p *Probe) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	evidence := Evidence{Outcome: "unsupported_path"}
	if p.observer != nil {
		defer func() {
			evidence.ObservedAt = time.Now().UTC()
			p.observer(evidence)
		}()
	}
	if r.Method != http.MethodPost || !allowedPath(r.URL.Path) {
		http.Error(w, "unsupported probe path", http.StatusNotFound)
		return
	}
	evidence.Path = r.URL.Path
	evidence.Outcome = "local_authentication_rejected"
	key, ok := singleHeader(r.Header, "X-Relay-Key")
	if !ok {
		http.Error(w, "relay authentication required", http.StatusUnauthorized)
		return
	}
	caller, ok := p.registry.authenticate(key)
	if !ok {
		http.Error(w, "relay authentication rejected", http.StatusUnauthorized)
		return
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, maxBodyBytes+1))
	if err != nil || len(body) > maxBodyBytes {
		evidence.Outcome = "request_body_rejected"
		http.Error(w, "request body unavailable or too large", http.StatusRequestEntityTooLarge)
		return
	}
	if p.observer != nil {
		requestEvidence(body, &evidence)
	}
	if p.transform != nil {
		body, err = p.transform(r.Context(), caller, body)
		if err != nil {
			evidence.Outcome = "privacy_rejected"
			http.Error(w, "privacy policy rejected request", http.StatusBadRequest)
			return
		}
	}
	if p.upstreamModel != "" {
		var replaced bool
		body, _, replaced = bodyrewrite.ReplaceTopLevelString(body, "model", p.upstreamModel)
		if !replaced {
			evidence.Outcome = "model_rewrite_rejected"
			http.Error(w, "model rewrite unavailable", http.StatusBadRequest)
			return
		}
	}
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Minute)
	defer cancel()
	req, err := BuildRequest(ctx, r, body, true, OfficialOrigin, "")
	if err != nil {
		evidence.Outcome = "subscription_request_rejected"
		http.Error(w, "subscription request rejected", http.StatusBadRequest)
		return
	}
	if err := ctx.Err(); err != nil {
		evidence.Outcome = "client_cancelled"
		return
	}
	if !p.admitAttempt() {
		evidence.Outcome = "probe_send_limit"
		http.Error(w, "probe send limit reached", http.StatusTooManyRequests)
		return
	}
	// RoundTrip is intentional: redirects are returned to the client only after
	// removing Location, never followed with the caller's OAuth credentials.
	evidence.OfficialAttempt = true
	resp, err := p.transport.RoundTrip(req)
	if err != nil {
		evidence.Outcome = "upstream_transport_failed"
		http.Error(w, "upstream transport failed", http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()
	evidence.UpstreamStatus = resp.StatusCode
	if resp.StatusCode >= 300 && resp.StatusCode < 400 {
		evidence.Outcome = "upstream_redirect_rejected"
		http.Error(w, "upstream redirect rejected", http.StatusBadGateway)
		return
	}
	if p.observer != nil {
		resp.Body = routing.CaptureResponse(resp.Body, func(body []byte) { responseEvidence(body, &evidence) })
	}
	stripHopHeaders(resp.Header)
	for name, values := range resp.Header {
		if strings.EqualFold(name, "Authorization") || strings.EqualFold(name, "X-Api-Key") || strings.EqualFold(name, "X-Relay-Key") || strings.EqualFold(name, "Set-Cookie") {
			continue
		}
		for _, value := range values {
			w.Header().Add(name, value)
		}
	}
	w.WriteHeader(resp.StatusCode)
	buf := make([]byte, 16*1024)
	for {
		n, readErr := resp.Body.Read(buf)
		if n > 0 {
			if _, err = w.Write(buf[:n]); err != nil {
				evidence.Outcome = "client_write_failed"
				return
			}
			if f, ok := w.(http.Flusher); ok {
				f.Flush()
			}
		}
		if readErr != nil {
			evidence.ResponseComplete = readErr == io.EOF
			evidence.Outcome = "upstream_read_failed"
			if readErr == io.EOF {
				evidence.Outcome = "response_complete"
			}
			return
		}
	}
}

// ListenLocal prevents the feasibility harness from becoming a shared or
// remote credential gateway by accident.
func ListenLocal(address string) (net.Listener, error) {
	host, _, err := net.SplitHostPort(address)
	if err != nil {
		return nil, err
	}
	ip := net.ParseIP(host)
	if ip == nil || !ip.IsLoopback() {
		return nil, errors.New("probe requires an explicit loopback IP")
	}
	return net.Listen("tcp", address)
}
