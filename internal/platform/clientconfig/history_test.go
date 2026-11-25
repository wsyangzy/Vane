package clientconfig

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/wsyangzy/Vane/internal/subscriptions/accounts"
)

func TestHistoryReapplyExternalEditAndRestoreSelectedVersion(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("requires DPAPI")
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	h := History{Directory: filepath.Join(dir, "backup"), Vault: accounts.UserVault{}}
	original := []byte("# keep exact bytes\r\nsecret = 'private-token'\r\n")
	require.NoError(t, os.WriteFile(path, original, 0600))
	legacy := path + ".relay-subscription.dpapi"
	require.NoError(t, os.WriteFile(legacy, []byte("user-owned-backup"), 0600))
	s, _ := Read(path)
	first, err := h.Apply("codex", path, s.Revision(), "connection_apply", []byte("first"))
	require.NoError(t, err)
	cipher, err := os.ReadFile(filepath.Join(first.Directory, "config.dpapi"))
	require.NoError(t, err)
	require.False(t, bytes.Contains(cipher, []byte("private-token")))
	_, err = h.Apply("codex", path, s.Revision(), "connection_apply", []byte("stale"))
	require.ErrorContains(t, err, "重新预览")
	require.NoError(t, os.WriteFile(path, []byte("external-current"), 0600))
	s, _ = Read(path)
	second, err := h.Apply("codex", path, s.Revision(), "connection_apply", []byte("second"))
	require.NoError(t, err, "latest preview authorizes reapply despite external changes")
	entries, err := h.List("codex", path)
	require.NoError(t, err)
	require.Len(t, entries, 2)
	require.Equal(t, second.ID, entries[0].ID)
	s, _ = Read(path)
	require.Error(t, h.Restore("claude", path, first.ID, s.Revision()))
	require.Error(t, h.Restore("codex", filepath.Join(dir, "other"), first.ID, s.Revision()))
	require.Error(t, h.Restore("codex", path, "../../outside", s.Revision()))
	require.Error(t, h.Restore("codex", path, first.ID, "stale"))
	require.NoError(t, h.Restore("codex", path, first.ID, s.Revision()))
	actual, _ := os.ReadFile(path)
	require.Equal(t, original, actual)
	entries, err = h.List("codex", path)
	require.NoError(t, err)
	require.Len(t, entries, 3)
	require.Equal(t, "before_restore", entries[0].Operation)
	s, _ = Read(path)
	require.NoError(t, h.Restore("codex", path, entries[0].ID, s.Revision()))
	actual, _ = os.ReadFile(path)
	require.Equal(t, "second", string(actual), "restoring can itself be undone")
	actual, _ = os.ReadFile(legacy)
	require.Equal(t, "user-owned-backup", string(actual))
}

func TestHistoryMissingConfigAndFailureDoNotLoseCurrentFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	h := History{Directory: filepath.Join(dir, "backup"), Vault: failingVault{}}
	s, _ := Read(path)
	_, err := h.Apply("codex", path, s.Revision(), "connection_apply", []byte("generated"))
	require.Error(t, err)
	_, err = os.Stat(path)
	require.True(t, errors.Is(err, os.ErrNotExist))
	if runtime.GOOS != "windows" {
		return
	}
	h.Vault = accounts.UserVault{}
	first, err := h.Apply("codex", path, s.Revision(), "connection_apply", []byte("generated"))
	require.NoError(t, err)
	s, _ = Read(path)
	_, err = h.Apply("codex", path, s.Revision(), "connection_apply", s.Data)
	require.NoError(t, err)
	entries, err := h.List("codex", path)
	require.NoError(t, err)
	require.Len(t, entries, 1, "identical writes create no extra record")
	require.NoError(t, h.Restore("codex", path, first.ID, s.Revision()))
	_, err = os.Stat(path)
	require.True(t, errors.Is(err, os.ErrNotExist))
	entries, err = h.List("codex", path)
	require.NoError(t, err)
	require.Len(t, entries, 2)
	require.NoError(t, os.WriteFile(filepath.Join(first.Directory, "config.dpapi"), []byte("corrupt"), 0600))
	require.Error(t, h.Restore("codex", path, first.ID, (Snapshot{}).Revision()))
}

type changingVault struct{ path string }

func (v changingVault) Seal(data []byte) ([]byte, error) {
	if err := os.WriteFile(v.path, []byte("concurrent edit"), 0600); err != nil {
		return nil, err
	}
	return accounts.UserVault{}.Seal(data)
}
func (v changingVault) Open(data []byte) ([]byte, error) { return accounts.UserVault{}.Open(data) }

func TestHistoryRechecksRevisionAfterSavingBackup(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("requires DPAPI")
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	require.NoError(t, os.WriteFile(path, []byte("original"), 0600))
	s, _ := Read(path)
	h := History{Directory: filepath.Join(dir, "backup"), Vault: changingVault{path: path}}
	_, err := h.Apply("codex", path, s.Revision(), "connection_apply", []byte("overwrite"))
	require.ErrorContains(t, err, "配置已变化")
	actual, _ := os.ReadFile(path)
	require.Equal(t, "concurrent edit", string(actual))
}

func TestFirstIdenticalApplicationStillCreatesManagedBaseline(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("requires DPAPI")
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	require.NoError(t, os.WriteFile(path, []byte("already-current"), 0600))
	s, err := Read(path)
	require.NoError(t, err)
	h := History{Directory: filepath.Join(dir, "backup"), Vault: accounts.UserVault{}}
	record, err := h.Apply("codex", path, s.Revision(), "connection_apply", s.Data)
	require.NoError(t, err)
	require.NotEmpty(t, record.ID, "first application must create the first managed backup")
	entries, err := h.List("codex", path)
	require.NoError(t, err)
	require.Len(t, entries, 1)
	record, err = h.Apply("codex", path, s.Revision(), "connection_apply", s.Data)
	require.NoError(t, err)
	require.Empty(t, record.ID, "later identical applications should not duplicate backups")
}
