package app

import (
	"errors"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"

	"github.com/wsyangzy/Vane/internal/platform/clientconfig"
	claudesub "github.com/wsyangzy/Vane/internal/providers/claude/proxy/subscription"
	claudeservice "github.com/wsyangzy/Vane/internal/providers/claude/service"
	codexconfig "github.com/wsyangzy/Vane/internal/providers/codex/clientconfig"
	codexsub "github.com/wsyangzy/Vane/internal/providers/codex/proxy/subscription"
	codexservice "github.com/wsyangzy/Vane/internal/providers/codex/service"
	"github.com/wsyangzy/Vane/internal/routing"
	"github.com/wsyangzy/Vane/internal/subscriptions/accounts"
)

type SubscriptionConnectionCommand struct {
	ProfileID        int64  `json:"profile_id"`
	PoolID           int64  `json:"pool_id"`
	Provider         string `json:"provider"`
	Action           string `json:"action"`
	GroupID          int64  `json:"group_id"`
	LocalKey         string `json:"local_key"`
	Revision         string `json:"revision"`
	BindingVersion   int64  `json:"binding_version"`
	ContentRevision  string `json:"content_revision"`
	ModelCatalogMode string `json:"model_catalog_mode"`
	ModelCatalogPath string `json:"model_catalog_path"`
	CompactionMode   string `json:"compaction_mode"`
}

type SubscriptionConnectionResult struct {
	ProfileID        int64  `json:"profile_id"`
	ProfileName      string `json:"profile_name"`
	AuthEnabled      bool   `json:"auth_enabled"`
	ConfigPath       string `json:"config_path"`
	BaseURL          string `json:"base_url"`
	Running          bool   `json:"running"`
	BackupAvailable  bool   `json:"backup_available"`
	Revision         string `json:"revision"`
	BindingVersion   int64  `json:"binding_version"`
	EndpointReady    bool   `json:"endpoint_ready"`
	OfficialURL      string `json:"official_url"`
	Managed          bool   `json:"managed"`
	OfficialEnabled  bool   `json:"official_enabled"`
	Snippet          string `json:"snippet"`
	Command          string `json:"command"`
	ContentRevision  string `json:"content_revision"`
	ModelCatalogMode string `json:"model_catalog_mode"`
	ModelCatalogPath string `json:"model_catalog_path"`
	CompactionMode   string `json:"compaction_mode"`
	RemoteCompaction string `json:"remote_compaction"`
}

var subscriptionConnectionMu sync.Mutex

func subscriptionConfigPath(provider string) (string, error) {
	var dir string
	if provider == "codex" {
		dir = os.Getenv("CODEX_HOME")
	} else {
		dir = os.Getenv("CLAUDE_CONFIG_DIR")
	}
	if strings.TrimSpace(dir) == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", errors.New("无法定位客户端配置目录")
		}
		if provider == "codex" {
			dir = filepath.Join(home, ".codex")
		} else {
			dir = filepath.Join(home, ".claude")
		}
	}
	if !filepath.IsAbs(dir) {
		return "", errors.New("客户端配置目录必须为绝对路径")
	}
	if provider == "codex" {
		return filepath.Join(dir, "config.toml"), nil
	}
	return filepath.Join(dir, "settings.json"), nil
}

