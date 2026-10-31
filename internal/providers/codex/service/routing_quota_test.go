package service

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/wsyangzy/Vane/internal/routing"
	"github.com/wsyangzy/Vane/internal/subscriptions/accounts"
)

type quotaResolverFunc func(context.Context, string, string) (accounts.Credential, error)

func (f quotaResolverFunc) Resolve(ctx context.Context, p, id string) (accounts.Credential, error) {
	return f(ctx, p, id)
}

type quotaTransportFunc func(*http.Request) (*http.Response, error)

func (f quotaTransportFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestRoutingQuotaOfficialReadUsesShortTermCredentialsAndParsesWindows(t *testing.T) {
	ref := routing.CredentialRef{Provider: "codex", ChannelID: 1, GroupID: 2, AccountID: "managed"}
	resolver := quotaResolverFunc(func(_ context.Context, p, id string) (accounts.Credential, error) {
		if p != "codex" || id != "managed" {
			t.Error("wrong credential identity")
		}
		return accounts.Credential{AccessToken: "access-secret", AccountID: "workspace-secret"}, nil
	})
	client := &http.Client{Transport: quotaTransportFunc(func(r *http.Request) (*http.Response, error) {
		if r.Method != "GET" || r.URL.String() != "https://chatgpt.com/backend-api/wham/usage" || r.Header.Get("Authorization") != "Bearer access-secret" || r.Header.Get("ChatGPT-Account-Id") != "workspace-secret" {
			t.Errorf("unexpected official request: %s %s", r.Method, r.URL)
		}
		body := `{"rate_limit":{"primary_window":{"used_percent":20,"reset_after_seconds":3600},"secondary_window":{"used_percent":50,"reset_after_seconds":7200}},"additional_rate_limits":[{"model":"mini","rate_limit":{"primary_window":{"used_percent":90,"reset_after_seconds":60}}}]}`
		return &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(body))}, nil
	})}
	got, err := NewRoutingQuotaReader(resolver, client).Read(context.Background(), ref, "gpt-test")
	if err != nil || len(got) != 3 || *got[0].UsedPercent != 20 || got[0].ModelScope != "*" || got[2].ModelScope != "mini" || got[0].ResetAt == nil || got[0].AccountIDHash == "" {
		t.Fatalf("quota=%+v %v", got, err)
	}
}

func TestRoutingQuotaFailureNeverExposesRawProviderDataOrFollowsRedirects(t *testing.T) {
	resolver := quotaResolverFunc(func(context.Context, string, string) (accounts.Credential, error) {
		return accounts.Credential{AccessToken: "secret", AccountID: "account"}, nil
	})
	ref := routing.CredentialRef{Provider: "codex", ChannelID: 1, GroupID: 2, AccountID: "managed"}
	for _, status := range []int{302, 401, 500, 200} {
		var calls atomic.Int32
		client := &http.Client{Transport: quotaTransportFunc(func(*http.Request) (*http.Response, error) {
			calls.Add(1)
			return &http.Response{StatusCode: status, Header: http.Header{"Location": []string{"https://other.invalid/secret"}}, Body: io.NopCloser(strings.NewReader("secret-invalid-body"))}, nil
		})}
		_, err := NewRoutingQuotaReader(resolver, client).Read(context.Background(), ref, "")
		if err == nil || strings.Contains(err.Error(), "secret") || calls.Load() != 1 {
			t.Fatalf("unsafe error/redirect: %d %v %d", status, err, calls.Load())
		}
	}
}

func TestRoutingQuotaOfficialAccountSingleflightAcrossModels(t *testing.T) {
	var calls atomic.Int32
	entered, release := make(chan struct{}), make(chan struct{})
	resolver := quotaResolverFunc(func(context.Context, string, string) (accounts.Credential, error) {
		return accounts.Credential{AccessToken: "secret", AccountID: "account"}, nil
	})
	reader := NewRoutingQuotaReader(resolver, &http.Client{Transport: quotaTransportFunc(func(*http.Request) (*http.Response, error) {
		if calls.Add(1) == 1 {
			close(entered)
		}
		<-release
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{"rate_limit":{"primary_window":{"used_percent":20}}}`))}, nil
	})})
	ref := routing.CredentialRef{Provider: "codex", ChannelID: 1, GroupID: 2, AccountID: "managed"}
	var wg sync.WaitGroup
	wg.Add(1)
	go func() { defer wg.Done(); _, _ = reader.Read(context.Background(), ref, "a") }()
	<-entered
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := reader.Read(ctx, ref, "b"); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancel=%v", err)
	}
	if calls.Load() != 1 {
		t.Fatalf("duplicate account query=%d", calls.Load())
	}
	close(release)
	wg.Wait()
}

func TestRoutingQuotaResponseLearningSeparatesModelErrorsAndHeaderWindows(t *testing.T) {
	now := time.Now().UTC()
	ref := routing.CredentialRef{Provider: "codex", ChannelID: 1, GroupID: 2, AccountID: "a"}
	h := http.Header{"X-Codex-Primary-Used-Percent": []string{"35"}, "X-Codex-Primary-Reset-At": []string{strconv.FormatInt(now.Add(time.Hour).Unix(), 10)}}
	got := ParseResponseQuota(200, h, nil, ref, "model-a", now)
	if len(got) != 1 || *got[0].UsedPercent != 35 || got[0].Source != "response_header" || got[0].ModelScope != "*" {
		t.Fatalf("headers=%+v", got)
	}
	got = ParseResponseQuota(429, http.Header{"Retry-After": []string{"60"}}, []byte(`{"error":{"code":"model_usage_limit_reached","message":"secret"}}`), ref, "model-b", now)
	if len(got) != 1 || got[0].ModelScope != "model-b" || *got[0].UsedPercent != 100 || got[0].ResetAt == nil || got[0].Error != "" {
		t.Fatalf("error quota=%+v", got)
	}
}
