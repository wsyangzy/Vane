// Package subscription owns Claude client authentication and request-scoped
// subscription availability. It never opens client credential files or refreshes OAuth.
package subscription

import (
	"context"
	"errors"
	"time"

	"github.com/wsyangzy/Vane/internal/routing"
)

const (
	GroupCredential   = "group_credential"
	ClientPassthrough = "client_passthrough"
	OfficialOrigin    = "https://api.anthropic.com"
)

// One binding belongs to one credential group and one independent caller.
// KeyHash is SHA-256 of a cryptographically random local key, never OAuth.
type Binding struct {
	AccountID     string           `json:"account_id,omitempty"`
	GroupID       int64            `json:"group_id"`
	Channel       string           `json:"channel"`
	Version       int64            `json:"version"`
	Enabled       bool             `json:"enabled"`
	KeyHash       string           `json:"key_hash,omitempty"`
	BackupTargets []routing.Target `json:"backup_targets"`
	Action        string           `json:"action,omitempty"`
}

type Pause struct {
	GroupID         int64     `json:"group_id"`
	Version         int64     `json:"version"`
	Model           string    `json:"model"`
	Reason          string    `json:"reason"`
	Source          string    `json:"source"`
	ObservedAt      time.Time `json:"observed_at"`
	ResumeNotBefore time.Time `json:"resume_not_before"`
	ResetKnown      bool      `json:"reset_known"`
	State           string    `json:"state"`
	Revision        uint64    `json:"revision"`
}

type Store interface {
	ListBindings(context.Context) ([]Binding, error)
	ListPauses(context.Context) ([]Pause, error)
	PutPause(context.Context, Pause) error
	DeletePause(context.Context, Pause) error
}

type contextKey struct{}
type Request struct {
	Binding               Binding
	credentialUnavailable bool
	authorization         string
}

func FromContext(ctx context.Context) *Request {
	r, _ := ctx.Value(contextKey{}).(*Request)
	return r
}

var ErrUnavailable = errors.New("subscription_unavailable")
var ErrBindingChanged = errors.New("subscription_binding_changed")

func CallerScope(b Binding) string { return callerID(b.GroupID, b.Version) }
