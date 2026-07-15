package config

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"
	"gopkg.in/yaml.v3"
)

type Config struct {
	Server           ServerConfig           `yaml:"server"`
	Strategy         StrategyConfig         `yaml:"strategy"`
	Retry            RetryConfig            `yaml:"retry"`
	Health           HealthConfig           `yaml:"health"`
	Logging          LoggingConfig          `yaml:"logging"`
	Streaming        StreamingConfig        `yaml:"streaming"`
	Group            GroupConfig            `yaml:"group"`             // Group configuration (DEPRECATED: use Failover instead)
	Failover         FailoverConfig         `yaml:"failover"`          // 故障转移与请求挂起配置（见 failover.go）
	CodexFailover    FailoverConfig         `yaml:"-"`                 // Codex 独立故障转移配置（SQLite provider=codex）
	UsageTracking    UsageTrackingConfig    `yaml:"usage_tracking"`    // Usage tracking configuration
	TokenCounting    TokenCountingConfig    `yaml:"token_counting"`    // Token counting configuration
	EndpointsStorage EndpointsStorageConfig `yaml:"endpoints_storage"` // Endpoints storage configuration
	Proxy            ProxyConfig            `yaml:"proxy"`
	Auth             AuthConfig             `yaml:"auth"`
	CodexServer      CodexServerConfig      `yaml:"codex_server"`   // Codex 代理服务器配置
	GlobalTimeout    time.Duration          `yaml:"global_timeout"` // Global timeout for non-streaming requests
	Timezone         string                 `yaml:"timezone"`       // Global timezone setting for all components
	Endpoints        []EndpointConfig       `yaml:"endpoints"`

	// Runtime priority override (not serialized to YAML)
	PrimaryEndpoint string `yaml:"-"` // Primary endpoint name from command line
}

type ServerConfig struct {
	Host string `yaml:"host"`
	Port int    `yaml:"port"`
}

type StrategyConfig struct {
	Type             string        `yaml:"type"`                // "priority" or "fastest"
	FastTestEnabled  bool          `yaml:"fast_test_enabled"`   // Enable pre-request fast testing
	FastTestCacheTTL time.Duration `yaml:"fast_test_cache_ttl"` // Cache TTL for fast test results
	FastTestTimeout  time.Duration `yaml:"fast_test_timeout"`   // Timeout for individual fast tests
	FastTestPath     string        `yaml:"fast_test_path"`      // Path for fast testing (default: health path)
}

type RetryConfig struct {
	MaxAttempts int           `yaml:"max_attempts"`
	BaseDelay   time.Duration `yaml:"base_delay"`
	MaxDelay    time.Duration `yaml:"max_delay"`
	Multiplier  float64       `yaml:"multiplier"`

	// 🔧 [P5.3 新增] 全局重试预算：避免三层重试结构（渠道间×渠道内×单端点）导致最坏 50+ 次重试
	// 设计目标：客户端 keep-alive socket 在 60-120s 后会失效，必须在此之前给出最终响应
	// 任何一个预算耗尽都会终止重试，返回客户端
	MaxTotalAttempts int           `yaml:"max_total_attempts"` // 全局最大总尝试次数（含所有端点+渠道，默认 10）
	MaxTotalDuration time.Duration `yaml:"max_total_duration"` // 全局最大重试总时长（默认 60s）
}

type HealthConfig struct {
	CheckInterval time.Duration `yaml:"check_interval"`
	Timeout       time.Duration `yaml:"timeout"`
	HealthPath    string        `yaml:"health_path"`
}

type LoggingConfig struct {
	Level                string           `yaml:"level"`
	Format               string           `yaml:"format"`                 // "json" or "text"
	FileEnabled          bool             `yaml:"file_enabled"`           // Enable file logging
	FilePath             string           `yaml:"file_path"`              // Log file path
	MaxFileSize          string           `yaml:"max_file_size"`          // Max file size (e.g., "100MB")
	MaxFiles             int              `yaml:"max_files"`              // Max number of rotated files to keep
	CompressRotated      bool             `yaml:"compress_rotated"`       // Compress rotated log files
	DisableResponseLimit bool             `yaml:"disable_response_limit"` // Disable response content output limit when file logging is enabled
	TokenDebug           TokenDebugConfig `yaml:"token_debug"`            // Token debug configuration
}

