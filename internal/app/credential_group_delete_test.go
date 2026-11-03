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

func TestCredentialGroupDeletionUsesRoutingReferencesAndRollsBackResources(t *testing.T) {
	for _, provider := range []string{"claude", "codex"} {
		for _, outcome := range []string{"delete", "referenced", "deselected", "rollback", "cleanupRollback"} {
			t.Run(provider+"/"+outcome, func(t *testing.T) {
				a, member, endpointID := credentialGroupDeletionFixture(t, provider)
				ctx := context.Background()
				p := routing.DefaultProfile(provider)
				p.Name = "delete-target"
				if outcome == "referenced" {
					p.Members = []routing.RoutingMember{{ChannelID: member.ChannelID, GroupID: member.GroupID, Enabled: true}}
				} else if outcome != "delete" {
					p.Members = []routing.RoutingMember{{ChannelID: member.ChannelID, GroupID: member.GroupID, Enabled: false}}
				}
				p, err := a.SaveRoutingProfile(p)
				require.NoError(t, err)
				_, err = a.storeDB.Exec(`INSERT INTO request_logs(request_id,start_time,status,provider) VALUES('retained-history','2026-11-02 12:00:00','completed',?)`, provider)
				require.NoError(t, err)
				if outcome == "rollback" {
					table := "claude_credential_groups"
					if provider == "codex" {
						table = "codex_credential_groups"
					}
					_, err := a.storeDB.Exec(fmt.Sprintf(`CREATE TRIGGER fail_group_delete BEFORE DELETE ON %s BEGIN SELECT RAISE(ABORT,'forced group failure'); END`, table))
					require.NoError(t, err)
				}
				if outcome == "cleanupRollback" {
					_, err := a.storeDB.Exec(`CREATE TRIGGER fail_profile_cleanup BEFORE UPDATE ON routing_profiles WHEN OLD.name='delete-target' BEGIN SELECT RAISE(ABORT,'cleanup failed'); END`)
					require.NoError(t, err)
				}
				shouldRemain := outcome != "delete" && outcome != "deselected"
				if provider == "claude" {
					err = a.DeleteClaudeCredentialGroupByID(member.GroupID)
					group, readErr := a.claudeCredentialGroupStore.GetByID(ctx, member.GroupID)
					require.NoError(t, readErr)
					require.Equal(t, shouldRemain, group != nil)
					endpoint, readErr := a.endpointStore.GetByID(ctx, endpointID)
					require.NoError(t, readErr)
					require.Equal(t, shouldRemain, endpoint != nil)
				} else {
					err = a.DeleteCodexCredentialGroupByID(member.GroupID)
					group, readErr := a.codexCredentialGroupStore.GetByID(ctx, member.GroupID)
					require.NoError(t, readErr)
					require.Equal(t, shouldRemain, group != nil)
					endpoint, readErr := a.codexEndpointStore.GetByID(ctx, endpointID)
					require.NoError(t, readErr)
					require.Equal(t, shouldRemain, endpoint != nil)
				}
				switch outcome {
				case "referenced":
					require.ErrorContains(t, err, "调度方案引用")
					require.ErrorContains(t, err, p.Name)
				case "rollback":
					require.ErrorContains(t, err, "forced group failure")
				case "cleanupRollback":
					require.ErrorContains(t, err, "cleanup failed")
				default:
					require.NoError(t, err)
				}
				s, storeErr := a.profileStore(ctx)
				require.NoError(t, storeErr)
				stored, storeErr := s.Get(ctx, provider, p.ID)
				require.NoError(t, storeErr)
				if outcome == "deselected" {
					require.Equal(t, p.Revision+1, stored.Revision)
					require.Empty(t, stored.Members)
				} else {
					require.Equal(t, p.Revision, stored.Revision)
					require.Equal(t, p.Members, stored.Members)
				}
				var historyCount int
				require.NoError(t, a.storeDB.QueryRow(`SELECT COUNT(*) FROM request_logs WHERE request_id='retained-history'`).Scan(&historyCount))
				require.Equal(t, 1, historyCount)
			})
		}
	}
}

func credentialGroupDeletionFixture(t *testing.T, provider string) (*App, routing.PoolMember, int64) {
	t.Helper()
	a, members := dispatchTestApp(t, provider)
	ctx := context.Background()
	member := routing.PoolMember{ChannelID: members[0].ChannelID}
	if provider == "claude" {
		a.claudeCredentialGroupService = claudeservice.NewClaudeCredentialGroupService(a.claudeCredentialGroupStore, a.channelStore, a.endpointStore)
		group, err := a.claudeCredentialGroupStore.Create(ctx, &claudestore.ClaudeCredentialGroupRecord{Channel: "first", Name: "delete-me", ApiKey: "test-key"})
		require.NoError(t, err)
		member.GroupID = group.ID
		endpoint, err := a.endpointStore.Create(ctx, &claudestore.EndpointRecord{Channel: "first", CredentialGroupID: group.ID, Name: "delete-child", URL: "https://delete.invalid", FailoverEnabled: true})
		require.NoError(t, err)
		return a, member, endpoint.ID
	}
	group, err := a.codexCredentialGroupStore.Create(ctx, &codexstore.CodexCredentialGroupRecord{ChannelID: member.ChannelID, Name: "delete-me", ApiKey: "test-key"})
	require.NoError(t, err)
	member.GroupID = group.ID
	endpoint, err := a.codexEndpointStore.Create(ctx, &codexstore.CodexEndpointRecord{ChannelID: member.ChannelID, CredentialGroupID: group.ID, Name: "delete-child", BaseURL: "https://delete.invalid/v1", IsActive: true, FailoverEnabled: true})
	require.NoError(t, err)
	return a, member, endpoint.ID
}

func TestReferencedCredentialGroupRenameUpdatesRoutingResources(t *testing.T) {
	for _, provider := range []string{"claude", "codex"} {
		t.Run(provider, func(t *testing.T) {
			a, member, _ := credentialGroupDeletionFixture(t, provider)
			p := routing.DefaultProfile(provider)
			p.Name = "keeps-group-identity"
			p.Members = []routing.RoutingMember{{ChannelID: member.ChannelID, GroupID: member.GroupID, Enabled: true}}
			p, err := a.SaveRoutingProfile(p)
			require.NoError(t, err)
			if provider == "claude" {
				_, err = a.UpdateClaudeCredentialGroupByID(member.GroupID, CreateClaudeCredentialGroupInput{Channel: "first", Name: "renamed-group", ApiKey: "test-key"})
			} else {
				_, err = a.UpdateCodexCredentialGroupByID(member.GroupID, UpdateCodexCredentialGroupInput{Name: "renamed-group"})
			}
			require.NoError(t, err)
			state, err := a.ListRoutingProfiles(provider)
			require.NoError(t, err)
			found := false
			for _, account := range state.Accounts {
				if account.GroupID == member.GroupID {
					require.Equal(t, "renamed-group", account.GroupName)
					found = true
				}
			}
			require.True(t, found)
			for _, profile := range state.Profiles {
				if profile.ID == p.ID {
					require.Equal(t, p.Members, profile.Members)
					require.Equal(t, p.Revision, profile.Revision)
				}
			}
		})
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
