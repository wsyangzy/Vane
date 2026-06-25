package tracking

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

func endpointMultiplierLookupKeys(channel, groupName, endpointName string) []string {
	groupKey := endpointMultiplierGroupKey(channel, groupName, endpointName)
	baseKey := endpointMultiplierKey(channel, "", endpointName)
	fallbackKey := endpointMultiplierKey("", groupName, endpointName)
	if groupKey == "" && baseKey == "" && fallbackKey == "" {
		return nil
	}
	keys := make([]string, 0, 3)
	seen := map[string]struct{}{}
	for _, key := range []string{groupKey, baseKey, fallbackKey, endpointName} {
		if key == "" {
			continue
		}
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		keys = append(keys, key)
	}
	return keys
}

// EndpointMultiplierKey is the exported helper for building the multiplier key from channel + endpointName.
func EndpointMultiplierKey(channel, endpointName string) string {
	return endpointMultiplierKey(channel, "", endpointName)
}

// EndpointMultiplierGroupKey is the exported helper for group-scoped endpoint multipliers.
func EndpointMultiplierGroupKey(channel, groupName, endpointName string) string {
	return endpointMultiplierGroupKey(channel, groupName, endpointName)
}
