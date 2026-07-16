// server.go - Codex 代理 Server 工厂
// 创建完整的 HTTP handler（含中间件链）。

package codexproxy

import (
	"fmt"
	"log/slog"
	"net/http"
	"runtime/debug"
	"sync"
	"time"

	"ai-relay-forwarder/config"
	"ai-relay-forwarder/internal/privacy"
	"ai-relay-forwarder/internal/tracking"
	"ai-relay-forwarder/internal/transport"
)

// ServerConfig 服务器配置
// v7.2+ Codex 合并到主代理端口，仅保留 Timeout 用于转发超时控制。
// v7.3+ 增加 Retry/Failover 接入设置页配置（同端点重试 + 渠道内循环）。
// v7.3+ 增加 SuspensionMgr 接入请求挂起队列（loop_retry 全部耗尽后等待运维干预）。
type ServerConfig struct {
	Timeout       time.Duration
	Config        *config.Config
	Retry         config.RetryConfig    // 同端点重试配置（来自设置页 retry.*）
	Failover      config.FailoverConfig // 故障转移配置（来自设置页 failover.*）
	SuspensionMgr *SuspensionManager    // 请求挂起队列（nil 时不启用挂起）
}

// HTTPHandler 是可热更新 logger 的 Codex HTTP 处理器。
type HTTPHandler struct {
	handler *Handler
	logger  *slog.Logger
	mu      sync.RWMutex
}

// NewHTTPHandler 创建完整的 HTTP handler（含中间件）
// 中间件链: recovery → logging → handler
func NewHTTPHandler(manager *EndpointManager, cfg ServerConfig, logger *slog.Logger, usageTracker *tracking.UsageTracker, privacyService ...*privacy.Service) *HTTPHandler {
	timeout := cfg.Timeout
	if timeout <= 0 {
		timeout = 300 * time.Second
	}

	transportProvider := transport.NewProvider(cfg.Config)
	handler := NewHandlerWithTransport(manager, timeout, cfg.Retry, cfg.Failover, cfg.SuspensionMgr, logger, usageTracker, transportProvider, privacyService...)
	httpHandler := &HTTPHandler{handler: handler}
	httpHandler.UpdateLogger(logger)
	return httpHandler
}

// UpdateLogger 更新 Codex HTTP handler 及其内部转发器使用的 logger。
func (h *HTTPHandler) UpdateLogger(logger *slog.Logger) {
	codexLogger := normalizeCodexLogger(logger)

	h.mu.Lock()
	h.logger = codexLogger
	h.mu.Unlock()

	if h.handler != nil {
		h.handler.UpdateLogger(logger)
	}
}

func (h *HTTPHandler) UpdateConfig(retryCfg config.RetryConfig, failoverCfg config.FailoverConfig) {
	if h.handler != nil {
		h.handler.UpdateConfig(retryCfg, failoverCfg)
	}
}

func (h *HTTPHandler) currentLogger() *slog.Logger {
	h.mu.RLock()
	logger := h.logger
	h.mu.RUnlock()
	if logger == nil {
		return normalizeCodexLogger(nil)
	}
	return logger
}

// ServeHTTP 执行 Codex 代理请求日志、panic 恢复和实际转发。
func (h *HTTPHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	logger := h.currentLogger()
	defer func() {
		if rec := recover(); rec != nil {
			logger.Error("Codex 代理 panic 恢复",
				"panic", fmt.Sprintf("%v", rec),
				"stack", string(debug.Stack()),
			)
			http.Error(w, `{"error":{"message":"internal server error","type":"proxy_error"}}`, http.StatusInternalServerError)
		}
	}()

	start := time.Now()
	logger.Info("Codex 代理收到请求",
		"method", r.Method,
		"path", r.URL.Path,
		"remote", r.RemoteAddr,
	)
	if h.handler == nil {
		http.Error(w, `{"error":{"message":"codex handler unavailable","type":"proxy_error"}}`, http.StatusServiceUnavailable)
		return
	}
	h.handler.ServeHTTP(w, r)
	logger.Info("Codex 代理请求完成",
		"method", r.Method,
		"path", r.URL.Path,
		"duration_ms", time.Since(start).Milliseconds(),
	)
}
