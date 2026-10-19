package middleware

import (
	"ai-relay-forwarder/config"
	"context"
	"crypto/subtle"
	"net/http"
	"strings"
	"sync"
)

type AuthMiddleware struct {
	mu                   sync.RWMutex
	config               config.AuthConfig
	requestAuthenticator func(*http.Request) (context.Context, bool, error)
}

// SetRequestAuthenticator installs a provider-owned independent caller check.
// A handled error is always denied, including when legacy auth is disabled.
func (am *AuthMiddleware) SetRequestAuthenticator(auth func(*http.Request) (context.Context, bool, error)) {
	am.mu.Lock()
	defer am.mu.Unlock()
	am.requestAuthenticator = auth
}

func NewAuthMiddleware(cfg config.AuthConfig) *AuthMiddleware {
	return &AuthMiddleware{
		config: cfg,
	}
}

func (am *AuthMiddleware) Wrap(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		am.mu.RLock()
		cfg := am.config
		authenticate := am.requestAuthenticator
		am.mu.RUnlock()
		if authenticate != nil {
			ctx, handled, err := authenticate(r)
			if handled {
				if err != nil {
					http.Error(w, "Relay authentication rejected", http.StatusUnauthorized)
					return
				}
				next.ServeHTTP(w, r.WithContext(ctx))
				return
			}
		}
		if !cfg.Enabled {
			next.ServeHTTP(w, r)
			return
		}

		auth := r.Header.Get("Authorization")
		if auth == "" {
			http.Error(w, "Authorization header required", http.StatusUnauthorized)
			return
		}

		if !strings.HasPrefix(auth, "Bearer ") {
			http.Error(w, "Invalid authorization format. Expected 'Bearer <token>'", http.StatusUnauthorized)
			return
		}

		token := strings.TrimPrefix(auth, "Bearer ")
		// 使用 constant-time 比较防止 timing attack
		if subtle.ConstantTimeCompare([]byte(token), []byte(cfg.Token)) != 1 {
			http.Error(w, "Invalid token", http.StatusUnauthorized)
			return
		}

		next.ServeHTTP(w, r)
	})
}

// UpdateConfig updates the auth middleware configuration
func (am *AuthMiddleware) UpdateConfig(cfg config.AuthConfig) {
	am.mu.Lock()
	defer am.mu.Unlock()
	am.config = cfg
}
