package app

import (
	"context"
	"fmt"
	"time"

	"github.com/wsyangzy/Vane/internal/buildinfo"
	"github.com/wsyangzy/Vane/internal/platform/netutil"
	settingsservice "github.com/wsyangzy/Vane/internal/settings/service"
	settingsstore "github.com/wsyangzy/Vane/internal/settings/store"
)

func preferredPortSettingKey() string {
	if buildinfo.BuildMode == "dev" {
		return "preferred_port_dev"
	}
	return "preferred_port"
}

func scopedSettingKey(category, key string) string {
	if category == settingsservice.CategoryServer && key == "preferred_port" {
		return preferredPortSettingKey()
	}
	return key
}

func visibleSettingRecords(records []*settingsstore.SettingRecord) []*settingsstore.SettingRecord {
	if len(records) == 0 {
		return records
	}
	var basePort, scopedPort *settingsstore.SettingRecord
	visible := make([]*settingsstore.SettingRecord, 0, len(records))
	for _, record := range records {
		if record == nil || !isVisibleSetting(record.Category, record.Key) {
			continue
		}
		if record != nil && record.Category == settingsservice.CategoryServer {
			switch record.Key {
			case "preferred_port":
				basePort = record
				continue
			case "preferred_port_dev":
				scopedPort = record
				continue
			}
		}
		visible = append(visible, record)
	}

	selected := basePort
	if buildinfo.BuildMode == "dev" && scopedPort != nil {
		selected = scopedPort
	}
	if selected != nil {
		clone := *selected
		clone.Key = "preferred_port"
		if selected == scopedPort && basePort != nil {
			if clone.Label == "" {
				clone.Label = basePort.Label
			}
			if clone.Description == "" {
				clone.Description = basePort.Description
			}
			if clone.ValueType == "" {
				clone.ValueType = basePort.ValueType
			}
			if clone.DisplayOrder == 0 {
				clone.DisplayOrder = basePort.DisplayOrder
			}
			clone.RequiresRestart = basePort.RequiresRestart
		}
		visible = append(visible, &clone)
	}
	return visible
}

// Compatibility records stay available to legacy runtime APIs, but do not
// reappear as editable controls in the profile-based application's settings.
func isVisibleSetting(category, key string) bool {
	switch category {
	case settingsservice.CategoryStrategy, settingsservice.CategoryFailover,
		settingsservice.CategoryTimezone, settingsservice.CategoryStreaming, settingsservice.CategoryHotPool:
		return false
	case settingsservice.CategoryRetry:
		return settingsservice.IsCurrentRetrySetting(key)
	case settingsservice.CategoryRequest:
		return key == "global_timeout" || key == "response_header_timeout" || key == "default_response_header_timeout"
	default:
		return true
	}
}

// SettingInfo 设置信息（给前端用的结构体）
type SettingInfo struct {
	ID              int64                      `json:"id"`
	Provider        string                     `json:"provider"`
	Category        string                     `json:"category"`
	Key             string                     `json:"key"`
	Value           string                     `json:"value"`
	ValueType       string                     `json:"value_type"`
	Label           string                     `json:"label"`
	Description     string                     `json:"description"`
	DisplayOrder    int                        `json:"display_order"`
	EnumOptions     []settingsstore.EnumOption `json:"enum_options,omitempty"` // 枚举选项
	RequiresRestart bool                       `json:"requires_restart"`
	CreatedAt       string                     `json:"created_at"`
	UpdatedAt       string                     `json:"updated_at"`
}

// CategoryInfo 分类信息
type CategoryInfo struct {
	Name        string `json:"name"`
	Label       string `json:"label"`
	Description string `json:"description"`
	Icon        string `json:"icon"`
	Order       int    `json:"order"`
}

// SettingsStorageStatus 设置存储状态
type SettingsStorageStatus struct {
	Enabled       bool `json:"enabled"`
	TotalCount    int  `json:"total_count"`
	CategoryCount int  `json:"category_count"`
	IsInitialized bool `json:"is_initialized"`
}

// UpdateSettingInput 更新单个设置的输入
type UpdateSettingInput struct {
	Provider string `json:"provider"`
	Category string `json:"category"`
	Key      string `json:"key"`
	Value    string `json:"value"`
}

type ScopedSettingsInput struct {
	Provider string `json:"provider"`
	Category string `json:"category"`
}

