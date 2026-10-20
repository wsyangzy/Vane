package endpoint

import "github.com/wsyangzy/Vane/internal/config"

const endpointKeySeparator = "::"

// EndpointKey returns a stable identifier for an endpoint in runtime.
// - 数据库/channel 模式：使用 channel::name，避免不同渠道同名导致内部状态冲突。
// - 历史文本配置兼容：channel 为空时回退为 name。
func EndpointKey(channel, name string) string {
	if channel == "" {
		return name
	}
	if name == "" {
		return channel
	}
	return channel + endpointKeySeparator + name
}

// EndpointKeyForConfig returns the runtime key for a concrete endpoint config.
// SQLite-managed Claude endpoints can have the same display name under different
// credential groups, so recordToConfig supplies RuntimeKey for collision-free state.
func EndpointKeyForConfig(cfg config.EndpointConfig) string {
	if cfg.RuntimeKey != "" {
		return cfg.RuntimeKey
	}
	return EndpointKey(cfg.Channel, cfg.Name)
}

func endpointKeyFromConfig(cfg config.EndpointConfig) string {
	return EndpointKeyForConfig(cfg)
}