// TokenDebugConfig Token调试配置
type TokenDebugConfig struct {
	Enabled         bool   `yaml:"enabled"`           // 是否启用Token调试功能，默认: true
	SavePath        string `yaml:"save_path"`         // 调试文件保存目录，默认: logs
	MaxFiles        int    `yaml:"max_files"`         // 最大保留调试文件数量，默认: 50 (0=不限制)
	AutoCleanupDays int    `yaml:"auto_cleanup_days"` // 自动清理N天前的调试文件，默认: 7 (0=不清理)
}

type StreamingConfig struct {
	HeartbeatInterval     time.Duration `yaml:"heartbeat_interval"`
	ReadTimeout           time.Duration `yaml:"read_timeout"`
	MaxIdleTime           time.Duration `yaml:"max_idle_time"`
	ResponseHeaderTimeout time.Duration `yaml:"response_header_timeout"` // 流式响应头超时时间，默认: 120s（覆盖 Claude Opus thinking 模式首字节延迟）
	// 🔧 [P0.8 新增] 非流式响应头超时（Default Transport 使用），默认: 60s
	// 与 ResponseHeaderTimeout 分开，因为非流式典型场景更短（如 count_tokens）
	DefaultResponseHeaderTimeout time.Duration `yaml:"default_response_header_timeout"`
}

// GroupConfig (DEPRECATED in use FailoverConfig instead)
// Kept for backward compatibility with configurations
type GroupConfig struct {
	Cooldown                time.Duration `yaml:"cooldown"`                   // Cooldown duration for groups when all endpoints fail
	AutoSwitchBetweenGroups bool          `yaml:"auto_switch_between_groups"` // Whether to automatically switch between groups, default: true
}

// FailoverConfig 定义已迁移到 config/failover.go
// 新结构合并自历史的 FailoverConfig(v1) / FailoverConfigV2 / RequestSuspendConfig

// ModelPricing 模型定价配置
// Deprecated: 定价配置已迁移到 SQLite model_pricing 表，通过前端「定价」页面管理
// 保留此结构体仅为向后兼容，不再使用
type ModelPricing struct {
	Input         float64 `yaml:"input"`          // per 1M tokens
	Output        float64 `yaml:"output"`         // per 1M tokens
	CacheCreation float64 `yaml:"cache_creation"` // per 1M tokens (缓存创建)
	CacheRead     float64 `yaml:"cache_read"`     // per 1M tokens (缓存读取)
}

type UsageTrackingConfig struct {
	Enabled bool `yaml:"enabled"` // Enable usage tracking, default: false

	// 向后兼容：保留原有的 database_path 配置
	DatabasePath string `yaml:"database_path"` // SQLite database file path, default: data/usage.db

	// 新增：数据库配置（可选，优先级高于 database_path）
	Database *DatabaseBackendConfig `yaml:"database,omitempty"` // Database configuration (optional)

	BufferSize    int           `yaml:"buffer_size"`    // Event buffer size, default: 1000
	BatchSize     int           `yaml:"batch_size"`     // Batch write size, default: 100
	FlushInterval time.Duration `yaml:"flush_interval"` // Force flush interval, default: 30s
	MaxRetry      int           `yaml:"max_retry"`      // Max retry count for write failures, default: 3
	RetentionDays int           `yaml:"retention_days"` // Data retention days (0=permanent), default: 90
	// Deprecated: 历史请求固定每日维护并按自然月归档；保留字段仅兼容旧配置文件。
	CleanupInterval         time.Duration `yaml:"cleanup_interval"`
	MaxActiveDatabaseSizeMB int           `yaml:"max_active_database_size_mb"`

	// Deprecated: 以下定价配置已废弃，迁移到 SQLite model_pricing 表
	// 通过前端「定价」页面管理，这些字段仅保留用于向后兼容解析
	ModelPricing   map[string]ModelPricing `yaml:"model_pricing,omitempty"`   // [废弃] Model pricing configuration
	DefaultPricing ModelPricing            `yaml:"default_pricing,omitempty"` // [废弃] Default pricing for unknown models
}

