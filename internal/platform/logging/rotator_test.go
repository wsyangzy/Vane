package logging

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestParseSizeRejectsNonPositiveAndOverflow(t *testing.T) {
	for _, value := range []string{"0", "-1MB", "invalid", "9007199254740993KB", "9223372036854775807GB"} {
		if _, err := ParseSize(value); err == nil {
			t.Fatalf("invalid log size accepted: %q", value)
		}
	}
	for value, want := range map[string]int64{"1B": 1, "2KB": 2048, " 2mb ": 2 * 1024 * 1024, "1GB": 1024 * 1024 * 1024} {
		if got, err := ParseSize(value); err != nil || got != want {
			t.Fatalf("ParseSize(%q) = %d, %v", value, got, err)
		}
	}
}

func TestArchivingFileRotatorKeepsActiveFileAtRootAndArchivesPreviousDay(t *testing.T) {
	root := t.TempDir()
	activeFile := filepath.Join(root, "codex.log")
	rotator, err := NewArchivingFileRotator(activeFile, root, "release", "codex", 1024, 3, false)
	if err != nil {
		t.Fatalf("create archiving rotator: %v", err)
	}
	t.Cleanup(func() { _ = rotator.Close() })

	firstDay := time.Date(2026, time.July, 31, 23, 59, 0, 0, time.Local)
	secondDay := firstDay.Add(2 * time.Minute)
	if _, err := rotator.writeAt([]byte("july\n"), firstDay); err != nil {
		t.Fatalf("write july log: %v", err)
	}
	if _, err := rotator.writeAt([]byte("august\n"), secondDay); err != nil {
		t.Fatalf("write august log: %v", err)
	}

	assertFileContains(t, filepath.Join(root, "release", "codex", "2026-07", "codex-2026-07-31.log"), "july")
	assertFileContains(t, activeFile, "august")
}

func TestArchivingFileRotatorArchivesSizeRotation(t *testing.T) {
	root := t.TempDir()
	activeFile := filepath.Join(root, "app.log")
	rotator, err := NewArchivingFileRotator(activeFile, root, "dev", "app", 5, 2, false)
	if err != nil {
		t.Fatalf("create archiving rotator: %v", err)
	}
	t.Cleanup(func() { _ = rotator.Close() })

	now := time.Date(2026, 7, 14, 12, 0, 0, 0, time.Local)
	if _, err := rotator.writeAt([]byte("1234"), now); err != nil {
		t.Fatalf("write initial log: %v", err)
	}
	if _, err := rotator.writeAt([]byte("56"), now.Add(time.Second)); err != nil {
		t.Fatalf("write rotated log: %v", err)
	}

	assertFileContains(t, activeFile, "56")
	assertFileContains(t, filepath.Join(root, "dev", "app", "2026-07", "app-2026-07-14.log"), "1234")
}

func TestFileRotatorKeepsFixedPathAndSizeRotation(t *testing.T) {
	filename := filepath.Join(t.TempDir(), "legacy.log")
	rotator, err := NewFileRotator(filename, 5, 2, false)
	if err != nil {
		t.Fatalf("create fixed rotator: %v", err)
	}
	t.Cleanup(func() { _ = rotator.Close() })

	if _, err := rotator.writeAt([]byte("1234"), time.Date(2026, 7, 14, 12, 0, 0, 0, time.Local)); err != nil {
		t.Fatalf("write initial log: %v", err)
	}
	if _, err := rotator.writeAt([]byte("56"), time.Date(2026, 7, 14, 12, 0, 1, 0, time.Local)); err != nil {
		t.Fatalf("write rotated log: %v", err)
	}

	assertFileContains(t, filename, "56")
	rotated, err := filepath.Glob(filename + ".*")
	if err != nil {
		t.Fatalf("glob rotated logs: %v", err)
	}
	if len(rotated) != 1 {
		t.Fatalf("expected one rotated log, got %v", rotated)
	}
	assertFileContains(t, rotated[0], "1234")
}

func assertFileContains(t *testing.T, filename, expected string) {
	t.Helper()
	contents, err := os.ReadFile(filename)
	if err != nil {
		t.Fatalf("read %s: %v", filename, err)
	}
	if !strings.Contains(string(contents), expected) {
		t.Fatalf("%s does not contain %q: %s", filename, expected, contents)
	}
}
