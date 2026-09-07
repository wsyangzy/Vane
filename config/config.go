package config

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"time"
)

// Config is the in-memory runtime configuration assembled from code defaults
// and SQLite settings.
type Config struct {
	Server        ServerConfig
	Strategy      StrategyConfig
	Retry         RetryConfig
	Health        HealthConfig
	Logging       LoggingConfig
	Streaming     StreamingConfig
	Group         GroupConfig         // Group-level failover settings
	Failover      FailoverConfig      // 故障转移与请求挂起配置（见 failover.go）
	CodexFailover FailoverConfig      // Codex 独立故障转移配置（SQLite provider=codex）
	UsageTracking UsageTrackingConfig // Usage tracking configuration
	TokenCounting TokenCountingConfig // Token counting configuration
	Proxy         ProxyConfig
	Auth          AuthConfig
	CodexServer   CodexServerConfig // Codex 代理服务器配置
	GlobalTimeout time.Duration     // Global timeout for non-streaming requests
	Timezone      string            // Global timezone setting for all components
	Endpoints     []EndpointConfig

	// Runtime priority override.
	PrimaryEndpoint string // Primary endpoint name from command line
}

type ServerConfig struct {
	Host string
	Port int
}

type StrategyConfig struct {
	Type             string        // "priority" or "fastest"
	FastTestEnabled  bool          // Enable pre-request fast testing
	FastTestCacheTTL time.Duration // Cache TTL for fast test results
	FastTestTimeout  time.Duration // Timeout for individual fast tests
	FastTestPath     string        // Path for fast testing (default: health path)
}

type RetryConfig struct {
	MaxAttempts int
	BaseDelay   time.Duration
	MaxDelay    time.Duration
	Multiplier  float64

	// 🔧 [P5.3 新增] 全局重试预算：避免三层重试结构（渠道间×渠道内×单端点）导致最坏 50+ 次重试
	// 设计目标：客户端 keep-alive socket 在 60-120s 后会失效，必须在此之前给出最终响应
	// 任何一个预算耗尽都会终止重试，返回客户端
	MaxTotalAttempts int           // 全局最大总尝试次数（含所有端点+渠道，默认 10）
	MaxTotalDuration time.Duration // 全局最大重试总时长（默认 60s）
}

type HealthConfig struct {
	CheckInterval time.Duration
	Timeout       time.Duration
	HealthPath    string
}

type LoggingConfig struct {
	Level                string
	Format               string           // "json" or "text"
	FileEnabled          bool             // Enable file logging
	FilePath             string           // Log file path
	MaxFileSize          string           // Max file size (e.g., "100MB")
	MaxFiles             int              // Max number of rotated files to keep
	CompressRotated      bool             // Compress rotated log files
	DisableResponseLimit bool             // Disable response content output limit when file logging is enabled
	TokenDebug           TokenDebugConfig // Token debug configuration
}

// TokenDebugConfig Token调试配置
type TokenDebugConfig struct {
	Enabled         bool   // 是否启用Token调试功能，默认: true
	SavePath        string // 调试文件保存目录，默认: logs
	MaxFiles        int    // 最大保留调试文件数量，默认: 50 (0=不限制)
	AutoCleanupDays int    // 自动清理N天前的调试文件，默认: 7 (0=不清理)
}

type StreamingConfig struct {
	HeartbeatInterval     time.Duration
	ReadTimeout           time.Duration
	MaxIdleTime           time.Duration
	ResponseHeaderTimeout time.Duration // 流式响应头超时时间，默认: 120s（覆盖 Claude Opus thinking 模式首字节延迟）
	// 🔧 [P0.8 新增] 非流式响应头超时（Default Transport 使用），默认: 60s
	// 与 ResponseHeaderTimeout 分开，因为非流式典型场景更短（如 count_tokens）
	DefaultResponseHeaderTimeout time.Duration
}

// GroupConfig holds group-level failover settings.
type GroupConfig struct {
	Cooldown                time.Duration // Cooldown duration for groups when all endpoints fail
	AutoSwitchBetweenGroups bool          // Whether to automatically switch between groups, default: true
}

// FailoverConfig is defined in config/failover.go.

// ModelPricing 模型定价配置
// Model pricing values used by tracking calculations.
type ModelPricing struct {
	Input         float64 // per 1M tokens
	Output        float64 // per 1M tokens
	CacheCreation float64 // per 1M tokens (缓存创建)
	CacheRead     float64 // per 1M tokens (缓存读取)
}

