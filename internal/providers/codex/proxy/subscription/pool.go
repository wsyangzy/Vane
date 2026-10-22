package subscription

import (
	"context"
	"errors"
	"github.com/wsyangzy/Vane/internal/routing"
)

func (c *Controller) SetPoolRuntime(runtime *routing.PoolRuntime) { c.poolRuntime.Store(runtime) }
func (c *Controller) PoolBinding(ctx context.Context, channel, group int64) (Binding, error) {
	caller, ok := routing.PoolCallerFromContext(ctx)
	runtime := c.poolRuntime.Load()
	if !ok || runtime == nil || !runtime.ValidCaller(caller) {
		return Binding{}, errors.New("pool_authorization_required")
	}
	account, ok := runtime.Account(channel, group)
	if !ok || !account.Enabled || !account.Managed {
		return Binding{}, errors.New("managed_account_not_authorized")
	}
	bindings, err := c.store.ListBindings(ctx)
	if err != nil {
		return Binding{}, err
	}
	for _, b := range bindings {
		if b.ChannelID == channel && b.GroupID == group && b.AccountID != "" && b.Enabled {
			return b, nil
		}
	}
	return Binding{}, errors.New("subscription_binding_unavailable")
}
func (c *Controller) SelectPoolAccount(ctx context.Context, channel, group int64) (context.Context, error) {
	b, err := c.PoolBinding(ctx, channel, group)
	if err != nil {
		return ctx, err
	}
	if c.resolveAccount == nil {
		return ctx, errors.New("subscription_state_unavailable")
	}
	token, account, err := c.resolveAccount(ctx, b.AccountID)
	if err != nil {
		return ctx, errors.New("subscription_authentication_required")
	}
	_ = account
	return context.WithValue(ctx, contextKey{}, &Request{Binding: b, authorization: "Bearer " + token, accountID: account}), nil
}
