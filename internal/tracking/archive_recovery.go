package tracking

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// dumpFailedBatchToFile 将归档失败的事件序列化为 JSONL 写入本地降级文件
//
// 🔧 [P1.3 新增] 防止归档失败导致已完成请求数据丢失
//   - 文件位置：与数据库同目录下的 archive_failures.jsonl
//   - 格式：每行一个 JSON 对象（含完整 ArchiveEvent.Request）
//   - 滚动策略：单文件超过 10 MB 时自动重命名为 .{timestamp} 备份并新建
//   - 运维方式：用户/管理员可后续用脚本读取并通过 SQL 导入到数据库
func (am *ArchiveManager) dumpFailedBatchToFile(events []*ArchiveEvent, lastErr error) (err error) {
	if len(events) == 0 {
		return nil
	}

	// 推断降级文件路径：用 adapter 的数据库路径同目录
	dumpPath := am.getDumpFilePath()
	if dumpPath == "" {
		return fmt.Errorf("cannot determine dump file path")
	}

	am.dumpMu.Lock()
	defer am.dumpMu.Unlock()

	// 滚动策略：单文件超过 10 MB 时备份
	const maxFileSize = 10 * 1024 * 1024
	if info, err := os.Stat(dumpPath); err == nil && info.Size() > maxFileSize {
		backupPath := dumpPath + "." + time.Now().Format("20060102-150405.000000000")
		if renameErr := os.Rename(dumpPath, backupPath); renameErr != nil {
			slog.Warn("📦 降级文件备份失败（继续追加到原文件）",
				"path", dumpPath, "error", renameErr)
		}
	}

	f, err := os.OpenFile(dumpPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return fmt.Errorf("open dump file: %w", err)
	}
	defer func() {
		if closeErr := f.Close(); closeErr != nil && err == nil {
			err = fmt.Errorf("close dump file: %w", closeErr)
		}
	}()

	dumpedCount := 0
	for _, event := range events {
		if event == nil || event.Request == nil {
			continue
		}
		if event.Dumped {
			continue
		}
		// 构造降级记录：包含失败原因和原始事件，便于排查
		record := map[string]any{
			"dumped_at":   time.Now().Format(time.RFC3339Nano),
			"reason":      "archive_failed_after_retries",
			"last_error":  lastErr.Error(),
			"environment": event.Environment,
			"request":     event.Request,
		}
		data, marshalErr := json.Marshal(record)
		if marshalErr != nil {
			slog.Warn("📦 降级记录序列化失败", "request_id", event.Request.RequestID, "error", marshalErr)
			continue
		}
		if _, writeErr := f.Write(append(data, '\n')); writeErr != nil {
			return fmt.Errorf("write dump record: %w", writeErr)
		}
		event.Dumped = true
		dumpedCount++
	}

	if dumpedCount > 0 {
		slog.Warn("📦 批量归档失败：已降级到本地文件，等待手动恢复",
			"dump_file", dumpPath,
			"dumped_count", dumpedCount,
			"original_error", lastErr)
	}
	return nil
}

// getDumpFilePath 推断降级文件路径（与数据库同目录）
func (am *ArchiveManager) getDumpFilePath() string {
	if am.adapter == nil {
		return ""
	}
	dbPath := strings.TrimSpace(am.adapter.DatabasePath())
	if dbPath == "" || dbPath == ":memory:" {
		return ""
	}
	return dbPath + ".archive_failures.jsonl"
}

// ReplayFailedArchives 恢复降级文件中的完整请求。文件保留到所有批次成功，
// 这样进程中途退出时下次启动仍可幂等重放。
func (am *ArchiveManager) ReplayFailedArchives() error {
	path := am.getDumpFilePath()
	if path == "" {
		return nil
	}

	// 回放认领采用原子重命名。进程可能在认领后崩溃，因此启动/定时回放
	// 必须同时接管遗留的 .replaying-* 文件，不能只检查主文件。
	am.dumpMu.Lock()
	var backups, orphans []string
	if entries, err := os.ReadDir(filepath.Dir(path)); err == nil {
		prefix := filepath.Base(path) + "."
		for _, entry := range entries {
			if entry.IsDir() || !strings.HasPrefix(entry.Name(), prefix) {
				continue
			}
			suffix := strings.TrimPrefix(entry.Name(), prefix)
			candidate := filepath.Join(filepath.Dir(path), entry.Name())
			switch {
			case strings.HasPrefix(suffix, "replaying-"):
				orphans = append(orphans, candidate)
			case isArchiveBackupSuffix(suffix):
				backups = append(backups, candidate)
			}
		}
	} else if !os.IsNotExist(err) {
		slog.Warn("📦 扫描归档失败文件目录失败", "path", filepath.Dir(path), "error", err)
	}
	sort.Strings(backups)
	sort.Strings(orphans)
	// 旧轮转文件最早，遗留回放文件其次，当前主文件最新；同一 request_id
	// 出现多份快照时，后写入的最新快照保持权威。
	candidates := append(backups, orphans...)
	if _, err := os.Stat(path); err == nil {
		candidates = append(candidates, path)
	}
	am.dumpMu.Unlock()

	for _, candidate := range candidates {
		if err := am.replayFailedArchiveFile(path, candidate); err != nil {
			return err
		}
	}
	return nil
}