// DatabaseBackendConfig 数据库后端配置
// 简化为仅支持 SQLite，热池架构下无需外部数据库依赖
type DatabaseBackendConfig struct {
	Type string `yaml:"type"` // 仅支持 "sqlite"（保留用于向后兼容）

	// SQLite配置
	Path string `yaml:"path,omitempty"` // SQLite文件路径

	// 时区配置（用于时间格式化）
	Timezone string `yaml:"timezone,omitempty"`

	// ===== 以下字段已废弃，保留用于配置文件向后兼容（解析不报错）=====
	// 这些字段不再生效，请从配置文件中删除
	Host            string        `yaml:"host,omitempty"`               // DEPRECATED: MySQL not supported in
	Port            int           `yaml:"port,omitempty"`               // DEPRECATED: MySQL not supported in
	Database        string        `yaml:"database,omitempty"`           // DEPRECATED: MySQL not supported in
	Username        string        `yaml:"username,omitempty"`           // DEPRECATED: MySQL not supported in
	Password        string        `yaml:"password,omitempty"`           // DEPRECATED: MySQL not supported in
	MaxOpenConns    int           `yaml:"max_open_conns,omitempty"`     // DEPRECATED: MySQL not supported in
	MaxIdleConns    int           `yaml:"max_idle_conns,omitempty"`     // DEPRECATED: MySQL not supported in
	ConnMaxLifetime time.Duration `yaml:"conn_max_lifetime,omitempty"`  // DEPRECATED: MySQL not supported in
	ConnMaxIdleTime time.Duration `yaml:"conn_max_idle_time,omitempty"` // DEPRECATED: MySQL not supported in
	Charset         string        `yaml:"charset,omitempty"`            // DEPRECATED: MySQL not supported in
}

type ProxyConfig struct {
	Enabled  bool   `yaml:"enabled"`
	Type     string `yaml:"type"`     // "http", "https", "socks5"
	URL      string `yaml:"url"`      // Complete proxy URL
	Host     string `yaml:"host"`     // Proxy host
	Port     int    `yaml:"port"`     // Proxy port
	Username string `yaml:"username"` // Optional auth username
	Password string `yaml:"password"` // Optional auth password
}

type AuthConfig struct {
	Enabled bool   `yaml:"enabled"`         // Enable authentication, default: false
	Token   string `yaml:"token,omitempty"` // Bearer token for authentication
}

// CodexServerConfig Codex 代理服务器配置
// 注意：v7.2+ 起 Codex 代理合并到 Claude 代理同一端口，通过路径路由区分。
// Host/Port 已移除，复用主代理的监听地址。
type CodexServerConfig struct {
	Enabled bool          `yaml:"enabled"` // 是否启用 Codex 代理路由，运行时默认开启
	Timeout time.Duration `yaml:"timeout"` // 请求超时，默认: 300s
}

// TokenCountingConfig Token计数配置
type TokenCountingConfig struct {
	Enabled         bool    `yaml:"enabled"`          // 启用count_tokens支持
	EstimationRatio float64 `yaml:"estimation_ratio"` // Token估算比例 (1 token ≈ N 字符)
}

// EndpointsStorageConfig 端点存储配置
// 支持从 YAML 文件或 SQLite 数据库加载端点配置
type EndpointsStorageConfig struct {
	Type string `yaml:"type"` // 存储类型: "yaml" | "sqlite"，默认 "yaml"
}