// BatchUpdateSettingsInput 批量更新设置的输入
type BatchUpdateSettingsInput struct {
	Settings []UpdateSettingInput `json:"settings"`
}

// PortInfo 端口信息
type PortInfo struct {
	PreferredPort int  `json:"preferred_port"`
	ActualPort    int  `json:"actual_port"`
	IsDefault     bool `json:"is_default"`
	WasOccupied   bool `json:"was_occupied"`
}

func (a *App) errSettingsServiceDisabled() error {
	dbPath := a.getEffectiveUsageDBPath()
	if dbPath != "" {
		return fmt.Errorf("设置服务未启用（管理数据库未就绪，db=%s）。请稍等重试；若一直失败，请检查是否有另一个 Vane 实例占用数据库或重启应用。", dbPath)
	}
	return fmt.Errorf("设置服务未启用（管理数据库未就绪）。请稍等重试；若一直失败，请检查是否有另一个 Vane 实例占用数据库或重启应用。")
}

// GetSettingsStorageStatus 获取设置存储状态
func (a *App) GetSettingsStorageStatus() SettingsStorageStatus {
	a.mu.RLock()
	settingsService := a.settingsService
	a.mu.RUnlock()

	status := SettingsStorageStatus{
		Enabled: false,
	}

	if settingsService == nil {
		return status
	}

	status.Enabled = true

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// 获取统计信息
	initialized, _ := settingsService.IsInitialized(ctx)
	status.IsInitialized = initialized

	records, err := settingsService.GetAll(ctx)
	if err == nil {
		status.TotalCount = len(visibleSettingRecords(records))
	}

	categories := settingsService.GetCategories()
	status.CategoryCount = len(categories)

	return status
}

// GetSettingCategories 获取所有设置分类
func (a *App) GetSettingCategories() []CategoryInfo {
	a.mu.RLock()
	settingsService := a.settingsService
	a.mu.RUnlock()

	if settingsService == nil {
		return []CategoryInfo{}
	}

	serviceCategories := settingsService.GetCategories()
	result := make([]CategoryInfo, 0, len(serviceCategories))

	for _, cat := range serviceCategories {
		result = append(result, CategoryInfo{
			Name:        cat.Name,
			Label:       cat.Label,
			Description: cat.Description,
			Icon:        cat.Icon,
			Order:       cat.Order,
		})
	}

	return result
}

