package subscription

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"

	"github.com/wsyangzy/Vane/internal/routing"
)

func callerID(id, version int64) string { return fmt.Sprintf("codex:%d:%d", id, version) }
func SingleHeader(h http.Header, name string) (string, bool) {
	var values []string
	for k, v := range h {
		if strings.EqualFold(k, name) {
			values = append(values, v...)
		}
	}
	if len(values) != 1 {
		return "", false
	}
	s := strings.TrimSpace(values[0])
	return s, s != ""
}

func Path(r *http.Request) string {
	if r == nil || r.URL == nil || r.URL.RawPath != "" {
		return ""
	}
	p := r.URL.Path
	if strings.HasPrefix(p, "/backend-api/codex/") {
		return strings.TrimPrefix(p, "/backend-api/codex")
	}
	return strings.TrimPrefix(p, "/v1")
}
func AllowedPath(r *http.Request) bool {
	switch Path(r) {
	case "/models":
		return r.Method == http.MethodGet
	case "/responses", "/responses/compact":
		return r.Method == http.MethodPost || (r.Method == http.MethodGet && strings.EqualFold(r.Header.Get("Upgrade"), "websocket"))
	default:
		return false
	}
}
func ValidateEndpoint(base string) error {
	if base != OfficialBaseURL {
		return errors.New("订阅端点必须为 https://chatgpt.com/backend-api/codex")
	}
	return nil
}

func (c *Controller) Authenticate(r *http.Request) (context.Context, bool, error) {
	if caller, ok := routing.PoolCallerFromContext(r.Context()); ok {
		if c.poolRuntime.Load() == nil || !c.poolRuntime.Load().ValidCaller(caller) || !AllowedPath(r) {
			return r.Context(), true, errors.New("pool_request_rejected")
		}
		return r.Context(), true, nil
	}
	present := false
	for k := range r.Header {
		if strings.EqualFold(k, "X-Relay-Key") {
			present = true
		}
	}
	if !present {
		return r.Context(), false, nil
	}
	for k := range r.Header {
		if strings.EqualFold(k, "Origin") {
			return r.Context(), true, errors.New("subscription_request_rejected")
		}
	}
	if !AllowedPath(r) {
		return r.Context(), true, errors.New("subscription_request_rejected")
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil || !net.ParseIP(host).IsLoopback() {
		return r.Context(), true, errors.New("subscription_requires_loopback")
	}
	key, ok := SingleHeader(r.Header, "X-Relay-Key")
	if !ok {
		return r.Context(), true, errors.New("relay_authentication_rejected")
	}
	if runtime := c.poolRuntime.Load(); runtime != nil {
		if caller, err := runtime.Authenticate("codex", key); err == nil {
			return routing.WithPoolCaller(r.Context(), caller), true, nil
		}
	}
	bindings, err := c.store.ListBindings(r.Context())
	if err != nil {
		return r.Context(), true, errors.New("subscription_configuration_unavailable")
	}
	digest := sha256.Sum256([]byte(key))
	var b *Binding
	for _, v := range bindings {
		stored, e := hex.DecodeString(v.KeyHash)
		if e == nil && len(stored) == 32 && subtle.ConstantTimeCompare(stored, digest[:]) == 1 {
			if b != nil {
				return r.Context(), true, errors.New("ambiguous_subscription_caller")
			}
			copy := v
			b = &copy
		}
	}
	if b == nil {
		return r.Context(), true, errors.New("relay_authentication_rejected")
	}
	for k := range r.Header {
		if strings.EqualFold(k, "X-Api-Key") {
			return r.Context(), true, errors.New("conflicting_subscription_authentication")
		}
	}
	auth, ok := SingleHeader(r.Header, "Authorization")
	account, accountOK := SingleHeader(r.Header, "ChatGPT-Account-ID")
	unavailable := false
	if b.AccountID != "" {
		auth = ""
		account = ""
		unavailable = true
		if b.Enabled && c.resolveAccount != nil {
			token, id, e := c.resolveAccount(r.Context(), b.AccountID)
			if e == nil {
				auth = "Bearer " + token
				account = id
				unavailable = false
			}
		}
	} else {
		if !ok || !strings.HasPrefix(auth, "Bearer ") || strings.TrimSpace(strings.TrimPrefix(auth, "Bearer ")) == "" {
			return r.Context(), true, errors.New("subscription_authentication_required")
		}
		// Absence is allowed by the official gateway contract; duplicates are not.
		for k := range r.Header {
			if strings.EqualFold(k, "ChatGPT-Account-ID") && !accountOK {
				return r.Context(), true, errors.New("subscription_account_header_rejected")
			}
		}
	}
	if strings.ContainsAny(account, "\r\n\x00") {
		return r.Context(), true, errors.New("subscription_account_header_rejected")
	}
	ctx := routing.WithCallerScope(r.Context(), CallerScope(*b))
	return context.WithValue(ctx, contextKey{}, &Request{Binding: *b, authorization: auth, accountID: account, credentialUnavailable: unavailable}), true, nil
}

// AuthorizeOutbound runs after all regular headers have been copied. Only the
// fixed official origin receives the request-local token and workspace ID.
func AuthorizeOutbound(src, dst *http.Request, mode string, groupID int64) error {
	for k := range dst.Header {
		if strings.EqualFold(k, "X-Relay-Key") || strings.EqualFold(k, "ChatGPT-Account-ID") {
			delete(dst.Header, k)
		}
	}
	r := FromContext(src.Context())
	if r != nil {
		dst.Header.Del("Cookie")
		dst.Header.Del("Proxy-Authorization")
	}
	if mode != ClientPassthrough {
		return nil
	}
	if r == nil || r.Binding.GroupID != groupID {
		return errors.New("subscription_caller_required")
	}
	u := dst.URL
	if !AllowedPath(src) || u.Scheme != "https" || u.Host != "chatgpt.com" || u.User != nil || u.Fragment != "" || u.RawPath != "" || u.Path != "/backend-api/codex"+Path(src) || u.RawQuery != src.URL.RawQuery || dst.Host != "chatgpt.com" {
		return errors.New("subscription_egress_rejected")
	}
	dst.Header.Del("Authorization")
	dst.Header.Del("X-Api-Key")
	dst.Header.Set("Authorization", r.authorization)
	if r.accountID != "" {
		dst.Header.Set("ChatGPT-Account-ID", r.accountID)
	}
	return nil
}

func TargetURL(src *http.Request, base string, subscription bool) string {
	if subscription {
		u, _ := url.Parse(OfficialBaseURL + Path(src))
		u.RawQuery = src.URL.RawQuery
		return u.String()
	}
	return ""
}

func Redact(ctx context.Context, text string) string {
	r := FromContext(ctx)
	if r == nil {
		return text
	}
	token := strings.TrimPrefix(r.authorization, "Bearer ")
	if token != "" {
		text = strings.ReplaceAll(text, token, "[REDACTED]")
	}
	if r.accountID != "" {
		text = strings.ReplaceAll(text, r.accountID, "[REDACTED]")
	}
	return text
}
