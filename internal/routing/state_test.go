package routing

import (
	"context"
	"io"
	"strings"
	"testing"
	"time"
)

func TestStateOwnershipIncludesCallerAndActualFallback(t *testing.T) {
	s := NewStateTracker()
	a := Candidate{Provider: "p", Endpoint: "a", Credential: "g", Model: "x"}
	b := a
	b.Endpoint = "fallback"
	ctxA := WithCallerScope(context.Background(), "a")
	ctxB := WithCallerScope(context.Background(), "b")
	keyA := StateKey(ctxA, "sealed", "opaque")
	keyB := StateKey(ctxB, "sealed", "opaque")
	s.Observe([]string{keyA}, "turn", b)
	if owner, ok := s.Preference([]string{keyA, "unknown-history"}, ""); !ok || owner != b {
		t.Fatal("actual fallback writer was not preferred")
	}
	if _, ok := s.Preference([]string{keyB}, ""); ok {
		t.Fatal("routing hint crossed caller boundary")
	}
	s.Observe([]string{keyA}, "", a)
	if owner, _ := s.Preference([]string{keyA}, ""); owner != b {
		t.Fatal("echo reassigned state ownership")
	}
	if !s.Preferred("turn", b) || s.Preferred("turn", a) {
		t.Fatal("turn preference lost actual writer")
	}
}

func TestCaptureRequiresCompleteBoundedRead(t *testing.T) {
	called := 0
	for _, input := range []string{"complete", strings.Repeat("x", 1024*1024+1)} {
		body := CaptureResponse(io.NopCloser(strings.NewReader(input)), func([]byte) { called++ })
		_, _ = io.Copy(io.Discard, body)
		_ = body.Close()
	}
	if called != 1 {
		t.Fatalf("observations=%d", called)
	}
	body := CaptureResponse(io.NopCloser(strings.NewReader("partial")), func([]byte) { called++ })
	_ = body.Close()
	if called != 1 {
		t.Fatal("close without EOF was registered")
	}
}

func TestStatePreferenceExpiresAndUsesLatestKnownHistory(t *testing.T) {
	var oldSlot, reloadedSlot uint64
	oldVersion := StateVersion(&oldSlot)
	if oldVersion == 0 || StateVersion(&oldSlot) != oldVersion {
		t.Fatal("unstable state version")
	}
	s := NewStateTracker()
	candidate := Candidate{Provider: "p", Endpoint: "e", Credential: "g", Model: "m", StateVersion: oldVersion}
	s.Observe([]string{"sealed"}, "", candidate)
	candidate.StateVersion = StateVersion(&reloadedSlot)
	s.Observe([]string{"new-state"}, "", candidate)
	if owner, ok := s.Preference([]string{"sealed", "unknown", "new-state"}, ""); !ok || owner != candidate {
		t.Fatal("latest successful response was not preferred")
	}
	for key, owner := range s.states {
		owner.expires = time.Now().Add(-time.Second)
		s.states[key] = owner
	}
	if _, ok := s.Preference([]string{"sealed", "new-state"}, ""); ok {
		t.Fatal("expired history retained a routing hint")
	}
}
