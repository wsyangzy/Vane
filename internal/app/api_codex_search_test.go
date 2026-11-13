package app

import (
	"context"
	"encoding/json"
	"runtime"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/wsyangzy/Vane/internal/routing"
)

func TestCodexLegacySearchFailureDoesNotBlockClaudeProfiles(t *testing.T) {
	a, _ := dispatchTestApp(t, "claude")
	ctx := context.Background()
	_, err := a.profileStore(ctx)
	require.NoError(t, err)
	_, err = a.storeDB.ExecContext(ctx, `INSERT INTO settings(provider,category,key,value) VALUES('global','search','mode','invalid-legacy-mode')`)
	require.NoError(t, err)
	_, err = a.ListRoutingProfiles("claude")
	require.NoError(t, err)
	require.Error(t, a.migrateCodexSearch(ctx))
}

func TestCodexProfileSearchSecretsStaySeparateAndRuntimeUsesCurrentProfile(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("UserVault uses Windows DPAPI")
	}
	a, members := dispatchTestApp(t, "codex")
	p := routing.DefaultProfile("codex")
	p.Name, p.IsDefault = "search profile", true
	p.Members = []routing.RoutingMember{{ChannelID: members[0].ChannelID, GroupID: members[0].GroupID, Enabled: true}}
	policy := routing.DefaultSearchPolicy()
	policy.Mode = "service"
	p.Search = &policy
	key := "profile-private-search-key"
	saved, err := a.SaveCodexRoutingProfile(CodexRoutingProfileInput{Profile: p, SearchAPIKey: &key})
	require.NoError(t, err)
	require.True(t, saved.Search.HasAPIKey)
	encoded, err := json.Marshal(saved)
	require.NoError(t, err)
	require.NotContains(t, string(encoded), key)
	cfg, err := a.codexSearchConfig(context.Background())
	require.NoError(t, err)
	require.Equal(t, key, cfg.APIKey)
	require.Equal(t, "service", cfg.Mode)
	saved.Search.Mode = "native"
	saved, err = a.SaveCodexRoutingProfile(CodexRoutingProfileInput{Profile: saved})
	require.NoError(t, err)
	require.True(t, saved.Search.HasAPIKey)
	saved.Search.Vendor = "searxng"
	saved.Search.Endpoint = "https://search.example.org/search"
	saved.Search.Mode = "service"
	saved, err = a.SaveCodexRoutingProfile(CodexRoutingProfileInput{Profile: saved})
	require.NoError(t, err)
	require.False(t, saved.Search.HasAPIKey)
	cfg, err = a.codexSearchConfig(context.Background())
	require.NoError(t, err)
	require.Empty(t, cfg.APIKey)
	require.Equal(t, "searxng", cfg.Vendor)
	state, err := a.ListRoutingProfiles("codex")
	require.NoError(t, err)
	encoded, err = json.Marshal(state)
	require.NoError(t, err)
	require.False(t, strings.Contains(string(encoded), key), "secret leaked into profile workspace")
	saved.Search.Vendor = "tavily"
	saved.Search.Endpoint = routing.DefaultSearchPolicy().Endpoint
	saved, err = a.SaveCodexRoutingProfile(CodexRoutingProfileInput{Profile: saved})
	require.NoError(t, err)
	require.True(t, saved.Search.HasAPIKey, "changing protocol lost that protocol's stored credential")
	stale := saved
	stale.Revision--
	replacement := "must-not-commit"
	_, err = a.SaveCodexRoutingProfile(CodexRoutingProfileInput{Profile: stale, SearchAPIKey: &replacement})
	require.Error(t, err)
	cfg, err = a.codexSearchConfig(context.Background())
	require.NoError(t, err)
	require.Equal(t, key, cfg.APIKey)
}

func TestCodexSearchAPIKeyRevealIsScopedAndDoesNotChangePublicProfiles(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("UserVault uses Windows DPAPI")
	}
	a, members := dispatchTestApp(t, "codex")
	profile := routing.DefaultProfile("codex")
	profile.Name, profile.IsDefault = "first search profile", true
	profile.Members = []routing.RoutingMember{{ChannelID: members[0].ChannelID, GroupID: members[0].GroupID, Enabled: true}}
	policy := routing.DefaultSearchPolicy()
	profile.Search = &policy
	firstKey := "test-first-profile-tavily"
	first, err := a.SaveCodexRoutingProfile(CodexRoutingProfileInput{Profile: profile, SearchAPIKey: &firstKey})
	require.NoError(t, err)
	profile.Name, profile.IsDefault = "second search profile", false
	secondKey := "test-second-profile-tavily"
	second, err := a.SaveCodexRoutingProfile(CodexRoutingProfileInput{Profile: profile, SearchAPIKey: &secondKey})
	require.NoError(t, err)
	first.Search.Vendor, first.Search.Endpoint = "brave", "https://api.search.brave.com/res/v1/web/search"
	braveKey := "test-first-profile-brave"
	first, err = a.SaveCodexRoutingProfile(CodexRoutingProfileInput{Profile: first, SearchAPIKey: &braveKey})
	require.NoError(t, err)
	for _, tc := range []struct {
		id     int64
		vendor string
		key    string
	}{{first.ID, "tavily", firstKey}, {first.ID, "brave", braveKey}, {second.ID, "tavily", secondKey}, {first.ID, "exa", ""}} {
		got, err := a.GetCodexSearchAPIKey(tc.id, tc.vendor)
		require.NoError(t, err)
		require.Equal(t, tc.key, got)
	}
	for _, id := range []int64{0, -1, second.ID + 1000} {
		got, err := a.GetCodexSearchAPIKey(id, "tavily")
		require.Error(t, err)
		require.Empty(t, got)
	}
	got, err := a.GetCodexSearchAPIKey(first.ID, "unsupported")
	require.Error(t, err)
	require.Empty(t, got)
	state, err := a.ListRoutingProfiles("codex")
	require.NoError(t, err)
	encoded, err := json.Marshal(state)
	require.NoError(t, err)
	for _, key := range []string{firstKey, secondKey, braveKey} {
		require.NotContains(t, string(encoded), key)
	}
	read, err := a.GetRoutingProfile("codex", first.ID)
	require.NoError(t, err)
	require.Equal(t, first.Revision, read.Revision)
	require.Equal(t, "brave", read.Search.Vendor)
	_, err = a.storeDB.ExecContext(context.Background(), `UPDATE routing_profiles SET provider='claude' WHERE id=?`, second.ID)
	require.NoError(t, err)
	got, err = a.GetCodexSearchAPIKey(second.ID, "tavily")
	require.Error(t, err)
	require.Empty(t, got)
}
