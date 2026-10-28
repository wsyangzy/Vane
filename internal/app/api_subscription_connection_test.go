package app

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/pelletier/go-toml/v2"
	"github.com/stretchr/testify/require"
	"github.com/wsyangzy/Vane/internal/config"
	claudeendpoint "github.com/wsyangzy/Vane/internal/providers/claude/proxy/endpoint"
	claudesub "github.com/wsyangzy/Vane/internal/providers/claude/proxy/subscription"
	codexsub "github.com/wsyangzy/Vane/internal/providers/codex/proxy/subscription"
	"github.com/wsyangzy/Vane/internal/routing"
)

func connectionTestApp(t *testing.T, provider string) (*App, routing.RoutingProfile, string) {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("CODEX_HOME", dir)
	t.Setenv("CLAUDE_CONFIG_DIR", dir)
	path, err := subscriptionConfigPath(provider)
	require.NoError(t, err)
	a, members := dispatchTestApp(t, provider)
	profile := routing.DefaultProfile(provider)
	profile.Name, profile.IsDefault = "default-client-entry", true
	profile.Members = []routing.RoutingMember{{ChannelID: members[0].ChannelID, GroupID: members[0].GroupID, Enabled: true}}
	profile, err = a.SaveRoutingProfile(profile)
	require.NoError(t, err)
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, listener.Close()) })
	a.isRunning, a.proxyServer = true, &http.Server{}
	a.config.Server.Host, a.config.Server.Port = "127.0.0.1", listener.Addr().(*net.TCPAddr).Port
	a.config.Auth = config.AuthConfig{Enabled: true, Token: "application-client-entry-token"}
	return a, profile, path
}

func TestUnifiedClientConnectionPreviewUsesApplicationAuthAndDefaultProfile(t *testing.T) {
	for _, provider := range []string{"claude", "codex"} {
		t.Run(provider, func(t *testing.T) {
			a, profile, path := connectionTestApp(t, provider)
			input := SubscriptionConnectionCommand{Provider: provider, Action: "status"}
			status, err := a.ManageSubscriptionConnection(input)
			require.NoError(t, err)
			require.True(t, status.EndpointReady)
			require.True(t, status.AuthEnabled)
			require.True(t, status.Managed, "API-only profiles need no official client login")
			require.Equal(t, profile.ID, status.ProfileID)
			require.Equal(t, profile.Name, status.ProfileName)
			require.NotContains(t, status.Snippet+status.Command, a.config.Auth.Token)
			input.Action, input.LocalKey = "preview", "old-group-key"
			_, err = a.ManageSubscriptionConnection(input)
			require.ErrorContains(t, err, "application_auth_required")
			input.LocalKey = a.config.Auth.Token
			preview, err := a.ManageSubscriptionConnection(input)
			require.NoError(t, err)
			request := httptest.NewRequest(http.MethodPost, "/responses", nil)
			if provider == "claude" {
				var settings struct {
					Env map[string]string `json:"env"`
				}
				require.NoError(t, json.Unmarshal([]byte(preview.Snippet), &settings))
				_, key, ok := strings.Cut(settings.Env["ANTHROPIC_CUSTOM_HEADERS"], ": ")
				require.True(t, ok)
				request.Header.Set("X-Relay-Key", key)
			} else {
				var settings struct {
					Providers map[string]struct {
						Headers map[string]string `toml:"http_headers"`
					} `toml:"model_providers"`
				}
				require.NoError(t, toml.Unmarshal([]byte(preview.Snippet), &settings))
				for _, config := range settings.Providers {
					request.Header.Set("X-Relay-Key", config.Headers["X-Relay-Key"])
				}
			}
			request.Header.Set("Authorization", "Bearer client-official-credential")
			ctx, handled, err := a.authenticateRoutingProfile(request, provider)
			require.NoError(t, err)
			require.True(t, handled)
			require.Equal(t, "profile:"+provider, routing.CallerScope(ctx))
			require.Empty(t, request.Header.Get("X-Relay-Key"))
			require.Equal(t, "Bearer client-official-credential", request.Header.Get("Authorization"))
			_, err = os.Stat(path)
			require.True(t, os.IsNotExist(err), "preview must not create the client config")
			a.config.Auth.Enabled = false
			input.LocalKey = ""
			preview, err = a.ManageSubscriptionConnection(input)
			require.NoError(t, err)
			require.False(t, preview.AuthEnabled)
			require.NotContains(t, preview.Snippet, "application-client-entry-token")
		})
	}
}