// GetAllSettings 获取所有设置
func (a *App) GetAllSettings() ([]SettingInfo, error) {
	a.mu.RLock()
	settingsService := a.settingsService
	a.mu.RUnlock()

	if settingsService == nil {
		return nil, a.errSettingsServiceDisabled()
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	records, err := settingsService.GetAll(ctx)
	if err != nil {
		return nil, fmt.Errorf("获取设置列表失败: %w", err)
	}
	records = visibleSettingRecords(records)

	result := make([]SettingInfo, 0, len(records))
	for _, r := range records {
		result = append(result, a.settingRecordToInfo(r))
	}

	return result, nil
}

// GetSettingsByCategory 获取指定分类的设置
func (a *App) GetSettingsByCategory(category string) ([]SettingInfo, error) {
	a.mu.RLock()
	settingsService := a.settingsService
	a.mu.RUnlock()

	if settingsService == nil {
		return nil, a.errSettingsServiceDisabled()
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	records, err := settingsService.GetByCategory(ctx, category)
	if err != nil {
		return nil, fmt.Errorf("获取分类设置失败: %w", err)
	}
	records = visibleSettingRecords(records)

	result := make([]SettingInfo, 0, len(records))
	for _, r := range records {
		result = append(result, a.settingRecordToInfo(r))
	}

	return result, nil
}

func (a *App) GetSettingsByScope(input ScopedSettingsInput) ([]SettingInfo, error) {
	a.mu.RLock()
	settingsService := a.settingsService
	a.mu.RUnlock()
	if settingsService == nil {
		return nil, a.errSettingsServiceDisabled()
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	records, err := settingsService.GetByCategoryScoped(ctx, settingsservice.SettingsProvider(input.Provider), input.Category)
	if err != nil {
		return nil, fmt.Errorf("获取 Provider 设置失败: %w", err)
	}
	result := make([]SettingInfo, 0, len(records))
	for _, record := range records {
		result = append(result, a.settingRecordToInfo(record))
	}
	return result, nil
}

// GetSetting 获取单个设置
func (a *App) GetSetting(category, key string) (SettingInfo, error) {
	a.mu.RLock()
	settingsService := a.settingsService
	a.mu.RUnlock()

	if settingsService == nil {
		return SettingInfo{}, a.errSettingsServiceDisabled()
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	physicalKey := scopedSettingKey(category, key)
	record, err := settingsService.Get(ctx, category, physicalKey)
	if err != nil {
		return SettingInfo{}, fmt.Errorf("获取设置失败: %w", err)
	}
	if record == nil {
		return SettingInfo{}, fmt.Errorf("设置 '%s.%s' 不存在", category, key)
	}
	if physicalKey != key {
		recordCopy := *record
		recordCopy.Key = key
		record = &recordCopy
	}

	return a.settingRecordToInfo(record), nil
}

// UpdateSetting 更新单个设置
func (a *App) UpdateSetting(input UpdateSettingInput) error {
	a.mu.RLock()
	settingsService := a.settingsService
	logger := a.logger
	a.mu.RUnlock()

	if settingsService == nil {
		return a.errSettingsServiceDisabled()
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	provider := input.Provider
	if provider == "" {
		provider = string(settingsservice.ProviderGlobal)
		if input.Category == settingsservice.CategoryFailover {
			provider = string(settingsservice.ProviderClaude)
		}
	}
	key := scopedSettingKey(input.Category, input.Key)
	if err := settingsService.SetScoped(ctx, settingsservice.SettingsProvider(provider), input.Category, key, input.Value); err != nil {
		return fmt.Errorf("更新设置失败: %w", err)
	}

	if logger != nil {
		logger.Info("✅ 设置已更新", "category", input.Category, "key", key)
	}

	a.emitEndpointUpdate()
	return nil
}

// BatchUpdateSettings 批量更新设置
func (a *App) BatchUpdateSettings(input BatchUpdateSettingsInput) error {
	a.mu.RLock()
	settingsService := a.settingsService
	logger := a.logger
	a.mu.RUnlock()

	if settingsService == nil {
		return a.errSettingsServiceDisabled()
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// 转换为 settingsstore.SettingRecord
	records := make([]*settingsstore.SettingRecord, 0, len(input.Settings))
	for _, s := range input.Settings {
		key := scopedSettingKey(s.Category, s.Key)
		records = append(records, &settingsstore.SettingRecord{
			Provider: s.Provider,
			Category: s.Category,
			Key:      key,
			Value:    s.Value,
		})
	}

	if err := settingsService.UpdateAndApply(ctx, records); err != nil {
		return fmt.Errorf("批量更新设置失败: %w", err)
	}

	if logger != nil {
		logger.Info("✅ 设置已批量更新并应用", "count", len(records))
	}

	a.emitEndpointUpdate()
	return nil
}

func (a *App) ResetSettingsByScope(input ScopedSettingsInput) error {
	a.mu.RLock()
	settingsService := a.settingsService
	a.mu.RUnlock()
	if settingsService == nil {
		return a.errSettingsServiceDisabled()
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	return settingsService.ResetCategoryScoped(ctx, settingsservice.SettingsProvider(input.Provider), input.Category)
}

// ResetCategorySettings 重置分类设置为默认值
func (a *App) ResetCategorySettings(category string) error {
	a.mu.RLock()
	settingsService := a.settingsService
	logger := a.logger
	a.mu.RUnlock()

	if settingsService == nil {
		return a.errSettingsServiceDisabled()
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := settingsService.ResetCategory(ctx, category); err != nil {
		return fmt.Errorf("重置设置失败: %w", err)
	}

	if logger != nil {
		logger.Info("✅ 设置分类已重置", "category", category)
	}

	return nil
}

// GetPortInfo 获取端口信息
func (a *App) GetPortInfo() PortInfo {
	a.mu.RLock()
	cfg := a.config
	portManager := a.portManager
	a.mu.RUnlock()

	// 默认值
	defaultPort := 0
	if cfg != nil {
		defaultPort = cfg.Server.Port
	}
	info := PortInfo{
		PreferredPort: defaultPort,
		ActualPort:    defaultPort,
		IsDefault:     true,
		WasOccupied:   false,
	}

	if portManager != nil {
		portInfo := portManager.GetPortInfo()
		info = PortInfo{
			PreferredPort: portInfo.PreferredPort,
			ActualPort:    portInfo.ActualPort,
			IsDefault:     portInfo.IsDefault,
			WasOccupied:   portInfo.WasOccupied,
		}
	}

	return info
}

// UpdatePreferredPort 更新首选端口（需要重启生效）
func (a *App) UpdatePreferredPort(port int) error {
	a.mu.RLock()
	settingsService := a.settingsService
	portManager := a.portManager
	logger := a.logger
	a.mu.RUnlock()

	if settingsService == nil {
		return a.errSettingsServiceDisabled()
	}

	// 验证端口范围
	if port < 1 || port > 65535 {
		return fmt.Errorf("端口号必须在 1-65535 之间")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := settingsService.Set(ctx, settingsservice.CategoryServer, preferredPortSettingKey(), fmt.Sprintf("%d", port)); err != nil {
		return fmt.Errorf("更新首选端口失败: %w", err)
	}

	// 更新 PortManager
	if portManager != nil {
		portManager.SetPreferredPort(port)
	}

	if logger != nil {
		logger.Info("✅ 首选端口已更新（需要重启生效）", "port", port)
	}

	return nil
}

// CheckPortAvailable 检查端口是否可用
func (a *App) CheckPortAvailable(port int) bool {
	return netutil.IsPortAvailable(port)
}

// settingRecordToInfo 将数据库记录转换为前端 Info 结构
func (a *App) settingRecordToInfo(r *settingsstore.SettingRecord) SettingInfo {
	info := SettingInfo{
		ID:              r.ID,
		Provider:        r.Provider,
		Category:        r.Category,
		Key:             r.Key,
		Value:           r.Value,
		ValueType:       r.ValueType,
		Label:           r.Label,
		Description:     r.Description,
		DisplayOrder:    r.DisplayOrder,
		EnumOptions:     r.EnumOptions, // 传递枚举选项
		RequiresRestart: r.RequiresRestart,
	}

	if !r.CreatedAt.IsZero() {
		info.CreatedAt = r.CreatedAt.Format("2006-01-02 15:04:05")
	}
	if !r.UpdatedAt.IsZero() {
		info.UpdatedAt = r.UpdatedAt.Format("2006-01-02 15:04:05")
	}

	return info
}

// ConfigSourceInfo 配置项来源信息（用于前端配置溯源诊断）
// 🔧 [P5.5 新增]
type ConfigSourceInfo struct {
	Category     string `json:"category"`
	Key          string `json:"key"`
	CurrentValue string `json:"current_value"`
	Source       string `json:"source"`        // default / database / environment
	DefaultValue string `json:"default_value"` // 代码常量默认值
	Notes        string `json:"notes,omitempty"`
}

// GetConfigSourceInfo 返回所有配置项的来源诊断信息
//
// 🔧 [P5.5 新增] 用户可以通过此 API 查看：
//   - 每个配置项的"当前值"
//   - 来源是什么（默认值/数据库）
//   - 与默认值的差异
//
// 用途：解决"我现在用的是哪个值？"的困惑
// 这是 P5.5 配置单一来源的第一阶段，让用户先有可观察性
func (a *App) GetConfigSourceInfo() ([]ConfigSourceInfo, error) {
	a.mu.RLock()
	settingsService := a.settingsService
	cfg := a.config
	a.mu.RUnlock()

	if cfg == nil {
		return nil, fmt.Errorf("配置未加载")
	}

	infos := make([]ConfigSourceInfo, 0, 32)

	// 已纳入 SettingsService 管理的配置（来源：database 或 default）
	if settingsService != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		dbSettings, err := settingsService.GetAll(ctx)
		if err == nil {
			dbSettings = visibleSettingRecords(dbSettings)
			for _, s := range dbSettings {
				source := string(settingsservice.ConfigSourceDatabase)
				notes := ""
				if s.UpdatedAt.Equal(s.CreatedAt) {
					// 创建后未修改 → 视为默认值（来自首次初始化）
					source = string(settingsservice.ConfigSourceDefault)
					notes = "首次初始化默认值"
				}
				infos = append(infos, ConfigSourceInfo{
					Category:     s.Category,
					Key:          s.Key,
					CurrentValue: s.Value,
					Source:       source,
					DefaultValue: "", // SettingsService 已合并默认值，此处不再单独显示
					Notes:        notes,
				})
			}
		}
	}

	return infos, nil
}
