package service

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/wsyangzy/Vane/internal/subscriptions/accounts"
)

type SubscriptionAccountAdapter struct{ Client *http.Client }

func (a SubscriptionAccountAdapter) Login() accounts.LoginSpec {
	return accounts.LoginSpec{Command: "codex", Args: []string{"login"}, DeviceArgs: []string{"login", "--device-auth"}, DirectoryEnv: []string{"CODEX_HOME"}, CredentialFile: "auth.json", ConfigFile: "config.toml", ConfigContent: "cli_auth_credentials_store = \"file\"\n", AllowedHosts: []string{"auth.openai.com"}, BrowserLogin: a.browserLogin}
}
func (SubscriptionAccountAdapter) Parse(raw []byte) (accounts.Credential, error) {
	var p struct {
		Type   string `json:"type"`
		Mode   string `json:"auth_mode"`
		Tokens struct {
			Access  string `json:"access_token"`
			Refresh string `json:"refresh_token"`
			ID      string `json:"id_token"`
			Account string `json:"account_id"`
		} `json:"tokens"`
	}
	if json.Unmarshal(raw, &p) != nil {
		return accounts.Credential{}, errors.New("凭证不是有效 JSON")
	}
	if p.Type != "" && p.Type != "codex" {
		return accounts.Credential{}, errors.New("请选择 Codex 订阅凭证")
	}
	if p.Tokens.Access == "" {
		var flat struct {
			Access  string `json:"access_token"`
			Refresh string `json:"refresh_token"`
			ID      string `json:"id_token"`
			Account string `json:"account_id"`
		}
		_ = json.Unmarshal(raw, &flat)
		p.Tokens.Access, p.Tokens.Refresh, p.Tokens.ID, p.Tokens.Account = flat.Access, flat.Refresh, flat.ID, flat.Account
	}
	if (p.Mode != "" && p.Mode != "chatgpt") || !accounts.ValidToken(p.Tokens.Access) {
		return accounts.Credential{}, errors.New("请选择 Codex 的 ChatGPT 登录 auth.json，API Key 登录不属于订阅账号")
	}
	claims := accounts.JWTClaims(p.Tokens.ID)
	if accounts.Claim(claims, "sub") == "" {
		claims = accounts.JWTClaims(p.Tokens.Access)
	}
	auth, _ := claims["https://api.openai.com/auth"].(map[string]any)
	if p.Tokens.Account == "" {
		p.Tokens.Account = accounts.Claim(auth, "chatgpt_account_id")
	}
	sub := accounts.Claim(claims, "sub")
	if sub == "" {
		sub = accounts.Claim(auth, "chatgpt_user_id")
	}
	if sub == "" || !accounts.ValidToken(p.Tokens.Account) {
		return accounts.Credential{}, errors.New("Codex 凭证缺少 ChatGPT 账号身份，请重新登录")
	}
	if !accounts.ValidToken(p.Tokens.Refresh) {
		return accounts.Credential{}, errors.New("Codex 凭证缺少有效的 refresh token，请重新登录或选择完整凭证")
	}
	return accounts.Credential{AccessToken: p.Tokens.Access, RefreshToken: p.Tokens.Refresh, IDToken: p.Tokens.ID, AccountID: p.Tokens.Account, Subject: sub, Email: accounts.Claim(claims, "email"), Plan: accounts.Claim(auth, "chatgpt_plan_type"), ExpiresAt: accounts.TokenExpiry(p.Tokens.Access)}, nil
}
func (a SubscriptionAccountAdapter) Refresh(ctx context.Context, c accounts.Credential) (accounts.Credential, error) {
	if c.RefreshToken == "" {
		return accounts.Credential{}, accounts.ErrSignInRequired
	}
	fresh, err := accounts.Exchange(ctx, a.Client, "https://auth.openai.com/oauth/token", map[string]any{"client_id": "app_EMoamEEZ73f0CkXaXp7hrann", "grant_type": "refresh_token", "refresh_token": c.RefreshToken, "scope": "openid profile email"})
	if err != nil {
		return accounts.Credential{}, err
	}
	c.AccessToken = fresh.AccessToken
	if fresh.RefreshToken != "" {
		c.RefreshToken = fresh.RefreshToken
	}
	if fresh.IDToken != "" {
		c.IDToken = fresh.IDToken
		claims := accounts.JWTClaims(fresh.IDToken)
		auth, _ := claims["https://api.openai.com/auth"].(map[string]any)
		if sub := accounts.Claim(claims, "sub"); sub != "" && sub != c.Subject {
			return accounts.Credential{}, errors.New("subscription_refresh_identity_changed")
		}
		if id := accounts.Claim(auth, "chatgpt_account_id"); id != "" && id != c.AccountID {
			return accounts.Credential{}, errors.New("subscription_refresh_identity_changed")
		}
		if plan := accounts.Claim(auth, "chatgpt_plan_type"); plan != "" {
			c.Plan = plan
		}
	}
	c.ExpiresAt = accounts.TokenExpiry(c.AccessToken)
	if c.ExpiresAt.IsZero() && fresh.ExpiresIn > 0 {
		c.ExpiresAt = time.Now().Add(time.Duration(fresh.ExpiresIn) * time.Second)
	}
	return c, nil
}
