package config

import "fmt"

func (c *Config) validateEndpointSource() error {
	if len(c.Endpoints) == 0 && c.EndpointsStorage.Type != "sqlite" {
		return fmt.Errorf("at least one endpoint must be configured (or set endpoints_storage.type: sqlite)")
	}
	return nil
}

func (c *Config) validateStrategy() error {
	if c.Strategy.Type != "priority" && c.Strategy.Type != "fastest" {
		return fmt.Errorf("strategy type must be 'priority' or 'fastest'")
	}
	return nil
}

func (c *Config) validateProxy() error {
	if !c.Proxy.Enabled {
		return nil
	}
	if c.Proxy.Type == "" {
		return fmt.Errorf("proxy type is required when proxy is enabled")
	}
	if c.Proxy.Type != "http" && c.Proxy.Type != "https" && c.Proxy.Type != "socks5" {
		return fmt.Errorf("proxy type must be 'http', 'https', or 'socks5'")
	}
	if c.Proxy.URL == "" && (c.Proxy.Host == "" || c.Proxy.Port == 0) {
		return fmt.Errorf("proxy URL or host:port must be specified when proxy is enabled")
	}
	return nil
}

func (c *Config) validateUsageTracking() error {
	if !c.UsageTracking.Enabled {
		return nil
	}
	if c.UsageTracking.DatabasePath == "" {
		return fmt.Errorf("database path is required when usage tracking is enabled")
	}
	if c.UsageTracking.BufferSize <= 0 {
		return fmt.Errorf("buffer size must be greater than 0 when usage tracking is enabled")
	}
	if c.UsageTracking.BatchSize <= 0 {
		return fmt.Errorf("batch size must be greater than 0 when usage tracking is enabled")
	}
	if c.UsageTracking.BatchSize > c.UsageTracking.BufferSize {
		return fmt.Errorf("batch size cannot be larger than buffer size")
	}
	if c.UsageTracking.FlushInterval <= 0 {
		return fmt.Errorf("flush interval must be greater than 0 when usage tracking is enabled")
	}
	if c.UsageTracking.MaxRetry <= 0 {
		return fmt.Errorf("max retry count must be greater than 0 when usage tracking is enabled")
	}
	if c.UsageTracking.RetentionDays < 0 {
		return fmt.Errorf("retention days cannot be negative")
	}
	if c.UsageTracking.CleanupInterval <= 0 && c.UsageTracking.RetentionDays > 0 {
		return fmt.Errorf("cleanup interval must be greater than 0 when retention is enabled")
	}
	return nil
}

func (c *Config) validateEndpoints() error {
	for i, endpoint := range c.Endpoints {
		if endpoint.Name == "" {
			return fmt.Errorf("endpoint %d: name is required", i)
		}
		if endpoint.URL == "" {
			return fmt.Errorf("endpoint %s: URL is required", endpoint.Name)
		}
		if endpoint.Priority < 0 {
			return fmt.Errorf("endpoint %s: priority must be non-negative", endpoint.Name)
		}
		if endpoint.Token != "" && len(endpoint.Tokens) > 0 {
			return fmt.Errorf("endpoint %s: 'token' 和 'tokens' 不能同时配置，请选择其一", endpoint.Name)
		}
		if endpoint.ApiKey != "" && len(endpoint.ApiKeys) > 0 {
			return fmt.Errorf("endpoint %s: 'api-key' 和 'api-keys' 不能同时配置，请选择其一", endpoint.Name)
		}
		if err := validateTokenValues(endpoint); err != nil {
			return err
		}
		if err := validateAPIKeyValues(endpoint); err != nil {
			return err
		}
	}
	return nil
}

func validateTokenValues(endpoint EndpointConfig) error {
	for j, token := range endpoint.Tokens {
		if token.Value == "" {
			return fmt.Errorf("endpoint %s: tokens[%d] 必须设置 value", endpoint.Name, j)
		}
	}
	return nil
}

func validateAPIKeyValues(endpoint EndpointConfig) error {
	for j, apiKey := range endpoint.ApiKeys {
		if apiKey.Value == "" {
			return fmt.Errorf("endpoint %s: api-keys[%d] 必须设置 value", endpoint.Name, j)
		}
	}
	return nil
}
