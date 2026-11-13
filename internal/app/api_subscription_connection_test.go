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
			input.Action = "preview"
			preview, err := a.ManageSubscriptionConnection(input)
			require.NoError(t, err)
			if provider == "codex" {
				require.True(t, strings.HasSuffix(preview.BaseURL, "/v1"))
			}
			request := httptest.NewRequest(http.MethodPost, "/responses", nil)
			request.Header.Set("Authorization", "Bearer client-official-credential")
			remainingAuth := "Bearer client-official-credential"
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
						Bearer  string            `toml:"experimental_bearer_token"`
					} `toml:"model_providers"`
				}
				require.NoError(t, toml.Unmarshal([]byte(preview.Snippet), &settings))
				for _, config := range settings.Providers {
					require.NotContains(t, config.Headers, "X-Relay-Key")
					require.Equal(t, a.config.Auth.Token, config.Bearer)
					request.Header.Set("Authorization", "Bearer "+config.Bearer)
				}
				remainingAuth = ""
			}
			ctx, handled, err := a.authenticateRoutingProfile(request, provider)
			require.NoError(t, err)
			require.True(t, handled)
			require.Equal(t, "profile:"+provider, routing.CallerScope(ctx))
			require.Empty(t, request.Header.Get("X-Relay-Key"))
			require.Equal(t, remainingAuth, request.Header.Get("Authorization"))
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

func TestCodexGlobalConfigMigratesSubscriptionRouteToUnifiedModeTwoAndIPv6(t *testing.T) {
	t.Setenv("CODEX_HOME", t.TempDir())
	path, _ := subscriptionConfigPath("codex")
	os.WriteFile(path, []byte("model_provider = \"relay_subscription\"\n[model_providers.relay_subscription]\nbase_url = \"http://[::1]:9000/backend-api/codex\"\nrequires_openai_auth = true\n[model_providers.relay_subscription.http_headers]\nX-Relay-Key = 'keep-local-key'\n"), 0600)
	a := &App{config: &config.Config{Server: config.ServerConfig{Host: "::1", Port: 9000}}}
	if err := a.UpdateCodexGlobalConfig(CodexGlobalConfigInput{Model: "keep-model"}); err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(path)
	var parsed map[string]any
	require.NoError(t, toml.Unmarshal(got, &parsed))
	p := parsed["model_providers"].(map[string]any)["OpenAI"].(map[string]any)
	if parsed["model_provider"] != "OpenAI" || p["name"] != "OpenAI" || p["base_url"] != "http://[::1]:9000/v1" || p["requires_openai_auth"] != false || p["experimental_bearer_token"] != "vane-local" {
		t.Fatal("ordinary settings did not migrate legacy subscription connection")
	}
}

