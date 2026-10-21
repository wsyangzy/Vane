package service

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/url"

	"github.com/wsyangzy/Vane/internal/subscriptions/accounts"
)

const codexOAuthClient = "app_EMoamEEZ73f0CkXaXp7hrann"

func (a SubscriptionAccountAdapter) browserLogin() (accounts.BrowserLogin, error) {
	verifierBytes, stateBytes := make([]byte, 48), make([]byte, 24)
	if _, err := rand.Read(verifierBytes); err != nil {
		return accounts.BrowserLogin{}, err
	}
	if _, err := rand.Read(stateBytes); err != nil {
		return accounts.BrowserLogin{}, err
	}
	verifier, state := base64.RawURLEncoding.EncodeToString(verifierBytes), base64.RawURLEncoding.EncodeToString(stateBytes)
	challenge := sha256.Sum256([]byte(verifier))
	redirect := "http://localhost:1455/auth/callback"
	query := url.Values{
		"response_type": {"code"}, "client_id": {codexOAuthClient}, "redirect_uri": {redirect},
		"scope":          {"openid profile email offline_access api.connectors.read api.connectors.invoke"},
		"code_challenge": {base64.RawURLEncoding.EncodeToString(challenge[:])}, "code_challenge_method": {"S256"},
		"state": {state}, "id_token_add_organizations": {"true"}, "codex_cli_simplified_flow": {"true"}, "originator": {"codex_cli_rs"},
	}
	return accounts.BrowserLogin{URL: "https://auth.openai.com/oauth/authorize?" + query.Encode(), RedirectURL: redirect, State: state,
		Exchange: func(ctx context.Context, code string) ([]byte, error) {
			form := url.Values{"grant_type": {"authorization_code"}, "client_id": {codexOAuthClient}, "redirect_uri": {redirect}, "code": {code}, "code_verifier": {verifier}}
			tokens, err := accounts.ExchangeForm(ctx, a.Client, "https://auth.openai.com/oauth/token", form)
			if err != nil {
				return nil, err
			}
			if tokens.IDToken == "" || tokens.RefreshToken == "" {
				return nil, errors.New("incomplete_oauth_credentials")
			}
			return json.Marshal(map[string]any{"auth_mode": "chatgpt", "tokens": tokens})
		}}, nil
}
