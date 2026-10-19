package handlers

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"ai-relay-forwarder/internal/privacy"
)

type staticPrivacyFilter struct {
	output []byte
	err    error
}

func (f staticPrivacyFilter) Apply(_ context.Context, _ privacy.Request, body []byte) (privacy.ApplyResult, error) {
	if f.err != nil {
		return privacy.ApplyResult{Body: body, Action: privacy.ModeRedact}, f.err
	}
	return privacy.ApplyResult{
		Body:     f.output,
		Changed:  true,
		Action:   privacy.ModeRedact,
		HitCount: 1,
		RuleHits: []privacy.RuleHit{{RuleID: 1, RuleName: "token rule", Count: 1, Matches: []string{"sk-body-token"}}},
	}, nil
}

// 脱敏只改请求体。URL 与认证头属于传输元数据，一旦被改写就可能触发
// 上游的客户端指纹检测（见 spec: error-handling 的请求头透传规范）。
func TestApplyPrivacyFilterDoesNotTouchTransportMetadata(t *testing.T) {
	req := httptest.NewRequest("POST", "https://provider.example/v1/messages?api_key=query-auth-token", nil)
	req.Header.Set("Authorization", "Bearer upstream-auth-token")
	req.Header.Set("X-Api-Key", "upstream-api-key")
	req.Header.Set("X-Custom-Auth", "custom-endpoint-auth")
	req.Header.Set("Content-Type", "application/json")

	originalURL := req.URL.String()
	originalAuthorization := req.Header.Get("Authorization")
	originalAPIKey := req.Header.Get("X-Api-Key")
	originalCustomAuth := req.Header.Get("X-Custom-Auth")

	body := []byte(`{"message":"sk-body-token"}`)
	bodies, err := applyPrivacyFilter(
		context.Background(),
		staticPrivacyFilter{output: []byte(`{"message":"fake_token_0001"}`)},
		req,
		"claude",
		body,
		nil,
		nil,
	)
	if err != nil {
		t.Fatalf("apply privacy filter: %v", err)
	}
	if redacted := bodies.forChannel("any"); string(redacted) != `{"message":"fake_token_0001"}` {
		t.Fatalf("unexpected redacted body: %s", redacted)
	}
	if req.URL.String() != originalURL {
		t.Fatalf("transport URL changed: %s", req.URL.String())
	}
	if req.Header.Get("Authorization") != originalAuthorization {
		t.Fatalf("Authorization header changed: %q", req.Header.Get("Authorization"))
	}
	if req.Header.Get("X-Api-Key") != originalAPIKey {
		t.Fatalf("X-Api-Key header changed: %q", req.Header.Get("X-Api-Key"))
	}
	if req.Header.Get("X-Custom-Auth") != originalCustomAuth {
		t.Fatalf("custom auth header changed: %q", req.Header.Get("X-Custom-Auth"))
	}
}

func TestApplyPrivacyFilterNilFilterPassesBodyThrough(t *testing.T) {
	req := httptest.NewRequest("POST", "https://provider.example/v1/messages", nil)
	body := []byte(`{"message":"hello"}`)

	bodies, err := applyPrivacyFilter(context.Background(), nil, req, "claude", body, nil, nil)
	if err != nil {
		t.Fatalf("nil filter must not error: %v", err)
	}
	if out := bodies.forChannel("any"); string(out) != string(body) {
		t.Fatalf("nil filter must pass the body through unchanged, got %s", out)
	}
}

// 渠道隐私级别：同一请求故障转移到不同渠道时，发往三方中转的必须是脱敏正文，
// 可信渠道收到原文；跟随全局的渠道用全局模式的结果。
func TestPrivacyBodiesSelectByChannelLevel(t *testing.T) {
	original := []byte(`{"message":"13800138000"}`)
	redacted := []byte(`{"message":"[手机号]"}`)
	levels := map[string]string{"relay": privacy.ChannelLevelThirdParty, "official": privacy.ChannelLevelTrusted}
	recorder := &privacyRecordingLifecycle{}

	// 全局「仅检测」：Body 是原文，RedactedBody 是脱敏正文
	bodies := &privacyBodies{
		original: original,
		result: privacy.ApplyResult{
			Body: original, RedactedBody: redacted, Action: privacy.ModeDetect, HitCount: 1,
			RuleHits: []privacy.RuleHit{{RuleID: 1, RuleName: "手机号", Count: 1}},
		},
		levels:   func(channel string) string { return levels[channel] },
		recorder: recorder,
	}
	if got := bodies.forChannel("default"); string(got) != string(original) {
		t.Fatalf("channels following a detect-only global mode must receive the original body, got %s", got)
	}
	if recorder.calls != 0 {
		t.Fatalf("no redaction happened yet, summary must not be recorded")
	}
	if got := bodies.forChannel("relay"); string(got) != string(redacted) {
		t.Fatalf("third-party relays must always receive the redacted body, got %s", got)
	}
	if got := bodies.forChannel("official"); string(got) != string(original) {
		t.Fatalf("trusted channels must receive the original body, got %s", got)
	}
	bodies.forChannel("relay")
	if recorder.calls != 1 {
		t.Fatalf("redaction summary must be recorded once per request, got %d", recorder.calls)
	}
	if !strings.Contains(recorder.summary, `"changed":true`) || strings.Contains(recorder.summary, "13800138000") {
		t.Fatalf("summary must mark the change and never carry plaintext: %s", recorder.summary)
	}

	// 全局关闭是总开关：三方中转也不改写
	disabled := &privacyBodies{
		original: original,
		result:   privacy.ApplyResult{Body: original, RedactedBody: redacted, Action: privacy.ModeDisabled},
		levels:   func(channel string) string { return levels[channel] },
	}
	if got := disabled.forChannel("relay"); string(got) != string(original) {
		t.Fatalf("a disabled global mode must win over the channel level, got %s", got)
	}
}

type privacyRecordingLifecycle struct {
	RequestLifecycleManager
	calls   int
	summary string
}

func (p *privacyRecordingLifecycle) RecordPrivacyRedaction(summaryJSON string) {
	p.calls++
	p.summary = summaryJSON
}

// 策略错误必须能被 errors.As 识别，调用方据此短路，不进入重试与故障转移。
func TestApplyPrivacyFilterSurfacesPolicyError(t *testing.T) {
	req := httptest.NewRequest("POST", "https://provider.example/v1/messages", nil)
	policy := &privacy.PolicyError{
		StatusCode: http.StatusForbidden,
		Code:       privacy.CodeScanFailed,
		Message:    "scan failed",
	}

	_, err := applyPrivacyFilter(
		context.Background(),
		staticPrivacyFilter{err: policy},
		req,
		"claude",
		[]byte(`{"message":"hi"}`),
		nil,
		nil,
	)
	if err == nil {
		t.Fatal("expected the policy error to surface")
	}
	got := AsPrivacyPolicyError(err)
	if got == nil {
		t.Fatalf("policy error must be recognizable via errors.As, got %v", err)
	}
	if reason := PrivacyFailureReason(got); reason != PrivacyFailureReasonScanFailed {
		t.Fatalf("unexpected failure reason: %s", reason)
	}

	// 写给客户端的响应体不得含命中原文
	rec := httptest.NewRecorder()
	WritePrivacyPolicyErrorResponse(rec, got)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("unexpected status: %d", rec.Code)
	}
	if strings.Contains(rec.Body.String(), "sk-body-token") {
		t.Fatalf("policy error response leaked raw matches: %s", rec.Body.String())
	}
}
