package service

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/mail"
	"net/url"
	"strings"
	"time"

	"github.com/wsyangzy/Vane/internal/subscriptions/accounts"
)

// PrepareSubscriptionImport normalizes only Claude's native and CLIProxyAPI
// formats. An explicit email supplies identity missing from a native file; it
// never overrides an identity already present in the file.
func PrepareSubscriptionImport(raw []byte, email string) ([]byte, error) {
	var p map[string]json.RawMessage
	if json.Unmarshal(bytes.TrimPrefix(bytes.TrimSpace(raw), []byte{0xef, 0xbb, 0xbf}), &p) != nil || p == nil {
		return nil, errors.New("请选择有效的 Claude JSON 凭证")
	}
	read := func(key string) string { var s string; _ = json.Unmarshal(p[key], &s); return strings.TrimSpace(s) }
	if kind := read("type"); kind != "" && kind != "claude" {
		return nil, errors.New("请选择 Claude 订阅凭证")
	}
	if _, ok := p["tokens"]; ok {
		return nil, errors.New("请选择 Claude 订阅凭证")
	}
	if _, ok := p["claudeAiOauth"]; !ok {
		var scopes []string
		if json.Unmarshal(p["scope"], &scopes) != nil {
			scopes = strings.Fields(read("scope"))
		}
		var expiry int64
		if t, e := time.Parse(time.RFC3339, read("expired")); e == nil {
			expiry = t.UnixMilli()
		}
		p["claudeAiOauth"], _ = json.Marshal(map[string]any{"accessToken": read("access_token"), "refreshToken": read("refresh_token"), "expiresAt": expiry, "scopes": scopes, "subscriptionType": read("subscription_type")})
	}
	if read("email") == "" && read("email_address") != "" {
		p["email"], _ = json.Marshal(read("email_address"))
	}
	if profile := p["oauthAccount"]; len(profile) > 0 {
		var account struct {
			ID    string `json:"accountUuid"`
			Email string `json:"emailAddress"`
		}
		if json.Unmarshal(profile, &account) == nil {
			if read("account_uuid") == "" {
				p["account_uuid"], _ = json.Marshal(account.ID)
			}
			if read("email") == "" {
				p["email"], _ = json.Marshal(account.Email)
			}
		}
	}
	email = strings.TrimSpace(email)
	if email != "" {
		parsed, e := mail.ParseAddress(email)
		if e != nil || parsed.Address != email || len(email) > 254 {
			return nil, errors.New("请填写有效的账号邮箱")
		}
		if own := read("email"); own != "" && !strings.EqualFold(own, email) {
			return nil, errors.New("填写的邮箱与凭证中的账号不一致")
		}
		p["email"], _ = json.Marshal(email)
	}
	return json.Marshal(p)
}

func (SubscriptionAccountAdapter) MatchAccount(current, incoming accounts.Credential) (accounts.Credential, bool) {
	strong := func(subject string) bool {
		return subject != "" && !strings.HasPrefix(subject, "credential:") && !strings.HasPrefix(subject, "email:")
	}
	if strong(current.Subject) && strong(incoming.Subject) && current.Subject != incoming.Subject {
		return incoming, false
	}
	sameEmail := current.Email != "" && incoming.Email != "" && strings.EqualFold(strings.TrimSpace(current.Email), strings.TrimSpace(incoming.Email))
	sameToken := (current.RefreshToken != "" && current.RefreshToken == incoming.RefreshToken) || (current.AccessToken != "" && current.AccessToken == incoming.AccessToken)
	if !sameEmail && !sameToken {
		return incoming, false
	}
	// Prefer the UUID once known, while preserving the existing row and bindings.
	// Keeping an email identity after a browser login could merge another UUID.
	if strong(current.Subject) || !strong(incoming.Subject) {
		incoming.Subject = current.Subject
	}
	if incoming.Email == "" {
		incoming.Email = current.Email
	}
	return incoming, true
}

func validateClaudeCode(raw, link string) error {
	code, state, ok := strings.Cut(raw, "#")
	if !ok || code == "" || state == "" || strings.ContainsAny(raw, " \t") {
		return errors.New("请粘贴 Claude 页面显示的完整授权码（包含 # 后的部分）")
	}
	if u, e := url.Parse(link); e == nil {
		if expected := u.Query().Get("state"); expected != "" && expected != state {
			return errors.New("授权码与本次登录不匹配")
		}
	}
	return nil
}
