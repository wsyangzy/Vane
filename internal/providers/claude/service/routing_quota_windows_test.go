//go:build windows

package service

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"golang.org/x/sys/windows"
)

func TestRoutingQuotaWindowsJobStopsCLIAndChild(t *testing.T) {
	directory := t.TempDir()
	script := filepath.Join(directory, "fake-cli.cmd")
	content := "@echo off\r\npowershell.exe -NoProfile -NonInteractive -Command \"$taskChild = Start-Process -FilePath 'powershell.exe' -WindowStyle Hidden -ArgumentList '-NoProfile','-NonInteractive','-Command','Start-Sleep -Seconds 30' -PassThru; [System.IO.File]::WriteAllText('child.pid', [string]$taskChild.Id); Start-Sleep -Seconds 30\"\r\n"
	if err := os.WriteFile(script, []byte(content), 0600); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	done := make(chan error, 1)
	go func() {
		_, err := (isolatedQuotaCLI{}).Run(ctx, script, directory, os.Environ())
		done <- err
	}()
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	var childID int
	for childID == 0 {
		select {
		case err := <-done:
			t.Fatalf("fake CLI exited before spawning child: %v", err)
		case <-ctx.Done():
			t.Fatal("fake CLI did not spawn child")
		case <-ticker.C:
			if data, err := os.ReadFile(filepath.Join(directory, "child.pid")); err == nil {
				childID, _ = strconv.Atoi(strings.TrimSpace(string(data)))
			}
		}
	}
	child, err := windows.OpenProcess(windows.SYNCHRONIZE|windows.PROCESS_QUERY_LIMITED_INFORMATION, false, uint32(childID))
	if err != nil {
		t.Fatal(err)
	}
	defer windows.CloseHandle(child)
	cancel()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("cancelled CLI reported success")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("CLI cancellation did not complete")
	}
	state, err := windows.WaitForSingleObject(child, 3000)
	if err != nil || state != windows.WAIT_OBJECT_0 {
		t.Fatalf("CLI child survived Job close: state=%d err=%v", state, err)
	}
}
