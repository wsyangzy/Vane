package codexproxy

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"ai-relay-forwarder/internal/privacy"
	"ai-relay-forwarder/internal/providers/codex/store"
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
