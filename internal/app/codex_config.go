package app

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"

	codexconfig "github.com/wsyangzy/Vane/internal/providers/codex/clientconfig"
	codexstore "github.com/wsyangzy/Vane/internal/providers/codex/store"
)

// ensureCodexProviderSchema applies provider-owned schema on the management connection.
func (a *App) ensureCodexProviderSchema(ctx context.Context) error {
	a.mu.RLock()
	db := a.storeDB
	if db == nil && a.usageTracker != nil {
		db = a.usageTracker.GetDB()
	}
	a.mu.RUnlock()
	if db == nil {
		return fmt.Errorf("Codex 提供商数据库未就绪")
	}
	return codexstore.EnsureSchema(ctx, db, a.logger)
}

func (a *App) syncActiveCodexProviderToConfig(record *codexstore.CodexProviderRecord, previousName string) error {
	if record == nil {
		return fmt.Errorf("Codex 提供商记录为空")
	}

	configPath, originalConfig, configExisted, err := a.readCodexFileAllowMissing(codexconfig.GetCodexConfigPath)
	if err != nil {
		return err
	}
	authPath, originalAuth, authExisted, err := a.readCodexFileAllowMissing(codexconfig.GetCodexAuthPath)
	if err != nil {
		return err
	}

	updatedConfig := string(originalConfig)
	if previousName != "" && previousName != record.Name {
		updatedConfig = codexconfig.DeleteCodexProviderSection(updatedConfig, previousName)
	}

	updatedConfig, err = codexconfig.UpsertCodexProviderSection(updatedConfig, record)
	if err != nil {
		return err
	}
	if !record.Enable1MContext {
		updatedConfig = codexconfig.RemoveCodexTopLevelConfig(updatedConfig, []string{"model_context_window"})
	}
	updatedConfig, err = codexconfig.UpsertCodexTopLevelConfig(updatedConfig, codexconfig.BuildCodexTopLevelConfigValues(record))
	if err != nil {
		return err
	}

	updatedAuth, err := codexconfig.UpsertCodexAuthConfig(string(originalAuth), record.ApiKey)
	if err != nil {
		return err
	}

	if err := codexconfig.WriteCodexLiveFiles(
		authPath,
		originalAuth,
		authExisted,
		updatedAuth,
		configPath,
		originalConfig,
		configExisted,
		[]byte(updatedConfig),
	); err != nil {
		return err
	}

	if a.logger != nil {
		a.logger.Info(
			"✅ Codex 配置已接管",
			"config_path", configPath,
			"auth_path", authPath,
			"model_provider", record.Name,
			"model", record.Model,
			"enable_1m_context", record.Enable1MContext,
		)
	}

	return nil
}

func (a *App) removeCodexProviderConfig(name string, nextActive *codexstore.CodexProviderRecord) error {
	if strings.TrimSpace(name) == "" {
		return fmt.Errorf("Codex 提供商标识不能为空")
	}

	configPath, originalConfig, configExisted, err := a.readCodexFileAllowMissing(codexconfig.GetCodexConfigPath)
	if err != nil {
		return err
	}

	updatedConfig := codexconfig.DeleteCodexProviderSection(string(originalConfig), name)
	if nextActive == nil {
		updatedConfig = codexconfig.RemoveCodexTopLevelConfig(updatedConfig, []string{"model", "model_context_window", "model_provider"})
		if err := codexconfig.WriteCodexFile(configPath, []byte(updatedConfig)); err != nil {
			if rollbackErr := codexconfig.RestoreCodexFile(configPath, originalConfig, configExisted); rollbackErr != nil {
				return fmt.Errorf("写入 Codex 配置文件失败 (%s): %w；回滚也失败: %v", configPath, err, rollbackErr)
			}
			return fmt.Errorf("写入 Codex 配置文件失败 (%s): %w", configPath, err)
		}
		return nil
	}

	authPath, originalAuth, authExisted, err := a.readCodexFileAllowMissing(codexconfig.GetCodexAuthPath)
	if err != nil {
		return err
	}

	updatedConfig, err = codexconfig.UpsertCodexProviderSection(updatedConfig, nextActive)
	if err != nil {
		return err
	}
	if !nextActive.Enable1MContext {
		updatedConfig = codexconfig.RemoveCodexTopLevelConfig(updatedConfig, []string{"model_context_window"})
	}
	updatedConfig, err = codexconfig.UpsertCodexTopLevelConfig(updatedConfig, codexconfig.BuildCodexTopLevelConfigValues(nextActive))
	if err != nil {
		return err
	}

	updatedAuth, err := codexconfig.UpsertCodexAuthConfig(string(originalAuth), nextActive.ApiKey)
	if err != nil {
		return err
	}

	return codexconfig.WriteCodexLiveFiles(
		authPath,
		originalAuth,
		authExisted,
		updatedAuth,
		configPath,
		originalConfig,
		configExisted,
		[]byte(updatedConfig),
	)
}

