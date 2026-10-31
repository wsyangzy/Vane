package app

import (
	"context"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
	claudeservice "github.com/wsyangzy/Vane/internal/providers/claude/service"
	claudestore "github.com/wsyangzy/Vane/internal/providers/claude/store"
	codexstore "github.com/wsyangzy/Vane/internal/providers/codex/store"
	"github.com/wsyangzy/Vane/internal/routing"
)

func TestCredentialGroupDeletionRemovesProfileReferencesAtomically(t *testing.T) {
	for _, provider := range []string{"claude", "codex"} {
		for _, failCleanup := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/rollback=%t", provider, failCleanup), func(t *testing.T) {
				a, members := dispatchTestApp(t, provider)
				ctx := context.Background()
				var groupID int64
				var endpointID int64
				if provider == "claude" {
					a.claudeCredentialGroupService = claudeservice.NewClaudeCredentialGroupService(a.claudeCredentialGroupStore, a.channelStore, a.endpointStore)
					group, err := a.claudeCredentialGroupStore.Create(ctx, &claudestore.ClaudeCredentialGroupRecord{Channel: "first", Name: "delete-me", ApiKey: "test-key"})
					require.NoError(t, err)
					groupID = group.ID
				} else {
					group, err := a.codexCredentialGroupStore.Create(ctx, &codexstore.CodexCredentialGroupRecord{ChannelID: members[0].ChannelID, Name: "delete-me", ApiKey: "test-key"})
					require.NoError(t, err)
					groupID = group.ID
					endpoint, err := a.codexEndpointStore.Create(ctx, &codexstore.CodexEndpointRecord{ChannelID: members[0].ChannelID, CredentialGroupID: groupID, Name: "inactive-child", BaseURL: "https://inactive.invalid/v1", FailoverEnabled: false})
					require.NoError(t, err)
					endpointID = endpoint.ID
				}
				p := routing.DefaultProfile(provider)
				p.Name = "delete-target"
				p.Members = []routing.RoutingMember{{ChannelID: members[0].ChannelID, GroupID: groupID, Enabled: true}, {ChannelID: members[0].ChannelID, GroupID: members[0].GroupID, Enabled: true}}
				p, err := a.SaveRoutingProfile(p)
				require.NoError(t, err)
				if failCleanup {
					_, err := a.storeDB.Exec(`CREATE TRIGGER fail_profile_cleanup BEFORE UPDATE ON routing_profiles WHEN OLD.name='delete-target' BEGIN SELECT RAISE(ABORT,'cleanup failed'); END`)
					require.NoError(t, err)
				}
				if provider == "claude" {
					err = a.DeleteClaudeCredentialGroupByID(groupID)
					group, readErr := a.claudeCredentialGroupStore.GetByID(ctx, groupID)
					require.NoError(t, readErr)
					require.Equal(t, failCleanup, group != nil)
				} else {
					err = a.DeleteCodexCredentialGroupByID(groupID)
					group, readErr := a.codexCredentialGroupStore.GetByID(ctx, groupID)
					require.NoError(t, readErr)
					require.Equal(t, failCleanup, group != nil)
					endpoint, readErr := a.codexEndpointStore.GetByID(ctx, endpointID)
					require.NoError(t, readErr)
					require.Equal(t, failCleanup, endpoint != nil)
				}
				s, storeErr := a.profileStore(ctx)
				require.NoError(t, storeErr)
				stored, storeErr := s.Get(ctx, provider, p.ID)
				require.NoError(t, storeErr)
				if failCleanup {
					require.ErrorContains(t, err, "cleanup failed")
					require.Len(t, stored.Members, 2)
					require.Equal(t, p.Revision, stored.Revision)
				} else {
					require.NoError(t, err)
					require.Len(t, stored.Members, 1)
					require.Equal(t, members[0].GroupID, stored.Members[0].GroupID)
					_, err = a.SaveRoutingProfile(*stored)
					require.NoError(t, err)
				}
			})
		}
	}
}

func TestListRoutingProfilesRepairsPreviouslyDeletedCredentialGroups(t *testing.T) {
	for _, provider := range []string{"claude", "codex"} {
		t.Run(provider, func(t *testing.T) {
			a, members := dispatchTestApp(t, provider)
			ctx := context.Background()
			s, err := a.profileStore(ctx)
			require.NoError(t, err)
			p := routing.DefaultProfile(provider)
			p.Name = "old-build-orphan"
			p.Members = []routing.RoutingMember{{ChannelID: members[0].ChannelID, GroupID: members[0].GroupID, Enabled: true}, {ChannelID: members[0].ChannelID, GroupID: 999999, Enabled: false}}
			require.NoError(t, s.Save(ctx, &p))
			state, err := a.ListRoutingProfiles(provider)
			require.NoError(t, err)
			for _, profile := range state.Profiles {
				if profile.ID == p.ID {
					require.Len(t, profile.Members, 1)
					_, err := a.SaveRoutingProfile(profile)
					require.NoError(t, err)
					return
				}
			}
			t.Fatal("repaired profile missing")
		})
	}
}
