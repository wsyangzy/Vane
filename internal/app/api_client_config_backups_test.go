package app

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/stretchr/testify/require"
	settingsservice "github.com/wsyangzy/Vane/internal/settings/service"
	settingsstore "github.com/wsyangzy/Vane/internal/settings/store"
)

func TestManagedBackupsStartFreshAndModelWritesParticipate(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("requires DPAPI")
	}
	for _, provider := range []string{"claude", "codex"} {
		t.Run(provider, func(t *testing.T) {
			a, _, path := connectionTestApp(t, provider)
			original := []byte("{\r\n\"env\":{},\"custom\":true\r\n}")
			if provider == "codex" {
				original = []byte("# original\r\nmodel = 'original-model'\r\n[mcp_servers.example]\r\ncommand = 'example'\r\n")
			}
			require.NoError(t, os.WriteFile(path, original, 0600))
			legacy := path + ".relay-subscription.dpapi"
			require.NoError(t, os.WriteFile(legacy, []byte("previous-user-backup"), 0600))
			svc := settingsservice.NewSettingsService(settingsstore.NewSQLiteSettingsStore(a.storeDB))
			require.NoError(t, svc.InitDefaults(context.Background()))
			a.settingsService = svc
			list, err := a.ManageClientConfigBackups(ClientConfigBackupCommand{Provider: provider, Action: "list"})
			require.NoError(t, err)
			require.Empty(t, list.Records)
			require.Equal(t, filepath.Join(os.Getenv("APPDATA"), "Vane", "backup"), list.Directory)
			apply := func() {
				preview, err := a.ManageSubscriptionConnection(SubscriptionConnectionCommand{Provider: provider, Action: "preview"})
				require.NoError(t, err)
				_, err = a.ManageSubscriptionConnection(SubscriptionConnectionCommand{Provider: provider, Action: "apply", ProfileID: preview.ProfileID, BindingVersion: preview.BindingVersion, Revision: preview.Revision, ContentRevision: preview.ContentRevision})
				require.NoError(t, err)
			}
			apply()
			list, err = a.ManageClientConfigBackups(ClientConfigBackupCommand{Provider: provider, Action: "list"})
			require.NoError(t, err)
			require.Len(t, list.Records, 1)
			firstID := list.Records[0].ID
			require.Contains(t, list.Records[0].Directory, "_"+provider+"_"+firstID)
			_, err = a.ManageClientConfigBackups(ClientConfigBackupCommand{Provider: provider, Action: "rename", ID: firstID, Name: "首次接入"})
			require.NoError(t, err)
			list, err = a.ManageClientConfigBackups(ClientConfigBackupCommand{Provider: provider, Action: "list"})
			require.NoError(t, err)
			require.Equal(t, "首次接入", list.Records[0].Name)
			_, err = a.ManageClientConfigBackups(ClientConfigBackupCommand{Provider: provider, Action: "rename", ID: firstID, Name: "bad\nname"})
			require.Error(t, err)
			_, err = a.ManageClientConfigBackups(ClientConfigBackupCommand{Provider: provider, Action: "open_directory", ID: "unknown"})
			require.ErrorContains(t, err, "找不到")
			if provider == "codex" {
				require.NoError(t, a.UpdateCodexGlobalConfig(CodexGlobalConfigInput{Model: "new-model", Enable1MContext: true}))
				list, err = a.ManageClientConfigBackups(ClientConfigBackupCommand{Provider: provider, Action: "list"})
				require.NoError(t, err)
				require.Len(t, list.Records, 2)
				require.Equal(t, "model_settings", list.Records[0].Operation)
			}
			// A user edit becomes the current snapshot on a fresh preview.
			current, _ := os.ReadFile(path)
			if provider == "codex" {
				current = append(current, []byte("\n# manual edit\n")...)
			} else {
				current = []byte(`{"env":{},"manual_edit":true}`)
			}
			require.NoError(t, os.WriteFile(path, current, 0600))
			apply()
			list, err = a.ManageClientConfigBackups(ClientConfigBackupCommand{Provider: provider, Action: "list"})
			require.NoError(t, err)
			_, err = a.ManageClientConfigBackups(ClientConfigBackupCommand{Provider: provider, Action: "restore", ID: firstID, Revision: "stale"})
			require.ErrorContains(t, err, "重新预览")
			_, err = a.ManageClientConfigBackups(ClientConfigBackupCommand{Provider: provider, Action: "restore", ID: firstID, Revision: list.Revision})
			require.NoError(t, err)
			restored, err := os.ReadFile(path)
			require.NoError(t, err)
			require.Equal(t, original, restored)
			retained, err := os.ReadFile(legacy)
			require.NoError(t, err)
			require.Equal(t, "previous-user-backup", string(retained))
			oldDirectory := list.Directory
			newDirectory := filepath.Join(t.TempDir(), "custom-backups")
			require.NoError(t, a.UpdateSetting(UpdateSettingInput{Category: "backup", Key: "directory", Value: newDirectory}))
			apply()
			list, err = a.ManageClientConfigBackups(ClientConfigBackupCommand{Provider: provider, Action: "list"})
			require.NoError(t, err)
			require.Equal(t, newDirectory, list.Directory)
			require.Len(t, list.Records, 1)
			oldEntries, err := os.ReadDir(oldDirectory)
			require.NoError(t, err)
			require.NotEmpty(t, oldEntries)
			require.Error(t, a.UpdateSetting(UpdateSettingInput{Category: "backup", Key: "directory", Value: "relative"}))
			require.Error(t, a.UpdateSetting(UpdateSettingInput{Provider: "codex", Category: "backup", Key: "directory", Value: newDirectory}))
		})
	}
}
