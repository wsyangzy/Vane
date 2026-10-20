package service

import (
	"context"
	"fmt"
	"time"

	settingsstore "github.com/wsyangzy/Vane/internal/settings/store"
)

// SettingCategory 设置分类常量
const (
	CategoryStrategy      = "strategy"
	CategoryRetry         = "retry"
	CategoryHealth        = "health"
	CategoryFailover      = "failover"
	CategoryRequest       = "request"
	CategoryStreaming     = "streaming"
	CategoryAuth          = "auth"
	CategoryTokenCounting = "token_counting"
	CategoryRetention     = "retention"
	CategoryHotPool       = "hot_pool"
	CategoryCache         = "cache"
	CategoryServer        = "server"

	// 运行配置分类
	CategoryLogging  = "logging"  // 日志级别/格式
	CategoryTimezone = "timezone" // 全局时区
)

type SettingsProvider string

const (
	ProviderGlobal SettingsProvider = "global"
	ProviderClaude SettingsProvider = "claude"
	ProviderCodex  SettingsProvider = "codex"
)

func validateSettingsProvider(provider SettingsProvider) error {
	switch provider {
	case ProviderGlobal, ProviderClaude, ProviderCodex:
		return nil
	default:
		return fmt.Errorf("无效的设置 Provider: %s", provider)
	}
}

// ConfigSource 表示配置项的来源
// 🔧 [P5.5 新增] 用于前端配置溯源诊断
type ConfigSource string

const (
	ConfigSourceDefault     ConfigSource = "default"     // 代码常量默认值
	ConfigSourceDatabase    ConfigSource = "database"    // SQLite settings 表（用户修改）
	ConfigSourceEnvironment ConfigSource = "environment" // 环境变量（暂未启用）
)

// SettingValueType 设置值类型常量
const (
	ValueTypeString   = "string"
	ValueTypeInt      = "int"
	ValueTypeFloat    = "float"
	ValueTypeBool     = "bool"
	ValueTypeDuration = "duration"
	ValueTypePassword = "password"
	ValueTypeJSON     = "json"
	ValueTypeEnum     = "enum" // 新增枚举类型
)

// CategoryInfo 分类信息
type CategoryInfo struct {
	Name        string `json:"name"`
	Label       string `json:"label"`
	Description string `json:"description"`
	Icon        string `json:"icon"`
	Order       int    `json:"order"`
}

// SettingsService 设置管理业务服务
type SettingsService struct {
	store          settingsstore.SettingsStore
	onChangeFunc   func() error // 配置变更回调
	categoryLabels map[string]CategoryInfo
}

type scopedSettingDeleter interface {
	DeleteScoped(ctx context.Context, provider, category, key string) error
}

const settingsRollbackTimeout = 5 * time.Second

func runSettingsRollback(fn func(context.Context) error) error {
	ctx, cancel := context.WithTimeout(context.Background(), settingsRollbackTimeout)
	defer cancel()
	return fn(ctx)
}

// NewSettingsService 创建设置服务实例
func NewSettingsService(store settingsstore.SettingsStore) *SettingsService {
	svc := &SettingsService{
		store: store,
		categoryLabels: map[string]CategoryInfo{
			CategoryAuth: {
				Name:        CategoryAuth,
				Label:       "访问鉴权",
				Description: "配置代理服务的访问令牌",
				Icon:        "🔐",
				Order:       1,
			},
			CategoryStrategy: {
				Name:        CategoryStrategy,
				Label:       "路由策略",
				Description: "配置渠道内端点选择策略",
				Icon:        "🔀",
				Order:       2,
			},
			CategoryHealth: {
				Name:        CategoryHealth,
				Label:       "健康检查",
				Description: "配置端点健康检查参数",
				Icon:        "❤️",
				Order:       3,
			},
			CategoryRetry: {
				Name:        CategoryRetry,
				Label:       "重试配置",
				Description: "配置请求失败重试行为",
				Icon:        "🔄",
				Order:       4,
			},
			CategoryFailover: {
				Name:        CategoryFailover,
				Label:       "故障转移",
				Description: "配置渠道间故障转移行为",
				Icon:        "🔁",
				Order:       5,
			},
			CategoryRequest: {
				Name:        CategoryRequest,
				Label:       "请求控制",
				Description: "配置请求超时和流式传输行为",
				Icon:        "⏱️",
				Order:       6,
			},
			// CategoryStreaming 和 CategoryHotPool 不在前端设置页面展示，使用代码默认值。
			CategoryTokenCounting: {
				Name:        CategoryTokenCounting,
				Label:       "Token 计数",
				Description: "配置 Token 计数功能",
				Icon:        "🔢",
				Order:       7,
			},
			CategoryRetention: {
				Name:        CategoryRetention,
				Label:       "数据保留",
				Description: "配置请求历史保留策略；历史请求自动按自然月归档",
				Icon:        "📦",
				Order:       8,
			},
			CategoryCache: {
				Name:        CategoryCache,
				Label:       "缓存性能",
				Description: "配置统计聚合缓存和状态观测",
				Icon:        "⚡",
				Order:       9,
			},
			CategoryServer: {
				Name:        CategoryServer,
				Label:       "服务端口",
				Description: "配置 Claude 与 Codex 代理服务端口",
				Icon:        "📡",
				Order:       0, // 最前（特殊处理，单独显示）
			},
			// 🔧 [P5.5 新增] Logging / Timezone 分类
			CategoryLogging: {
				Name:        CategoryLogging,
				Label:       "日志设置",
				Description: "配置日志级别和输出格式",
				Icon:        "📝",
				Order:       10,
			},
			CategoryTimezone: {
				Name:        CategoryTimezone,
				Label:       "时区设置",
				Description: "配置全局时区（影响所有时间显示）",
				Icon:        "🕐",
				Order:       11,
			},
		},
	}
	return svc
}

// SetOnChangeCallback 设置配置变更回调
func (s *SettingsService) SetOnChangeCallback(fn func() error) {
	s.onChangeFunc = fn
}

// GetCategories 获取所有分类信息
func (s *SettingsService) GetCategories() []CategoryInfo {
	categories := make([]CategoryInfo, 0, len(s.categoryLabels))
	for _, info := range s.categoryLabels {
		categories = append(categories, info)
	}
	// 按 Order 排序
	for i := 0; i < len(categories)-1; i++ {
		for j := i + 1; j < len(categories); j++ {
			if categories[i].Order > categories[j].Order {
				categories[i], categories[j] = categories[j], categories[i]
			}
		}
	}
	return categories
}

// GetCategoryInfo 获取分类信息
func (s *SettingsService) GetCategoryInfo(category string) *CategoryInfo {
	if info, ok := s.categoryLabels[category]; ok {
		return &info
	}
	return nil
}
