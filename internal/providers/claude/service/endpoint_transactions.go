package service

import (
	"context"
	"fmt"
	"sort"

	"github.com/wsyangzy/Vane/internal/providers/claude/store"
)

func (s *EndpointService) findNextFailoverEndpoint(ctx context.Context, channel string, currentID int64) (*store.EndpointRecord, error) {
	if s == nil || s.store == nil {
		return nil, fmt.Errorf("端点存储服务未启用")
	}
	candidates, err := s.store.ListByChannel(ctx, channel)
	if err != nil {
		return nil, fmt.Errorf("获取渠道端点列表失败: %w", err)
	}
	filtered := make([]*store.EndpointRecord, 0, len(candidates))
	for _, candidate := range candidates {
		if candidate == nil || candidate.ID == currentID || !candidate.FailoverEnabled {
			continue
		}
		filtered = append(filtered, candidate)
	}
	if len(filtered) == 0 {
		return nil, nil
	}
	sort.SliceStable(filtered, func(i, j int) bool {
		if filtered[i].Priority != filtered[j].Priority {
			return filtered[i].Priority < filtered[j].Priority
		}
		if !filtered[i].CreatedAt.Equal(filtered[j].CreatedAt) {
			return filtered[i].CreatedAt.Before(filtered[j].CreatedAt)
		}
		return filtered[i].ID < filtered[j].ID
	})
	return filtered[0], nil
}

func (s *EndpointService) withPriorityTransaction(ctx context.Context, fn func(store.EndpointStore) error) error {
	if s == nil || s.store == nil {
		return fmt.Errorf("端点存储服务未启用")
	}
	endpointPriorityMu.Lock()
	defer endpointPriorityMu.Unlock()

	starter, ok := s.store.(endpointTransactionStarter)
	if !ok {
		return fn(s.store)
	}
	tx, err := starter.BeginTx(ctx)
	if err != nil {
		return fmt.Errorf("开启端点优先级事务失败: %w", err)
	}
	txStore := s.store.WithTx(tx)
	if err := fn(txStore); err != nil {
		_ = tx.Rollback()
		return err
	}
	if err := tx.Commit(); err != nil {
		_ = tx.Rollback()
		return fmt.Errorf("提交端点优先级事务失败: %w", err)
	}
	return nil
}
