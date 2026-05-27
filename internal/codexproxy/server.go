// server.go - Codex 代理 Server 工厂
// 创建完整的 HTTP handler（含中间件链）。

package codexproxy

import (
	"fmt"
	"log/slog"
	"net/http"
	"runtime/debug"
	"time"

	"claude-request-forwarder/internal/tracking"
)

// ServerConfig 服务器配置
// v7.2+ Codex 合并到主代理端口，仅保留 Timeout 用于转发超时控制。
type ServerConfig struct {
	Timeout time.Duration
}

// NewHTTPHandler 创建完整的 HTTP handler（含中间件）
// 中间件链: recovery → logging → handler
func NewHTTPHandler(manager *EndpointManager, cfg ServerConfig, logger *slog.Logger, usageTracker *tracking.UsageTracker) http.Handler {
	if logger == nil {
		logger = slog.Default()
	}
	// 所有 Codex 代理日志附加 provider=codex，用于文件分流
	logger = logger.With("provider", "codex")

	timeout := cfg.Timeout
	if timeout <= 0 {
		timeout = 300 * time.Second
	}

	handler := NewHandler(manager, timeout, logger, usageTracker)

	// 包装 logging 中间件
	logged := loggingMiddleware(handler, logger)

	// 包装 recovery 中间件
	return recoveryMiddleware(logged, logger)
}

// loggingMiddleware 请求日志中间件
func loggingMiddleware(next http.Handler, logger *slog.Logger) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		logger.Info("Codex 代理收到请求",
			"method", r.Method,
			"path", r.URL.Path,
			"remote", r.RemoteAddr,
		)
		next.ServeHTTP(w, r)
		logger.Info("Codex 代理请求完成",
			"method", r.Method,
			"path", r.URL.Path,
			"duration_ms", time.Since(start).Milliseconds(),
		)
	})
}

// recoveryMiddleware panic 恢复中间件
func recoveryMiddleware(next http.Handler, logger *slog.Logger) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if rec := recover(); rec != nil {
				logger.Error("Codex 代理 panic 恢复",
					"panic", fmt.Sprintf("%v", rec),
					"stack", string(debug.Stack()),
				)
				http.Error(w, `{"error":{"message":"internal server error","type":"proxy_error"}}`, http.StatusInternalServerError)
			}
		}()
		next.ServeHTTP(w, r)
	})
}
