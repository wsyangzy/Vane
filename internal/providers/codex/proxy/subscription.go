package codexproxy

import (
	"context"
	"errors"
	"math"
	"net/http"
	"strconv"
	"time"

	"ai-relay-forwarder/internal/bodyrewrite"
	"ai-relay-forwarder/internal/providers/codex/proxy/subscription"
	"ai-relay-forwarder/internal/routing"
)

func (h *HTTPHandler) SetSubscriptionController(c *subscription.Controller) {
	h.handler.subscriptions = c
}

type subscriptionAdmissionKey struct{}

func leaseFromContext(ctx context.Context) *subscription.Lease {
	l, _ := ctx.Value(subscriptionAdmissionKey{}).(*subscription.Lease)
	return l
}

func (h *Handler) admitSubscription(r *http.Request, ep *CodexEndpoint, body []byte) (*http.Request, *subscription.Lease, error) {
	if ep.AuthMode != subscription.ClientPassthrough {
		return r, nil, nil
	}
	model, _ := bodyrewrite.LookupTopLevelString(body, "model")
	if ok, reason := h.subscriptionEligibility(r.Context(), ep, model); !ok {
		return r, nil, errors.New(reason)
	}
	sub := subscription.FromContext(r.Context())
	lease, e := h.subscriptions.Acquire(r.Context(), sub.Binding, model)
	if e != nil {
		return r, nil, e
	}
	return r.WithContext(context.WithValue(r.Context(), subscriptionAdmissionKey{}, lease)), lease, nil
}

func (h *Handler) subscriptionEligibility(ctx context.Context, ep *CodexEndpoint, model string) (bool, string) {
	r := subscription.FromContext(ctx)
	if r != nil && (h.subscriptions == nil || !h.subscriptions.Valid(ctx, r.Binding)) {
		return false, "subscription_binding_changed"
	}
	if ep.AuthMode != subscription.ClientPassthrough {
		if r == nil {
			return true, ""
		}
		for _, t := range r.Binding.BackupTargets {
			if t.ChannelID == ep.ChannelID && t.GroupID == ep.CredentialGroupID && (t.UpstreamModel == "" || t.UpstreamModel == model) {
				return true, ""
			}
		}
		return false, "subscription_backup_not_authorized"
	}
	if r == nil || r.Binding.GroupID != ep.CredentialGroupID {
		return false, "subscription_caller_required"
	}
	if h.subscriptions == nil {
		return false, "subscription_state_unavailable"
	}
	return h.subscriptions.Eligible(ctx, r.Binding, model)
}

func (h *Handler) planSubscriptionRoute(r *http.Request, body []byte, streaming bool) *codexRoute {
	b := subscription.FromContext(r.Context()).Binding
	source := h.manager.ChannelByID(b.ChannelID)
	route := &codexRoute{kind: routing.KindSubscription}
	decision := routing.Decision{Kind: routing.KindSubscription, Mode: routing.ModeFallback}
	model, _ := bodyrewrite.LookupTopLevelString(body, "model")
	if source != nil && subscription.Path(r) == "/responses" {
		if selected := routing.Decide(h.manager.ModelRules(source.ID), source.NonStreamRoute, model, streaming); selected.Routed() {
			decision = selected
			route.kind = selected.Kind
		}
	}
	route.snapshot = routing.NewSnapshot(decision, b.Channel)
	add := func(channelID, groupID int64, model string, index int) {
		ch := h.manager.ChannelByID(channelID)
		if ch == nil {
			return
		}
		route.stages = append(route.stages, codexRouteStage{channel: ch, groupID: groupID, upstreamModel: model, index: index, snapshotJSON: route.snapshot.JSON(index), subscriptionStatus: func() *routing.SubscriptionSnapshot { return h.subscriptionSnapshot(r.Context(), b) }})
	}
	if route.kind != routing.KindSubscription {
		for _, stage := range decision.Stages() {
			if stage.Target == nil {
				add(b.ChannelID, b.GroupID, "", 0)
				continue
			}
			for _, allowed := range b.BackupTargets {
				t := stage.Target
				if allowed.ChannelID == t.ChannelID && (t.GroupID == 0 || allowed.GroupID == t.GroupID) && (allowed.UpstreamModel == "" || allowed.UpstreamModel == t.UpstreamModel) {
					add(allowed.ChannelID, allowed.GroupID, t.UpstreamModel, stage.Index)
				}
			}
		}
	} else {
		add(b.ChannelID, b.GroupID, "", 0)
		// Models and native compaction stay on the subscribed backend. A generic
		// API model catalogue cannot replace the official Codex catalogue.
		if subscription.Path(r) == "/responses" {
			for i, t := range b.BackupTargets {
				add(t.ChannelID, t.GroupID, t.UpstreamModel, i+1)
			}
		}
	}
	return route
}

func (h *Handler) subscriptionSnapshot(ctx context.Context, b subscription.Binding) *routing.SubscriptionSnapshot {
	s := &routing.SubscriptionSnapshot{GroupID: b.GroupID, BindingVersion: b.Version, OfficialEnabled: b.Enabled, OfficialVerification: "not_verified"}
	if h.subscriptions != nil {
		for _, p := range h.subscriptions.Status(ctx, b) {
			s.Availability = append(s.Availability, routing.AvailabilitySnapshot{Model: p.Model, State: p.State, Reason: p.Reason, Source: p.Source, ResumeNotBefore: p.ResumeNotBefore.Format(time.RFC3339), ResetKnown: p.ResetKnown})
		}
	}
	return s
}
func (h *Handler) subscriptionRetryAfter(ctx context.Context, b subscription.Binding) string {
	seconds := 60.0
	if h.subscriptions != nil {
		for _, p := range h.subscriptions.Status(ctx, b) {
			if wait := time.Until(p.ResumeNotBefore).Seconds(); wait > 0 && wait < seconds {
				seconds = wait
			}
		}
	}
	return strconv.Itoa(int(math.Max(1, math.Ceil(seconds))))
}