type UsageTrackingConfig struct {
	Enabled bool // Enable usage tracking, default: false

	// SQLite database path.
	DatabasePath string // SQLite database file path, default: data/usage.db

	// Optional SQLite backend settings.
	Database *DatabaseBackendConfig // Database configuration (optional)

	BufferSize    int           // Event buffer size, default: 1000
	BatchSize     int           // Batch write size, default: 100
	FlushInterval time.Duration // Force flush interval, default: 30s
	MaxRetry      int           // Max retry count for write failures, default: 3
	RetentionDays int           // Data retention days (0=permanent), default: 90
	// Retention maintenance settings.
	CleanupInterval         time.Duration
	MaxActiveDatabaseSizeMB int

	// Optional in-memory pricing overrides.
	ModelPricing   map[string]ModelPricing // Model pricing configuration
	DefaultPricing ModelPricing            // Fallback pricing for unknown models
}

// DatabaseBackendConfig 数据库后端配置
// 简化为仅支持 SQLite，热池架构下无需外部数据库依赖
type DatabaseBackendConfig struct {
	Type string // SQLite is the only supported backend.

	// SQLite配置
	Path string // SQLite文件路径

	// 时区配置（用于时间格式化）
	Timezone string

	// Backend-specific fields retained for struct compatibility; SQLite ignores them.
	Host            string
	Port            int
	Database        string
	Username        string
	Password        string
	MaxOpenConns    int
	MaxIdleConns    int
	ConnMaxLifetime time.Duration
	ConnMaxIdleTime time.Duration
	Charset         string
}

type ProxyConfig struct {
	Enabled  bool
	Type     string // "http", "https", "socks5"
	URL      string // Complete proxy URL
	Host     string // Proxy host
	Port     int    // Proxy port
	Username string // Optional auth username
	Password string // Optional auth password
}

type AuthConfig struct {
	Enabled bool   // Enable authentication, default: false
	Token   string // Bearer token for authentication
}

// CodexServerConfig Codex 代理服务器配置
// 注意：v7.2+ 起 Codex 代理合并到 Claude 代理同一端口，通过路径路由区分。
// Host/Port 已移除，复用主代理的监听地址。
type CodexServerConfig struct {
	Enabled bool          // 是否启用 Codex 代理路由，运行时默认开启
	Timeout time.Duration // 请求超时，默认: 300s
}

// TokenCountingConfig Token计数配置
type TokenCountingConfig struct {
	Enabled         bool    // 启用count_tokens支持
	EstimationRatio float64 // Token估算比例 (1 token ≈ N 字符)
}

type EndpointConfig struct {
	Name                 string
	URL                  string
	Channel              string  // 渠道标签（用于分组展示）
	RuntimeKey           string  // SQLite runtime key
	CredentialGroupID    int64   // SQLite Claude credential group ID
	CredentialGroupName  string  // SQLite Claude credential group display name
	CredentialGroupOrder int     // SQLite Claude credential group order
	DailyCostLimitUSD    float64 // SQLite Claude daily cost limit; 0 means unlimited
	Priority             int
	Group                string         // Group label used by runtime grouping
	GroupPriority        int            // Optional group priority override
	FailoverEnabled      *bool          // 是否参与故障转移，默认: true
	Cooldown             *time.Duration // 端点级冷却时间（可选），默认使用全局配置
	SkipHealthCheck      bool           // 跳过健康检查（部分中转商不支持健康检查端点）
	Token                string         // Single-token shorthand
	ApiKey               string         // Single API key shorthand
	Tokens               []TokenConfig  // 多 Token 配置（新功能）
	ApiKeys              []ApiKeyConfig // 多 API Key 配置（新功能）
	Timeout              time.Duration
	Headers              map[string]string
	SupportsCountTokens  bool  // 是否支持count_tokens端点
	Enabled              *bool // 是否激活为代理端点（SQLite模式），默认: true
	StreamOnly           bool  // 仅支持流式请求（非流请求不会路由到此端点）
}

// TokenConfig Token 配置项，用于多 Token 切换功能
type TokenConfig struct {
	Name  string // Key 标识名称（用于 UI 显示）
	Value string // Token 值
}

// ApiKeyConfig API Key 配置项，用于多 API Key 切换功能
type ApiKeyConfig struct {
	Name  string // Key 标识名称（用于 UI 显示）
	Value string // API Key 值
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
