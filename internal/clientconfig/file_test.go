package clientconfig

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"ai-relay-forwarder/internal/subscriptions/accounts"
)

func TestApplyRestorePreservesOriginalAndRejectsExternalChanges(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("requires Windows DPAPI")
	}
	path := filepath.Join(t.TempDir(), "settings.json")
	original := []byte("{\r\n  \"private\":\"do-not-expose\"\r\n}\r\n")
	if err := os.WriteFile(path, original, 0600); err != nil {
		t.Fatal(err)
	}
	s, _ := Read(path)
	vault := accounts.UserVault{}
	if err := Apply(path, s.Revision(), []byte("first apply"), vault); err != nil {
		t.Fatal(err)
	}
	cipher, _ := os.ReadFile(backupPath(path))
	if bytes.Contains(cipher, []byte("do-not-expose")) {
		t.Fatal("backup contains plaintext")
	}
	if err := Apply(path, s.Revision(), []byte("stale"), vault); err == nil {
		t.Fatal("stale preview applied")
	}
	current, _ := Read(path)
	if err := Apply(path, current.Revision(), []byte("second apply"), vault); err != nil {
		t.Fatal(err)
	}
	applied, _ := Read(path)
	os.WriteFile(path, []byte("external edit"), 0600)
	external, _ := Read(path)
	if err := Restore(path, external.Revision(), vault); err == nil {
		t.Fatal("external edits overwritten")
	}
	if err := Apply(path, external.Revision(), []byte("overwrite"), vault); err == nil {
		t.Fatal("external edits replaced on reapply")
	}
	os.WriteFile(path, applied.Data, 0600)
	if err := Restore(path, applied.Revision(), vault); err != nil {
		t.Fatal(err)
	}
	restored, _ := os.ReadFile(path)
	if !bytes.Equal(restored, original) || BackupAvailable(path) {
		t.Fatal("rollback lost original bytes or left backup")
	}
}

type failingVault struct{}

func (failingVault) Seal([]byte) ([]byte, error) { return nil, errors.New("unavailable") }
func (failingVault) Open([]byte) ([]byte, error) { return nil, errors.New("unavailable") }

func TestMissingFileAndEncryptionFailure(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "config.toml")
	s, _ := Read(path)
	if err := Apply(path, s.Revision(), []byte("generated"), failingVault{}); err == nil {
		t.Fatal("encryption failure ignored")
	}
	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("file written without backup")
	}
	if runtime.GOOS != "windows" {
		return
	}
	if err := Apply(path, s.Revision(), []byte("generated"), accounts.UserVault{}); err != nil {
		t.Fatal(err)
	}
	current, _ := Read(path)
	if err := Restore(path, current.Revision(), accounts.UserVault{}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("new config not removed on rollback")
	}
}

func TestInterruptedReplacementCanRestorePreviousApplication(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("requires Windows DPAPI")
	}
	path := filepath.Join(t.TempDir(), "config.toml")
	os.WriteFile(path, []byte("original"), 0600)
	s, _ := Read(path)
	vault := accounts.UserVault{}
	if err := Apply(path, s.Revision(), []byte("first"), vault); err != nil {
		t.Fatal(err)
	}
	first, _ := Read(path)
	if err := Apply(path, first.Revision(), []byte("second"), vault); err != nil {
		t.Fatal(err)
	}
	// Emulate the file replacement failing after the second backup was durable.
	os.WriteFile(path, first.Data, 0600)
	if err := Restore(path, first.Revision(), vault); err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(path)
	if string(got) != "original" {
		t.Fatal("failed replacement destroyed baseline")
	}
}
