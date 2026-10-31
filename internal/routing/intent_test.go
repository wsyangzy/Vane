package routing

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func classifierProfile() RoutingProfile {
	p := DefaultProfile("claude")
	p.ID = 1
	p.Revision = 1
	p.Name = "test"
	p.Classifier = &ClassifierConfig{Enabled: true, Model: "router", Provider: "claude", ChannelID: 1, GroupID: 2, Timeout: time.Second, FailureMode: "previous"}
	p.Intents = []IntentRule{{ID: "coding", Enabled: true, Name: "代码", Description: "修改代码"}}
	return p
}

func TestClassifierSharesInflightAndInvalidatesRevision(t *testing.T) {
	c := NewIntentClassifier()
	p := classifierProfile()
	turn := TurnInput{ConversationID: "session", TurnID: "turn", NewTurn: true, CurrentUserMessage: "write code"}
	var calls atomic.Int32
	entered := make(chan struct{})
	release := make(chan struct{})
	call := func(ctx context.Context, p RoutingProfile, in TurnInput) (IntentResult, error) {
		if calls.Add(1) == 1 {
			close(entered)
		}
		<-release
		return IntentResult{IntentID: "coding", Confidence: 1}, nil
	}
	var wg sync.WaitGroup
	results := make([]IntentResult, 12)
	for i := range results {
		wg.Add(1)
		go func() { defer wg.Done(); results[i] = c.Classify(context.Background(), p, turn, "", call) }()
	}
	<-entered
	close(release)
	wg.Wait()
	if calls.Load() != 1 {
		t.Fatalf("calls=%d", calls.Load())
	}
	for _, r := range results {
		if r.IntentID != "coding" {
			t.Fatalf("%+v", r)
		}
	}
	turn.NewTurn = false
	r := c.Classify(context.Background(), p, turn, "", call)
	if !r.Cached || calls.Load() != 1 {
		t.Fatalf("tool continuation reclassified: %+v", r)
	}
	p.Revision++
	turn.NewTurn = true
	c.Classify(context.Background(), p, turn, "", call)
	if calls.Load() != 2 {
		t.Fatal("revision did not invalidate cache")
	}
}

func TestClassifierTimeoutAndFailureFallbackAreIsolated(t *testing.T) {
	c := NewIntentClassifier()
	p := classifierProfile()
	p.Classifier.Timeout = time.Millisecond
	turn := TurnInput{TurnID: "t", NewTurn: true, Continuation: true, CurrentUserMessage: "继续"}
	call := func(ctx context.Context, _ RoutingProfile, _ TurnInput) (IntentResult, error) {
		<-ctx.Done()
		return IntentResult{}, ctx.Err()
	}
	r := c.Classify(context.Background(), p, turn, "coding", call)
	if r.IntentID != "coding" || r.Fallback != "previous" || r.Reason != "classifier_timeout" {
		t.Fatalf("%+v", r)
	}
	turn.TurnID = "t2"
	r = c.Classify(context.Background(), p, turn, "", call)
	if r.Reason != "classifier_cooling_down" || r.Fallback != "default" {
		t.Fatalf("%+v", r)
	}
	if len(c.entries) != 1 {
		t.Fatal("cooldown created fake classification")
	}
}

func TestClassifierRejectsInvalidIntentAndPrivacyFailure(t *testing.T) {
	for _, tc := range []struct {
		reason string
		call   ClassifierCall
	}{
		{"classifier_invalid_output", func(context.Context, RoutingProfile, TurnInput) (IntentResult, error) {
			return IntentResult{IntentID: "unknown", Confidence: 1}, nil
		}},
		{"classifier_privacy_rejected", func(context.Context, RoutingProfile, TurnInput) (IntentResult, error) {
			return IntentResult{}, errors.New("classifier_privacy_rejected")
		}},
	} {
		t.Run(tc.reason, func(t *testing.T) {
			c := NewIntentClassifier()
			r := c.Classify(context.Background(), classifierProfile(), TurnInput{TurnID: "t", NewTurn: true, CurrentUserMessage: "x"}, "", tc.call)
			if r.Reason != tc.reason || r.IntentID != "" {
				t.Fatalf("%+v", r)
			}
		})
	}
}

