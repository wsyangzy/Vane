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

func callerID(id, version int64) string { return fmt.Sprintf("claude:%d:%d", id, version) }

func SingleHeader(h http.Header, name string) (string, bool) {
	var values []string
	for key, items := range h {
		if strings.EqualFold(key, name) {
			values = append(values, items...)
		}
	}
	returnValue := ""
	if len(values) == 1 {
		returnValue = strings.TrimSpace(values[0])
	}
	return returnValue, len(values) == 1 && returnValue != ""
}

func (c *Controller) Authenticate(r *http.Request) (context.Context, bool, error) {
	present := false
	for name := range r.Header {
		if strings.EqualFold(name, "X-Relay-Key") {
			present = true
		}
	}
	if !present {
		return r.Context(), false, nil
	}
	for name := range r.Header {
		if strings.EqualFold(name, "Origin") {
			return r.Context(), true, errors.New("subscription_request_rejected")
		}
	}
	if !AllowedPath(r) {
		return r.Context(), true, errors.New("subscription_request_rejected")
	}
	key, ok := SingleHeader(r.Header, "X-Relay-Key")
	if !ok {
		return r.Context(), true, errors.New("relay_authentication_rejected")
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil || !net.ParseIP(host).IsLoopback() {
		return r.Context(), true, errors.New("subscription_requires_loopback")
	}
	for name := range r.Header {
		if strings.EqualFold(name, "X-Api-Key") {
			return r.Context(), true, errors.New("conflicting_subscription_authentication")
		}
	}
	bindings, err := c.store.ListBindings(r.Context())
	if err != nil {
		return r.Context(), true, errors.New("subscription_configuration_unavailable")
	}
	digest := sha256.Sum256([]byte(key))
	var selected *Binding
	for _, b := range bindings {
		stored, err := hex.DecodeString(b.KeyHash)
		if err == nil && len(stored) == 32 && subtle.ConstantTimeCompare(stored, digest[:]) == 1 {
			if selected != nil {
				return r.Context(), true, errors.New("ambiguous_subscription_caller")
			}
			copy := b
			selected = &copy
		}
	}
	if selected == nil {
		return r.Context(), true, errors.New("relay_authentication_rejected")
	}
	auth, authPresent := SingleHeader(r.Header, "Authorization")
	unavailable := false
	if selected.AccountID != "" {
		auth = ""
		unavailable = true
		if selected.Enabled && c.resolveAccount != nil {
			token, _, e := c.resolveAccount(r.Context(), selected.AccountID)
			if e == nil {
				auth = "Bearer " + token
				unavailable = false
			}
		}
	} else if !authPresent || !strings.HasPrefix(auth, "Bearer ") || strings.TrimSpace(strings.TrimPrefix(auth, "Bearer ")) == "" {
		return r.Context(), true, errors.New("subscription_authentication_required")
	}
	// Disabled callers may still use their explicitly authorized backup, but
	// may never send OAuth to official until manually enabled again.
	ctx := routing.WithCallerScope(r.Context(), CallerScope(*selected))
	return context.WithValue(ctx, contextKey{}, &Request{Binding: *selected, authorization: auth, credentialUnavailable: unavailable}), true, nil
}

func AllowedPath(r *http.Request) bool {
	return r != nil && r.URL != nil && r.Method == http.MethodPost &&
		(r.URL.Path == "/v1/messages" || r.URL.Path == "/v1/messages/count_tokens") && r.URL.RawPath == "" &&
		(r.URL.RawQuery == "" || r.URL.RawQuery == "beta=true")
}

func ValidateEndpoint(origin string, headers map[string]string) error {
	u, err := url.Parse(origin)
	if err != nil || u.String() != OfficialOrigin {
		return errors.New("订阅端点必须为 https://api.anthropic.com，不含路径、端口或查询参数")
	}
	for name := range headers {
		if strings.EqualFold(name, "Authorization") || strings.EqualFold(name, "X-Api-Key") || strings.EqualFold(name, "X-Relay-Key") || strings.EqualFold(name, "Host") || strings.EqualFold(name, "Connection") {
			return errors.New("订阅端点不能覆盖认证、Host 或 Connection 头")
		}
	}
	return nil
}

// AuthorizeOutbound runs after endpoint custom headers are applied. All hops
// strip local/OAuth auth first, and only the exact official destination can
// receive the request-local subscription credential.
func AuthorizeOutbound(ctx context.Context, src, dst *http.Request, mode string, groupID int64, headers map[string]string) error {
	if FromContext(ctx) != nil {
		dst.Header.Del("Cookie")
		dst.Header.Del("Proxy-Authorization")
	}
	for name := range dst.Header {
		if strings.EqualFold(name, "X-Relay-Key") {
			delete(dst.Header, name)
		}
	}
	if mode != ClientPassthrough {
		return nil
	}
	r := FromContext(ctx)
	if r == nil || r.Binding.GroupID != groupID {
		return errors.New("subscription_caller_required")
	}
	if !AllowedPath(src) || ValidateEndpoint(dst.URL.Scheme+"://"+dst.URL.Host, headers) != nil || dst.Host != "api.anthropic.com" || dst.URL.Path != src.URL.Path || dst.URL.RawQuery != src.URL.RawQuery || dst.URL.RawPath != "" || dst.URL.User != nil || dst.URL.Fragment != "" {
		return errors.New("subscription_egress_rejected")
	}
	for name := range dst.Header {
		if strings.EqualFold(name, "Authorization") || strings.EqualFold(name, "X-Api-Key") || strings.EqualFold(name, "Host") {
			delete(dst.Header, name)
		}
	}
	dst.Header.Set("Authorization", r.authorization)
	if r.Binding.AccountID != "" && !strings.Contains(dst.Header.Get("Anthropic-Beta"), "oauth-2025-04-20") {
		value := dst.Header.Get("Anthropic-Beta")
		if value != "" {
			value += ","
		}
		dst.Header.Set("Anthropic-Beta", value+"oauth-2025-04-20")
	}
	return nil
}