type EndpointConfig struct {
	Name                 string            `yaml:"name"`
	URL                  string            `yaml:"url"`
	Channel              string            `yaml:"channel,omitempty"` // 渠道标签（用于分组展示）
	RuntimeKey           string            `yaml:"-"`                 // SQLite 运行时唯一键，不写入 YAML
	CredentialGroupID    int64             `yaml:"-"`                 // SQLite Claude 凭证分组 ID
	CredentialGroupName  string            `yaml:"-"`                 // SQLite Claude 凭证分组显示名
	CredentialGroupOrder int               `yaml:"-"`                 // SQLite Claude 凭证分组运行时顺序
	Priority             int               `yaml:"priority"`
	Group                string            `yaml:"group,omitempty"`             // DEPRECATED in
	GroupPriority        int               `yaml:"group-priority,omitempty"`    // DEPRECATED in use Priority instead
	FailoverEnabled      *bool             `yaml:"failover_enabled,omitempty"`  // 是否参与故障转移，默认: true
	Cooldown             *time.Duration    `yaml:"cooldown,omitempty"`          // 端点级冷却时间（可选），默认使用全局配置
	SkipHealthCheck      bool              `yaml:"skip_health_check,omitempty"` // 跳过健康检查（部分中转商不支持健康检查端点）
	Token                string            `yaml:"token,omitempty"`             // 单 Token 配置（向后兼容）
	ApiKey               string            `yaml:"api-key,omitempty"`           // 单 API Key 配置（向后兼容）
	Tokens               []TokenConfig     `yaml:"tokens,omitempty"`            // 多 Token 配置（新功能）
	ApiKeys              []ApiKeyConfig    `yaml:"api-keys,omitempty"`          // 多 API Key 配置（新功能）
	Timeout              time.Duration     `yaml:"timeout"`
	Headers              map[string]string `yaml:"headers,omitempty"`
	SupportsCountTokens  bool              `yaml:"supports_count_tokens,omitempty"` // 是否支持count_tokens端点
	Enabled              *bool             `yaml:"enabled,omitempty"`               // 是否激活为代理端点（SQLite模式），默认: true
	StreamOnly           bool              `yaml:"stream_only,omitempty"`           // 仅支持流式请求（非流请求不会路由到此端点）
}

// TokenConfig Token 配置项，用于多 Token 切换功能
type TokenConfig struct {
	Name  string `yaml:"name"`  // Key 标识名称（用于 UI 显示）
	Value string `yaml:"value"` // Token 值
}

// ApiKeyConfig API Key 配置项，用于多 API Key 切换功能
type ApiKeyConfig struct {
	Name  string `yaml:"name"`  // Key 标识名称（用于 UI 显示）
	Value string `yaml:"value"` // API Key 值
}

// LoadConfig loads configuration from file
func LoadConfig(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read config file: %w", err)
	}

	// Check if auto_switch_between_groups is explicitly set in YAML
	hasAutoSwitchConfig := strings.Contains(string(data), "auto_switch_between_groups")

	// Check if failover config is present
	hasFailoverConfig := strings.Contains(string(data), "failover:")

	var config Config
	if err := yaml.Unmarshal(data, &config); err != nil {
		return nil, fmt.Errorf("failed to parse config file: %w", err)
	}

	// Set defaults
	config.setDefaults()

	// Handle auto_switch_between_groups default for backward compatibility
	if !hasAutoSwitchConfig {
		config.Group.AutoSwitchBetweenGroups = true // Default to auto mode for backward compatibility
	}

	// → 配置映射（向后兼容旧版 group 配置）
	if !hasFailoverConfig {
		// 没有 failover 配置块，回退到旧版 group 配置语义
		config.Failover.Enabled = config.Group.AutoSwitchBetweenGroups
	}

	// 应用 Failover 默认值
	config.Failover.SetDefaults()

	// Validate configuration
	if err := config.validate(); err != nil {
		return nil, fmt.Errorf("invalid configuration: %w", err)
	}

	return &config, nil
}

