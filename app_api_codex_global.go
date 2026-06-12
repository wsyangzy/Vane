package main

// app_api_codex_global.go - Codex 全局配置 API
// 读写 ~/.codex/config.toml 中的关键字段（model / reasoning_effort / 1M / base_url）
// 这些字段在代理模式下不再由渠道级管理，而是作为全局配置由 Codex CLI 直接读取。
//
// 设计原则：
//   - base_url 不让用户手填，自动取自「设置 → 服务端口」的 host:port + /v1
//   - 路径固定为 ~/.codex/config.toml，UI 上不再展示

import (
	"context"
	"fmt"
	"strings"
	"time"
)

// codexDefaultProviderName 当 config.toml 中 model_provider 字段为空时使用的约定名称
const codexDefaultProviderName = "ai-relay-forwarder"
const codexGlobalSettingsCategory = "codex_global"
const codexGlobalDefaultModelKey = "default_model"
const codexGlobalDefaultReasoningEffortKey = "default_reasoning_effort"
const codexGlobalDefaultEnable1MContextKey = "default_enable_1m_context"

// CodexGlobalConfigInfo 用于读取 ~/.codex/config.toml 中的全局配置（前端展示）
// base_url 由系统自动同步自服务端口，不向前端暴露
type CodexGlobalConfigInfo struct {
	Model                    string `json:"model"`                       // 用户默认 model
	ReasoningEffort          string `json:"reasoning_effort"`            // 用户默认 model_reasoning_effort
	Enable1MContext          bool   `json:"enable_1m_context"`           // 用户默认 1M 开关
	EffectiveModel           string `json:"effective_model"`             // 当前写入 config.toml 的生效 model
	EffectiveReasoningEffort string `json:"effective_reasoning_effort"`  // 当前生效 model_reasoning_effort
	EffectiveEnable1MContext bool   `json:"effective_enable_1m_context"` // 当前生效 1M 开关
	GlobalTimeoutSeconds     int    `json:"global_timeout_seconds"`      // 全局非流式请求超时
}

// CodexGlobalConfigInput 用于更新 Codex 全局配置（前端写入）
// 不接受 base_url / provider_name，由后端自动决定
type CodexGlobalConfigInput struct {
	Model           string `json:"model"`
	ReasoningEffort string `json:"reasoning_effort"`
	Enable1MContext bool   `json:"enable_1m_context"`
}

// GetCodexGlobalConfig 读取 Codex 全局配置（~/.codex/config.toml 中的关键字段）
func (a *App) GetCodexGlobalConfig() (CodexGlobalConfigInfo, error) {
	effective, err := a.readCodexEffectiveConfig()
	if err != nil {
		return CodexGlobalConfigInfo{}, err
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	defaults := a.loadCodexDefaultConfig(ctx, effective)
	return CodexGlobalConfigInfo{
		Model:                    defaults.Model,
		ReasoningEffort:          defaults.ReasoningEffort,
		Enable1MContext:          defaults.Enable1MContext,
		EffectiveModel:           effective.Model,
		EffectiveReasoningEffort: effective.ReasoningEffort,
		EffectiveEnable1MContext: effective.Enable1MContext,
		GlobalTimeoutSeconds:     a.getCodexGlobalTimeoutSeconds(),
	}, nil
}

func (a *App) getCodexGlobalTimeoutSeconds() int {
	a.mu.RLock()
	defer a.mu.RUnlock()
	if a.config != nil && a.config.GlobalTimeout > 0 {
		return int(a.config.GlobalTimeout.Seconds())
	}
	return 300
}

func (a *App) readCodexEffectiveConfig() (CodexGlobalConfigInput, error) {
	_, content, existed, err := a.readCodexFileAllowMissing(a.getCodexConfigPath)
	if err != nil {
		return CodexGlobalConfigInput{}, err
	}
	if !existed {
		return CodexGlobalConfigInput{}, nil
	}

	text := string(content)
	info := CodexGlobalConfigInput{
		Model:           readCodexTopLevelString(text, "model"),
		ReasoningEffort: readCodexTopLevelString(text, "model_reasoning_effort"),
	}
	if window := readCodexTopLevelString(text, "model_context_window"); window != "" {
		info.Enable1MContext = window == codexProvider1MContextWindow
	}
	return info, nil
}

func (a *App) loadCodexDefaultConfig(ctx context.Context, fallback CodexGlobalConfigInput) CodexGlobalConfigInput {
	defaults := fallback
	if defaults.ReasoningEffort == "" {
		defaults.ReasoningEffort = "xhigh"
	}

	a.mu.RLock()
	settingsService := a.settingsService
	a.mu.RUnlock()
	if settingsService == nil {
		return defaults
	}

	if model, err := settingsService.GetValue(ctx, codexGlobalSettingsCategory, codexGlobalDefaultModelKey); err == nil && strings.TrimSpace(model) != "" {
		defaults.Model = strings.TrimSpace(model)
	}
	if effort, err := settingsService.GetValue(ctx, codexGlobalSettingsCategory, codexGlobalDefaultReasoningEffortKey); err == nil && strings.TrimSpace(effort) != "" {
		defaults.ReasoningEffort = strings.TrimSpace(effort)
	}
	if enabled, err := settingsService.GetValue(ctx, codexGlobalSettingsCategory, codexGlobalDefaultEnable1MContextKey); err == nil && strings.TrimSpace(enabled) != "" {
		defaults.Enable1MContext = parseCodexBool(enabled)
	}
	return defaults
}

func (a *App) saveCodexDefaultConfig(ctx context.Context, input CodexGlobalConfigInput) error {
	a.mu.RLock()
	settingsService := a.settingsService
	a.mu.RUnlock()
	if settingsService == nil {
		return nil
	}

	if err := settingsService.Set(ctx, codexGlobalSettingsCategory, codexGlobalDefaultModelKey, strings.TrimSpace(input.Model)); err != nil {
		return err
	}
	if err := settingsService.Set(ctx, codexGlobalSettingsCategory, codexGlobalDefaultReasoningEffortKey, strings.TrimSpace(input.ReasoningEffort)); err != nil {
		return err
	}
	if err := settingsService.Set(ctx, codexGlobalSettingsCategory, codexGlobalDefaultEnable1MContextKey, renderCodexBool(input.Enable1MContext)); err != nil {
		return err
	}
	return nil
}

func parseCodexBool(value string) bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "1", "true", "yes", "on", "enabled":
		return true
	default:
		return false
	}
}

