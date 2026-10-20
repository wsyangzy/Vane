package service

import (
	"context"
	"fmt"
	"log/slog"
	"strconv"

	settingsstore "github.com/wsyangzy/Vane/internal/settings/store"
)

// Set 设置单个值
func (s *SettingsService) Set(ctx context.Context, category, key, value string) error {
	provider := ProviderGlobal
	if category == CategoryFailover {
		provider = ProviderClaude
	}
	return s.SetScoped(ctx, provider, category, key, value)
}

func (s *SettingsService) SetScoped(ctx context.Context, provider SettingsProvider, category, key, value string) error {
	if s == nil || s.store == nil {
		return fmt.Errorf("设置存储服务未启用")
	}
	if err := validateSettingValue(provider, category, key, value); err != nil {
		return err
	}
	previous, err := s.store.GetScoped(ctx, string(provider), category, key)
	if err != nil {
		return err
	}
	if err := s.store.SetScoped(ctx, string(provider), category, key, value); err != nil {
		return err
	}
	if err := s.triggerOnChangeScoped(provider, category, key); err != nil {
		rollbackErr := runSettingsRollback(func(rollbackCtx context.Context) error {
			return s.restoreScopedSetting(rollbackCtx, provider, category, key, previous)
		})
		if rollbackErr != nil {
			return fmt.Errorf("应用设置失败: %w；恢复旧值失败: %v", err, rollbackErr)
		}
		return fmt.Errorf("应用设置失败: %w", err)
	}
	return nil
}

func (s *SettingsService) triggerOnChangeScoped(provider SettingsProvider, category, key string) error {
	record, _ := s.store.GetScoped(context.Background(), string(provider), category, key)
	if record != nil && record.RequiresRestart {
		return nil
	}
	if s.onChangeFunc != nil {
		return s.onChangeFunc()
	}
	return nil
}

func (s *SettingsService) restoreScopedSetting(ctx context.Context, provider SettingsProvider, category, key string, previous *settingsstore.SettingRecord) error {
	if previous != nil {
		return s.store.SetScoped(ctx, string(provider), category, key, previous.Value)
	}
	if deleter, ok := s.store.(scopedSettingDeleter); ok {
		return deleter.DeleteScoped(ctx, string(provider), category, key)
	}
	return s.store.SetScoped(ctx, string(provider), category, key, "")
}

func (s *SettingsService) restoreCategory(ctx context.Context, provider *SettingsProvider, category string, previous []*settingsstore.SettingRecord) error {
	var err error
	if provider == nil {
		err = s.store.DeleteByCategory(ctx, category)
	} else {
		err = s.store.DeleteByCategoryScoped(ctx, string(*provider), category)
	}
	if err != nil {
		return err
	}
	if len(previous) == 0 {
		return nil
	}
	return s.store.BatchSet(ctx, previous)
}

// BatchSet 批量设置（不触发回调，需手动触发）
func (s *SettingsService) BatchSet(ctx context.Context, records []*settingsstore.SettingRecord) error {
	return s.store.BatchSet(ctx, records)
}

// UpdateAndApply 批量更新并应用（触发热更新）
// 只更新 value，保留 label、description 等元数据
func (s *SettingsService) UpdateAndApply(ctx context.Context, records []*settingsstore.SettingRecord) error {
	if s == nil || s.store == nil {
		return fmt.Errorf("设置存储服务未启用")
	}
	previous := make([]*settingsstore.SettingRecord, 0, len(records))
	for _, record := range records {
		if record == nil {
			return fmt.Errorf("设置记录不能为空")
		}
		provider := SettingsProvider(record.Provider)
		if provider == "" {
			provider = ProviderGlobal
			if record.Category == CategoryFailover {
				provider = ProviderClaude
			}
		}
		if err := validateSettingValue(provider, record.Category, record.Key, record.Value); err != nil {
			return err
		}
		old, err := s.store.GetScoped(ctx, string(provider), record.Category, record.Key)
		if err != nil {
			return err
		}
		previous = append(previous, old)
	}
	if err := s.store.BatchUpdateValues(ctx, records); err != nil {
		return fmt.Errorf("保存设置失败: %w", err)
	}

	// 触发配置热更新
	if s.onChangeFunc != nil {
		if err := s.onChangeFunc(); err != nil {
			rollbackErr := runSettingsRollback(func(rollbackCtx context.Context) error {
				for i, record := range records {
					provider := SettingsProvider(record.Provider)
					if provider == "" {
						provider = ProviderGlobal
						if record.Category == CategoryFailover {
							provider = ProviderClaude
						}
					}
					if restoreErr := s.restoreScopedSetting(rollbackCtx, provider, record.Category, record.Key, previous[i]); restoreErr != nil {
						return restoreErr
					}
				}
				return nil
			})
			if rollbackErr != nil {
				return fmt.Errorf("应用设置失败: %w；恢复旧值失败: %v", err, rollbackErr)
			}
			return fmt.Errorf("应用设置失败: %w", err)
		}
		slog.Info("✅ [SettingsService] 设置已保存并应用热更新")
	}

	return nil
}

