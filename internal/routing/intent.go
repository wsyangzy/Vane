package routing

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"math"
	"net/http"
	"strings"
	"sync"
	"time"
)

// TurnInput exists only for the lifetime of a request. Cache keys are hashes;
// neither the cache nor dispatch snapshots retain these text fields.
type TurnInput struct {
	ConversationID        string `json:"-"`
	BranchID              string `json:"-"`
	TurnID                string `json:"-"`
	NewTurn               bool   `json:"-"`
	Continuation          bool   `json:"-"`
	CurrentUserMessage    string `json:"current_user_message"`
	PreviousUserMessage   string `json:"previous_user_message,omitempty"`
	PreviousAssistantText string `json:"previous_assistant_text,omitempty"`
}

func routingHash(parts ...string) string {
	sum := sha256.Sum256([]byte(strings.Join(parts, "\x00")))
	return hex.EncodeToString(sum[:])
}

// ResolveTurn reads only explicit user/assistant text blocks. Tool outputs,
// system messages, tool definitions, images and reasoning are never extracted.
// Length limits are applied after the classifier's privacy scan, not here.
func ResolveTurn(ctx context.Context, h http.Header, body []byte, provider string) TurnInput {
	var root map[string]any
	if json.Unmarshal(body, &root) != nil {
		return TurnInput{}
	}
	t := TurnInput{ConversationID: h.Get("X-Session-Id"), BranchID: h.Get("X-Branch-Id")}
	for _, key := range []string{"X-Claude-Session-Id", "X-Codex-Session-Id", "Session_id", "Conversation_id"} {
		if t.ConversationID == "" {
			t.ConversationID = h.Get(key)
		}
	}
	if t.ConversationID == "" {
		if metadata, ok := root["metadata"].(map[string]any); ok {
			t.ConversationID, _ = metadata["conversation_id"].(string)
		}
	}
	var messages []any
	if provider == "claude" {
		messages, _ = root["messages"].([]any)
	} else {
		switch input := root["input"].(type) {
		case string:
			messages = []any{map[string]any{"role": "user", "content": input}}
		case []any:
			messages = input
		}
	}
	texts := func(content any) string {
		if s, ok := content.(string); ok {
			return s
		}
		var parts []string
		if blocks, ok := content.([]any); ok {
			for _, b := range blocks {
				obj, ok := b.(map[string]any)
				if !ok {
					continue
				}
				typ, _ := obj["type"].(string)
				if typ == "text" || typ == "input_text" || typ == "output_text" {
					s, _ := obj["text"].(string)
					parts = append(parts, s)
				}
			}
		}
		return strings.Join(parts, "\n")
	}
	lastUser := -1
	var userTexts []string
	for i, item := range messages {
		obj, ok := item.(map[string]any)
		if !ok {
			continue
		}
		role, _ := obj["role"].(string)
		text := texts(obj["content"])
		if role == "user" && strings.TrimSpace(text) != "" && !strings.HasPrefix(strings.TrimSpace(text), "<system-reminder>") {
			lastUser = i
			userTexts = append(userTexts, text)
		}
	}
	if len(userTexts) > 0 {
		t.CurrentUserMessage = userTexts[len(userTexts)-1]
		if len(userTexts) > 1 {
			t.PreviousUserMessage = userTexts[len(userTexts)-2]
		}
		for i := lastUser - 1; i >= 0; i-- {
			obj, ok := messages[i].(map[string]any)
			if ok && obj["role"] == "assistant" {
				t.PreviousAssistantText = texts(obj["content"])
				break
			}
		}
	}
	t.NewTurn = lastUser >= 0 && lastUser == len(messages)-1
	for _, key := range []string{"X-Claude-Turn-Id", "X-Claude-Prompt-Id", "X-Codex-Turn-Id", "X-Codex-Prompt-Id", "X-Claude-User-Message-ID", "X-Codex-User-Message-ID"} {
		if value := h.Get(key); value != "" {
			t.TurnID = routingHash(CallerScope(ctx), provider, t.ConversationID, t.BranchID, value)
			break
		}
	}
	if t.TurnID == "" && lastUser >= 0 && t.ConversationID != "" {
		raw, _ := json.Marshal(messages[:lastUser+1])
		t.TurnID = routingHash(CallerScope(ctx), provider, t.ConversationID, t.BranchID, string(raw))
	}
	current := strings.TrimSpace(t.CurrentUserMessage)
	t.Continuation = !t.NewTurn || len([]rune(current)) < 40 && (strings.Contains(current, "继续") || strings.Contains(current, "刚才") || current == "修复这个" || strings.EqualFold(current, "continue"))
	if !t.Continuation {
		t.PreviousUserMessage = ""
		t.PreviousAssistantText = ""
	}
	return t
}

