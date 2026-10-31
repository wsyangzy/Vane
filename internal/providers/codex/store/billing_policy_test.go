package store

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCredentialGroupBillingPolicyRoundTrip(t *testing.T) {
	db := newCodexSubscriptionTestDB(t)
	defer db.Close()
	s := NewSQLiteCodexCredentialGroupStore(db)
	ctx := context.Background()
	created, err := s.Create(ctx, &CodexCredentialGroupRecord{ChannelID: 1, Name: "billing", ServiceTierSource: "request", CacheWriteTTLPricing: true})
	require.NoError(t, err)
	loaded, err := s.GetByID(ctx, created.ID)
	require.NoError(t, err)
	require.Equal(t, "request", loaded.ServiceTierSource)
	require.True(t, loaded.CacheWriteTTLPricing)
	loaded.ServiceTierSource, loaded.CacheWriteTTLPricing = "response", false
	loaded.BillingRules = []CodexBillingRuleRecord{{Mode: "fixed", GroupMultiplierMode: "inherit", InputPrice: 2}, {Mode: "fixed", ServiceTier: "fast", GroupMultiplierMode: "override", InputPrice: 3}}
	require.NoError(t, s.Update(ctx, loaded))
	loaded, err = s.GetByID(ctx, loaded.ID)
	require.NoError(t, err)
	require.Equal(t, "response", loaded.ServiceTierSource)
	require.False(t, loaded.CacheWriteTTLPricing)
	require.Equal(t, "inherit", loaded.BillingRules[0].GroupMultiplierMode)
	require.Equal(t, "override", loaded.BillingRules[1].GroupMultiplierMode)
}