func renderCodexBool(value bool) string {
	if value {
		return "true"
	}
	return "false"
}

// UpdateCodexGlobalConfig 更新 Codex 全局配置并写回 ~/.codex/config.toml
// base_url 自动取自系统当前的 server.host:port，确保与「设置 → 服务端口」一致
func (a *App) UpdateCodexGlobalConfig(input CodexGlobalConfigInput) error {
	path, content, existed, err := a.readCodexFileAllowMissing(a.getCodexConfigPath)
	if err != nil {
		return err
	}

	original := append([]byte(nil), content...)
	text := string(content)

	// 1. 决定 model_provider 名（沿用 config.toml 现有值 > 约定默认值）
	providerName := strings.TrimSpace(readCodexTopLevelString(text, "model_provider"))
	if providerName == "" {
		providerName = codexDefaultProviderName
	}

	// 2. 自动计算 base_url（取自系统 server 配置）
	baseURL := a.computeCodexProxyBaseURL()

	// 3. 写顶层字段（model / model_reasoning_effort / model_provider）
	topValues := map[string]codexTopLevelValue{
		"model":                  {Value: strings.TrimSpace(input.Model), Quoted: true},
		"model_reasoning_effort": {Value: strings.TrimSpace(input.ReasoningEffort), Quoted: true},
		"model_provider":         {Value: providerName, Quoted: true},
	}
	if input.Enable1MContext {
		topValues["model_context_window"] = codexTopLevelValue{Value: codexProvider1MContextWindow, Quoted: false}
	}
	updated, err := upsertCodexTopLevelConfig(text, topValues)
	if err != nil {
		return fmt.Errorf("写入顶层配置失败: %w", err)
	}
	// 关闭 1M 上下文时清理 model_context_window 字段
	if !input.Enable1MContext {
		updated = removeCodexTopLevelConfig(updated, []string{"model_context_window"})
	}

	// 4. 写 [model_providers.{name}].base_url（自动同步系统代理地址）
	updated, err = upsertCodexProviderBaseURL(updated, providerName, baseURL)
	if err != nil {
		return fmt.Errorf("写入 base_url 失败: %w", err)
	}

	// 5. 写回文件（带回滚保护）
	if err := writeCodexFile(path, []byte(updated)); err != nil {
		if rollbackErr := restoreCodexFile(path, original, existed); rollbackErr != nil {
			return fmt.Errorf("写入 config.toml 失败 (%s): %w；回滚也失败: %v", path, err, rollbackErr)
		}
		return fmt.Errorf("写入 config.toml 失败 (%s): %w", path, err)
	}

	if a.logger != nil {
		a.logger.Info("✅ Codex 全局配置已更新",
			"path", path,
			"provider_name", providerName,
			"model", input.Model,
			"reasoning_effort", input.ReasoningEffort,
			"enable_1m_context", input.Enable1MContext,
			"base_url", baseURL,
		)
	}
	return nil
}