type IntentResult struct {
	IntentID     string  `json:"intent_id"`
	Confidence   float64 `json:"confidence"`
	Model        string  `json:"model"`
	Fallback     string  `json:"fallback"`
	Reason       string  `json:"reason"`
	DurationMS   int64   `json:"duration_ms"`
	InputTokens  int64   `json:"input_tokens"`
	OutputTokens int64   `json:"output_tokens"`
	Cost         float64 `json:"cost"`
	Cached       bool    `json:"cached"`
}
type ClassifierCall func(context.Context, RoutingProfile, TurnInput) (IntentResult, error)
type intentEntry struct {
	result            IntentResult
	expires, lastUsed time.Time
}
type intentFlight struct {
	done   chan struct{}
	result IntentResult
}
type IntentClassifier struct {
	mu        sync.Mutex
	entries   map[string]intentEntry
	flights   map[string]*intentFlight
	cooldowns map[string]time.Time
	slots     chan struct{}
	now       func() time.Time
}

func NewIntentClassifier() *IntentClassifier {
	return &IntentClassifier{entries: map[string]intentEntry{}, flights: map[string]*intentFlight{}, cooldowns: map[string]time.Time{}, slots: make(chan struct{}, 64), now: time.Now}
}

// Only the caller that actually invoked the model owns its usage. Reused
// decisions carry no additional charge, including waiters of an in-flight call.
func reusedIntent(r IntentResult) IntentResult {
	r.Cached = true
	r.InputTokens = 0
	r.OutputTokens = 0
	r.Cost = 0
	return r
}

