package app

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLogExportCancellationNeverWrites(t *testing.T) {
	for _, err := range []error{errors.New("cancelled by user"), fmt.Errorf("native dialog: %w", errors.New("cancelled by user"))} {
		t.Run(err.Error(), func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "existing.txt")
			if writeErr := os.WriteFile(path, []byte("keep existing content"), 0600); writeErr != nil {
				t.Fatal(writeErr)
			}
			result := completeLogExport("replacement", path, err)
			if result.Success || result.Error != "cancelled" {
				t.Fatalf("expected cancellation, got %+v", result)
			}
			content, readErr := os.ReadFile(path)
			if readErr != nil || string(content) != "keep existing content" {
				t.Fatalf("cancellation changed the existing file: %q, %v", content, readErr)
			}
		})
	}
}

func TestLogExportEmptySelectionIsCancellation(t *testing.T) {
	result := completeLogExport("log", "", nil)
	if result.Success || result.Error != "cancelled" {
		t.Fatalf("expected cancellation for an empty selection, got %+v", result)
	}
}

func TestLogExportDialogFailureIsNotCancellation(t *testing.T) {
	for _, err := range []error{errors.New("access denied"), errors.New("cancelled by user: failed to release dialog")} {
		path := filepath.Join(t.TempDir(), "never-written.txt")
		result := completeLogExport("log", path, err)
		if result.Success || !strings.Contains(result.Error, "打开保存对话框失败: "+err.Error()) {
			t.Fatalf("expected the real dialog error, got %+v", result)
		}
		if _, statErr := os.Stat(path); !os.IsNotExist(statErr) {
			t.Fatalf("dialog failure created a file: %v", statErr)
		}
	}
}

func TestLogExportWriteFailureIsReported(t *testing.T) {
	result := completeLogExport("log", t.TempDir(), nil)
	if result.Success || !strings.HasPrefix(result.Error, "写入文件失败:") {
		t.Fatalf("expected a write error, got %+v", result)
	}
}

func TestLogExportWritesContentAndReportsFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "系统日志.txt")
	content := "[INFO] 运行正常\n[WARN] retry=0"
	result := completeLogExport(content, path, nil)
	if !result.Success || result.Error != "" || result.FilePath != path || result.FileName != "系统日志.txt" || result.FileSize != int64(len(content)) {
		t.Fatalf("unexpected saved file: %+v", result)
	}
	actual, err := os.ReadFile(path)
	if err != nil || string(actual) != content {
		t.Fatalf("unexpected file contents: %q, %v", actual, err)
	}
}
