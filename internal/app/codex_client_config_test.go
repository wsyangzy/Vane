package app

import (
	"context"
	"os"
	"testing"

	"github.com/pelletier/go-toml/v2"
	"github.com/stretchr/testify/require"
	codexservice "github.com/wsyangzy/Vane/internal/providers/codex/service"
	"github.com/wsyangzy/Vane/internal/routing"
)

func clientTestCompactionMode(t *testing.T, content []byte) string {
	t.Helper()
	var config map[string]any
	require.NoError(t, toml.Unmarshal(content, &config))
	provider := config["model_providers"].(map[string]any)["OpenAI"].(map[string]any)
	capabilities, _ := provider["capabilities"].(map[string]any)
	mode, _ := capabilities["remote_compaction"].(string)
	return mode
}

func TestCodexClientCompactionOptionsBelongToClientEntry(t *testing.T) {
	a := &App{}
	catalog := &codexservice.ClientModelCatalog{Mode: "remote"}
	for _, tc := range []struct{ mode, want string }{
		{"", "summary"}, {"summary", "summary"}, {"native", "native"}, {"preserve", "preserve"},
	} {
		t.Run(tc.want+"_"+tc.mode, func(t *testing.T) {
			options, err := a.codexClientConfigOptions(catalog, tc.mode)
			require.NoError(t, err)
			require.Same(t, catalog, options.ModelCatalog)
			require.Equal(t, tc.want, options.CompactionMode)
		})
	}
	options, err := a.codexClientConfigOptions(nil)
	require.NoError(t, err)
	require.Equal(t, "summary", options.CompactionMode)
	require.Nil(t, options.ModelCatalog)
	_, err = a.codexClientConfigOptions(catalog, "invalid")
	require.ErrorContains(t, err, "会话压缩方式")
}

func TestCodexClientCompactionPreviewApplyAndStalePreview(t *testing.T) {
	a, _, path := connectionTestApp(t, "codex")
	preview, err := a.ManageSubscriptionConnection(SubscriptionConnectionCommand{Provider: "codex", Action: "preview", CompactionMode: "summary"})
	require.NoError(t, err)
	require.Equal(t, "summary", preview.CompactionMode)
	require.Equal(t, "unsupported", preview.RemoteCompaction)
	require.Equal(t, "unsupported", clientTestCompactionMode(t, []byte(preview.Snippet)))
	_, err = os.Stat(path)
	require.True(t, os.IsNotExist(err), "preview must not create the client file")
	command := SubscriptionConnectionCommand{Provider: "codex", Action: "apply", CompactionMode: "native", Revision: preview.Revision,
		ContentRevision: preview.ContentRevision, ProfileID: preview.ProfileID, BindingVersion: preview.BindingVersion}
	_, err = a.ManageSubscriptionConnection(command)
	require.ErrorContains(t, err, "接入配置已变化")
	_, err = os.Stat(path)
	require.True(t, os.IsNotExist(err))
	command.CompactionMode = "summary"
	_, err = a.ManageSubscriptionConnection(command)
	require.NoError(t, err)
	content, err := os.ReadFile(path)
	require.NoError(t, err)
	require.Equal(t, "unsupported", clientTestCompactionMode(t, content))
	preview, err = a.ManageSubscriptionConnection(SubscriptionConnectionCommand{Provider: "codex", Action: "preview", CompactionMode: "native"})
	require.NoError(t, err)
	command.CompactionMode = "native"
	command.Revision, command.ContentRevision = preview.Revision, preview.ContentRevision
	_, err = a.ManageSubscriptionConnection(command)
	require.NoError(t, err)
	content, err = os.ReadFile(path)
	require.NoError(t, err)
	require.Equal(t, "v2", clientTestCompactionMode(t, content))
	status, err := a.ManageSubscriptionConnection(SubscriptionConnectionCommand{Provider: "codex", Action: "status"})
	require.NoError(t, err)
	require.Equal(t, "native", status.CompactionMode)
	require.Equal(t, "v2", status.RemoteCompaction)
	require.NoError(t, a.UpdateCodexGlobalConfig(CodexGlobalConfigInput{Model: "gpt-6.1-sol"}))
	content, err = os.ReadFile(path)
	require.NoError(t, err)
	require.Equal(t, "v2", clientTestCompactionMode(t, content), "ordinary model saves must preserve the client choice")
	preview, err = a.ManageSubscriptionConnection(SubscriptionConnectionCommand{Provider: "codex", Action: "preview", CompactionMode: "preserve"})
	require.NoError(t, err)
	require.Equal(t, "preserve", preview.CompactionMode)
	require.Equal(t, "v2", preview.RemoteCompaction)
	require.Equal(t, "v2", clientTestCompactionMode(t, []byte(preview.Snippet)))
}

