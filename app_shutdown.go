package main

import (
	"context"
	"time"
)

// shutdown 在 Wails 应用关闭时调用
func (a *App) shutdown() {
	if a.subscriptionAccounts != nil {
		a.subscriptionAccounts.Close()
	}
	a.mu.Lock()
	logger := a.logger
	proxyServer := a.proxyServer
	usageTracker := a.usageTracker
	storeDB := a.storeDB
	endpointManager := a.endpointManager
	eventBus := a.eventBus
	logEmitter := a.logEmitter
	a.isRunning = false
	a.mu.Unlock()

	// 0. 关闭 goroutine 退出信号
	close(a.stopChan)

	if logger != nil {
		logger.Info("🛑 正在关闭 AI Relay Forwarder...")
	}

	// 1. 停止接收新请求
	if proxyServer != nil {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		if err := proxyServer.Shutdown(shutdownCtx); err != nil {
			if closeErr := proxyServer.Close(); closeErr != nil && logger != nil {
				logger.Error("代理服务器强制关闭失败", "error", closeErr)
			}
			if logger != nil {
				logger.Error("代理服务器关闭失败", "error", err)
			}
		}
	}

	// 1.1 停止 Codex 健康检查
	a.stopCodexProxy()

	// 2. 关闭使用追踪 (flush 数据库)
	if usageTracker != nil {
		done := make(chan struct{})
		go func() {
			defer close(done)
			if err := usageTracker.Close(); err != nil {
				if logger != nil {
					logger.Error("使用追踪器关闭失败", "error", err)
				}
			}
		}()

		select {
		case <-done:
		case <-time.After(3 * time.Second):
			if logger != nil {
				logger.Warn("使用追踪器关闭超时，强制继续退出")
			}
		}
	}

	// 2.1 关闭管理/配置 DB
	if storeDB != nil {
		if err := storeDB.Close(); err != nil {
			if logger != nil {
				logger.Error("管理数据库关闭失败", "error", err)
			}
		}
	}

	// 3. 关闭端点管理器
	if endpointManager != nil {
		endpointManager.Stop()
	}

	// 4. 关闭事件总线
	if eventBus != nil {
		if err := eventBus.Stop(); err != nil {
			if logger != nil {
				logger.Error("事件总线关闭失败", "error", err)
			}
		}
	}

	// 5. 停止日志事件发射器
	if logEmitter != nil {
		logEmitter.Stop()
	}

	a.mu.Lock()
	a.proxyServer = nil
	a.usageTracker = nil
	a.storeDB = nil
	a.mu.Unlock()

	if logger != nil {
		logger.Info("✅ AI Relay Forwarder 已关闭")
	}
}
