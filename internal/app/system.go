package app

import (
	"fmt"
	"time"

	"github.com/wsyangzy/Vane/internal/buildinfo"
)

type SystemStatus struct {
	Version       string `json:"version"`
	Uptime        string `json:"uptime"`
	UptimeSeconds int64  `json:"uptime_seconds"`
	StartTime     string `json:"start_time"` // ISO8601 格式的启动时间
	ProxyPort     int    `json:"proxy_port"`
	ProxyHost     string `json:"proxy_host"`
	ProxyRunning  bool   `json:"proxy_running"`
	ActiveGroup   string `json:"active_group"`
	// ConfigPath 保留以兼容既有前端绑定；运行时不再使用文件配置，始终为空。
	ConfigPath  string `json:"config_path"`
	AuthEnabled bool   `json:"auth_enabled"`

	// Codex 代理状态（v7.2+ 合并到主端口，运行时默认可用）
	CodexProxyEnabled bool `json:"codex_proxy_enabled"`
}

func (a *App) GetSystemStatus() SystemStatus {
	a.mu.RLock()
	defer a.mu.RUnlock()

	uptime := time.Since(a.startTime)

	status := SystemStatus{
		Version:       buildinfo.Version,
		Uptime:        formatDuration(uptime),
		UptimeSeconds: int64(uptime.Seconds()),
		StartTime:     a.startTime.Format(time.RFC3339),
		ProxyRunning:  a.isRunning && a.proxyServer != nil,
	}

	if a.config != nil {
		status.ProxyPort = a.config.Server.Port
		status.ProxyHost = a.config.Server.Host
		status.AuthEnabled = a.config.Auth.Enabled

		// 真正检测端口是否在监听
		if status.ProxyRunning {
			status.ProxyRunning = a.checkPortListening(status.ProxyHost, status.ProxyPort)
		}

		// Codex 代理与 Claude 代理共用主端口，保留字段用于前端兼容。
		status.CodexProxyEnabled = true
	}

	if a.endpointManager != nil {
		gm := a.endpointManager.GetGroupManager()
		if gm != nil {
			activeGroups := gm.GetActiveGroups()
			if len(activeGroups) > 0 {
				status.ActiveGroup = activeGroups[0].Name
			}
		}
	}

	return status
}

type ConfigInfo struct {
	ServerHost      string `json:"server_host"`
	ServerPort      int    `json:"server_port"`
	AuthEnabled     bool   `json:"auth_enabled"`
	ProxyEnabled    bool   `json:"proxy_enabled"`
	TrackingEnabled bool   `json:"tracking_enabled"`
	FailoverEnabled bool   `json:"failover_enabled"`
	// FailoverDefaultCooldownSeconds 全局默认冷却时间（秒），用于前端在端点未设置专属冷却时展示"实际生效值"。
	// 设置页面已移除此配置项（loop_retry 模式下冷却时间与循环重试逻辑冲突），使用固定默认值 600 秒。
	FailoverDefaultCooldownSeconds int `json:"failover_default_cooldown_seconds"`
	EndpointCount                  int `json:"endpoint_count"`
}

func (a *App) GetConfig() ConfigInfo {
	a.mu.RLock()
	defer a.mu.RUnlock()

	if a.config == nil {
		return ConfigInfo{}
	}

	return ConfigInfo{
		ServerHost:                     a.config.Server.Host,
		ServerPort:                     a.config.Server.Port,
		AuthEnabled:                    a.config.Auth.Enabled,
		ProxyEnabled:                   a.config.Proxy.Enabled,
		TrackingEnabled:                a.config.UsageTracking.Enabled,
		FailoverEnabled:                a.config.Failover.Enabled,
		FailoverDefaultCooldownSeconds: 600, // 固定默认值，设置页面已移除此配置项
		EndpointCount:                  len(a.config.Endpoints),
	}
}

func (a *App) GetProxyURL() string {
	a.mu.RLock()
	defer a.mu.RUnlock()

	if a.config == nil {
		return ""
	}

	// 统一使用 127.0.0.1，避免 localhost 在某些环境下的解析问题
	host := a.config.Server.Host
	if host == "" || host == "localhost" {
		host = "127.0.0.1"
	}
	return fmt.Sprintf("http://%s:%d", host, a.config.Server.Port)
}

func (a *App) IsProxyRunning() bool {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.isRunning
}
