package storage

import (
	"context"
	"fmt"

	_ "modernc.org/sqlite"
)

func (s *SQLiteAdapter) normalizeCodexChannelPriorities(ctx context.Context) error {
	exists, err := s.tableExists(ctx, "codex_channels")
	if err != nil || !exists {
		return err
	}
	exists, err = s.columnExists(ctx, "codex_channels", "priority")
	if err != nil || !exists {
		return err
	}
	rows, err := s.writeDB.QueryContext(ctx, `
		SELECT id, priority
		FROM codex_channels
		ORDER BY priority ASC, id ASC`)
	if err != nil {
		return fmt.Errorf("查询 Codex 渠道优先级失败: %w", err)
	}
	type channelPriority struct {
		id       int64
		priority int
	}
	var channels []channelPriority
	maxPriority := -1
	for rows.Next() {
		var channel channelPriority
		if err := rows.Scan(&channel.id, &channel.priority); err != nil {
			rows.Close()
			return fmt.Errorf("读取 Codex 渠道优先级失败: %w", err)
		}
		channels = append(channels, channel)
		if channel.priority > maxPriority {
			maxPriority = channel.priority
		}
	}
	if err := rows.Close(); err != nil {
		return err
	}

	used := make(map[int]struct{}, len(channels))
	for _, channel := range channels {
		if _, exists := used[channel.priority]; !exists {
			used[channel.priority] = struct{}{}
			continue
		}
		maxPriority++
		if _, err := s.writeDB.ExecContext(ctx, `UPDATE codex_channels SET priority = ? WHERE id = ?`, maxPriority, channel.id); err != nil {
			return fmt.Errorf("修复 Codex 渠道重复优先级失败: %w", err)
		}
		used[maxPriority] = struct{}{}
	}
	return nil
}
