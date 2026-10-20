// key_switch.go - Key 切换功能
// 包含多 API Key 切换、Token 管理等

package endpoint

import (
	"fmt"
	"log/slog"
	"time"

	"github.com/wsyangzy/Vane/internal/platform/events"
)

// GetKeyManager 返回 Key 管理器
func (m *Manager) GetKeyManager() *KeyManager {
	return m.keyManager
}

// SwitchEndpointToken 切换端点的 Token
func (m *Manager) SwitchEndpointToken(endpointName string, index int) error {
	// 验证端点存在
	ep := m.GetEndpointByNameAny(endpointName)
	if ep == nil {
		return fmt.Errorf("端点 '%s' 未找到", endpointName)
	}

	// 验证该端点支持多 Token
	if len(ep.Config.Tokens) == 0 {
		return fmt.Errorf("端点 '%s' 未配置多 Token", endpointName)
	}

	err := m.keyManager.SwitchToken(endpointName, index)
	if err != nil {
		return err
	}

	// 获取切换后的 Token 名称用于日志
	tokenName := ""
	if index >= 0 && index < len(ep.Config.Tokens) {
		tokenName = ep.Config.Tokens[index].Name
		if tokenName == "" {
			tokenName = fmt.Sprintf("Token %d", index+1)
		}
	}

	slog.Info(fmt.Sprintf("🔑 [Key切换] 端点 %s 的 Token 已切换到: %s (索引: %d)", endpointName, tokenName, index))

	// 发布事件通知
	if m.eventBus != nil {
		m.eventBus.Publish(events.Event{
			Type:     "endpoint_key_changed",
			Source:   "key_manager",
			Priority: events.PriorityHigh,
			Data: map[string]interface{}{
				"endpoint":  endpointName,
				"key_type":  "token",
				"new_index": index,
				"key_name":  tokenName,
				"timestamp": time.Now().Format("2006-01-02 15:04:05"),
			},
		})
	}

	return nil
}

// SwitchEndpointApiKey 切换端点的 API Key
func (m *Manager) SwitchEndpointApiKey(endpointName string, index int) error {
	ep := m.GetEndpointByNameAny(endpointName)
	if ep == nil {
		return fmt.Errorf("端点 '%s' 未找到", endpointName)
	}

	if len(ep.Config.ApiKeys) == 0 {
		return fmt.Errorf("端点 '%s' 未配置多 API Key", endpointName)
	}

	err := m.keyManager.SwitchApiKey(endpointName, index)
	if err != nil {
		return err
	}

	// 获取切换后的 API Key 名称用于日志
	keyName := ""
	if index >= 0 && index < len(ep.Config.ApiKeys) {
		keyName = ep.Config.ApiKeys[index].Name
		if keyName == "" {
			keyName = fmt.Sprintf("API Key %d", index+1)
		}
	}

	slog.Info(fmt.Sprintf("🔑 [Key切换] 端点 %s 的 API Key 已切换到: %s (索引: %d)", endpointName, keyName, index))

	if m.eventBus != nil {
		m.eventBus.Publish(events.Event{
			Type:     "endpoint_key_changed",
			Source:   "key_manager",
			Priority: events.PriorityHigh,
			Data: map[string]interface{}{
				"endpoint":  endpointName,
				"key_type":  "api_key",
				"new_index": index,
				"key_name":  keyName,
				"timestamp": time.Now().Format("2006-01-02 15:04:05"),
			},
		})
	}

	return nil
}

// GetEndpointKeysInfo 获取端点的 Key 信息（用于 API，Key 值脱敏）
func (m *Manager) GetEndpointKeysInfo(endpointName string) map[string]interface{} {
	ep := m.GetEndpointByNameAny(endpointName)
	if ep == nil {
		return nil
	}

	state := m.keyManager.GetEndpointKeyState(endpointName)

	// 构建 Token 列表（脱敏）
	tokens := make([]map[string]interface{}, 0)
	for i, t := range ep.Config.Tokens {
		tokens = append(tokens, map[string]interface{}{
			"index":     i,
			"name":      t.Name,
			"masked":    maskKey(t.Value),
			"is_active": state != nil && state.ActiveTokenIndex == i,
		})
	}
	// 单 Token 情况
	if len(tokens) == 0 && ep.Config.Token != "" {
		tokens = append(tokens, map[string]interface{}{
			"index":     0,
			"name":      "default",
			"masked":    maskKey(ep.Config.Token),
			"is_active": true,
		})
	}

	// 构建 API Key 列表（脱敏）
	apiKeys := make([]map[string]interface{}, 0)
	for i, k := range ep.Config.ApiKeys {
		apiKeys = append(apiKeys, map[string]interface{}{
			"index":     i,
			"name":      k.Name,
			"masked":    maskKey(k.Value),
			"is_active": state != nil && state.ActiveApiKeyIndex == i,
		})
	}
	if len(apiKeys) == 0 && ep.Config.ApiKey != "" {
		apiKeys = append(apiKeys, map[string]interface{}{
			"index":     0,
			"name":      "default",
			"masked":    maskKey(ep.Config.ApiKey),
			"is_active": true,
		})
	}

	result := map[string]interface{}{
		"endpoint":           endpointName,
		"tokens":             tokens,
		"api_keys":           apiKeys,
		"supports_switching": len(ep.Config.Tokens) > 1 || len(ep.Config.ApiKeys) > 1,
	}

	if state != nil && !state.LastSwitchTime.IsZero() {
		result["last_switch_time"] = state.LastSwitchTime.Format("2006-01-02 15:04:05")
	}

	return result
}

// maskKey 脱敏 Key 值，只显示前4位和后4位
func maskKey(key string) string {
	if len(key) <= 8 {
		return "****"
	}
	return key[:4] + "****" + key[len(key)-4:]
}
