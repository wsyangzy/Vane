//go:build windows

package accounts

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoginBatchLauncherHandlesSpacesAndUnicode(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "Program Files 中文")
	if e := os.MkdirAll(dir, 0700); e != nil {
		t.Fatal(e)
	}
	for _, ext := range []string{".cmd", ".bat"} {
		path := filepath.Join(dir, "official-cli"+ext)
		if e := os.WriteFile(path, []byte("@echo off\r\necho VANE_ARGS %*\r\n"), 0600); e != nil {
			t.Fatal(e)
		}
		cmd := loginCommand(context.Background(), path, []string{"login", "--device-auth"})
		out, e := cmd.CombinedOutput()
		if e != nil || !strings.Contains(string(out), `VANE_ARGS "login" "--device-auth"`) {
			t.Fatalf("npm-style shim failed: %s %v", out, e)
		}
	}
}
func TestLoginBatchLauncherRejectsShellExpansion(t *testing.T) {
	for _, arg := range []string{"x&whoami", "%PATH%", "x\ny"} {
		cmd := loginCommand(context.Background(), `C:\official.cmd`, []string{arg})
		if cmd.Err == nil {
			t.Fatalf("unsafe batch argument accepted: %q", arg)
		}
	}
}
