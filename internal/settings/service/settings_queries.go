package service

import (
	"context"
	"fmt"
	"log/slog"
	"strconv"
	"time"

	settingsstore "github.com/wsyangzy/Vane/internal/settings/store"
)

// Get 获取单个设置值
func (s *SettingsService) Get(ctx context.Context, category, key string) (*settingsstore.SettingRecord, error) {
	record, err := s.store.Get(ctx, category, key)
	if err != nil || record == nil {
		return record, err
	}
	// 动态填充枚举选项
	s.enrichEnumOptions([]*settingsstore.SettingRecord{record})
	return record, nil
}

func (s *SettingsService) GetScoped(ctx context.Context, provider SettingsProvider, category, key string) (*settingsstore.SettingRecord, error) {
	if err := validateSettingsProvider(provider); err != nil {
		return nil, err
	}
	record, err := s.store.GetScoped(ctx, string(provider), category, key)
	if err != nil || record == nil {
		return record, err
	}
	s.enrichEnumOptions([]*settingsstore.SettingRecord{record})
	return record, nil
}

func (s *SettingsService) GetBoolScoped(ctx context.Context, provider SettingsProvider, category, key string, defaultVal bool) bool {
	record, err := s.GetScoped(ctx, provider, category, key)
	if err != nil || record == nil || record.Value == "" {
		return defaultVal
	}
	value, err := strconv.ParseBool(record.Value)
	if err != nil {
		return defaultVal
	}
	return value
}

// GetBoolScopedStrict 获取指定 Provider 的布尔值，并返回读取或解析错误。
func (s *SettingsService) GetBoolScopedStrict(ctx context.Context, provider SettingsProvider, category, key string, defaultVal bool) (bool, error) {
	if s == nil || s.store == nil {
		return defaultVal, fmt.Errorf("设置存储服务未启用")
	}
	record, err := s.GetScoped(ctx, provider, category, key)
	if err != nil {
		return defaultVal, err
	}
	if record == nil {
		return defaultVal, nil
	}
	return parseBoolStrict(record.Value, defaultVal)
}

func (s *SettingsService) GetIntScoped(ctx context.Context, provider SettingsProvider, category, key string, defaultVal int) int {
	record, err := s.GetScoped(ctx, provider, category, key)
	if err != nil || record == nil {
		return defaultVal
	}
	value, err := strconv.Atoi(record.Value)
	if err != nil {
		return defaultVal
	}
	return value
}

func (s *SettingsService) GetDurationScoped(ctx context.Context, provider SettingsProvider, category, key string, defaultVal time.Duration) time.Duration {
	record, err := s.GetScoped(ctx, provider, category, key)
	if err != nil || record == nil {
		return defaultVal
	}
	value, err := time.ParseDuration(record.Value)
	if err != nil {
		return defaultVal
	}
	return value
}

// GetValue 获取设置值（仅返回值字符串）
func (s *SettingsService) GetValue(ctx context.Context, category, key string) (string, error) {
	record, err := s.store.Get(ctx, category, key)
	if err != nil {
		return "", err
	}
	if record == nil {
		return "", nil
	}
	return record.Value, nil
}

// GetInt 获取整数值
func (s *SettingsService) GetInt(ctx context.Context, category, key string, defaultVal int) int {
	val, err := s.GetValue(ctx, category, key)
	if err != nil || val == "" {
		return defaultVal
	}
	if i, err := strconv.Atoi(val); err == nil {
		return i
	}
	return defaultVal
}

// GetFloat 获取浮点数值
func (s *SettingsService) GetFloat(ctx context.Context, category, key string, defaultVal float64) float64 {
	val, err := s.GetValue(ctx, category, key)
	if err != nil || val == "" {
		return defaultVal
	}
	if f, err := strconv.ParseFloat(val, 64); err == nil {
		return f
	}
	return defaultVal
}

