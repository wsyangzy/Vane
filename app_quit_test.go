package main

import (
	"strings"
	"testing"
)

func TestParseDevWatcherPID(t *testing.T) {
	pid, err := parseDevWatcherPID([]byte(" 12345\r\n"))
	if err != nil || pid != 12345 {
		t.Fatalf("parse valid PID: pid=%d err=%v", pid, err)
	}
	for _, value := range []string{"", "0", "-1", "wails3"} {
		if _, err := parseDevWatcherPID([]byte(value)); err == nil {
			t.Fatalf("expected invalid PID %q to fail", value)
		}
	}
}

func TestDevWatcherStopScriptValidatesTarget(t *testing.T) {
	script := devWatcherStopScript(12345)
	for _, expected := range []string{
		"ProcessId = 12345",
		"$process.Name -ne 'wails3.exe'",
		"$commandLine -notmatch '(^|\\s)dev(\\s|$)'",
		"taskkill.exe /T /F /PID 12345",
	} {
		if !strings.Contains(script, expected) {
			t.Fatalf("shutdown script missing %q", expected)
		}
	}
}