func TestCodexClientCompactionIgnoresLegacyChannelConflicts(t *testing.T) {
	a, profile, path := connectionTestApp(t, "codex")
	channels, err := a.codexChannelService.ListChannels(context.Background())
	require.NoError(t, err)
	first := profile.Members[0].ChannelID
	var second int64
	for _, channel := range channels {
		if channel.ID != first {
			second = channel.ID
			break
		}
	}
	require.NotZero(t, second)
	// Legacy channel metadata is retained during the client-setting transition.
	_, err = a.storeDB.Exec("UPDATE codex_channels SET compaction_mode = 'native' WHERE id = ?", first)
	require.NoError(t, err)
	_, err = a.storeDB.Exec("UPDATE codex_channels SET compaction_mode = 'summary' WHERE id = ?", second)
	require.NoError(t, err)
	groups, err := a.codexCredentialGroupService.ListByChannelID(context.Background(), second)
	require.NoError(t, err)
	require.NotEmpty(t, groups)
	profile.Members = append(profile.Members, routing.RoutingMember{ChannelID: second, GroupID: groups[0].ID, Enabled: true})
	profile.Rules = []routing.RoutingRule{{Enabled: true, Kind: "model", MatchModel: "routed-model", Mode: "prefer",
		Targets: []routing.RouteTarget{{ChannelID: second, GroupID: groups[0].ID}}}}
	_, err = a.SaveRoutingProfile(profile)
	require.NoError(t, err)
	for _, tc := range []struct{ mode, want string }{{"", "unsupported"}, {"summary", "unsupported"}, {"native", "v2"}, {"preserve", ""}} {
		preview, err := a.ManageSubscriptionConnection(SubscriptionConnectionCommand{Provider: "codex", Action: "preview", CompactionMode: tc.mode})
		require.NoError(t, err, "channel metadata must not determine client compaction")
		require.Equal(t, tc.want, clientTestCompactionMode(t, []byte(preview.Snippet)))
	}
	_, err = os.Stat(path)
	require.True(t, os.IsNotExist(err))
	require.NoError(t, a.UpdateCodexGlobalConfig(CodexGlobalConfigInput{Model: "gpt-6.1-sol"}), "legacy channel conflicts must not block ordinary model saves")
}

func TestCodexClientCompactionInvalidModeDoesNotModifyConfig(t *testing.T) {
	a, _, path := connectionTestApp(t, "codex")
	original := []byte("model_provider = 'OpenAI'\n[model_providers.OpenAI.capabilities]\nremote_compaction = 'v2'\nexternal_web_access = false\n")
	require.NoError(t, os.WriteFile(path, original, 0600))
	for _, action := range []string{"preview", "apply"} {
		_, err := a.ManageSubscriptionConnection(SubscriptionConnectionCommand{Provider: "codex", Action: action, CompactionMode: "invalid"})
		require.ErrorContains(t, err, "会话压缩方式")
		actual, err := os.ReadFile(path)
		require.NoError(t, err)
		require.Equal(t, original, actual)
	}
}