func TestUnifiedClientConnectionFindsPassthroughInEnabledRuleTargets(t *testing.T) {
	for _, provider := range []string{"claude", "codex"} {
		t.Run(provider, func(t *testing.T) {
			a, profile, _ := connectionTestApp(t, provider)
			ctx := context.Background()
			var channelID, groupID int64
			if provider == "claude" {
				groups, err := a.claudeCredentialGroupStore.ListByChannel(ctx, "backup")
				require.NoError(t, err)
				group := groups[0]
				group.AuthMode, group.ApiKey = claudesub.ClientPassthrough, ""
				group.Subscription = &claudesub.Binding{Version: 1, Enabled: true}
				require.NoError(t, a.claudeCredentialGroupStore.Update(ctx, group))
				cfg := config.NewDefaultConfig()
				for _, ep := range a.endpointManager.GetAllEndpoints() {
					entry := ep.Config
					if entry.CredentialGroupID == group.ID {
						entry.AuthMode, entry.ApiKey = claudesub.ClientPassthrough, ""
					}
					cfg.Endpoints = append(cfg.Endpoints, entry)
				}
				a.endpointManager = claudeendpoint.NewManager(cfg)
				channels, err := a.channelStore.List(ctx)
				require.NoError(t, err)
				for _, channel := range channels {
					if channel.Name == "backup" {
						channelID = channel.ID
					}
				}
				groupID = group.ID
			} else {
				channels, err := a.codexChannelStore.List(ctx)
				require.NoError(t, err)
				for _, channel := range channels {
					if channel.Name == "backup" {
						channelID = channel.ID
					}
				}
				groups, err := a.codexCredentialGroupStore.ListByChannelID(ctx, channelID)
				require.NoError(t, err)
				group := groups[0]
				group.AuthMode, group.ApiKey = codexsub.ClientPassthrough, ""
				group.Subscription = &codexsub.Binding{Version: 1, Enabled: true}
				require.NoError(t, a.codexCredentialGroupStore.Update(ctx, group))
				require.NoError(t, a.codexEndpointMgr.Reload())
				groupID = group.ID
			}
			profile.Rules = []routing.RoutingRule{{ID: 1, Name: "official", Kind: "non_stream", Targets: []routing.RouteTarget{{ChannelID: channelID, GroupID: groupID}}}}
			var err error
			profile, err = a.SaveRoutingProfile(profile)
			require.NoError(t, err)
			status, err := a.ManageSubscriptionConnection(SubscriptionConnectionCommand{Provider: provider, Action: "status"})
			require.NoError(t, err)
			require.True(t, status.Managed, "disabled rules must not require an official login")
			profile.Rules[0].Enabled = true
			_, err = a.SaveRoutingProfile(profile)
			require.NoError(t, err)
			status, err = a.ManageSubscriptionConnection(SubscriptionConnectionCommand{Provider: provider, Action: "status"})
			require.NoError(t, err)
			require.False(t, status.Managed, "an enabled target may require the client's official login")
		})
	}
}

func TestUnifiedClientConnectionRejectsStaleDefaultAndKeepsRestoreAvailable(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("requires Windows DPAPI")
	}
	for _, provider := range []string{"claude", "codex"} {
		t.Run(provider, func(t *testing.T) {
			a, profile, path := connectionTestApp(t, provider)
			original := `{"env":{"KEEP":"private-existing-value"}}`
			if provider == "codex" {
				original = "model_reasoning_effort = 'high'\n[model_providers.other]\nexperimental_bearer_token = 'private-existing-value'\n"
			}
			require.NoError(t, os.WriteFile(path, []byte(original), 0600))
			authPath := filepath.Join(filepath.Dir(path), "auth.json")
			require.NoError(t, os.WriteFile(authPath, []byte("original-login-file"), 0600))
			input := SubscriptionConnectionCommand{Provider: provider, Action: "preview", LocalKey: a.config.Auth.Token}
			preview, err := a.ManageSubscriptionConnection(input)
			require.NoError(t, err)
			require.NotContains(t, preview.Snippet, "private-existing-value")
			current, err := os.ReadFile(path)
			require.NoError(t, err)
			require.Equal(t, original, string(current))
			input.Action, input.ProfileID, input.Revision, input.BindingVersion, input.ContentRevision = "apply", preview.ProfileID, preview.Revision, preview.BindingVersion, "stale-preview"
			_, err = a.ManageSubscriptionConnection(input)
			require.ErrorContains(t, err, "重新预览")
			input.ContentRevision = preview.ContentRevision
			input.ProfileID++
			_, err = a.ManageSubscriptionConnection(input)
			require.ErrorContains(t, err, "默认调度方案已变化")
			input.ProfileID = preview.ProfileID
			applied, err := a.ManageSubscriptionConnection(input)
			require.NoError(t, err)
			require.True(t, applied.BackupAvailable)
			actual, err := os.ReadFile(authPath)
			require.NoError(t, err)
			require.Equal(t, "original-login-file", string(actual))
			// Restore remains independent of default profile, auth and server state.
			_, err = a.storeDB.ExecContext(context.Background(), "DELETE FROM routing_profiles WHERE id=?", profile.ID)
			require.NoError(t, err)
			a.config.Auth.Token = "changed-token"
			a.isRunning = false
			_, err = a.ManageSubscriptionConnection(SubscriptionConnectionCommand{Provider: provider, Action: "restore", Revision: applied.Revision})
			require.NoError(t, err)
			actual, err = os.ReadFile(path)
			require.NoError(t, err)
			require.Equal(t, original, string(actual))
		})
	}
}

func TestCodexGlobalConfigKeepsSubscriptionRouteAndIPv6(t *testing.T) {
	t.Setenv("CODEX_HOME", t.TempDir())
	path, _ := subscriptionConfigPath("codex")
	os.WriteFile(path, []byte("model_provider = \"relay_subscription\"\n[model_providers.relay_subscription]\nbase_url = \"http://[::1]:9000/backend-api/codex\"\nrequires_openai_auth = true\n[model_providers.relay_subscription.http_headers]\nX-Relay-Key = 'keep-local-key'\n"), 0600)
	a := &App{config: &config.Config{Server: config.ServerConfig{Host: "::1", Port: 9000}}}
	if err := a.UpdateCodexGlobalConfig(CodexGlobalConfigInput{Model: "keep-model"}); err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(path)
	if !strings.Contains(string(got), "http://[::1]:9000/backend-api/codex") || !strings.Contains(string(got), "keep-local-key") {
		t.Fatal("model settings replaced subscription connection")
	}
}
