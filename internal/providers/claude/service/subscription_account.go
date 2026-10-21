package service

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/wsyangzy/Vane/internal/subscriptions/accounts"
)

type SubscriptionAccountAdapter struct{ Client *http.Client }

func (SubscriptionAccountAdapter) Login() accounts.LoginSpec {
	return accounts.LoginSpec{Command: "claude", Args: []string{"auth", "login", "--claudeai"}, DirectoryEnv: []string{"CLAUDE_CONFIG_DIR", "CLAUDE_SECURESTORAGE_CONFIG_DIR"}, CredentialFile: ".credentials.json", ProfileFile: ".claude.json", PrepareImport: prepareClaudeLoginImport, AllowedHosts: []string{"claude.com", "claude.ai", "platform.claude.com", "console.anthropic.com"}, CaptureBrowser: true, ValidateCode: validateClaudeCode}
}

func (SubscriptionAccountAdapter) Parse(raw []byte) (accounts.Credential, error) {
	prepared, err := PrepareSubscriptionImport(raw, "")
	if err != nil {
		return accounts.Credential{}, err
	}
	defer clear(prepared)
	var p struct {
		OAuth struct {
			Access  string   `json:"accessToken"`
			Refresh string   `json:"refreshToken"`
			Expiry  int64    `json:"expiresAt"`
			Scopes  []string `json:"scopes"`
			Plan    string   `json:"subscriptionType"`
		} `json:"claudeAiOauth"`
		Email   string `json:"email"`
		Subject string `json:"account_uuid"`
	}
	if json.Unmarshal(prepared, &p) != nil || !accounts.ValidToken(p.OAuth.Access) {
		return accounts.Credential{}, errors.New("请选择 Claude Code 的 .credentials.json 登录凭证")
	}
	if !accounts.ValidToken(p.OAuth.Refresh) {
		return accounts.Credential{}, errors.New("Claude 凭证缺少有效的 refresh token，请重新登录或选择完整凭证")
	}
	if p.Subject == "" {
		if p.Email == "" {
			return accounts.Credential{}, errors.New("Claude 凭证缺少账号身份，请填写账号邮箱，或改用浏览器登录")
		}
		p.Subject = "email:" + strings.ToLower(strings.TrimSpace(p.Email))
	}
	if !accounts.ValidToken(p.Subject) || len(p.Subject) > 512 {
		return accounts.Credential{}, errors.New("invalid_subscription_identity")
	}
	var expiry time.Time
	if p.OAuth.Expiry > 0 {
		expiry = time.UnixMilli(p.OAuth.Expiry)
	}
	return accounts.Credential{AccessToken: p.OAuth.Access, RefreshToken: p.OAuth.Refresh, ExpiresAt: expiry, Subject: p.Subject, Email: p.Email, Plan: p.OAuth.Plan, Scopes: p.OAuth.Scopes}, nil
}

func (a SubscriptionAccountAdapter) Refresh(ctx context.Context, c accounts.Credential) (accounts.Credential, error) {
	if c.RefreshToken == "" {
		return accounts.Credential{}, accounts.ErrSignInRequired
	}
	// Endpoint and public client ID verified against installed Claude Code
	// 2.1.293. They are never supplied by an imported credential file.
	payload := map[string]any{"grant_type": "refresh_token", "refresh_token": c.RefreshToken, "client_id": "9d1c250a-e61b-44d9-88ed-5944d1962f5e"}
	if len(c.Scopes) > 0 {
		payload["scope"] = strings.Join(c.Scopes, " ")
	}
	fresh, err := accounts.Exchange(ctx, a.Client, "https://platform.claude.com/v1/oauth/token", payload)
	if err != nil {
		return accounts.Credential{}, err
	}
	c.AccessToken = fresh.AccessToken
	if fresh.RefreshToken != "" {
		c.RefreshToken = fresh.RefreshToken
	}
	if fresh.ExpiresIn > 0 {
		c.ExpiresAt = time.Now().Add(time.Duration(fresh.ExpiresIn) * time.Second)
	} else {
		c.ExpiresAt = accounts.TokenExpiry(c.AccessToken)
	}
	if fresh.Scope != "" {
		c.Scopes = strings.Fields(fresh.Scope)
	}
	return c, nil
}

func prepareClaudeLoginImport(credential, profile []byte) ([]byte, error) {
	var raw map[string]any
	if err := json.Unmarshal(credential, &raw); err != nil {
		return nil, errors.New("invalid_login_credential")
	}
	var p struct {
		Account struct {
			ID    string `json:"accountUuid"`
			Email string `json:"emailAddress"`
		} `json:"oauthAccount"`
	}
	if json.Unmarshal(profile, &p) == nil {
		if p.Account.ID != "" {
			raw["account_uuid"] = p.Account.ID
		}
		if p.Account.Email != "" {
			raw["email"] = p.Account.Email
		}
	}
	return json.Marshal(raw)
}
