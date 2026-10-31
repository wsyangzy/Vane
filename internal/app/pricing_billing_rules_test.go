package app

import (
	"testing"

	"github.com/stretchr/testify/require"
	codexstore "github.com/wsyangzy/Vane/internal/providers/codex/store"
)

func TestCodexBillingRuleMappingPreservesGroupRelationship(t *testing.T) {
	rules := codexBillingRulesToTracking([]codexstore.CodexBillingRuleRecord{
		{Mode: "multiplier", GroupMultiplierMode: "inherit", CostMultiplier: 1.25},
		{Mode: "fixed", GroupMultiplierMode: "override", InputPrice: 4},
	})
	require.Equal(t, "inherit", rules[0].GroupMultiplierMode)
	require.Equal(t, 1.25, rules[0].CostMultiplier)
	require.Equal(t, "override", rules[1].GroupMultiplierMode)
	require.Equal(t, 4.0, rules[1].InputPrice)
}
