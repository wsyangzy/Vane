package app

import (
	"fmt"
	"net"

	"github.com/wsyangzy/Vane/internal/platform/netutil"
)

type PortConflictStatus struct {
	HasConflict  bool   `json:"has_conflict"`  // 是否存在冲突
	ConflictPort int    `json:"conflict_port"` // 冲突的端口号
	ErrorMessage string `json:"error_message"` // 错误信息
	ProxyAddress string `json:"proxy_address"` // 当前代理地址（成功时）
	ProxyRunning bool   `json:"proxy_running"` // 代理是否正在运行
}

func (a *App) GetPortConflictStatus() PortConflictStatus {
	a.mu.RLock()
	defer a.mu.RUnlock()

	status := PortConflictStatus{
		HasConflict:  a.portConflict,
		ConflictPort: a.portConflictPort,
		ErrorMessage: a.portConflictErr,
		ProxyRunning: a.isRunning && a.proxyServer != nil,
	}

	if status.ProxyRunning {
		status.ProxyAddress = a.GetProxyAddress()
	}

	return status
}

type PortConflictResult struct {
	Success      bool   `json:"success"`
	ProxyAddress string `json:"proxy_address"` // 成功时的代理地址
	ErrorMessage string `json:"error_message"` // 失败时的错误信息
}

func (a *App) ResolvePortConflict(action string) PortConflictResult {
	a.mu.Lock()

	if !a.portConflict {
		addr := a.GetProxyAddress()
		a.mu.Unlock()
		return PortConflictResult{
			Success:      true,
			ProxyAddress: addr,
		}
	}

	preferredPort := a.config.Server.Port
	if a.portManager != nil {
		preferredPort = a.portManager.GetPreferredPort()
	}

	var listener net.Listener
	var actualPort int
	var err error

	switch action {
	case "retry":
		// 重试首选端口
		addr := fmt.Sprintf("%s:%d", a.config.Server.Host, preferredPort)
		listener, err = net.Listen("tcp", addr)
		if err != nil {
			a.mu.Unlock()
			return PortConflictResult{
				Success:      false,
				ErrorMessage: fmt.Sprintf("端口 %d 仍被占用: %v", preferredPort, err),
			}
		}
		actualPort = preferredPort

	case "switch":
		// 切换到其他可用端口（使用配置的 host 地址绑定）
		host := a.config.Server.Host
		if host == "" || host == "localhost" {
			host = "127.0.0.1"
		}
		listener, actualPort, err = netutil.FindAndBindWithHost(host, preferredPort, 10)
		if err != nil {
			a.mu.Unlock()
			return PortConflictResult{
				Success:      false,
				ErrorMessage: fmt.Sprintf("无法找到可用端口: %v", err),
			}
		}

	default:
		a.mu.Unlock()
		return PortConflictResult{
			Success:      false,
			ErrorMessage: "无效的操作: " + action,
		}
	}

	// 成功绑定端口，启动代理服务器
	a.portConflict = false
	a.portConflictPort = 0
	a.portConflictErr = ""

	if a.portManager != nil {
		a.portManager.SetActualPort(actualPort)
	}
	a.config.Server.Port = actualPort

	// 创建并启动代理服务器
	a.startProxyServerWithListener(listener, actualPort)

	// 代理地址统一使用 127.0.0.1（避免 localhost 在某些环境下解析问题）
	displayHost := a.config.Server.Host
	if displayHost == "" || displayHost == "localhost" {
		displayHost = "127.0.0.1"
	}
	proxyAddr := fmt.Sprintf("http://%s:%d", displayHost, actualPort)
	a.logger.Info("✅ 代理服务器启动成功", "address", proxyAddr)

	// 释放锁后发送系统状态更新事件，通知前端刷新端口信息
	a.mu.Unlock()
	a.emitSystemStatus()

	return PortConflictResult{
		Success:      true,
		ProxyAddress: proxyAddr,
	}
}
