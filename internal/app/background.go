package app

import (
	"fmt"
	"runtime/debug"
	"time"
)

func (a *App) setupEventBridges() {
	// 注意：当前 EventBus 实现不支持订阅回调
	// 我们使用定时轮询来更新前端状态
	a.runResilientGoroutine("event-bridge", func() {
		ticker := time.NewTicker(2 * time.Second)
		defer ticker.Stop()

		for {
			select {
			case <-a.stopChan:
				return
			case <-ticker.C:
				// 使用读锁检查运行状态，避免竞态条件
				a.mu.RLock()
				running := a.isRunning
				window := a.window
				a.mu.RUnlock()

				if running && window != nil {
					a.emitSystemStatus()
					a.emitUsageUpdate()
				}
			}
		}
	})

	a.logger.Info("📡 事件桥接已启用")
}

func (a *App) startHistoryCollector() {
	if a.monitoringMiddleware == nil {
		a.logger.Warn("⚠️  监控中间件未初始化，跳过历史数据收集器启动")
		return
	}

	// 立即收集一次初始数据点
	// 注意：必须直接在原始 *Metrics 上调用 AddHistoryDataPoints()
	// 不能调用 GetMetrics() 获取副本，因为那样修改的是副本而不是原始数据
	metrics := a.monitoringMiddleware.GetMetrics()
	if metrics != nil {
		metrics.AddHistoryDataPoints()
		a.logger.Info("📊 初始历史数据点已收集")
	}

	// 🔧 [P0.3 修复] 改用 runResilientGoroutine，panic 后自动重启
	a.runResilientGoroutine("history-collector", func() {
		ticker := time.NewTicker(30 * time.Second) // 每30秒收集一次
		defer ticker.Stop()

		a.logger.Info("📊 历史数据收集器已启动 (30秒间隔)")

		for {
			select {
			case <-a.stopChan:
				a.logger.Info("📊 历史数据收集器已停止")
				return
			case <-ticker.C:
				// 收集历史数据点
				// 直接在原始 *Metrics 上调用，而不是获取副本
				metrics := a.monitoringMiddleware.GetMetrics()
				if metrics != nil {
					metrics.AddHistoryDataPoints()
				}
			}
		}
	})
}

func (a *App) runResilientGoroutine(name string, work func()) {
	go func() {
		const maxRestarts = 5
		const restartWindow = 60 * time.Second // 60s 内累计超过 maxRestarts 则放弃

		var restartCount int
		var restartWindowStart time.Time

		for {
			// 检查 stopChan，正常退出不重启
			select {
			case <-a.stopChan:
				if a.logger != nil {
					a.logger.Info(fmt.Sprintf("🛑 %s goroutine 收到停止信号，退出", name))
				}
				return
			default:
			}

			func() {
				defer func() {
					if r := recover(); r != nil {
						if a.logger != nil {
							a.logger.Error(fmt.Sprintf("💥 %s goroutine panic", name),
								"recover", r,
								"stack", string(debug.Stack()))
						}
					}
				}()

				work()
			}()

			// work() 正常 return 不重启
			// 只有 panic 才会到这里继续循环

			// 滑动窗口计数
			now := time.Now()
			if now.Sub(restartWindowStart) > restartWindow {
				restartWindowStart = now
				restartCount = 0
			}
			restartCount++

			if restartCount > maxRestarts {
				if a.logger != nil {
					a.logger.Error(fmt.Sprintf("❌ %s goroutine 在 %v 内累计 panic %d 次，停止重启",
						name, restartWindow, restartCount))
				}
				return
			}

			// 指数退避（最长 16s）
			backoff := time.Duration(1<<uint(restartCount-1)) * time.Second
			if backoff > 16*time.Second {
				backoff = 16 * time.Second
			}

			if a.logger != nil {
				a.logger.Warn(fmt.Sprintf("🔄 %s goroutine 将在 %v 后重启 (第 %d 次)",
					name, backoff, restartCount))
			}

			select {
			case <-a.stopChan:
				return
			case <-time.After(backoff):
			}
		}
	}()
}

func (a *App) emitError(title, message string) {
	a.safeEmitEvent("error", map[string]string{
		"title":   title,
		"message": message,
	})
}