func (c *IntentClassifier) Classify(ctx context.Context, p RoutingProfile, t TurnInput, previous string, call ClassifierCall) IntentResult {
	if p.Classifier == nil || !p.Classifier.Enabled {
		return IntentResult{Reason: "classifier_disabled"}
	}
	cfg := p.Classifier
	data, _ := json.Marshal(p.Intents)
	identity, _ := json.Marshal([]any{cfg.Provider, cfg.ChannelID, cfg.GroupID, cfg.Model})
	modelKey := routingHash(string(identity))
	key := routingHash(CallerScope(ctx), p.Provider, fmtProfileRevision(p), t.ConversationID, t.BranchID, t.TurnID, string(data), modelKey)
	fallback := func(reason string) IntentResult {
		r := IntentResult{Model: cfg.Model, Reason: reason, Fallback: "default"}
		if cfg.FailureMode == "no_intent" {
			r.Fallback = "no_intent"
		}
		if cfg.FailureMode == "previous" && t.Continuation && previous != "" {
			for _, intent := range p.Intents {
				if intent.Enabled && intent.ID == previous {
					r.IntentID = previous
					r.Fallback = "previous"
					break
				}
			}
		}
		return r
	}
	c.mu.Lock()
	now := c.now()
	if cached, ok := c.entries[key]; ok && now.Before(cached.expires) {
		cached.lastUsed = now
		c.entries[key] = cached
		r := reusedIntent(cached.result)
		c.mu.Unlock()
		return r
	}
	if f := c.flights[key]; f != nil {
		c.mu.Unlock()
		select {
		case <-ctx.Done():
			return fallback("classifier_cancelled")
		case <-f.done:
			return reusedIntent(f.result)
		}
	}
	if !t.NewTurn || t.TurnID == "" || t.CurrentUserMessage == "" {
		c.mu.Unlock()
		return fallback("classifier_no_new_turn")
	}
	if now.Before(c.cooldowns[modelKey]) {
		c.mu.Unlock()
		return fallback("classifier_cooling_down")
	}
	if len(c.flights) >= 64 {
		c.mu.Unlock()
		return fallback("classifier_busy")
	}
	select {
	case c.slots <- struct{}{}:
	default:
		c.mu.Unlock()
		return fallback("classifier_busy")
	}
	f := &intentFlight{done: make(chan struct{})}
	c.flights[key] = f
	c.mu.Unlock()
	// A caller cancellation only stops that caller's wait. The bounded flight
	// has its own deadline so it can still serve concurrent duplicate requests.
	go func() {
		timeout := cfg.Timeout
		if timeout <= 0 {
			timeout = 8 * time.Second
		}
		callCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), timeout)
		defer cancel()
		start := c.now()
		var r IntentResult
		var err error
		type outcome struct {
			result IntentResult
			err    error
		}
		completed := make(chan outcome, 1)
		go func() {
			defer func() { <-c.slots }()
			if call == nil {
				completed <- outcome{err: errors.New("classifier_unavailable")}
				return
			}
			result, callErr := call(callCtx, p, t)
			completed <- outcome{result, callErr}
		}()
		select {
		case result := <-completed:
			r, err = result.result, result.err
		case <-callCtx.Done():
			err = callCtx.Err()
		}
		if callCtx.Err() != nil {
			err = callCtx.Err()
		}
		charged := r
		if err != nil {
			reason := "classifier_upstream_error"
			if errors.Is(err, context.DeadlineExceeded) || callCtx.Err() == context.DeadlineExceeded {
				reason = "classifier_timeout"
			}
			if err.Error() == "classifier_privacy_rejected" {
				reason = err.Error()
			}
			r = fallback(reason)
		} else {
			valid := false
			for _, in := range p.Intents {
				if in.Enabled && in.ID == r.IntentID {
					valid = true
					break
				}
			}
			if !valid || math.IsNaN(r.Confidence) || math.IsInf(r.Confidence, 0) || r.Confidence < 0 || r.Confidence > 1 || r.Confidence < cfg.Confidence {
				r = fallback("classifier_invalid_output")
			}
		}
		r.InputTokens, r.OutputTokens, r.Cost = charged.InputTokens, charged.OutputTokens, charged.Cost
		r.Model = cfg.Model
		r.DurationMS = c.now().Sub(start).Milliseconds()
		c.mu.Lock()
		defer c.mu.Unlock()
		now = c.now()
		if r.Reason != "" {
			if _, exists := c.cooldowns[modelKey]; !exists && len(c.cooldowns) >= 2048 {
				oldest := ""
				var until time.Time
				for k, expiry := range c.cooldowns {
					if oldest == "" || expiry.Before(until) {
						oldest, until = k, expiry
					}
				}
				delete(c.cooldowns, oldest)
			}
			c.cooldowns[modelKey] = now.Add(30 * time.Second)
		}
		if len(c.entries) >= 2048 {
			oldest := ""
			var used time.Time
			for k, e := range c.entries {
				if oldest == "" || e.lastUsed.Before(used) {
					oldest = k
					used = e.lastUsed
				}
			}
			delete(c.entries, oldest)
		}
		c.entries[key] = intentEntry{reusedIntent(r), now.Add(10 * time.Minute), now}
		f.result = r
		delete(c.flights, key)
		close(f.done)
	}()
	select {
	case <-ctx.Done():
		return fallback("classifier_cancelled")
	case <-f.done:
		return f.result
	}
}
func fmtProfileRevision(p RoutingProfile) string {
	b, _ := json.Marshal([]int64{p.ID, p.Revision})
	return string(b)
}