// setDefaults sets default values for configuration
func (c *Config) setDefaults() {
	c.setServerDefaults()
	c.setStrategyDefaults()
	c.setRetryDefaults()
	c.setHealthDefaults()
	c.setLoggingDefaults()
	c.setStreamingDefaults()
	c.setGlobalDefaults()
	c.setGroupDefaults()
	c.setStorageDefaults()
	c.setCodexServerDefaults()
	c.setUsageTrackingDefaults()
	c.setTokenCountingDefaults()
	c.setEndpointDefaults()
}

// ApplyPrimaryEndpoint applies primary endpoint override from command line
// Returns error if the specified endpoint is not found
func (c *Config) ApplyPrimaryEndpoint(logger *slog.Logger) error {
	if c.PrimaryEndpoint == "" {
		return nil
	}

	// Find the specified endpoint
	primaryIndex := c.findEndpointIndex(c.PrimaryEndpoint)
	if primaryIndex == -1 {
		// Create list of available endpoints for better error message
		var availableEndpoints []string
		for _, endpoint := range c.Endpoints {
			availableEndpoints = append(availableEndpoints, endpoint.Name)
		}

		err := fmt.Errorf("指定的主端点 '%s' 未找到，可用端点: %v", c.PrimaryEndpoint, availableEndpoints)
		if logger != nil {
			logger.Error(fmt.Sprintf("❌ 主端点设置失败 - 端点: %s, 可用端点: %v",
				c.PrimaryEndpoint, availableEndpoints))
		}
		return err
	}

	// Store original priority for logging
	originalPriority := c.Endpoints[primaryIndex].Priority

	// Set the primary endpoint to priority 1
	c.Endpoints[primaryIndex].Priority = 1

	// Adjust other endpoints' priorities to ensure they are lower than primary
	adjustedCount := 0
	for i := range c.Endpoints {
		if i != primaryIndex && c.Endpoints[i].Priority <= 1 {
			c.Endpoints[i].Priority = c.Endpoints[i].Priority + 2 // Use consistent increment
			adjustedCount++
		}
	}

	if logger != nil {
		logger.Info(fmt.Sprintf("✅ 主端点优先级设置成功 - 端点: %s, 原优先级: %d → 新优先级: %d, 调整了%d个其他端点",
			c.PrimaryEndpoint, originalPriority, 1, adjustedCount))
	}

	return nil
}

// findEndpointIndex finds the index of an endpoint by name
func (c *Config) findEndpointIndex(name string) int {
	for i, endpoint := range c.Endpoints {
		if endpoint.Name == name {
			return i
		}
	}
	return -1
}

// validate validates the configuration
func (c *Config) validate() error {
	if err := c.validateEndpointSource(); err != nil {
		return err
	}
	if err := c.validateStrategy(); err != nil {
		return err
	}
	if err := c.validateProxy(); err != nil {
		return err
	}
	if err := c.Failover.Validate(); err != nil {
		return fmt.Errorf("failover configuration error: %w", err)
	}
	if err := c.validateUsageTracking(); err != nil {
		return err
	}
	if err := c.validateEndpoints(); err != nil {
		return err
	}
	return nil
}

// ConfigWatcher handles automatic configuration reloading
type ConfigWatcher struct {
	configPath    string
	config        *Config
	mutex         sync.RWMutex
	watcher       *fsnotify.Watcher
	logger        *slog.Logger
	callbacks     []func(*Config)
	lastModTime   time.Time
	debounceTimer *time.Timer
}

// NewConfigWatcher creates a new configuration watcher
func NewConfigWatcher(configPath string, logger *slog.Logger) (*ConfigWatcher, error) {
	// Load initial configuration
	config, err := LoadConfig(configPath)
	if err != nil {
		return nil, fmt.Errorf("failed to load initial config: %w", err)
	}

	// Get initial modification time
	fileInfo, err := os.Stat(configPath)
	if err != nil {
		return nil, fmt.Errorf("failed to get file info: %w", err)
	}

	// Create file watcher
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, fmt.Errorf("failed to create file watcher: %w", err)
	}

	cw := &ConfigWatcher{
		configPath:  configPath,
		config:      config,
		watcher:     watcher,
		logger:      logger,
		callbacks:   make([]func(*Config), 0),
		lastModTime: fileInfo.ModTime(),
	}

	// Add config file to watcher
	if err := watcher.Add(configPath); err != nil {
		watcher.Close()
		return nil, fmt.Errorf("failed to watch config file: %w", err)
	}

	// Start watching in background
	go cw.watchLoop()

	return cw, nil
}

