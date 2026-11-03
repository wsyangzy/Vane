package store

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/wsyangzy/Vane/internal/routing"
)

func TestCredentialGroupDeletionChecksEveryRoutingReference(t *testing.T) {
	for _, tc := range []struct {
		name    string
		edit    func(*routing.RoutingProfile)
		blocked bool
	}{
		{"member", func(p *routing.RoutingProfile) {
			p.Members = []routing.RoutingMember{{ChannelID: 7, GroupID: 44, Enabled: true}}
		}, true},
		{"deselected member", func(p *routing.RoutingProfile) { p.Members = []routing.RoutingMember{{ChannelID: 7, GroupID: 44}} }, false},
		{"disabled profile", func(p *routing.RoutingProfile) {
			p.Enabled = false
			p.Members = []routing.RoutingMember{{ChannelID: 7, GroupID: 44, Enabled: true}}
		}, true},
		{"all channel groups", func(p *routing.RoutingProfile) {
			p.Members = []routing.RoutingMember{{ChannelID: 7, GroupID: 0, Enabled: true}}
		}, true},
		{"other channel", func(p *routing.RoutingProfile) { p.Members = []routing.RoutingMember{{ChannelID: 8, GroupID: 0}} }, false},
		{"model target", func(p *routing.RoutingProfile) {
			p.Rules = []routing.RoutingRule{{Kind: "model", Name: "model", MatchModel: "gpt-*", Targets: []routing.RouteTarget{{ChannelID: 7, GroupID: 44}}}}
		}, true},
		{"non-stream channel target", func(p *routing.RoutingProfile) {
			p.Rules = []routing.RoutingRule{{Kind: "non_stream", Name: "non-stream", Targets: []routing.RouteTarget{{ChannelID: 7}}}}
		}, true},
		{"intent target", func(p *routing.RoutingProfile) {
			p.Intents = []routing.IntentRule{{ID: "coding", Name: "coding", Targets: []routing.RouteTarget{{ChannelID: 7, GroupID: 44}}}}
		}, true},
		{"classifier", func(p *routing.RoutingProfile) { p.Classifier = &routing.ClassifierConfig{ChannelID: 7, GroupID: 44} }, true},
		{"cross-provider classifier", func(p *routing.RoutingProfile) {
			p.Provider = "codex"
			p.Classifier = &routing.ClassifierConfig{Provider: "claude", ChannelID: 7, GroupID: 44}
		}, true},
		{"same group ID in another provider", func(p *routing.RoutingProfile) {
			p.Provider = "codex"
			p.Members = []routing.RoutingMember{{ChannelID: 7, GroupID: 44}}
		}, false},
		{"unused group", func(p *routing.RoutingProfile) { p.Members = []routing.RoutingMember{{ChannelID: 7, GroupID: 45}} }, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, db := newProfileTestStore(t)
			ctx := context.Background()
			p := routing.DefaultProfile("claude")
			p.Name = "referencing-profile"
			tc.edit(&p)
			require.NoError(t, s.Save(ctx, &p))
			tx, err := db.BeginTx(ctx, nil)
			require.NoError(t, err)
			err = CheckCredentialGroupReferences(ctx, tx, "claude", 7, 44)
			if tc.blocked {
				require.ErrorContains(t, err, p.Name)
			} else {
				require.NoError(t, err)
			}
			require.NoError(t, tx.Rollback())
			stored, err := s.Get(ctx, p.Provider, p.ID)
			require.NoError(t, err)
			require.Equal(t, p.Revision, stored.Revision)
			require.Equal(t, p.Members, stored.Members)
		})
	}
}

