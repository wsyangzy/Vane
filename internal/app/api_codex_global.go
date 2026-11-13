package app

import (
	"context"
	"fmt"
	"net"
	"strconv"
	"strings"
	"time"

	"github.com/pelletier/go-toml/v2"
	codexconfig "github.com/wsyangzy/Vane/internal/providers/codex/clientconfig"
	codexservice "github.com/wsyangzy/Vane/internal/providers/codex/service"
)

const codexGlobalSettingsCategory = "codex_global"

const codexGlobalDefaultModelKey = "default_model"

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
	ReasoningEffort string `json:"reasoning_effort"` // Deprecated: 仅保留 Wails 兼容，更新时忽略
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
	_, content, existed, err := a.readCodexFileAllowMissing(codexconfig.GetCodexConfigPath)
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
		info.Enable1MContext = window == codexconfig.ContextWindow1M
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
	path, content, existed, err := a.readCodexFileAllowMissing(codexconfig.GetCodexConfigPath)
	if err != nil {
		return err
	}

	original := append([]byte(nil), content...)
	text := string(content)

	// 1. 自动计算 base_url（取自系统 server 配置）
	baseURL := a.computeCodexProxyBaseURL()

	// 2. 写 Vane 管理的模型字段；model_reasoning_effort 由客户端管理，保持原值
	topValues := map[string]codexconfig.TopLevelValue{
		"model": {Value: strings.TrimSpace(input.Model), Quoted: true},
	}
	if input.Enable1MContext {
		topValues["model_context_window"] = codexconfig.TopLevelValue{Value: codexconfig.ContextWindow1M, Quoted: false}
	}
	updated, err := codexconfig.UpsertCodexTopLevelConfig(text, topValues)
	if err != nil {
		return fmt.Errorf("写入顶层配置失败: %w", err)
	}
	// 关闭 1M 上下文时清理 model_context_window 字段
	if !input.Enable1MContext {
		updated = codexconfig.RemoveCodexTopLevelConfig(updated, []string{"model_context_window"})
	}

	// 3. 生成统一的客户端接入配置（自动同步系统代理地址和鉴权）
	localKey := "vane-local"
	a.mu.RLock()
	if a.config != nil && a.config.Auth.Enabled {
		localKey = a.config.Auth.Token
	}
	a.mu.RUnlock()
	if !validRoutingAuthToken(localKey) {
		return fmt.Errorf("应用级代理令牌无效，请先在设置中保存有效令牌")
	}
	rendered, _, _, err := codexservice.RenderClientConfig([]byte(updated), baseURL, localKey, a.clientConnectionManaged("codex"))
	if err != nil {
		return fmt.Errorf("生成 Codex 接入配置失败: %w", err)
	}
	updated = string(rendered)

	// 4. 写回文件（带回滚保护）
	if err := codexconfig.WriteCodexFile(path, []byte(updated)); err != nil {
		if rollbackErr := codexconfig.RestoreCodexFile(path, original, existed); rollbackErr != nil {
			return fmt.Errorf("写入 config.toml 失败 (%s): %w；回滚也失败: %v", path, err, rollbackErr)
		}
		return fmt.Errorf("写入 config.toml 失败 (%s): %w", path, err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := a.saveCodexDefaultConfig(ctx, input); err != nil {
		if rollbackErr := codexconfig.RestoreCodexFile(path, original, existed); rollbackErr != nil {
			return fmt.Errorf("保存 Codex 全局默认配置失败: %w；回滚 config.toml 也失败: %v", err, rollbackErr)
		}
		return fmt.Errorf("保存 Codex 全局默认配置失败: %w", err)
	}

	if a.logger != nil {
		a.logger.Info("✅ Codex 全局配置已更新",
			"path", path,
			"provider_name", codexservice.ClientProviderID,
			"model", input.Model,
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
	host := strings.Trim(strings.TrimSpace(cfg.Server.Host), "[]")
	if host == "" || host == "0.0.0.0" {
		host = "127.0.0.1"
	}
	if host == "::" {
		host = "::1"
	}
	port := cfg.Server.Port
	if port <= 0 {
		port = 8080
	}
	return "http://" + net.JoinHostPort(host, strconv.Itoa(port)) + "/v1"
}

// readCodexTopLevelString 从 TOML 顶层（首个 section 之前）读取一个字符串/数字字段的值
// 兼容带引号与不带引号两种情形；返回不含引号的原始值
func readCodexTopLevelString(content, key string) string {
	var config map[string]any
	if toml.Unmarshal([]byte(strings.TrimPrefix(content, "\ufeff")), &config) != nil {
		return ""
	}
	switch value := config[key].(type) {
	case string:
		return value
	case int64:
		return strconv.FormatInt(value, 10)
	}
	return ""
}

// upsertCodexProviderBaseURL 替换或插入 [model_providers.{name}].base_url 行
// - section 存在 + base_url 行存在 → 替换值
// - section 存在 + base_url 行不存在 → 在 section 头部追加
// - section 不存在 → 在文件末尾新建一个最简 section
func upsertCodexProviderBaseURL(content, providerName, baseURL string, localKeys ...string) (string, error) {
	if providerName == "" {
		return "", fmt.Errorf("provider_name 不能为空")
	}
	lines := codexconfig.SplitCodexLines(content)
	header := codexconfig.CodexProviderSectionHeader(providerName)
	headers := codexconfig.CodexProviderSectionHeaders(providerName)
	newLine := fmt.Sprintf(`base_url = "%s"`, codexconfig.EscapeTomlString(baseURL))
	catalogLine := fmt.Sprintf(`model_catalog_url = "%s"`, codexconfig.EscapeTomlString(strings.TrimRight(baseURL, "/")+"/models"))

	sectionStart := -1
	sectionEnd := len(lines) // 默认到文件末尾
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if sectionStart == -1 {
			if codexconfig.ContainsString(headers, trimmed) {
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
		localKey := "vane-local"
		if len(localKeys) > 0 {
			localKey = localKeys[0]
		}
		if len(lines) > 0 && strings.TrimSpace(lines[len(lines)-1]) != "" {
			lines = append(lines, "")
		}
		lines = append(lines,
			header,
			fmt.Sprintf(`name = "%s"`, codexconfig.EscapeTomlString(providerName)),
			newLine,
			catalogLine,
			fmt.Sprintf(`wire_api = "%s"`, codexconfig.WireAPI),
			"requires_openai_auth = false",
			fmt.Sprintf(`experimental_bearer_token = "%s"`, codexconfig.EscapeTomlString(localKey)),
			"",
		)
		return codexconfig.JoinCodexLines(lines), nil
	}

	// The catalog follows the same local listener, including port changes. Other
	// provider fields (authentication and extension headers) remain client-owned.
	lines[sectionStart] = header
	baseFound, catalogFound := false, false
	for i := sectionStart + 1; i < sectionEnd; i++ {
		trimmed := strings.TrimSpace(lines[i])
		if codexconfig.IsTopLevelTomlAssignment(trimmed, "base_url") {
			lines[i] = newLine
			baseFound = true
		}
		if codexconfig.IsTopLevelTomlAssignment(trimmed, "model_catalog_url") {
			lines[i] = catalogLine
			catalogFound = true
		}
	}
	missing := []string{}
	if !baseFound {
		missing = append(missing, newLine)
	}
	if !catalogFound {
		missing = append(missing, catalogLine)
	}
	insertAt := sectionStart + 1
	updated := make([]string, 0, len(lines)+len(missing))
	updated = append(updated, lines[:insertAt]...)
	updated = append(updated, missing...)
	updated = append(updated, lines[insertAt:]...)
	return codexconfig.JoinCodexLines(updated), nil
}