func TestTurnExtractionExcludesToolsSystemAndReasoning(t *testing.T) {
	body := []byte(`{"system":"system-secret","tools":[{"description":"tool-secret"}],"messages":[{"role":"user","content":"最初的问题"},{"role":"assistant","content":[{"type":"thinking","thinking":"hidden-secret"},{"type":"text","text":"先修复测试"}]},{"role":"user","content":[{"type":"tool_result","content":"tool-result-secret"},{"type":"text","text":"继续"}]}]}`)
	h := http.Header{"X-Claude-Session-Id": []string{"s"}}
	t1 := ResolveTurn(context.Background(), h, body, "claude")
	if !t1.NewTurn || t1.TurnID == "" || t1.PreviousAssistantText != "先修复测试" || t1.PreviousUserMessage != "最初的问题" {
		t.Fatalf("%+v", t1)
	}
	raw, _ := json.Marshal(t1)
	if strings.Contains(string(raw), "secret") {
		t.Fatalf("private protocol context leaked: %s", raw)
	}
	tool := strings.TrimSuffix(string(body), "]}") + `,{"role":"assistant","content":[{"type":"tool_use","input":{"secret":"do not classify"}}]},{"role":"user","content":[{"type":"tool_result","content":"more-secret"}]}]}`
	t2 := ResolveTurn(context.Background(), h, []byte(tool), "claude")
	if t2.NewTurn || t2.TurnID != t1.TurnID {
		t.Fatalf("tool changed turn: %+v %+v", t1, t2)
	}
	h.Set("X-Claude-Session-Id", "other")
	t3 := ResolveTurn(context.Background(), h, body, "claude")
	if t3.TurnID == t1.TurnID {
		t.Fatal("conversation collision")
	}
	noID := ResolveTurn(context.Background(), http.Header{}, body, "claude")
	if !noID.NewTurn || noID.TurnID != "" {
		t.Fatal("unscoped user content must rotate without creating a persistent turn identity")
	}
}

func TestTurnExtractionPreservesUnicodeBeforePrivacy(t *testing.T) {
	input := strings.Repeat("前", 4000) + strings.Repeat("尾", 2000)
	r := ResolveTurn(context.Background(), http.Header{"X-Codex-Turn-Id": []string{"t"}}, []byte(`{"input":`+strconvJSON(input)+`}`), "codex")
	if r.CurrentUserMessage != input {
		t.Fatal("turn extraction truncated text before the privacy scan")
	}
}
func strconvJSON(s string) string { b, _ := json.Marshal(s); return string(b) }

func TestClassifierChargesOnceAndRetainsInvalidOutputUsage(t *testing.T) {
	c := NewIntentClassifier()
	p := classifierProfile()
	turn := TurnInput{TurnID: "t", NewTurn: true, CurrentUserMessage: "x"}
	call := func(context.Context, RoutingProfile, TurnInput) (IntentResult, error) {
		return IntentResult{IntentID: "invalid", Confidence: 1, InputTokens: 12, OutputTokens: 4, Cost: .25}, nil
	}
	r := c.Classify(context.Background(), p, turn, "", call)
	cached := c.Classify(context.Background(), p, turn, "", call)
	if r.Reason != "classifier_invalid_output" || r.InputTokens != 12 || r.OutputTokens != 4 || r.Cost != .25 {
		t.Fatalf("failed classification lost charge: %+v", r)
	}
	if !cached.Cached || cached.InputTokens != 0 || cached.OutputTokens != 0 || cached.Cost != 0 {
		t.Fatalf("cached decision charged again: %+v", cached)
	}
}

