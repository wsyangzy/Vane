package config

import (
	"path/filepath"
	"strings"
	"time"
)

// BeijingTimezone is the fixed application timezone for display and statistics.
const BeijingTimezone = "Asia/Shanghai"

// NewDefaultConfig returns the runtime configuration baseline.
// Runtime settings and provider records are loaded from SQLite after startup;
// no filesystem configuration is required.
func NewDefaultConfig() *Config {
	c := &Config{
		Logging: LoggingConfig{
			FileEnabled:     true,
			CompressRotated: true,
		},
		UsageTracking: UsageTrackingConfig{Enabled: true},
	}
	c.setDefaults()
	c.Group.AutoSwitchBetweenGroups = true
	c.Failover.Enabled = true
	c.Failover.SetDefaults()
	return c
}

func (c *Config) setServerDefaults() {
	if c.Server.Host == "" {
		c.Server.Host = "localhost"
	}
	if c.Server.Port == 0 {
		c.Server.Port = 8080
	}
}

func (c *Config) setStrategyDefaults() {
	if c.Strategy.Type == "" {
		c.Strategy.Type = "priority"
	}
	if c.Strategy.FastTestCacheTTL == 0 {
		c.Strategy.FastTestCacheTTL = 3 * time.Second
	}
	if c.Strategy.FastTestTimeout == 0 {
		c.Strategy.FastTestTimeout = time.Second
	}
	if c.Strategy.FastTestPath == "" {
		c.Strategy.FastTestPath = c.Health.HealthPath
	}
}

func (c *Config) setRetryDefaults() {
	if c.Retry.MaxAttempts == 0 {
		c.Retry.MaxAttempts = 3
	}
	if c.Retry.BaseDelay == 0 {
		c.Retry.BaseDelay = time.Second
	}
	if c.Retry.MaxDelay == 0 {
		c.Retry.MaxDelay = 30 * time.Second
	}
	if c.Retry.Multiplier == 0 {
		c.Retry.Multiplier = 2.0
	}
	if c.Retry.MaxTotalAttempts == 0 {
		c.Retry.MaxTotalAttempts = 10
	}
	if c.Retry.MaxTotalDuration == 0 {
		c.Retry.MaxTotalDuration = 60 * time.Second
	}
}

func (c *Config) setHealthDefaults() {
	if c.Health.CheckInterval == 0 {
		c.Health.CheckInterval = 30 * time.Second
	}
	if c.Health.Timeout == 0 {
		c.Health.Timeout = 5 * time.Second
	}
	if c.Health.HealthPath == "" {
		c.Health.HealthPath = "/v1/models"
	}
}

func (c *Config) setLoggingDefaults() {
	if c.Logging.Level == "" {
		c.Logging.Level = "info"
	}
	if c.Logging.Format == "" {
		c.Logging.Format = "text"
	}
	if c.Logging.FileEnabled && c.Logging.FilePath == "" {
		c.Logging.FilePath = "logs/app.log"
	}
	if c.Logging.FileEnabled && c.Logging.MaxFileSize == "" {
		c.Logging.MaxFileSize = "100MB"
	}
	if c.Logging.FileEnabled && c.Logging.MaxFiles == 0 {
		c.Logging.MaxFiles = 10
	}

	if c.Logging.TokenDebug.SavePath == "" {
		c.Logging.TokenDebug.SavePath = "logs"
	}
	if c.Logging.TokenDebug.MaxFiles == 0 {
		c.Logging.TokenDebug.MaxFiles = 50
	}
	if c.Logging.TokenDebug.AutoCleanupDays == 0 {
		c.Logging.TokenDebug.AutoCleanupDays = 7
	}
}

func (c *Config) setStreamingDefaults() {
	if c.Streaming.HeartbeatInterval == 0 {
		c.Streaming.HeartbeatInterval = 30 * time.Second
	}
	if c.Streaming.ReadTimeout == 0 {
		c.Streaming.ReadTimeout = 10 * time.Second
	}
	if c.Streaming.MaxIdleTime == 0 {
		c.Streaming.MaxIdleTime = 120 * time.Second
	}
	if c.Streaming.ResponseHeaderTimeout == 0 {
		c.Streaming.ResponseHeaderTimeout = 120 * time.Second
	}
	if c.Streaming.DefaultResponseHeaderTimeout == 0 {
		c.Streaming.DefaultResponseHeaderTimeout = 60 * time.Second
	}
}