func isArchiveBackupSuffix(suffix string) bool {
	for _, layout := range []string{"20060102-150405", "20060102-150405.000000000"} {
		if _, err := time.Parse(layout, suffix); err == nil {
			return true
		}
	}
	return false
}

func (am *ArchiveManager) replayFailedArchiveFile(path, candidate string) error {
	am.dumpMu.Lock()
	processingPath := path + ".replaying-" + time.Now().Format("20060102-150405.000000000")
	err := os.Rename(candidate, processingPath)
	am.dumpMu.Unlock()
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("claim failed archive file: %w", err)
	}

	f, err := os.Open(processingPath)
	if os.IsNotExist(err) {
		return am.restoreReplayFile(path, processingPath, fmt.Errorf("open failed archive file: %w", err))
	}
	if err != nil {
		return am.restoreReplayFile(path, processingPath, fmt.Errorf("open failed archive file: %w", err))
	}

	var batch []*ArchiveEvent
	var replayedIDs []string
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 64*1024), 8*1024*1024)
	for scanner.Scan() {
		var record struct {
			Environment string         `json:"environment"`
			Request     *ActiveRequest `json:"request"`
		}
		if err := json.Unmarshal(scanner.Bytes(), &record); err != nil {
			_ = f.Close()
			return am.restoreReplayFile(path, processingPath, fmt.Errorf("decode failed archive record: %w", err))
		}
		if record.Request == nil || record.Request.RequestID == "" {
			_ = f.Close()
			return am.restoreReplayFile(path, processingPath, fmt.Errorf("invalid failed archive record: request is missing"))
		}
		if record.Environment == "" {
			record.Environment = am.config.Environment
		}
		batch = append(batch, &ArchiveEvent{Request: record.Request, Timestamp: time.Now(), Environment: normalizeTrackingEnvironment(record.Environment)})
		replayedIDs = append(replayedIDs, record.Request.RequestID)
		if len(batch) >= am.config.BatchSize {
			if err := am.batchInsert(batch); err != nil {
				_ = f.Close()
				return am.restoreReplayFile(path, processingPath, err)
			}
			batch = batch[:0]
		}
	}
	if err := scanner.Err(); err != nil {
		_ = f.Close()
		return am.restoreReplayFile(path, processingPath, fmt.Errorf("read failed archive file: %w", err))
	}
	if len(batch) > 0 {
		if err := am.batchInsert(batch); err != nil {
			_ = f.Close()
			return am.restoreReplayFile(path, processingPath, err)
		}
	}
	if err := f.Close(); err != nil {
		return am.restoreReplayFile(path, processingPath, fmt.Errorf("close replay file: %w", err))
	}
	am.dumpMu.Lock()
	err = os.Rename(processingPath, path+".replayed-"+time.Now().Format("20060102-150405.000000000"))
	am.dumpMu.Unlock()
	if err != nil {
		// 保留 .replaying-* 孤儿文件，下一轮会自动重新认领；数据库已成功写入，
		// 但在文件完成改名前不能清理热池，避免“文件状态未完成”时丢恢复线索。
		return fmt.Errorf("archive failed file after replay: %w", err)
	}
	if am.hotPool != nil && len(replayedIDs) > 0 {
		am.hotPool.ConfirmArchived(replayedIDs)
	}
	slog.Info("📦 已回放归档失败文件", "path", path)
	return nil
}

func (am *ArchiveManager) restoreReplayFile(path, processingPath string, replayErr error) error {
	if processingPath == "" {
		return replayErr
	}
	am.dumpMu.Lock()
	defer am.dumpMu.Unlock()
	f, err := os.Open(processingPath)
	if err != nil {
		return fmt.Errorf("%v; reopen replay file: %w", replayErr, err)
	}
	out, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		_ = f.Close()
		return fmt.Errorf("%v; reopen dump file: %w", replayErr, err)
	}
	_, copyErr := io.Copy(out, f)
	closeErr := out.Close()
	inputCloseErr := f.Close()
	if copyErr != nil || closeErr != nil || inputCloseErr != nil {
		return fmt.Errorf("%v; restore replay file failed: copy=%v close=%v input_close=%v", replayErr, copyErr, closeErr, inputCloseErr)
	}
	if err := os.Remove(processingPath); err != nil {
		return fmt.Errorf("%v; remove replay file: %w", replayErr, err)
	}
	return replayErr
}
