package service

import (
	"context"
	"fmt"
	"log/slog"
	"math"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/wsyangzy/Vane/internal/platform/logging"
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
	if err := s.validateCooldownUpdate(ctx, []*settingsstore.SettingRecord{{Category: category, Key: key, Value: value}}); err != nil {
		return err
	}
	if err := s.validateAuthUpdate(ctx, []*settingsstore.SettingRecord{{Provider: string(provider), Category: category, Key: key, Value: value}}); err != nil {
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
	if err := s.validateCooldownUpdate(ctx, records); err != nil {
		return err
	}
	if err := s.validateAuthUpdate(ctx, records); err != nil {
		return err
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
	if category == CategoryTimezone {
		return fmt.Errorf("时区固定为北京时间（UTC+8），不支持修改")
	}
	if category == CategoryRetry {
		switch key {
		case "model_rule_fallback", "mapping_rule_fallback", "non_stream_rule_fallback", "intent_rule_fallback":
			return fmt.Errorf("各类规则的默认回退已合并，请修改全局规则回退设置")
		}
	}
	// Validate the controls offered by the current settings page. Hidden legacy
	// records keep their compatibility contracts.
	current := category == CategoryServer || category == CategoryHealth || category == CategoryAuth ||
		category == CategoryTokenCounting || category == CategoryCache || category == CategoryLogging ||
		category == CategoryRetry && IsCurrentRetrySetting(key) ||
		category == CategoryRequest && (key == "global_timeout" || key == "response_header_timeout" || key == "default_response_header_timeout") ||
		category == CategoryRetention && key == "retention_days"
	if current {
		if provider != ProviderGlobal {
			return fmt.Errorf("此设置由 Claude 与 Codex 共用，请使用 global 作用域")
		}
		for _, setting := range (&SettingsService{}).getDefaultsForCategory(category) {
			if setting.Key != key {
				continue
			}
			switch setting.ValueType {
			case ValueTypeBool:
				if value != "true" && value != "false" {
					return fmt.Errorf("%s必须选择开启或关闭", setting.Label)
				}
			case ValueTypeDuration:
				if duration, err := time.ParseDuration(value); err != nil || duration <= 0 {
					return fmt.Errorf("%s必须是大于 0 的有效时长", setting.Label)
				}
			case ValueTypeInt:
				number, err := strconv.Atoi(value)
				minimum := 1
				if category == CategoryRetention {
					minimum = 0
				}
				if err != nil || number < minimum {
					return fmt.Errorf("%s必须是大于或等于 %d 的整数", setting.Label, minimum)
				}
				if category == CategoryServer && number > 65535 {
					return fmt.Errorf("端口号必须在 1–65535 之间")
				}
			case ValueTypeFloat:
				number, err := strconv.ParseFloat(value, 64)
				if err != nil || math.IsNaN(number) || math.IsInf(number, 0) || number <= 0 {
					return fmt.Errorf("%s必须是大于 0 的有效数字", setting.Label)
				}
			case ValueTypeEnum:
				valid := false
				for _, option := range setting.EnumOptions {
					valid = valid || value == option.Value
				}
				if !valid {
					return fmt.Errorf("%s不是有效选项", setting.Label)
				}
			}
			break
		}
		if category == CategoryHealth && key == "health_path" {
			path, err := url.ParseRequestURI(value)
			if err != nil || !strings.HasPrefix(value, "/") || strings.HasPrefix(value, "//") || path.IsAbs() || path.Fragment != "" {
				return fmt.Errorf("检查路径必须是以 / 开头的 API 路径")
			}
		}
		if category == CategoryLogging && key == "max_file_size" {
			if size, err := logging.ParseSize(value); err != nil || size <= 0 {
				return fmt.Errorf("单文件轮转大小必须大于 0，例如 100MB；支持 B、KB、MB、GB")
			}
		}
		if category == CategoryAuth && key == "token" && value != "" &&
			(strings.TrimSpace(value) != value || strings.ContainsAny(value, "\r\n\x00\t ")) {
			return fmt.Errorf("应用访问令牌不能包含空白或控制字符")
		}
	}
	if category == CategoryRetry && (key == "max_total_attempts" || key == "max_total_duration") {
		if provider != ProviderGlobal {
			return fmt.Errorf("调度保护为 Claude 与 Codex 共用设置，请使用 global 作用域")
		}
		if key == "max_total_attempts" {
			attempts, err := strconv.Atoi(value)
			if err != nil || attempts < 1 || attempts > 100 {
				return fmt.Errorf("总尝试次数上限必须是 1–100 的整数，包含首次发送")
			}
		} else {
			duration, err := time.ParseDuration(value)
			if err != nil || duration < time.Second || duration > 10*time.Minute {
				return fmt.Errorf("调度等待时限必须在 1 秒至 10 分钟之间")
			}
		}
	}
	if category == CategoryRetry {
		switch key {
		case "cooldown_initial", "cooldown_max":
			duration, err := time.ParseDuration(value)
			if err != nil || duration < time.Second || duration > time.Hour {
				return fmt.Errorf("冷却时间必须在 1 秒至 1 小时之间")
			}
		case "cooldown_multiplier":
			number, err := strconv.ParseFloat(value, 64)
			if err != nil || math.IsNaN(number) || math.IsInf(number, 0) || number < 1 || number > 10 {
				return fmt.Errorf("连续失败递增倍数必须在 1–10 之间")
			}
		}
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

func (s *SettingsService) validateAuthUpdate(ctx context.Context, records []*settingsstore.SettingRecord) error {
	changed := false
	for _, record := range records {
		changed = changed || record.Category == CategoryAuth
	}
	if !changed {
		return nil
	}
	enabled := s.GetBool(ctx, CategoryAuth, "enabled", false)
	token, err := s.GetValue(ctx, CategoryAuth, "token")
	if err != nil {
		return err
	}
	for _, record := range records {
		if record.Category != CategoryAuth {
			continue
		}
		switch record.Key {
		case "enabled":
			enabled = record.Value == "true"
		case "token":
			token = record.Value
		}
	}
	if enabled && token == "" {
		return fmt.Errorf("启用访问鉴权前请填写应用访问令牌；清空令牌时请同时关闭鉴权")
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