func TestClassifierCallerCancellationDoesNotCancelSharedFlight(t *testing.T) {
	c := NewIntentClassifier()
	p := classifierProfile()
	turn := TurnInput{TurnID: "t", NewTurn: true, CurrentUserMessage: "x"}
	ctx, cancel := context.WithCancel(context.Background())
	entered := make(chan struct{})
	release := make(chan struct{})
	done := make(chan IntentResult, 1)
	call := func(ctx context.Context, _ RoutingProfile, _ TurnInput) (IntentResult, error) {
		close(entered)
		select {
		case <-release:
			return IntentResult{IntentID: "coding", Confidence: 1}, nil
		case <-ctx.Done():
			return IntentResult{}, ctx.Err()
		}
	}
	go func() { done <- c.Classify(ctx, p, turn, "", call) }()
	<-entered
	cancel()
	if r := <-done; r.Reason != "classifier_cancelled" {
		t.Fatalf("%+v", r)
	}
	close(release)
	r := c.Classify(context.Background(), p, turn, "", call)
	if r.IntentID != "coding" || !r.Cached {
		t.Fatalf("remaining caller lost shared classification: %+v", r)
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.cooldowns) != 0 {
		t.Fatal("caller cancellation cooled model")
	}
}

func TestClassifierTimeoutRejectsLateSuccessAndBoundsFlights(t *testing.T) {
	c := NewIntentClassifier()
	p := classifierProfile()
	p.Classifier.Timeout = time.Millisecond
	release := make(chan struct{})
	completed := make(chan struct{})
	r := c.Classify(context.Background(), p, TurnInput{TurnID: "t", NewTurn: true, CurrentUserMessage: "x"}, "", func(context.Context, RoutingProfile, TurnInput) (IntentResult, error) {
		defer close(completed)
		<-release
		return IntentResult{IntentID: "coding", Confidence: 1}, nil
	})
	close(release)
	<-completed
	if r.Reason != "classifier_timeout" || r.IntentID != "" {
		t.Fatalf("late success accepted: %+v", r)
	}
	c.mu.Lock()
	if len(c.flights) != 0 {
		t.Fatal("timed-out flight retained")
	}
	for i := range 64 {
		c.flights[strconvJSON(string(rune(i)))] = &intentFlight{done: make(chan struct{})}
	}
	c.mu.Unlock()
	p.Classifier.Model = "other"
	r = c.Classify(context.Background(), p, TurnInput{TurnID: "new", NewTurn: true, CurrentUserMessage: "x"}, "", nil)
	if r.Reason != "classifier_busy" {
		t.Fatalf("unbounded flights: %+v", r)
	}
}

func TestClassifierCannotReuseDisabledPreviousIntent(t *testing.T) {
	p := classifierProfile()
	p.Intents[0].Enabled = false
	r := NewIntentClassifier().Classify(context.Background(), p, TurnInput{Continuation: true}, "coding", nil)
	if r.IntentID != "" || r.Fallback != "default" {
		t.Fatalf("disabled previous intent survived revision: %+v", r)
	}
}

func TestClassifierRejectsInvalidConfidenceWithoutLosingUsage(t *testing.T) {
	for _, confidence := range []float64{2, -1, math.NaN(), math.Inf(1)} {
		r := NewIntentClassifier().Classify(context.Background(), classifierProfile(), TurnInput{TurnID: "t", NewTurn: true, CurrentUserMessage: "x"}, "", func(context.Context, RoutingProfile, TurnInput) (IntentResult, error) {
			return IntentResult{IntentID: "coding", Confidence: confidence, InputTokens: 1}, nil
		})
		if r.Reason != "classifier_invalid_output" || r.IntentID != "" || r.InputTokens != 1 {
			t.Fatalf("invalid confidence accepted or usage lost: %+v", r)
		}
	}
}