func validateSettingValue(provider SettingsProvider, category, key, value string) error {
	if err := validateSettingsProvider(provider); err != nil {
		return err
	}
	if category != CategoryRetention {
		return nil
	}

	switch key {
	case "retention_days":
		days, err := strconv.Atoi(value)
		if err != nil || days < 0 {
			return fmt.Errorf("数据保留天数必须是大于或等于 0 的整数")
		}
	}
	return nil
}

// ResetCategory 重置分类设置为默认值
func (s *SettingsService) ResetCategory(ctx context.Context, category string) error {
	if s == nil || s.store == nil {
		return fmt.Errorf("设置存储服务未启用")
	}
	previous, err := s.store.GetByCategory(ctx, category)
	if err != nil {
		return err
	}
	// 删除当前分类的所有设置
	if err := s.store.DeleteByCategory(ctx, category); err != nil {
		return fmt.Errorf("删除分类设置失败: %w", err)
	}

	// 重新初始化默认值
	defaults := s.getDefaultsForCategory(category)
	if len(defaults) > 0 {
		if err := s.store.BatchSet(ctx, defaults); err != nil {
			return fmt.Errorf("重置默认值失败: %w", err)
		}
	}

	// 触发热更新
	if s.onChangeFunc != nil {
		if err := s.onChangeFunc(); err != nil {
			rollbackErr := runSettingsRollback(func(rollbackCtx context.Context) error {
				return s.restoreCategory(rollbackCtx, nil, category, previous)
			})
			if rollbackErr != nil {
				return fmt.Errorf("应用重置设置失败: %w；恢复旧值失败: %v", err, rollbackErr)
			}
			return fmt.Errorf("应用重置设置失败: %w", err)
		}
	}

	slog.Info(fmt.Sprintf("✅ [SettingsService] 分类 %s 已重置为默认值", category))
	return nil
}

func (s *SettingsService) ResetCategoryScoped(ctx context.Context, provider SettingsProvider, category string) error {
	if err := validateSettingsProvider(provider); err != nil {
		return err
	}
	previous, err := s.store.GetByCategoryScoped(ctx, string(provider), category)
	if err != nil {
		return err
	}
	if err := s.store.DeleteByCategoryScoped(ctx, string(provider), category); err != nil {
		return fmt.Errorf("删除分类设置失败: %w", err)
	}
	defaults := s.getDefaultsForCategory(category)
	for _, record := range defaults {
		record.Provider = string(provider)
		if provider == ProviderCodex && category == CategoryFailover && record.Key == "enabled" {
			record.Value = "false"
		}
	}
	if len(defaults) > 0 {
		if err := s.store.BatchSet(ctx, defaults); err != nil {
			return fmt.Errorf("重置默认值失败: %w", err)
		}
	}
	if s.onChangeFunc != nil {
		if err := s.onChangeFunc(); err != nil {
			rollbackErr := runSettingsRollback(func(rollbackCtx context.Context) error {
				return s.restoreCategory(rollbackCtx, &provider, category, previous)
			})
			if rollbackErr != nil {
				return fmt.Errorf("应用重置设置失败: %w；恢复旧值失败: %v", err, rollbackErr)
			}
			return fmt.Errorf("应用重置设置失败: %w", err)
		}
	}
	return nil
}

// triggerOnChange 触发变更回调（检查是否需要重启）
func (s *SettingsService) triggerOnChange(category, key string) error {
	record, _ := s.store.Get(context.Background(), category, key)
	if record != nil && record.RequiresRestart {
		slog.Info(fmt.Sprintf("⚠️ [SettingsService] 设置 %s.%s 已保存，需要重启生效", category, key))
		return nil // 需要重启的配置不触发热更新
	}

	if s.onChangeFunc != nil {
		return s.onChangeFunc()
	}
	return nil
}