func TestCodexConnectionCatalogPreviewApplyAndRestore(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("requires Windows DPAPI")
	}
	a, _, path := connectionTestApp(t, "codex")
	original := []byte("model = 'keep-model'\n[features]\ngoals = false\n")
	require.NoError(t, os.WriteFile(path, original, 0600))
	catalogPath := filepath.Join(filepath.Dir(path), "codex-models.json")
	catalog := []byte(`{"models":[{"slug":"keep-model","base_instructions":"Local model instructions"}]}`)
	require.NoError(t, os.WriteFile(catalogPath, catalog, 0600))
	authPath := filepath.Join(filepath.Dir(path), "auth.json")
	require.NoError(t, os.WriteFile(authPath, []byte("existing-official-login"), 0600))
	for _, mode := range []string{"local", "remote"} {
		input := SubscriptionConnectionCommand{Provider: "codex", Action: "preview", ModelCatalogMode: mode, ModelCatalogPath: catalogPath}
		preview, err := a.ManageSubscriptionConnection(input)
		require.NoError(t, err)
		require.Equal(t, mode, preview.ModelCatalogMode)
		before, err := os.ReadFile(path)
		require.NoError(t, err)
		input.Action, input.ProfileID, input.Revision, input.BindingVersion, input.ContentRevision = "apply", preview.ProfileID, preview.Revision, preview.BindingVersion, preview.ContentRevision
		// Changing the selection after a preview must not write a different config.
		input.ModelCatalogMode = "remote"
		if mode == "remote" {
			input.ModelCatalogMode = "local"
		}
		_, err = a.ManageSubscriptionConnection(input)
		require.ErrorContains(t, err, "重新预览")
		input.ModelCatalogMode = mode
		if mode == "local" {
			require.NoError(t, os.WriteFile(catalogPath, []byte(`{"data":[{"id":"not-a-native-catalog"}]}`), 0600))
			_, err = a.ManageSubscriptionConnection(input)
			require.ErrorContains(t, err, "model_catalog_json 无效")
			after, err := os.ReadFile(path)
			require.NoError(t, err)
			require.Equal(t, before, after)
			require.NoError(t, os.WriteFile(catalogPath, catalog, 0600))
		}
		applied, err := a.ManageSubscriptionConnection(input)
		require.NoError(t, err)
		require.True(t, applied.BackupAvailable)
		status, err := a.ManageSubscriptionConnection(SubscriptionConnectionCommand{Provider: "codex", Action: "status"})
		require.NoError(t, err)
		require.Equal(t, mode, status.ModelCatalogMode)
		require.Equal(t, catalogPath, status.ModelCatalogPath)
		var parsed map[string]any
		actual, err := os.ReadFile(path)
		require.NoError(t, err)
		require.NoError(t, toml.Unmarshal(actual, &parsed))
		require.Equal(t, "keep-model", parsed["model"])
		require.Equal(t, false, parsed["features"].(map[string]any)["goals"])
		if mode == "local" {
			require.Equal(t, catalogPath, parsed["model_catalog_json"])
		} else {
			require.NotContains(t, parsed, "model_catalog_json")
		}
	}
	status, err := a.ManageSubscriptionConnection(SubscriptionConnectionCommand{Provider: "codex", Action: "status"})
	require.NoError(t, err)
	_, err = a.ManageSubscriptionConnection(SubscriptionConnectionCommand{Provider: "codex", Action: "restore", Revision: status.Revision})
	require.NoError(t, err)
	actual, err := os.ReadFile(path)
	require.NoError(t, err)
	require.Equal(t, original, actual)
	actual, err = os.ReadFile(catalogPath)
	require.NoError(t, err)
	require.Equal(t, catalog, actual)
	actual, err = os.ReadFile(authPath)
	require.NoError(t, err)
	require.Equal(t, "existing-official-login", string(actual))
}

func TestCodexConnectionCatalogStatusCanRepairMissingFile(t *testing.T) {
	a, _, path := connectionTestApp(t, "codex")
	status, err := a.ManageSubscriptionConnection(SubscriptionConnectionCommand{Provider: "codex", Action: "status"})
	require.NoError(t, err)
	require.Equal(t, "local", status.ModelCatalogMode, "new configurations start with the local file option")
	require.Equal(t, filepath.Join(filepath.Dir(path), "codex-models.json"), status.ModelCatalogPath)
	require.NoError(t, os.WriteFile(path, []byte("model_catalog_json = 'missing-models.json'\n"), 0600))
	status, err = a.ManageSubscriptionConnection(SubscriptionConnectionCommand{Provider: "codex", Action: "status"})
	require.NoError(t, err)
	require.Equal(t, "local", status.ModelCatalogMode)
	require.Equal(t, "missing-models.json", status.ModelCatalogPath)
	_, err = a.ManageSubscriptionConnection(SubscriptionConnectionCommand{Provider: "codex", Action: "preview", ModelCatalogMode: "remote"})
	require.NoError(t, err, "switching to remote can repair a missing local catalog")
	_, err = a.ManageSubscriptionConnection(SubscriptionConnectionCommand{Provider: "codex", Action: "choose_model_catalog"})
	require.ErrorContains(t, err, "桌面应用")
	require.NoError(t, os.WriteFile(path, []byte("broken = ["), 0600))
	_, err = a.ManageSubscriptionConnection(SubscriptionConnectionCommand{Provider: "codex", Action: "status"})
	require.NoError(t, err, "damaged TOML must not disable status and restore")
	_, err = a.ManageSubscriptionConnection(SubscriptionConnectionCommand{Provider: "codex", Action: "preview", ModelCatalogMode: "remote"})
	require.ErrorContains(t, err, "config.toml 无效")
}
