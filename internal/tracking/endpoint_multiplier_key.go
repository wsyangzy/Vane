package tracking

import "strings"

const endpointMultiplierKeySeparator = "::"

// endpointMultiplierKey builds the lookup key for endpoint multipliers.
// 允许不同渠道端点同名，为避免倍率冲突，倍率缓存以 "channel::endpointName" 为 key。
// 兼容：若 channel 为空则回退到 groupName，再回退到 endpointName。
func endpointMultiplierKey(channel, groupName, endpointName string) string {
	if endpointName == "" {
		return ""
	}
	if channel == "" {
		channel = groupName
	}
	if channel == "" {
		return endpointName
	}
	return channel + endpointMultiplierKeySeparator + endpointName
}

func endpointMultiplierGroupKey(channel, groupName, endpointName string) string {
	if endpointName == "" {
		return ""
	}
	if channel == "" || groupName == "" || groupName == channel {
		return endpointMultiplierKey(channel, groupName, endpointName)
	}
	return channel + endpointMultiplierKeySeparator + groupName + endpointMultiplierKeySeparator + endpointName
}

// scopeMultiplierKeyByProvider 为倍率 key 补上 provider 前缀。
// Claude 与 Codex 允许配置同名的渠道 / 凭证分组 / 端点（例如两侧都有
// anyrouter::默认分组::主站直连）。倍率缓存是两个 provider 共用的一张 map，
// 若 key 不含 provider，后写入的 Codex 条目会覆盖 Claude 条目，
// 导致 Claude 请求套用 Codex 的倍率。
func scopeMultiplierKeyByProvider(provider, key string) string {
	if key == "" {
		return ""
	}
	return defaultProvider(strings.TrimSpace(provider)) + endpointMultiplierKeySeparator + key
}

// endpointMultiplierBaseKeys 返回不含 provider 前缀的候选 key，由精确到宽泛排列。
func endpointMultiplierBaseKeys(channel, groupName, endpointName string) []string {
	return []string{
		endpointMultiplierGroupKey(channel, groupName, endpointName),
		endpointMultiplierKey(channel, "", endpointName),
		endpointMultiplierKey("", groupName, endpointName),
		endpointName,
	}
}

// endpointMultiplierLookupKeys 先查 provider 域内的 key，未命中再退回不含
// provider 的 legacy key。写入侧（app.go）只写 provider 域的 key，legacy 分支
// 仅用于兼容直接构造倍率 map 的调用点。
func endpointMultiplierLookupKeys(provider, channel, groupName, endpointName string) []string {
	baseKeys := endpointMultiplierBaseKeys(channel, groupName, endpointName)
	keys := make([]string, 0, len(baseKeys)*2)
	seen := map[string]struct{}{}
	appendKey := func(key string) {
		if key == "" {
			return
		}
		if _, ok := seen[key]; ok {
			return
		}
		seen[key] = struct{}{}
		keys = append(keys, key)
	}
	for _, key := range baseKeys {
		appendKey(scopeMultiplierKeyByProvider(provider, key))
	}
	for _, key := range baseKeys {
		appendKey(key)
	}
	return keys
}

// EndpointMultiplierKey is the exported helper for building the multiplier key from provider + channel + endpointName.
func EndpointMultiplierKey(provider, channel, endpointName string) string {
	return scopeMultiplierKeyByProvider(provider, endpointMultiplierKey(channel, "", endpointName))
}

// EndpointMultiplierGroupKey is the exported helper for group-scoped endpoint multipliers.
func EndpointMultiplierGroupKey(provider, channel, groupName, endpointName string) string {
	return scopeMultiplierKeyByProvider(provider, endpointMultiplierGroupKey(channel, groupName, endpointName))
}

// ScopeEndpointMultiplierKey 供调用方给已构造好的 runtime key（如 Claude 的
// channel::g<ID>::name）补上 provider 前缀，使其与本包生成的 key 处于同一命名空间。
func ScopeEndpointMultiplierKey(provider, key string) string {
	return scopeMultiplierKeyByProvider(provider, key)
}
