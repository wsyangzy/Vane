package app

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/wsyangzy/Vane/internal/platform/clientconfig"
	claudesub "github.com/wsyangzy/Vane/internal/providers/claude/proxy/subscription"
	claudeservice "github.com/wsyangzy/Vane/internal/providers/claude/service"
	codexsub "github.com/wsyangzy/Vane/internal/providers/codex/proxy/subscription"
	codexservice "github.com/wsyangzy/Vane/internal/providers/codex/service"
	"github.com/wsyangzy/Vane/internal/subscriptions/accounts"
)

type SubscriptionConnectionCommand struct {
	PoolID          int64  `json:"pool_id"`
	Provider        string `json:"provider"`
	Action          string `json:"action"`
	GroupID         int64  `json:"group_id"`
	LocalKey        string `json:"local_key"`
	Revision        string `json:"revision"`
	BindingVersion  int64  `json:"binding_version"`
	ContentRevision string `json:"content_revision"`
}

type SubscriptionConnectionResult struct {
	ConfigPath      string `json:"config_path"`
	BaseURL         string `json:"base_url"`
	Running         bool   `json:"running"`
	BackupAvailable bool   `json:"backup_available"`
	Revision        string `json:"revision"`
	BindingVersion  int64  `json:"binding_version"`
	EndpointReady   bool   `json:"endpoint_ready"`
	OfficialURL     string `json:"official_url"`
	Managed         bool   `json:"managed"`
	OfficialEnabled bool   `json:"official_enabled"`
	Snippet         string `json:"snippet"`
	Command         string `json:"command"`
	ContentRevision string `json:"content_revision"`
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
	switch input.Action {
	case "status", "preview", "apply", "restore", "prepare_endpoint":
	default:
		return result, errors.New("未知的订阅接入操作")
	}
	path, err := subscriptionConfigPath(input.Provider)
	if err != nil {
		return result, err
	}
	result.ConfigPath = path
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
		result.BaseURL += "/backend-api/codex"
		result.OfficialURL = codexsub.OfficialBaseURL
	} else {
		result.OfficialURL = claudesub.OfficialOrigin
	}
	if input.PoolID > 0 {
		return a.managePoolConnection(input, result, current)
	}
	if input.GroupID <= 0 {
		if input.Action == "status" {
			return result, nil
		}
		return result, errors.New("请选择已保存的订阅分组")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	a.mu.RLock()
	claudeGroups, claudeEndpoints := a.claudeCredentialGroupStore, a.endpointStore
	codexGroups, codexEndpoints := a.codexCredentialGroupStore, a.codexEndpointStore
	a.mu.RUnlock()
	var keyHash string
	var channel string
	var channelID int64
	if input.Provider == "claude" {
		if claudeGroups == nil || claudeEndpoints == nil {
			return result, errors.New("Claude 存储尚未就绪")
		}
		g, e := claudeGroups.GetByID(ctx, input.GroupID)
		if e != nil {
			return result, errors.New("读取订阅分组失败")
		}
		if g == nil || g.AuthMode != claudesub.ClientPassthrough || g.Subscription == nil {
			return result, errors.New("请选择 Claude 官方订阅分组")
		}
		b := g.Subscription
		result.BindingVersion, result.Managed, result.OfficialEnabled = b.Version, b.AccountID != "", b.Enabled
		keyHash, channel = b.KeyHash, g.Channel
		endpoints, e := claudeEndpoints.ListByCredentialGroupID(ctx, g.ID)
		if e != nil {
			return result, errors.New("读取官方端点失败")
		}
		for _, ep := range endpoints {
			if claudesub.ValidateEndpoint(ep.URL, ep.Headers) == nil && ep.Token == "" && ep.ApiKey == "" && ep.FailoverEnabled {
				result.EndpointReady = true
			}
		}
	} else {
		if codexGroups == nil || codexEndpoints == nil {
			return result, errors.New("Codex 存储尚未就绪")
		}
		g, e := codexGroups.GetByID(ctx, input.GroupID)
		if e != nil {
			return result, errors.New("读取订阅分组失败")
		}
		if g == nil || g.AuthMode != codexsub.ClientPassthrough || g.Subscription == nil {
			return result, errors.New("请选择 Codex 官方订阅分组")
		}
		b := g.Subscription
		result.BindingVersion, result.Managed, result.OfficialEnabled = b.Version, b.AccountID != "", b.Enabled
		keyHash, channelID = b.KeyHash, g.ChannelID
		endpoints, e := codexEndpoints.ListByChannelID(ctx, g.ChannelID)
		if e != nil {
			return result, errors.New("读取官方端点失败")
		}
		for _, ep := range endpoints {
			if ep.CredentialGroupID == g.ID && codexsub.ValidateEndpoint(ep.BaseURL) == nil && ep.ApiKey == "" && ep.FailoverEnabled {
				result.EndpointReady = true
			}
		}
	}
	if input.Action == "status" {
		return result, nil
	}
	if input.Action == "prepare_endpoint" {
		if !result.EndpointReady {
			// Reuse existing creation paths for DB validation and runtime reload.
			if input.Provider == "claude" {
				err = a.CreateEndpointRecord(CreateEndpointInput{Channel: channel, CredentialGroupID: input.GroupID, Name: "官方订阅", URL: result.OfficialURL, Priority: -1, FailoverEnabled: true, SkipHealthCheck: true, SupportsCountTokens: true})
			} else {
				err = a.CreateCodexEndpoint(CreateCodexEndpointInput{ChannelID: channelID, CredentialGroupID: input.GroupID, Name: "官方订阅", BaseURL: result.OfficialURL, SkipHealthCheck: true})
			}
			if err != nil {
				return result, errors.New("无法新建官方端点；若已有相同地址，请在该分组端点卡片开启故障转移参与后刷新")
			}
		}
		result.EndpointReady = true
		return result, nil
	}
	if !result.EndpointReady {
		return result, errors.New("该分组缺少可参与选路的官方端点，请先准备官方端点")
	}
	key := strings.TrimSpace(input.LocalKey)
	if len(key) < 32 || len(key) > 256 || strings.ContainsAny(key, "\r\n\t ") {
		return result, errors.New("请输入该分组已保存的有效本地密钥")
	}
	digest := sha256.Sum256([]byte(key))
	stored, e := hex.DecodeString(keyHash)
	if e != nil || len(stored) != len(digest) || subtle.ConstantTimeCompare(stored, digest[:]) != 1 {
		return result, errors.New("本地密钥与所选分组不匹配；遗失时请先在分组中轮换并保存")
	}
	var updated, snippet []byte
	if input.Provider == "claude" {
		updated, snippet, result.Command, err = claudeservice.RenderSubscriptionClient(current.Data, result.BaseURL, key, result.Managed)
	} else {
		updated, snippet, result.Command, err = codexservice.RenderSubscriptionClient(current.Data, result.BaseURL, key, result.Managed)
	}
	if err != nil {
		return result, err
	}
	defer clear(updated)
	result.Snippet = string(snippet)
	result.ContentRevision = (clientconfig.Snapshot{Data: updated, Exists: true}).Revision()
	if input.Action == "apply" {
		if input.ContentRevision == "" || input.ContentRevision != result.ContentRevision {
			return SubscriptionConnectionResult{}, errors.New("接入配置已变化，请重新预览")
		}
		if input.BindingVersion != result.BindingVersion {
			return SubscriptionConnectionResult{}, errors.New("订阅绑定已变化，请重新预览")
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
