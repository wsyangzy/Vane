package service

import (
	"fmt"
	"strings"

	"github.com/wsyangzy/Vane/internal/providers/claude/store"
)

// validateRecord 验证端点记录
func (s *EndpointService) validateRecord(record *store.EndpointRecord) error {
	if record.Name == "" {
		return fmt.Errorf("端点名称不能为空")
	}
	if record.URL == "" {
		return fmt.Errorf("端点 URL 不能为空")
	}
	if record.Channel == "" {
		return fmt.Errorf("端点渠道不能为空")
	}
	modelMultipliers, err := normalizeEndpointModelMultipliers(record.ModelMultipliers)
	if err != nil {
		return err
	}
	record.ModelMultipliers = modelMultipliers
	return nil
}

func normalizeEndpointModelMultipliers(rules []store.EndpointModelMultiplierRecord) ([]store.EndpointModelMultiplierRecord, error) {
	if len(rules) == 0 {
		return nil, nil
	}

	normalized := make([]store.EndpointModelMultiplierRecord, 0, len(rules))
	seen := make(map[string]struct{}, len(rules))
	for _, rule := range rules {
		model := strings.TrimSpace(rule.Model)
		if model == "" {
			continue
		}
		if _, exists := seen[model]; exists {
			return nil, fmt.Errorf("模型附加倍率重复：%s", model)
		}
		seen[model] = struct{}{}

		normalized = append(normalized, store.EndpointModelMultiplierRecord{
			Model:                   model,
			CostMultiplier:          normalizePositiveMultiplier(rule.CostMultiplier),
			InputCostMultiplier:     normalizePositiveMultiplier(rule.InputCostMultiplier),
			OutputCostMultiplier:    normalizePositiveMultiplier(rule.OutputCostMultiplier),
			CacheReadCostMultiplier: normalizePositiveMultiplier(rule.CacheReadCostMultiplier),
		})
	}
	return normalized, nil
}

func normalizePositiveMultiplier(value float64) float64 {
	if value <= 0 {
		return 1.0
	}
	return value
}