func (c *Config) setGlobalDefaults() {
	if c.GlobalTimeout == 0 {
		c.GlobalTimeout = 300 * time.Second
	}
	c.Timezone = BeijingTimezone
}

func (c *Config) setGroupDefaults() {
	if c.Group.Cooldown == 0 {
		c.Group.Cooldown = 600 * time.Second
	}
}

func (c *Config) setCodexServerDefaults() {
	c.CodexServer.Enabled = true
	if c.CodexServer.Timeout == 0 {
		c.CodexServer.Timeout = 300 * time.Second
	}
}

func (c *Config) setUsageTrackingDefaults() {
	if c.UsageTracking.Database != nil && strings.TrimSpace(c.UsageTracking.Database.Path) != "" {
		c.UsageTracking.DatabasePath = strings.TrimSpace(c.UsageTracking.Database.Path)
	}
	if c.UsageTracking.DatabasePath == "" {
		c.UsageTracking.DatabasePath = filepath.Join(getConfigAppDataDir(), "data", "usage.db")
	}
	if c.UsageTracking.BufferSize == 0 {
		c.UsageTracking.BufferSize = 1000
	}
	if c.UsageTracking.BatchSize == 0 {
		c.UsageTracking.BatchSize = 100
	}
	if c.UsageTracking.FlushInterval == 0 {
		c.UsageTracking.FlushInterval = 30 * time.Second
	}
	if c.UsageTracking.MaxRetry == 0 {
		c.UsageTracking.MaxRetry = 3
	}
	if c.UsageTracking.RetentionDays == 0 {
		c.UsageTracking.RetentionDays = 90
	}
}

func (c *Config) setTokenCountingDefaults() {
	if c.TokenCounting.EstimationRatio == 0 {
		c.TokenCounting.EstimationRatio = 4.0
	}
}

func (c *Config) setEndpointDefaults() {
	var defaultEndpoint *EndpointConfig
	if len(c.Endpoints) > 0 {
		defaultEndpoint = &c.Endpoints[0]
	}

	currentGroup := "Default"
	currentGroupPriority := 1

	for i := range c.Endpoints {
		if c.Endpoints[i].Priority == 0 && c.Endpoints[i].GroupPriority > 0 {
			c.Endpoints[i].Priority = c.Endpoints[i].GroupPriority
		}

		if c.Endpoints[i].Group != "" {
			currentGroup = c.Endpoints[i].Group
			if c.Endpoints[i].GroupPriority != 0 {
				currentGroupPriority = c.Endpoints[i].GroupPriority
			}
		} else {
			c.Endpoints[i].Group = currentGroup
			c.Endpoints[i].GroupPriority = currentGroupPriority
		}

		if c.Endpoints[i].GroupPriority == 0 {
			c.Endpoints[i].GroupPriority = currentGroupPriority
		}

		if c.Endpoints[i].Timeout == 0 {
			if defaultEndpoint != nil && defaultEndpoint.Timeout != 0 {
				c.Endpoints[i].Timeout = defaultEndpoint.Timeout
			} else {
				c.Endpoints[i].Timeout = c.GlobalTimeout
			}
		}

		if c.Endpoints[i].ApiKey == "" && defaultEndpoint != nil && defaultEndpoint.ApiKey != "" {
			c.Endpoints[i].ApiKey = defaultEndpoint.ApiKey
		}

		applyEndpointHeaderDefaults(&c.Endpoints[i], defaultEndpoint)
	}
}

func applyEndpointHeaderDefaults(endpoint *EndpointConfig, defaultEndpoint *EndpointConfig) {
	if endpoint == nil || defaultEndpoint == nil || len(defaultEndpoint.Headers) == 0 {
		return
	}

	if len(endpoint.Headers) == 0 {
		endpoint.Headers = make(map[string]string, len(defaultEndpoint.Headers))
		for key, value := range defaultEndpoint.Headers {
			endpoint.Headers[key] = value
		}
		return
	}

	mergedHeaders := make(map[string]string, len(defaultEndpoint.Headers)+len(endpoint.Headers))
	for key, value := range defaultEndpoint.Headers {
		mergedHeaders[key] = value
	}
	for key, value := range endpoint.Headers {
		mergedHeaders[key] = value
	}
	endpoint.Headers = mergedHeaders
}
