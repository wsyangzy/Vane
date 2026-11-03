//go:build windows

package service

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"golang.org/x/sys/windows"
)

const quotaTestProcessMode = "VANE_QUOTA_TEST_PROCESS"

func TestMain(m *testing.M) {
	if mode := os.Getenv(quotaTestProcessMode); mode != "" {
		// The quota runner passes CLI flags (-p /usage), so handle helper mode
		// before m.Run parses the Go test flags.
		if err := runQuotaTestProcess(mode); err != nil {
			_ = os.WriteFile("fixture.error", []byte(err.Error()), 0600)
			os.Exit(1)
		}
		os.Exit(0)
	}
	os.Exit(m.Run())
}

func runQuotaTestProcess(mode string) error {
	switch mode {
	case "cli":
		if len(os.Args) != 3 || os.Args[1] != "-p" || os.Args[2] != "/usage" {
			return fmt.Errorf("unexpected quota CLI arguments: %q", os.Args[1:])
		}
		child := exec.Command(os.Args[0])
		child.Env = append(os.Environ(), quotaTestProcessMode+"=child")
		child.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: windows.CREATE_NO_WINDOW}
		if err := child.Start(); err != nil {
			return fmt.Errorf("start quota fixture child: %w", err)
		}
		return child.Wait()
	case "child":
		if err := os.WriteFile("child.pid", []byte(strconv.Itoa(os.Getpid())), 0600); err != nil {
			return fmt.Errorf("write quota fixture child PID: %w", err)
		}
		time.Sleep(30 * time.Second)
		return nil
	default:
		return fmt.Errorf("unexpected quota fixture mode: %q", mode)
	}
}

func TestRoutingQuotaWindowsJobStopsCLIAndChild(t *testing.T) {
	directory := t.TempDir()
	script := filepath.Join(directory, "fake-cli.cmd")
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	content := "@echo off\r\n\"" + executable + "\" %*\r\n"
	if err := os.WriteFile(script, []byte(content), 0600); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	done := make(chan error, 1)
	go func() {
		environment := append(os.Environ(), quotaTestProcessMode+"=cli")
		_, err := (isolatedQuotaCLI{}).Run(ctx, script, directory, environment)
		done <- err
	}()
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	var childID int
	for childID == 0 {
		select {
		case err := <-done:
			diagnostic, _ := os.ReadFile(filepath.Join(directory, "fixture.error"))
			t.Fatalf("fake CLI exited before spawning child: %v; fixture: %s", err, diagnostic)
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