// computeCodexProxyBaseURL 计算本程序代理地址用于 Codex base_url
// 取自 a.config.Server.Host / Port + /v1；监听地址为 0.0.0.0 或空时使用 127.0.0.1
func (a *App) computeCodexProxyBaseURL() string {
	a.mu.RLock()
	cfg := a.config
	a.mu.RUnlock()
	if cfg == nil {
		return "http://127.0.0.1:8080/v1"
	}
	host := strings.TrimSpace(cfg.Server.Host)
	if host == "" || host == "0.0.0.0" {
		host = "127.0.0.1"
	}
	port := cfg.Server.Port
	if port <= 0 {
		port = 8080
	}
	return fmt.Sprintf("http://%s:%d/v1", host, port)
}

// readCodexTopLevelString 从 TOML 顶层（首个 section 之前）读取一个字符串/数字字段的值
// 兼容带引号与不带引号两种情形；返回不含引号的原始值
func readCodexTopLevelString(content, key string) string {
	lines := splitCodexLines(content)
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "[") && strings.HasSuffix(trimmed, "]") {
			break // 进入 section，停止查找
		}
		if !isTopLevelTomlAssignment(trimmed, key) {
			continue
		}
		parts := strings.SplitN(trimmed, "=", 2)
		if len(parts) != 2 {
			continue
		}
		value := strings.TrimSpace(parts[1])
		// 去掉行内注释
		if idx := strings.Index(value, "#"); idx >= 0 {
			value = strings.TrimSpace(value[:idx])
		}
		// 去掉引号
		if len(value) >= 2 && value[0] == '"' && value[len(value)-1] == '"' {
			value = value[1 : len(value)-1]
		}
		return value
	}
	return ""
}

// upsertCodexProviderBaseURL 替换或插入 [model_providers.{name}].base_url 行
// - section 存在 + base_url 行存在 → 替换值
// - section 存在 + base_url 行不存在 → 在 section 头部追加
// - section 不存在 → 在文件末尾新建一个最简 section
func upsertCodexProviderBaseURL(content, providerName, baseURL string) (string, error) {
	if providerName == "" {
		return "", fmt.Errorf("provider_name 不能为空")
	}
	lines := splitCodexLines(content)
	header := fmt.Sprintf("[model_providers.%s]", providerName)
	newLine := fmt.Sprintf(`base_url = "%s"`, escapeTomlString(baseURL))

	sectionStart := -1
	sectionEnd := len(lines) // 默认到文件末尾
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if sectionStart == -1 {
			if trimmed == header {
				sectionStart = i
			}
			continue
		}
		// 已进入目标 section，找下一个 section 边界
		if strings.HasPrefix(trimmed, "[") && strings.HasSuffix(trimmed, "]") {
			sectionEnd = i
			break
		}
	}

	// Section 不存在 → 在文件末尾新建
	if sectionStart == -1 {
		if len(lines) > 0 && strings.TrimSpace(lines[len(lines)-1]) != "" {
			lines = append(lines, "")
		}
		lines = append(lines,
			header,
			fmt.Sprintf(`name = "%s"`, escapeTomlString(providerName)),
			newLine,
			fmt.Sprintf(`wire_api = "%s"`, codexProviderWireAPI),
			"requires_openai_auth = true",
			"",
		)
		return joinCodexLines(lines), nil
	}

	// Section 存在 → 找 base_url 行替换；找不到 → 在 section header 之后插入
	for i := sectionStart + 1; i < sectionEnd; i++ {
		trimmed := strings.TrimSpace(lines[i])
		if isTopLevelTomlAssignment(trimmed, "base_url") {
			lines[i] = newLine
			return joinCodexLines(lines), nil
		}
	}
	// 找不到 base_url 行：插入到 section header 后第一行
	insertAt := sectionStart + 1
	updated := make([]string, 0, len(lines)+1)
	updated = append(updated, lines[:insertAt]...)
	updated = append(updated, newLine)
	updated = append(updated, lines[insertAt:]...)
	return joinCodexLines(updated), nil
}
