package subscription

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/wsyangzy/Vane/internal/routing"
)

// Recovery reads availability for a selected resource without resolving or
// materializing credentials. A quota-blocked plan may have no eligible rows.
func (c *Controller) ProfileRecoveryTime(ctx context.Context, channel, group int64, model string) (time.Time, bool) {
	plan := routing.ProfileRequestFromContext(ctx)
	if plan == nil || plan.Preview || plan.Profile.Provider != "claude" {
		return time.Time{}, false
	}
	authorized := false
	for _, evaluation := range plan.Evaluations {
		candidate := evaluation.Candidate
		if candidate.Provider == "claude" && candidate.ChannelID == channel && candidate.GroupID == group {
			authorized = true
			break
		}
	}
	if !authorized {
		return time.Time{}, false
	}
	bindings, err := c.store.ListBindings(ctx)
	if err != nil {
		return time.Time{}, false
	}
	for _, binding := range bindings {
		if binding.GroupID == group && binding.Enabled {
			return c.RecoveryTime(ctx, binding, model)
		}
	}
	return time.Time{}, false
}

func (c *Controller) ProfileBinding(ctx context.Context, channel, group int64) (Binding, error) {
	plan := routing.ProfileRequestFromContext(ctx)
	if plan == nil || plan.Preview || plan.Error != "" || plan.Profile.Provider != "claude" {
		return Binding{}, errors.New("profile_authorization_required")
	}
	channelName := ""
	for _, candidate := range plan.EligibleCandidates {
		if candidate.Provider == "claude" && candidate.ChannelID == channel && candidate.GroupID == group {
			channelName = candidate.Identity().Channel
			break
		}
	}
	if channelName == "" {
		return Binding{}, errors.New("profile_candidate_not_authorized")
	}
	bindings, err := c.store.ListBindings(ctx)
	if err != nil {
		return Binding{}, errors.New("subscription_state_unavailable")
	}
	for _, b := range bindings {
		if b.GroupID == group && b.Enabled {
			return b, nil
		}
	}
	return Binding{}, errors.New("subscription_binding_unavailable")
}

func (c *Controller) SelectProfileAccount(ctx context.Context, channel, group int64, headers http.Header) (context.Context, error) {
	binding, err := c.ProfileBinding(ctx, channel, group)
	if err != nil {
		return ctx, err
	}
	authorization := ""
	if binding.AccountID != "" {
		if c.resolveAccount == nil {
			return ctx, errors.New("subscription_state_unavailable")
		}
		token, _, err := c.resolveAccount(ctx, binding.AccountID)
		if err != nil || strings.TrimSpace(token) == "" {
			return ctx, errors.New("subscription_authentication_required")
		}
		authorization = "Bearer " + token
	} else {
		var ok bool
		authorization, ok = SingleHeader(headers, "Authorization")
		if !ok || !strings.HasPrefix(authorization, "Bearer ") || strings.TrimSpace(strings.TrimPrefix(authorization, "Bearer ")) == "" {
			return ctx, errors.New("subscription_authentication_required")
		}
	}
	if strings.ContainsAny(authorization, "\r\n\x00") {
		return ctx, errors.New("subscription_authentication_rejected")
	}
	return context.WithValue(ctx, contextKey{}, &Request{Binding: binding, authorization: authorization}), nil
}
