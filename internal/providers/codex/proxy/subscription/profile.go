package subscription

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/wsyangzy/Vane/internal/routing"
)

// ProfileRecoveryTime reads availability without resolving credentials. A
// quota-blocked plan may have evaluations but no eligible candidate yet.
func (c *Controller) ProfileRecoveryTime(ctx context.Context, channel, group int64, model string) (time.Time, bool) {
	plan := routing.ProfileRequestFromContext(ctx)
	if plan == nil || plan.Preview || plan.Profile.Provider != "codex" {
		return time.Time{}, false
	}
	authorized := false
	for _, evaluation := range plan.Evaluations {
		candidate := evaluation.Candidate
		if candidate.Provider == "codex" && candidate.ChannelID == channel && candidate.GroupID == group {
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
		if binding.ChannelID == channel && binding.GroupID == group && binding.Enabled {
			return c.RecoveryTime(ctx, binding, model)
		}
	}
	return time.Time{}, false
}

// ProfileBinding authorizes the selected resource from the authenticated
// request plan. New routing never reads a legacy pool or binding access key.
func (c *Controller) ProfileBinding(ctx context.Context, channel, group int64) (Binding, error) {
	plan := routing.ProfileRequestFromContext(ctx)
	if plan == nil || plan.Preview || plan.Error != "" || plan.Profile.Provider != "codex" {
		return Binding{}, errors.New("profile_authorization_required")
	}
	authorized := false
	for _, candidate := range plan.EligibleCandidates {
		if candidate.Provider == "codex" && candidate.ChannelID == channel && candidate.GroupID == group {
			authorized = true
			break
		}
	}
	if !authorized {
		return Binding{}, errors.New("profile_candidate_not_authorized")
	}
	bindings, err := c.store.ListBindings(ctx)
	if err != nil {
		return Binding{}, errors.New("subscription_state_unavailable")
	}
	for _, binding := range bindings {
		if binding.ChannelID == channel && binding.GroupID == group && binding.Enabled {
			return binding, nil
		}
	}
	return Binding{}, errors.New("subscription_binding_unavailable")
}

func (c *Controller) SelectProfileAccount(ctx context.Context, channel, group int64, headers http.Header) (context.Context, error) {
	binding, err := c.ProfileBinding(ctx, channel, group)
	if err != nil {
		return ctx, err
	}
	authorization, account := "", ""
	if binding.AccountID != "" {
		if c.resolveAccount == nil {
			return ctx, errors.New("subscription_state_unavailable")
		}
		token, accountID, err := c.resolveAccount(ctx, binding.AccountID)
		if err != nil || strings.TrimSpace(token) == "" {
			return ctx, errors.New("subscription_authentication_required")
		}
		authorization, account = "Bearer "+token, accountID
	} else {
		var ok bool
		authorization, ok = SingleHeader(headers, "Authorization")
		if !ok || !strings.HasPrefix(authorization, "Bearer ") || strings.TrimSpace(strings.TrimPrefix(authorization, "Bearer ")) == "" {
			return ctx, errors.New("subscription_authentication_required")
		}
		var accountOK bool
		account, accountOK = SingleHeader(headers, "ChatGPT-Account-ID")
		for name := range headers {
			if strings.EqualFold(name, "ChatGPT-Account-ID") && !accountOK {
				return ctx, errors.New("subscription_account_header_rejected")
			}
		}
	}
	if strings.ContainsAny(authorization+account, "\r\n\x00") {
		return ctx, errors.New("subscription_authentication_rejected")
	}
	return context.WithValue(ctx, contextKey{}, &Request{Binding: binding, authorization: authorization, accountID: account}), nil
}
