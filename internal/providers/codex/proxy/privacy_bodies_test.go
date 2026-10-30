package codexproxy

import (
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/wsyangzy/Vane/internal/privacy"
	"github.com/wsyangzy/Vane/internal/providers/codex/store"
	"github.com/wsyangzy/Vane/internal/tracking"
)

func TestOutboundBodyIgnoresRetiredChannelPrivacyLevels(t *testing.T) {
	original := []byte(`{"input":"original-value"}`)
	redacted := []byte(`{"input":"token-value"}`)
	for _, mode := range []string{privacy.ModeDisabled, privacy.ModeDetect, privacy.ModeRedact} {
		tracker := newPrivacyTestTracker(t)
		handler := &Handler{logger: newTestLogger(), usageTracker: tracker}
		result := privacy.ApplyResult{Body: original, Action: mode, HitCount: 1, RuleHits: []privacy.RuleHit{{RuleID: 1, RuleName: "sensitive", Action: privacy.ActionRedact, Count: 1}}}
		if mode == privacy.ModeRedact {
			result.Body = redacted
			result.Changed = true
		}
		bodies := &codexPrivacyBodies{original: original, result: result}
		req := httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
		req = req.WithContext(contextWithCodexPrivacy(context.Background(), bodies))
		handler.recordStart("request", req, false, "official", "gpt-5.5", modelRewriteResult{}, "", "")
		for _, level := range []string{"trusted", "third_party", "", "trusted"} {
			channel := &store.CodexChannelRecord{Name: "channel", PrivacyLevel: level}
			if got := handler.outboundBody(req, "request", channel, original); string(got) != string(result.Body) {
				t.Fatalf("mode=%s level=%s got=%s", mode, level, got)
			}
		}
		detail := activeRequestDetail(t, tracker)
		if detail.PrivacyRedacted != (mode == privacy.ModeRedact) {
			t.Fatalf("mode=%s summary=%+v", mode, detail)
		}
		if strings.Contains(detail.PrivacyRedactionSummary, "original-value") {
			t.Fatal("summary contains raw value")
		}
		plain := httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
		if got := handler.outboundBody(plain, "plain", nil, original); string(got) != string(original) {
			t.Fatal("missing privacy result changed request")
		}
	}
}

func newPrivacyTestTracker(t *testing.T) *tracking.UsageTracker {
	t.Helper()
	tracker, err := tracking.NewUsageTracker(&tracking.Config{
		Enabled: true, DatabasePath: filepath.Join(t.TempDir(), "usage.db"), BufferSize: 10, BatchSize: 5,
		FlushInterval: time.Hour, MaxRetry: 1, CleanupInterval: time.Hour, RetentionDays: 1,
	})
	if err != nil {
		t.Fatalf("create tracker: %v", err)
	}
	t.Cleanup(func() { _ = tracker.Close() })
	return tracker
}

func activeRequestDetail(t *testing.T, tracker *tracking.UsageTracker) tracking.RequestDetail {
	t.Helper()
	details, _, err := tracker.QueryRequestDetailsWithHotPool(context.Background(), &tracking.QueryOptions{Limit: 1})
	if err != nil || len(details) != 1 {
		t.Fatalf("query request details: %v (%d rows)", err, len(details))
	}
	return details[0]
}
