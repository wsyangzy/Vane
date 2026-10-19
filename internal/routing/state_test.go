package routing

import (
	"context"
	"io"
	"strings"
	"testing"
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
	if ok, _ := s.Allowed([]string{keyA}, b); !ok {
		t.Fatal("actual fallback writer rejected")
	}
	if ok, _ := s.Allowed([]string{keyA}, a); ok {
		t.Fatal("default backend accepted fallback state")
	}
	if ok, reason := s.Allowed([]string{keyB}, b); ok || reason != "state_origin_unknown" {
		t.Fatal("state crossed caller boundary")
	}
	s.Observe([]string{keyA}, "", a)
	if ok, _ := s.Allowed([]string{keyA}, a); ok {
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

func TestOpaqueStateCannotFollowReboundConfiguration(t *testing.T) {
	var oldSlot, reloadedSlot uint64
	oldVersion := StateVersion(&oldSlot)
	if oldVersion == 0 || StateVersion(&oldSlot) != oldVersion {
		t.Fatal("unstable state version")
	}
	s := NewStateTracker()
	candidate := Candidate{Provider: "p", Endpoint: "e", Credential: "g", Model: "m", StateVersion: oldVersion}
	s.Observe([]string{"sealed"}, "", candidate)
	candidate.StateVersion = StateVersion(&reloadedSlot)
	if ok, _ := s.Allowed([]string{"sealed"}, candidate); ok {
		t.Fatal("opaque state followed a replaced configuration")
	}
}