// GetBool 获取布尔值
func (s *SettingsService) GetBool(ctx context.Context, category, key string, defaultVal bool) bool {
	val, err := s.GetValue(ctx, category, key)
	if err != nil || val == "" {
		return defaultVal
	}
	return val == "true" || val == "1" || val == "yes"
}

// GetBoolStrict 获取布尔值，并返回读取或解析错误。
func (s *SettingsService) GetBoolStrict(ctx context.Context, category, key string, defaultVal bool) (bool, error) {
	if s == nil || s.store == nil {
		return defaultVal, fmt.Errorf("设置存储服务未启用")
	}
	value, err := s.GetValue(ctx, category, key)
	if err != nil {
		return defaultVal, err
	}
	return parseBoolStrict(value, defaultVal)
}

func parseBoolStrict(value string, defaultVal bool) (bool, error) {
	if value == "" {
		return defaultVal, nil
	}
	parsed, err := strconv.ParseBool(value)
	if err != nil {
		return defaultVal, fmt.Errorf("解析布尔设置值 %q 失败: %w", value, err)
	}
	return parsed, nil
}

// GetDuration 获取时间间隔值
func (s *SettingsService) GetDuration(ctx context.Context, category, key string, defaultVal time.Duration) time.Duration {
	val, err := s.GetValue(ctx, category, key)
	if err != nil || val == "" {
		return defaultVal
	}
	if d, err := time.ParseDuration(val); err == nil {
		return d
	}
	return defaultVal
}

// GetByCategory 获取分类下的所有设置
func (s *SettingsService) GetByCategory(ctx context.Context, category string) ([]*settingsstore.SettingRecord, error) {
	records, err := s.store.GetByCategory(ctx, category)
	if err != nil {
		return nil, err
	}
	// 动态填充枚举选项
	s.enrichEnumOptions(records)
	return records, nil
}

func (s *SettingsService) GetByCategoryScoped(ctx context.Context, provider SettingsProvider, category string) ([]*settingsstore.SettingRecord, error) {
	if err := validateSettingsProvider(provider); err != nil {
		return nil, err
	}
	records, err := s.store.GetByCategoryScoped(ctx, string(provider), category)
	if err == nil {
		s.enrichEnumOptions(records)
	}
	return records, err
}

// GetAll 获取所有设置
func (s *SettingsService) GetAll(ctx context.Context) ([]*settingsstore.SettingRecord, error) {
	records, err := s.store.GetAll(ctx)
	if err != nil {
		return nil, err
	}
	// 动态填充枚举选项
	s.enrichEnumOptions(records)
	return records, nil
}

// enrichEnumOptions 为枚举类型的设置动态填充选项
//
// 🔧 [修复] 不再依赖数据库中的 value_type，而是以 getDefaultsForCategory 为权威源：
//   - 旧实现：if record.ValueType != ValueTypeEnum { continue }
//     导致用户已有数据库（value_type='string'）即使 SyncMetadata 失败也不会修正
//   - 新实现：以代码 defaults 为准，强制修正 value_type + 填充 enum_options
//     这样即使数据库元数据没及时更新（如 schema 迁移竞争），前端也能正确渲染下拉
func (s *SettingsService) enrichEnumOptions(records []*settingsstore.SettingRecord) {
	for _, record := range records {
		// 总是从 defaults 中查找：如果 defaults 里定义为 enum，则强制修正记录
		defaults := s.getDefaultsForCategory(record.Category)
		for _, def := range defaults {
			if def.Key != record.Key {
				continue
			}
			// 如果 defaults 定义为 enum 且有选项，强制覆盖
			if def.ValueType == ValueTypeEnum && len(def.EnumOptions) > 0 {
				if record.ValueType != ValueTypeEnum {
					slog.Info("[enrichEnumOptions] 修正记录的 ValueType（数据库未同步元数据）",
						"category", record.Category,
						"key", record.Key,
						"db_value_type", record.ValueType,
						"correct_value_type", ValueTypeEnum)
				}
				record.ValueType = ValueTypeEnum
				record.EnumOptions = def.EnumOptions
			}
			break
		}
	}
}
