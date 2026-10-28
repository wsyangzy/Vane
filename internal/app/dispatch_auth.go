package app

import (
	"context"
	"crypto/subtle"
	"errors"
	"net/http"
	"strings"

	"github.com/wsyangzy/Vane/internal/routing"
)

func (a *App) authenticateRoutingProfile(r *http.Request, provider string) (context.Context, bool, error) {
	a.mu.RLock()
	cfg := a.config.Auth
	a.mu.RUnlock()
	alias := r.Header.Get("X-Relay-Key")
	auth := r.Header.Get("Authorization")
	token := alias
	if token == "" && strings.HasPrefix(auth, "Bearer ") {
		token = strings.TrimPrefix(auth, "Bearer ")
	}
	if cfg.Enabled && (cfg.Token == "" || subtle.ConstantTimeCompare([]byte(token), []byte(cfg.Token)) != 1) {
		return r.Context(), true, errors.New("application_auth_required")
	}
	// An application token is never an upstream credential. With the alias,
	// a separate client Authorization header can carry passthrough credentials.
	if cfg.Token != "" && strings.HasPrefix(auth, "Bearer ") && subtle.ConstantTimeCompare([]byte(strings.TrimPrefix(auth, "Bearer ")), []byte(cfg.Token)) == 1 {
		r.Header.Del("Authorization")
	}
	r.Header.Del("X-Relay-Key")
	return routing.WithCallerScope(r.Context(), "profile:"+provider), true, nil
}