// GetConfig returns the current configuration (thread-safe)
func (cw *ConfigWatcher) GetConfig() *Config {
	cw.mutex.RLock()
	defer cw.mutex.RUnlock()
	return cw.config
}

// UpdateLogger updates the logger used by the config watcher
func (cw *ConfigWatcher) UpdateLogger(logger *slog.Logger) {
	cw.mutex.Lock()
	defer cw.mutex.Unlock()
	cw.logger = logger
}

// AddReloadCallback adds a callback function that will be called when config is reloaded
func (cw *ConfigWatcher) AddReloadCallback(callback func(*Config)) {
	cw.mutex.Lock()
	defer cw.mutex.Unlock()
	cw.callbacks = append(cw.callbacks, callback)
}

// watchLoop monitors the config file for changes
func (cw *ConfigWatcher) watchLoop() {
	for {
		select {
		case event, ok := <-cw.watcher.Events:
			if !ok {
				return
			}

			// Handle file write events
			if event.Has(fsnotify.Write) {
				// Check if file was actually modified by comparing modification time
				fileInfo, err := os.Stat(cw.configPath)
				if err != nil {
					cw.logger.Warn(fmt.Sprintf("⚠️ 无法获取配置文件信息: %v", err))
					continue
				}

				// Skip if modification time hasn't changed
				if !fileInfo.ModTime().After(cw.lastModTime) {
					continue
				}

				cw.lastModTime = fileInfo.ModTime()

				// Cancel any existing debounce timer
				if cw.debounceTimer != nil {
					cw.debounceTimer.Stop()
				}

				// Set up debounce timer to avoid multiple rapid reloads
				cw.debounceTimer = time.AfterFunc(500*time.Millisecond, func() {
					cw.logger.Info(fmt.Sprintf("🔄 检测到配置文件变更，正在重新加载... - 文件: %s", event.Name))
					if err := cw.reloadConfig(); err != nil {
						cw.logger.Error(fmt.Sprintf("❌ 配置文件重新加载失败: %v", err))
					} else {
						cw.logger.Info("✅ 配置文件重新加载成功")
					}
				})
			}

			// Handle file rename/remove events (some editors rename files during save)
			if event.Has(fsnotify.Remove) || event.Has(fsnotify.Rename) {
				// Re-add the file to watcher in case it was recreated
				time.Sleep(100 * time.Millisecond) // Give time for the file to be recreated
				if _, err := os.Stat(cw.configPath); err == nil {
					cw.watcher.Add(cw.configPath)
					cw.logger.Info(fmt.Sprintf("🔄 重新监听配置文件: %s", cw.configPath))
				}
			}

		case err, ok := <-cw.watcher.Errors:
			if !ok {
				return
			}
			cw.logger.Error(fmt.Sprintf("⚠️ 配置文件监听错误: %v", err))
		}
	}
}

// reloadConfig reloads the configuration from file
func (cw *ConfigWatcher) reloadConfig() error {
	newConfig, err := LoadConfig(cw.configPath)
	if err != nil {
		return err
	}

	cw.mutex.Lock()
	oldConfig := cw.config
	cw.config = newConfig
	callbacks := make([]func(*Config), len(cw.callbacks))
	copy(callbacks, cw.callbacks)
	cw.mutex.Unlock()

	// Call all registered callbacks
	for _, callback := range callbacks {
		callback(newConfig)
	}

	// Log configuration changes
	cw.logConfigChanges(oldConfig, newConfig)

	return nil
}