func (a *App) readCodexFileAllowMissing(pathGetter func() (string, error)) (string, []byte, bool, error) {
	path, err := pathGetter()
	if err != nil {
		return "", nil, false, err
	}

	content, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return path, nil, false, nil
		}
		return "", nil, false, fmt.Errorf("读取 Codex 文件失败 (%s): %w", path, err)
	}

	return path, content, true, nil
}

func (a *App) codexChannelEndpointAsProviderRecord(channel *codexstore.CodexChannelRecord, endpoint *codexstore.CodexEndpointRecord) *codexstore.CodexProviderRecord {
	if channel == nil || endpoint == nil {
		return nil
	}

	effectiveModel := channel.Model
	if effectiveModel == "" {
		// 读取 ~/.codex/config.toml 当前的全局 model 作为 fallback
		if _, content, existed, err := a.readCodexFileAllowMissing(codexconfig.GetCodexConfigPath); err == nil && existed {
			effectiveModel = readCodexTopLevelString(string(content), "model")
		}
	}
	effectiveApiKey := strings.TrimSpace(endpoint.ApiKey)
	if effectiveApiKey == "" && a.codexCredentialGroupStore != nil && endpoint.CredentialGroupID > 0 {
		if group, err := a.codexCredentialGroupStore.GetByID(context.Background(), endpoint.CredentialGroupID); err == nil && group != nil {
			effectiveApiKey = strings.TrimSpace(group.ApiKey)
		}
	}
	if effectiveApiKey == "" && a.codexCredentialGroupStore != nil {
		if group, err := a.codexCredentialGroupStore.GetDefaultByChannelID(context.Background(), channel.ID); err == nil && group != nil {
			effectiveApiKey = strings.TrimSpace(group.ApiKey)
		}
	}
	if effectiveApiKey == "" {
		effectiveApiKey = channel.ApiKey
	}

	return &codexstore.CodexProviderRecord{
		Name:            channel.Name,
		ProviderType:    channel.ProviderType,
		BaseURL:         endpoint.BaseURL,
		ApiKey:          effectiveApiKey,
		Model:           effectiveModel,
		ReasoningEffort: channel.ReasoningEffort,
		Enable1MContext: channel.Enable1MContext,
		Website:         channel.Website,
	}
}

func (a *App) syncActiveCodexChannelToConfig(channel *codexstore.CodexChannelRecord, endpoint *codexstore.CodexEndpointRecord, previousChannelName string) error {
	if channel == nil {
		return fmt.Errorf("Codex 渠道为空")
	}
	if endpoint == nil {
		return fmt.Errorf("Codex 渠道 '%s' 没有可用端点", channel.Name)
	}
	// 当 Codex 代理启用时，不再修改 ~/.codex/config.toml
	// 用户手动管理 model_providers，代理通过内部路由根据数据库中的活跃渠道转发请求
	if a.isCodexProxyEnabled() {
		if a.logger != nil {
			a.logger.Info("Codex 代理已启用，跳过 config.toml 写入（仅切换内部路由）",
				"channel", channel.Name,
				"endpoint", endpoint.Name,
			)
		}
		return nil
	}
	record := a.codexChannelEndpointAsProviderRecord(channel, endpoint)
	return a.syncActiveCodexProviderToConfig(record, previousChannelName)
}

func (a *App) isCodexProxyEnabled() bool {
	return true
}

func (a *App) applyCodexProxyOverride(_ *codexstore.CodexProviderRecord) {
	// no-op: Codex 代理启用时跳过 config.toml 写入
}

func (a *App) removeCodexChannelConfig(name string, nextChannel *codexstore.CodexChannelRecord, nextEndpoint *codexstore.CodexEndpointRecord) error {
	// 当 Codex 代理启用时，不再修改 config.toml
	if a.isCodexProxyEnabled() {
		if a.logger != nil {
			a.logger.Info("Codex 代理已启用，跳过 config.toml 删除（仅清理数据库记录）",
				"channel", name,
			)
		}
		return nil
	}
	var nextRecord *codexstore.CodexProviderRecord
	if nextChannel != nil && nextEndpoint != nil {
		nextRecord = a.codexChannelEndpointAsProviderRecord(nextChannel, nextEndpoint)
	}
	return a.removeCodexProviderConfig(name, nextRecord)
}