// ManageSubscriptionConnection is explicitly invoked from the connection panel.
// Status never reads login files; previews expose generated fields only.
func (a *App) ManageSubscriptionConnection(input SubscriptionConnectionCommand) (SubscriptionConnectionResult, error) {
	subscriptionConnectionMu.Lock()
	defer subscriptionConnectionMu.Unlock()
	var result SubscriptionConnectionResult
	if input.Provider != "claude" && input.Provider != "codex" {
		return result, errors.New("仅支持 Claude 和 Codex")
	}
	if input.PoolID != 0 {
		return result, errors.New("legacy_pool_entry_retired: 请使用应用级代理鉴权接入统一调度")
	}
	switch input.Action {
	case "status", "preview", "apply", "restore", "choose_model_catalog":
	default:
		return result, errors.New("未知的订阅接入操作")
	}
	if input.Action != "restore" && input.GroupID != 0 {
		return result, errors.New("legacy_group_entry_retired: 客户端接入由默认调度方案决定")
	}
	path, err := subscriptionConfigPath(input.Provider)
	if err != nil {
		return result, err
	}
	result.ConfigPath = path
	if input.Action == "choose_model_catalog" {
		if input.Provider != "codex" {
			return result, errors.New("仅 Codex 支持选择模型目录文件")
		}
		if a.wailsApp == nil {
			return result, errors.New("文件选择需要在桌面应用中使用，也可直接填写本地文件路径")
		}
		selected, err := a.wailsApp.Dialog.OpenFile().CanChooseFiles(true).CanChooseDirectories(false).
			AddFilter("Codex 模型目录 (*.json)", "*.json").SetDirectory(filepath.Dir(path)).
			SetTitle("选择 Codex 模型目录").PromptForSingleSelection()
		for cause := err; cause != nil; cause = errors.Unwrap(cause) {
			if cause.Error() == "cancelled by user" {
				return result, nil
			}
		}
		if err != nil || selected == "" {
			return result, err
		}
		result.ModelCatalogPath, _, err = codexconfig.ReadModelCatalog(selected)
		result.ModelCatalogMode = "local"
		return result, err
	}
	current, err := clientconfig.Read(path)
	if err != nil {
		return result, err
	}
	result.Revision = current.Revision()
	result.BackupAvailable = clientconfig.BackupAvailable(path)
	// Restore stays available after a group/account has been deleted or disabled.
	if input.Action == "restore" {
		if err = clientconfig.Restore(path, input.Revision, accounts.UserVault{}); err != nil {
			return result, err
		}
		current, err = clientconfig.Read(path)
		result.Revision = current.Revision()
		result.BackupAvailable = clientconfig.BackupAvailable(path)
		return result, err
	}
	if input.Provider == "codex" {
		compaction, compactionErr := codexservice.ClientCompactionFromConfig(current.Data)
		if compactionErr == nil {
			result.CompactionMode, result.RemoteCompaction = compaction.Mode, compaction.RemoteCompaction
		}
		catalog, err := codexservice.ClientModelCatalogFromConfig(current.Data)
		if err != nil && input.Action != "status" {
			return result, err
		}
		// Keep status and encrypted restore available even for a damaged TOML.
		// Preview still reports the parse error before generating any content.
		if err != nil {
			catalog = codexservice.ClientModelCatalog{Mode: "local"}
		}
		result.ModelCatalogMode, result.ModelCatalogPath = catalog.Mode, catalog.Path
		if !current.Exists {
			result.ModelCatalogMode = "local"
			result.CompactionMode = "summary"
		}
		if result.ModelCatalogPath == "" {
			result.ModelCatalogPath = filepath.Join(filepath.Dir(path), "codex-models.json")
			for _, name := range []string{"codex-models.json", "models.json"} {
				candidate := filepath.Join(filepath.Dir(path), name)
				if info, err := os.Stat(candidate); err == nil && !info.IsDir() {
					result.ModelCatalogPath = candidate
					break
				}
			}
		}
	}
	status := a.GetSystemStatus()
	result.Running = status.ProxyRunning
	host := strings.Trim(status.ProxyHost, "[]")
	if host == "" || host == "0.0.0.0" || host == "localhost" {
		host = "127.0.0.1"
	} else if host == "::" {
		host = "::1"
	}
	ip := net.ParseIP(host)
	if ip == nil || !ip.IsLoopback() {
		return result, errors.New("订阅接入需要本机回环监听，请在设置中调整监听地址")
	}
	if status.ProxyPort < 1 || status.ProxyPort > 65535 {
		return result, errors.New("代理端口尚未就绪")
	}
	result.BaseURL = "http://" + net.JoinHostPort(host, strconv.Itoa(status.ProxyPort))
	if input.Provider == "codex" {
		result.BaseURL += "/v1"
		result.OfficialURL = codexsub.OfficialBaseURL
	} else {
		result.OfficialURL = claudesub.OfficialOrigin
	}
	state, err := a.ListRoutingProfiles(input.Provider)
	if err != nil {
		return result, err
	}
	var profile routing.RoutingProfile
	for _, candidate := range state.Profiles {
		if candidate.IsDefault && candidate.Enabled {
			profile = candidate
			break
		}
	}
	result.ProfileID, result.ProfileName = profile.ID, profile.Name
	result.BindingVersion = profile.Revision
	result.Managed = a.clientConnectionManaged(input.Provider)
	configuration, err := a.CheckRoutingConfiguration(input.Provider)
	if err != nil {
		return result, err
	}
	result.EndpointReady = configuration.Ready
	a.mu.RLock()
	auth := a.config.Auth
	a.mu.RUnlock()
	result.AuthEnabled = auth.Enabled
	if input.Action == "status" {
		return result, nil
	}
	if !result.EndpointReady {
		return result, errors.New("默认调度方案尚未就绪，请先检查方案成员、凭证、端点和应用级鉴权")
	}
	key := "vane-local"
	if auth.Enabled {
		key = auth.Token
		if !validRoutingAuthToken(key) {
			return result, errors.New("应用级代理令牌无效，请先在设置中保存有效令牌")
		}
	}
	var updated, snippet []byte
	if input.Provider == "claude" {
		updated, snippet, result.Command, err = claudeservice.RenderSubscriptionClient(current.Data, result.BaseURL, key, result.Managed)
	} else {
		options, optionsErr := a.codexClientConfigOptions(&codexservice.ClientModelCatalog{Mode: input.ModelCatalogMode, Path: input.ModelCatalogPath}, input.CompactionMode)
		if optionsErr != nil {
			return result, optionsErr
		}
		updated, snippet, result.Command, err = codexservice.RenderClientConfigWithOptions(current.Data, result.BaseURL, key, result.Managed, options)
		if err == nil {
			result.CompactionMode = options.CompactionMode
			compaction, _ := codexservice.ClientCompactionFromConfig(updated)
			result.RemoteCompaction = compaction.RemoteCompaction
			catalog, _ := codexservice.ClientModelCatalogFromConfig(updated)
			result.ModelCatalogMode, result.ModelCatalogPath = catalog.Mode, catalog.Path
		}
	}
	if err != nil {
		return result, err
	}
	defer clear(updated)
	defer clear(snippet)
	result.Snippet = string(snippet)
	result.ContentRevision = (clientconfig.Snapshot{Data: updated, Exists: true}).Revision()
	if input.Action == "apply" {
		if input.ContentRevision == "" || input.ContentRevision != result.ContentRevision {
			return SubscriptionConnectionResult{}, errors.New("接入配置已变化，请重新预览")
		}
		if input.ProfileID != result.ProfileID || input.BindingVersion != result.BindingVersion {
			return SubscriptionConnectionResult{}, errors.New("默认调度方案已变化，请重新预览")
		}
		if !result.Running {
			return SubscriptionConnectionResult{}, errors.New("代理尚未运行，请先启动代理并重新预览")
		}
		if err = clientconfig.Apply(path, input.Revision, updated, accounts.UserVault{}); err != nil {
			return SubscriptionConnectionResult{}, err
		}
		result.BackupAvailable = true
		result.Revision = (clientconfig.Snapshot{Data: updated, Exists: true}).Revision()
	}
	return result, nil
}

// Authentication depends on the resources reachable by the default profile,
// not on which management screen initiated the configuration update.
func (a *App) clientConnectionManaged(provider string) bool {
	a.poolMu.Lock()
	runtime := a.profileRuntimes[provider]
	a.poolMu.Unlock()
	if runtime == nil {
		return true
	}
	profile, ok := runtime.Profile(0)
	if !ok {
		return true
	}
	targets := []routing.RouteTarget{{ProfileID: profile.ID}}
	for _, rule := range profile.Rules {
		if rule.Enabled {
			targets = append(targets, rule.Targets...)
		}
	}
	for _, intent := range profile.Intents {
		if intent.Enabled {
			targets = append(targets, intent.Targets...)
		}
	}
	for _, candidate := range runtime.ExpandTargets(profile, targets, "") {
		if candidate.Enabled && candidate.CredentialMode == "client_passthrough" {
			return false
		}
	}
	return true
}