// logConfigChanges logs the key differences between old and new configurations
func (cw *ConfigWatcher) logConfigChanges(oldConfig, newConfig *Config) {
	if len(oldConfig.Endpoints) != len(newConfig.Endpoints) {
		cw.logger.Info("📡 端点数量变更",
			"old_count", len(oldConfig.Endpoints),
			"new_count", len(newConfig.Endpoints))
	}

	if oldConfig.Server.Port != newConfig.Server.Port {
		cw.logger.Info("🌐 服务器端口变更",
			"old_port", oldConfig.Server.Port,
			"new_port", newConfig.Server.Port)
	}

	if oldConfig.Strategy.Type != newConfig.Strategy.Type {
		cw.logger.Info("🎯 策略类型变更",
			"old_strategy", oldConfig.Strategy.Type,
			"new_strategy", newConfig.Strategy.Type)
	}

	if oldConfig.Auth.Enabled != newConfig.Auth.Enabled {
		cw.logger.Info("🔐 鉴权状态变更",
			"old_enabled", oldConfig.Auth.Enabled,
			"new_enabled", newConfig.Auth.Enabled)
	}

	if oldConfig.Failover.SuspendOnExhausted != newConfig.Failover.SuspendOnExhausted {
		cw.logger.Info("⏸️ 请求挂起状态变更",
			"old_enabled", oldConfig.Failover.SuspendOnExhausted,
			"new_enabled", newConfig.Failover.SuspendOnExhausted)
	}

	// 记录故障转移配置变更
	if oldConfig.Failover.LoopRetry != newConfig.Failover.LoopRetry {
		cw.logger.Info("🔄 循环重试配置变更",
			"old_loop_retry", oldConfig.Failover.LoopRetry,
			"new_loop_retry", newConfig.Failover.LoopRetry)
	}

	if oldConfig.Failover.MaxLoops != newConfig.Failover.MaxLoops {
		cw.logger.Info("🔄 最大循环轮数变更",
			"old_max_loops", oldConfig.Failover.MaxLoops,
			"new_max_loops", newConfig.Failover.MaxLoops)
	}

	if oldConfig.Failover.MaxSuspendedRequests != newConfig.Failover.MaxSuspendedRequests {
		cw.logger.Info("⏸️ 最大挂起请求数变更",
			"old_max", oldConfig.Failover.MaxSuspendedRequests,
			"new_max", newConfig.Failover.MaxSuspendedRequests)
	}

	if oldConfig.Failover.SuspendTimeout != newConfig.Failover.SuspendTimeout {
		cw.logger.Info("⏸️ 请求挂起超时时间变更",
			"old_timeout", oldConfig.Failover.SuspendTimeout,
			"new_timeout", newConfig.Failover.SuspendTimeout)
	}

	if oldConfig.UsageTracking.Enabled != newConfig.UsageTracking.Enabled {
		cw.logger.Info("📊 使用跟踪状态变更",
			"old_enabled", oldConfig.UsageTracking.Enabled,
			"new_enabled", newConfig.UsageTracking.Enabled)
	}

	if oldConfig.UsageTracking.RetentionDays != newConfig.UsageTracking.RetentionDays {
		cw.logger.Info("📊 使用跟踪数据保留天数变更",
			"old_retention", oldConfig.UsageTracking.RetentionDays,
			"new_retention", newConfig.UsageTracking.RetentionDays)
	}

	if oldConfig.Timezone != newConfig.Timezone {
		cw.logger.Info("🌍 全局时区配置变更",
			"old_timezone", oldConfig.Timezone,
			"new_timezone", newConfig.Timezone)
	}
}

// Close stops the configuration watcher
func (cw *ConfigWatcher) Close() error {
	// Cancel any pending debounce timer
	if cw.debounceTimer != nil {
		cw.debounceTimer.Stop()
	}
	return cw.watcher.Close()
}

// SaveConfig saves configuration to file
func SaveConfig(config *Config, path string) error {
	// Marshal config to YAML
	data, err := yaml.Marshal(config)
	if err != nil {
		return fmt.Errorf("failed to marshal config: %w", err)
	}

	// Ensure directory exists
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("failed to create directory: %w", err)
	}

	// Write to file
	if err := os.WriteFile(path, data, 0644); err != nil {
		return fmt.Errorf("failed to write config file: %w", err)
	}

	return nil
}

