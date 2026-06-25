package main

import (
	"context"
	"fmt"
	"time"

	"ai-relay-forwarder/internal/privacy"
)

func (a *App) errPrivacyServiceDisabled() error {
	return fmt.Errorf("隐私防护服务未启用（管理数据库未就绪）。请稍等重试；若一直失败，请检查是否有另一个 AI Relay Forwarder 实例占用数据库或重启应用。")
}

func (a *App) GetPrivacyConfig() (privacy.PrivacyConfig, error) {
	a.mu.RLock()
	privacyService := a.privacyService
	a.mu.RUnlock()

	if privacyService == nil {
		return privacy.PrivacyConfig{}, a.errPrivacyServiceDisabled()
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	rules, err := privacyService.ListRules(ctx)
	if err != nil {
		return privacy.PrivacyConfig{}, fmt.Errorf("获取隐私规则失败: %w", err)
	}
	return privacy.PrivacyConfig{Enabled: true, Rules: rules}, nil
}

func (a *App) SetPrivacyEnabled(enabled bool) error {
	a.mu.RLock()
	logger := a.logger
	a.mu.RUnlock()

	if logger != nil {
		logger.Info("隐私防护全局开关已废弃，规则启用状态由单条规则控制", "requested_enabled", enabled)
	}
	return nil
}

func (a *App) CreatePrivacyRule(input privacy.PrivacyRuleInput) (privacy.PrivacyRule, error) {
	a.mu.RLock()
	privacyService := a.privacyService
	a.mu.RUnlock()

	if privacyService == nil {
		return privacy.PrivacyRule{}, a.errPrivacyServiceDisabled()
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	rule, err := privacyService.CreateRule(ctx, input)
	if err != nil {
		return privacy.PrivacyRule{}, fmt.Errorf("创建隐私规则失败: %w", err)
	}
	return rule, nil
}

func (a *App) UpdatePrivacyRule(id int64, input privacy.PrivacyRuleInput) (privacy.PrivacyRule, error) {
	a.mu.RLock()
	privacyService := a.privacyService
	a.mu.RUnlock()

	if privacyService == nil {
		return privacy.PrivacyRule{}, a.errPrivacyServiceDisabled()
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	rule, err := privacyService.UpdateRule(ctx, id, input)
	if err != nil {
		return privacy.PrivacyRule{}, fmt.Errorf("更新隐私规则失败: %w", err)
	}
	return rule, nil
}

func (a *App) DeletePrivacyRule(id int64) error {
	a.mu.RLock()
	privacyService := a.privacyService
	a.mu.RUnlock()

	if privacyService == nil {
		return a.errPrivacyServiceDisabled()
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := privacyService.DeleteRule(ctx, id); err != nil {
		return fmt.Errorf("删除隐私规则失败: %w", err)
	}
	return nil
}

func (a *App) GetPrivacySyntheticValues() ([]privacy.SyntheticValueRecord, error) {
	a.mu.RLock()
	privacyService := a.privacyService
	a.mu.RUnlock()

	if privacyService == nil {
		return nil, a.errPrivacyServiceDisabled()
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	records, err := privacyService.ListSyntheticValues(ctx)
	if err != nil {
		return nil, fmt.Errorf("获取模拟值记录失败: %w", err)
	}
	return records, nil
}

func (a *App) ClearPrivacySyntheticValues() error {
	a.mu.RLock()
	privacyService := a.privacyService
	a.mu.RUnlock()

	if privacyService == nil {
		return a.errPrivacyServiceDisabled()
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := privacyService.ClearSyntheticValues(ctx); err != nil {
		return fmt.Errorf("清空模拟值记录失败: %w", err)
	}
	return nil
}

func (a *App) DeletePrivacySyntheticValue(kind string, rawHash string) error {
	a.mu.RLock()
	privacyService := a.privacyService
	a.mu.RUnlock()

	if privacyService == nil {
		return a.errPrivacyServiceDisabled()
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := privacyService.DeleteSyntheticValue(ctx, kind, rawHash); err != nil {
		return fmt.Errorf("删除模拟值记录失败: %w", err)
	}
	return nil
}

func (a *App) PreviewPrivacyRedaction(input string) (privacy.PreviewResult, error) {
	a.mu.RLock()
	privacyService := a.privacyService
	a.mu.RUnlock()

	if privacyService == nil {
		return privacy.PreviewResult{}, a.errPrivacyServiceDisabled()
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	result, err := privacyService.Preview(ctx, input)
	if err != nil {
		return privacy.PreviewResult{}, fmt.Errorf("预览隐私脱敏失败: %w", err)
	}
	return result, nil
}
