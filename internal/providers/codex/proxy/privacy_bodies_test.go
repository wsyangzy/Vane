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

// 渠道隐私级别：同一请求在不同渠道上发出的正文由渠道级别决定。
// 全局「仅检测」下，三方中转必须收到脱敏正文，可信渠道收到原文，跟随全局的渠道用全局结果。
func TestOutboundBodyFollowsChannelPrivacyLevel(t *testing.T) {
	original := []byte(`{"input":"13800138000"}`)
	redacted := []byte(`{"input":"[手机号]"}`)
	bodies := &codexPrivacyBodies{
		original: original,
		result: privacy.ApplyResult{
			Body: original, RedactedBody: redacted, Action: privacy.ModeDetect, HitCount: 1,
			RuleHits: []privacy.RuleHit{{RuleID: 1, RuleName: "手机号", Count: 1}},
		},
	}
	req := httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	req = req.WithContext(contextWithCodexPrivacy(context.Background(), bodies))
	handler := &Handler{logger: newTestLogger()}

	relay := &store.CodexChannelRecord{Name: "relay", PrivacyLevel: privacy.ChannelLevelThirdParty}
	official := &store.CodexChannelRecord{Name: "official", PrivacyLevel: privacy.ChannelLevelTrusted}
	follow := &store.CodexChannelRecord{Name: "follow"}

	if got := handler.outboundBody(req, "req-1", follow, original); string(got) != string(original) {
		t.Fatalf("follow-global channel under detect mode must receive the original, got %s", got)
	}
	if got := handler.outboundBody(req, "req-1", relay, original); !strings.Contains(string(got), "[手机号]") {
		t.Fatalf("third-party relay must receive the redacted body, got %s", got)
	}
	if got := handler.outboundBody(req, "req-1", official, redacted); string(got) != string(original) {
		t.Fatalf("trusted channel must receive the original body even when the global mode redacts, got %s", got)
	}

	// 没经过隐私过滤的请求原样透传
	plain := httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	if got := handler.outboundBody(plain, "req-2", relay, original); string(got) != string(original) {
		t.Fatalf("requests without privacy results must pass through, got %s", got)
	}
}

// 全局「脱敏」下发往可信渠道的是原文：此时不得记录脱敏摘要，否则请求详情会误标「已脱敏」。
// 同一请求转移到三方中转、真正发出改写正文后才记录，且只记一次。
func TestOutboundBodyRecordsRedactionOnlyWhenBodyIsRewritten(t *testing.T) {
	tracker := newPrivacyTestTracker(t)
	requestID := "req-codex-privacy-trusted"
	original := []byte(`{"input":"13800138000"}`)
	redacted := []byte(`{"input":"[手机号]"}`)
	bodies := &codexPrivacyBodies{
		original: original,
		result: privacy.ApplyResult{
			Body: redacted, RedactedBody: redacted, Changed: true, Action: privacy.ModeRedact, HitCount: 1,
			RuleHits: []privacy.RuleHit{{RuleID: 1, RuleName: "手机号", Count: 1}},
		},
	}
	req := httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	req = req.WithContext(contextWithCodexPrivacy(context.Background(), bodies))
	handler := &Handler{logger: newTestLogger(), usageTracker: tracker}
	handler.recordStart(requestID, req, false, "official", "gpt-5.5", modelRewriteResult{}, "", "")

	official := &store.CodexChannelRecord{Name: "official", PrivacyLevel: privacy.ChannelLevelTrusted}
	if got := handler.outboundBody(req, requestID, official, redacted); string(got) != string(original) {
		t.Fatalf("trusted channel must receive the original body, got %s", got)
	}
	if detail := activeRequestDetail(t, tracker); detail.PrivacyRedacted {
		t.Fatalf("a request sent in plaintext must not be marked as redacted: %+v", detail.PrivacyRedactionSummary)
	}

	relay := &store.CodexChannelRecord{Name: "relay", PrivacyLevel: privacy.ChannelLevelThirdParty}
	handler.outboundBody(req, requestID, relay, redacted)
	handler.outboundBody(req, requestID, relay, redacted)
	detail := activeRequestDetail(t, tracker)
	if !detail.PrivacyRedacted || !strings.Contains(detail.PrivacyRedactionSummary, `"changed":true`) {
		t.Fatalf("redaction must be recorded once the rewritten body is sent, got %+v", detail)
	}
	if strings.Contains(detail.PrivacyRedactionSummary, "13800138000") {
		t.Fatalf("summary must never carry plaintext: %s", detail.PrivacyRedactionSummary)
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
