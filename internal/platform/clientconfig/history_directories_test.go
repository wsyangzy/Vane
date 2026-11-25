package clientconfig

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/wsyangzy/Vane/internal/subscriptions/accounts"
)

func TestHistoryRecordDirectoriesAndNamesPreserveSnapshot(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("requires DPAPI")
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	require.NoError(t, os.WriteFile(path, []byte("original"), 0600))
	h := History{Directory: filepath.Join(dir, "backup"), Vault: accounts.UserVault{}}
	s, err := Read(path)
	require.NoError(t, err)
	first, err := h.Apply("codex", path, s.Revision(), "connection_apply", []byte("applied"))
	require.NoError(t, err)
	require.Equal(t, h.Directory, filepath.Dir(first.Directory))
	require.Contains(t, filepath.Base(first.Directory), "_codex_"+first.ID)
	file := filepath.Join(first.Directory, "config.dpapi")
	cipher, err := os.ReadFile(file)
	require.NoError(t, err)
	require.NoError(t, h.Rename("codex", path, first.ID, "  首次接入 · 我的基线  "))
	entries, err := h.List("codex", path)
	require.NoError(t, err)
	require.Len(t, entries, 1)
	require.Equal(t, "首次接入 · 我的基线", entries[0].Name)
	require.Equal(t, first.Directory, entries[0].Directory)
	after, err := os.ReadFile(file)
	require.NoError(t, err)
	require.Equal(t, cipher, after, "renaming must not rewrite the encrypted snapshot")
	require.Error(t, h.Rename("claude", path, first.ID, "wrong client"))
	require.Error(t, h.Rename("codex", filepath.Join(dir, "other"), first.ID, "wrong path"))
	require.Error(t, h.Rename("codex", path, first.ID, strings.Repeat("字", 81)))
	require.Error(t, h.Rename("codex", path, first.ID, "bad\nname"))
	require.NoError(t, h.Rename("codex", path, first.ID, ""))
	entries, err = h.List("codex", path)
	require.NoError(t, err)
	require.Equal(t, defaultRecordName(first), entries[0].Name)
	s, err = Read(path)
	require.NoError(t, err)
	require.NoError(t, h.Restore("codex", path, first.ID, s.Revision()))
	actual, _ := os.ReadFile(path)
	require.Equal(t, "original", string(actual))
	entries, err = h.List("codex", path)
	require.NoError(t, err)
	require.Len(t, entries, 2)
	root, err := os.ReadDir(h.Directory)
	require.NoError(t, err)
	for _, e := range root {
		require.True(t, e.IsDir(), "backup root contains one directory per record")
	}
}

func TestHistoryOrganizesLegacyRecordWithoutChangingCiphertext(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("requires DPAPI")
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	require.NoError(t, os.WriteFile(path, []byte("original"), 0600))
	h := History{Directory: filepath.Join(dir, "backup"), Vault: accounts.UserVault{}}
	s, _ := Read(path)
	record, err := h.Apply("codex", path, s.Revision(), "connection_apply", []byte("applied"))
	require.NoError(t, err)
	currentFile := filepath.Join(record.Directory, "config.dpapi")
	cipher, err := os.ReadFile(currentFile)
	require.NoError(t, err)
	legacy := filepath.Join(h.Directory, record.ID+historySuffix)
	require.NoError(t, os.Rename(currentFile, legacy))
	require.NoError(t, os.Remove(record.Directory))
	// An interrupted migration may have already created the empty directory.
	require.NoError(t, os.Mkdir(record.Directory, 0700))
	entries, err := h.List("codex", path)
	require.NoError(t, err)
	require.Len(t, entries, 1)
	after, err := os.ReadFile(currentFile)
	require.NoError(t, err)
	require.Equal(t, cipher, after)
	_, err = os.Stat(legacy)
	require.True(t, os.IsNotExist(err))
	require.NoError(t, h.Rename("codex", path, record.ID, "旧记录"))
	s, _ = Read(path)
	require.NoError(t, h.Restore("codex", path, record.ID, s.Revision()))
	actual, _ := os.ReadFile(path)
	require.Equal(t, "original", string(actual))
}