// SaveConfigWithComments saves configuration to file while preserving all comments
func SavePriorityConfigWithComments(config *Config, path string) error {
	// Read existing file to preserve comments
	yamlFile, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("failed to read existing config file: %w", err)
	}

	var rootNode yaml.Node
	if len(yamlFile) > 0 {
		// Decode existing YAML to preserve structure and comments
		if err := yaml.Unmarshal(yamlFile, &rootNode); err != nil {
			return fmt.Errorf("failed to decode existing YAML: %w", err)
		}
	} else {
		// Create new YAML structure if file doesn't exist
		rootNode = yaml.Node{}
		if err := rootNode.Encode(config); err != nil {
			return fmt.Errorf("failed to create new YAML structure: %w", err)
		}
	}

	// Update endpoint priorities in the YAML node tree
	if len(rootNode.Content) > 0 {
		mappingNode := rootNode.Content[0]

		// Find endpoints section
		for i := 0; i < len(mappingNode.Content); i += 2 {
			keyNode := mappingNode.Content[i]
			valueNode := mappingNode.Content[i+1]

			if keyNode.Value == "endpoints" {
				// Update each endpoint's priority
				for _, endpointNode := range valueNode.Content {
					var endpointName string
					var priorityNode *yaml.Node

					// Find name and priority nodes for this endpoint
					for j := 0; j < len(endpointNode.Content); j += 2 {
						fieldKey := endpointNode.Content[j]
						fieldValue := endpointNode.Content[j+1]

						if fieldKey.Value == "name" {
							endpointName = fieldValue.Value
						} else if fieldKey.Value == "priority" {
							priorityNode = fieldValue
						}
					}

					// Find the corresponding endpoint in config and update priority
					if endpointName != "" && priorityNode != nil {
						for _, endpoint := range config.Endpoints {
							if endpoint.Name == endpointName {
								priorityNode.Value = fmt.Sprintf("%d", endpoint.Priority)
								break
							}
						}
					}
				}
				break
			}
		}
	}

	// Ensure directory exists
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("failed to create directory: %w", err)
	}

	// Directly write to the original file
	file, err := os.Create(path)
	if err != nil {
		return fmt.Errorf("failed to create config file: %w", err)
	}
	defer file.Close()

	// Encode with comments
	encoder := yaml.NewEncoder(file)
	encoder.SetIndent(2)
	if err := encoder.Encode(&rootNode); err != nil {
		return fmt.Errorf("failed to encode YAML: %w", err)
	}

	return nil
}

// getConfigAppDataDir 获取应用数据目录（跨平台）
// 复制自 internal/utils/appdir.go，避免循环依赖
// Windows: %APPDATA%\Claude-Request-Forwarder
// macOS: ~/Library/Application Support/Claude-Request-Forwarder
// Linux: ~/.local/share/ai-relay-forwarder
func getConfigAppDataDir() string {
	var baseDir string

	switch runtime.GOOS {
	case "windows":
		baseDir = os.Getenv("APPDATA")
		if baseDir == "" {
			baseDir = filepath.Join(os.Getenv("USERPROFILE"), "AppData", "Roaming")
		}
		return filepath.Join(baseDir, "Claude-Request-Forwarder")

	case "darwin":
		homeDir, _ := os.UserHomeDir()
		return filepath.Join(homeDir, "Library", "Application Support", "Claude-Request-Forwarder")

	case "linux":
		homeDir, _ := os.UserHomeDir()
		xdgDataHome := os.Getenv("XDG_DATA_HOME")
		if xdgDataHome != "" {
			return filepath.Join(xdgDataHome, "ai-relay-forwarder")
		}
		return filepath.Join(homeDir, ".local", "share", "ai-relay-forwarder")

	default:
		homeDir, _ := os.UserHomeDir()
		return filepath.Join(homeDir, ".ai-relay-forwarder")
	}
}
