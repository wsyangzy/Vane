package tracking

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

func (ut *UsageTracker) ArchiveDirectory() string {
	ut.historyMu.RLock()
	defer ut.historyMu.RUnlock()
	return ut.config.ArchiveDirectory
}

// SwitchArchiveDirectory copies a consistent snapshot of each monthly database
// before switching readers and writers together. Existing files are never merged
// or overwritten; the source remains available as a manual rollback copy.
func (ut *UsageTracker) SwitchArchiveDirectory(ctx context.Context, directory string) error {
	if !filepath.IsAbs(directory) {
		return fmt.Errorf("历史归档目录必须为绝对路径")
	}
	directory = filepath.Clean(directory)
	ut.historyMu.Lock()
	defer ut.historyMu.Unlock()
	ut.writeMu.Lock()
	defer ut.writeMu.Unlock()
	if sameArchiveDirectory(ut.config.ArchiveDirectory, directory) {
		return nil
	}
	if err := os.MkdirAll(directory, 0755); err != nil {
		return fmt.Errorf("无法创建历史归档目录: %w", err)
	}
	entries, err := os.ReadDir(directory)
	if err != nil {
		return err
	}
	if len(entries) != 0 {
		return fmt.Errorf("目标归档目录已有内容，请选择空目录，避免覆盖或混合历史数据")
	}
	probe, err := os.CreateTemp(directory, ".vane-write-check-*")
	if err != nil {
		return fmt.Errorf("目标归档目录不可写: %w", err)
	}
	probePath := probe.Name()
	if err := probe.Close(); err != nil {
		_ = os.Remove(probePath)
		return err
	}
	if err := os.Remove(probePath); err != nil {
		return err
	}
	files, err := ut.historyArchiveFilesLocked()
	if err != nil {
		return err
	}
	var copied []string
	committed := false
	defer func() {
		if !committed {
			for _, path := range copied {
				_ = os.Remove(path)
			}
		}
	}()
	for _, file := range files {
		path := filepath.Join(directory, filepath.Base(file.path))
		// Reserve only task-owned destinations, so a failed VACUUM cannot remove
		// an unrelated file created concurrently by another process.
		dest, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
		if err != nil {
			return fmt.Errorf("创建归档快照失败: %w", err)
		}
		_ = dest.Close()
		copied = append(copied, path)
		db, err := sql.Open("sqlite", file.path)
		if err != nil {
			return err
		}
		_, err = db.ExecContext(ctx, "VACUUM INTO '"+strings.ReplaceAll(path, "'", "''")+"'")
		closeErr := db.Close()
		if err != nil {
			return fmt.Errorf("复制归档 %s 失败: %w", file.month, err)
		}
		if closeErr != nil {
			return closeErr
		}
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	ut.config.ArchiveDirectory = directory
	ut.invalidateArchiveTotalsLocked()
	committed = true
	return nil
}

func sameArchiveDirectory(a, b string) bool {
	if filepath.Clean(a) == filepath.Clean(b) {
		return true
	}
	left, leftErr := os.Stat(a)
	right, rightErr := os.Stat(b)
	return leftErr == nil && rightErr == nil && os.SameFile(left, right)
}