func TestPruneMissingCredentialGroupReferencesPreservesOtherTargetsAndProviders(t *testing.T) {
	s, _ := newProfileTestStore(t)
	ctx := context.Background()
	p := routing.DefaultProfile("claude")
	p.Name = "affected"
	p.Members = []routing.RoutingMember{{ChannelID: 7, GroupID: 44, Enabled: true}, {ChannelID: 7, GroupID: 45, Enabled: true}, {ChannelID: 8, GroupID: 0, Enabled: true}}
	p.Rules = []routing.RoutingRule{
		{Kind: "mapping", Name: "lost mapping", MatchModel: "lost", Enabled: true, Targets: []routing.RouteTarget{{ChannelID: 7, GroupID: 44, UpstreamModel: "upstream"}}},
		{Kind: "model", Name: "mixed", MatchModel: "mixed", Enabled: true, Targets: []routing.RouteTarget{{ChannelID: 7, GroupID: 44}, {ChannelID: 7, GroupID: 45}, {ChannelID: 8}}},
	}
	p.Intents = []routing.IntentRule{{ID: "coding", Name: "coding", Enabled: true, Targets: []routing.RouteTarget{{ChannelID: 7, GroupID: 44}, {ChannelID: 7, GroupID: 45}}}}
	require.NoError(t, s.Save(ctx, &p))
	other := routing.DefaultProfile("codex")
	other.Name = "same group number, different provider"
	other.Members = []routing.RoutingMember{{ChannelID: 7, GroupID: 44, Enabled: true}}
	other.Classifier = &routing.ClassifierConfig{Enabled: true, Provider: "claude", ChannelID: 7, GroupID: 44, Model: "classifier"}
	require.NoError(t, s.Save(ctx, &other))
	unrelated := routing.DefaultProfile("claude")
	unrelated.Name = "unrelated"
	unrelated.Members = []routing.RoutingMember{{ChannelID: 7, GroupID: 45, Enabled: true}}
	require.NoError(t, s.Save(ctx, &unrelated))
	// Profile expansion references must survive cleanup of an individual group.
	p.Rules[1].Targets = append(p.Rules[1].Targets, routing.RouteTarget{ProfileID: unrelated.ID})
	require.NoError(t, s.Save(ctx, &p))
	beforeRevision := p.Revision

	require.NoError(t, s.PruneMissingCredentialGroupReferences(ctx, "claude", routing.Catalog{
		{ID: 7, Groups: []routing.CatalogGroup{{ID: 45}}}, {ID: 8},
	}))

	got, err := s.Get(ctx, "claude", p.ID)
	require.NoError(t, err)
	require.Equal(t, beforeRevision+1, got.Revision)
	require.Equal(t, []int64{45, 0}, []int64{got.Members[0].GroupID, got.Members[1].GroupID})
	require.Equal(t, 0, got.Members[0].Position)
	require.Equal(t, 1, got.Members[1].Position)
	require.Empty(t, got.Rules[0].Targets)
	require.False(t, got.Rules[0].Enabled)
	require.Len(t, got.Rules[1].Targets, 3)
	require.Equal(t, unrelated.ID, got.Rules[1].Targets[2].ProfileID)
	require.Equal(t, []routing.RouteTarget{{ChannelID: 7, GroupID: 45}}, got.Intents[0].Targets)
	otherAfter, err := s.Get(ctx, "codex", other.ID)
	require.NoError(t, err)
	require.Equal(t, int64(44), otherAfter.Members[0].GroupID)
	require.False(t, otherAfter.Classifier.Enabled)
	require.Zero(t, otherAfter.Classifier.GroupID)
	untouched, err := s.Get(ctx, "claude", unrelated.ID)
	require.NoError(t, err)
	require.Equal(t, unrelated.Revision, untouched.Revision)
	stale := p
	require.ErrorIs(t, s.Save(ctx, &stale), ErrProfileConflict)
	require.NoError(t, s.Save(ctx, got))
}

func TestPruneMissingCredentialGroupsKeepsDisabledAndWildcardMembers(t *testing.T) {
	s, _ := newProfileTestStore(t)
	ctx := context.Background()
	p := routing.DefaultProfile("codex")
	p.Name = "legacy orphan"
	p.Members = []routing.RoutingMember{{ChannelID: 3, GroupID: 44}, {ChannelID: 3, GroupID: 45}, {ChannelID: 3, GroupID: 0}}
	require.NoError(t, s.Save(ctx, &p))
	catalog := routing.Catalog{{ID: 3, Groups: []routing.CatalogGroup{{ID: 45}}}}
	require.NoError(t, s.PruneMissingCredentialGroupReferences(ctx, "codex", catalog))
	got, err := s.Get(ctx, "codex", p.ID)
	require.NoError(t, err)
	require.Len(t, got.Members, 2)
	require.Equal(t, int64(45), got.Members[0].GroupID)
	require.False(t, got.Members[0].Enabled)
	require.Zero(t, got.Members[1].GroupID)
	require.NoError(t, s.PruneMissingCredentialGroupReferences(ctx, "codex", catalog))
	again, err := s.Get(ctx, "codex", p.ID)
	require.NoError(t, err)
	require.Equal(t, got.Revision, again.Revision)
}
