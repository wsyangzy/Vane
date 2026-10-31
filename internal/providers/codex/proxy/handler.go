package codexproxy

import (
	"context"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/wsyangzy/Vane/internal/config"
	"github.com/wsyangzy/Vane/internal/platform/transport"
	"github.com/wsyangzy/Vane/internal/privacy"
	"github.com/wsyangzy/Vane/internal/providers/codex/proxy/subscription"
	"github.com/wsyangzy/Vane/internal/routing"
	"github.com/wsyangzy/Vane/internal/tracking"
)

// Handler 处理 Codex 代理请求
type Handler struct {
	profileResponse func(context.Context, routing.DispatchCandidate, int, http.Header, []byte)
	subscriptions   *subscription.Controller
	configMu        sync.RWMutex
	manager         *EndpointManager
	forwarder       *Forwarder
	streamFwd       *Forwarder
	tokenParser     *TokenParser
	classifier      *ErrorClassifier
	logger          *slog.Logger
	usageTracker    *tracking.UsageTracker
	privacyService  *privacy.Service
	states          *routing.StateTracker

	// retryCfg 同端点重试配置（来自设置页 retry.*）
	retryCfg config.RetryConfig
	// failoverCfg 故障转移与挂起配置（来自设置页 failover.*）
	failoverCfg config.FailoverConfig
	// suspensionMgr 请求挂起队列（loop_retry 全部耗尽后等待运维干预）
	// 为 nil 时不启用挂起，直接返回失败
	suspensionMgr *SuspensionManager
}

func (h *Handler) UpdateConfig(retryCfg config.RetryConfig, failoverCfg config.FailoverConfig) {
	h.configMu.Lock()
	h.retryCfg = retryCfg
	h.failoverCfg = failoverCfg
	h.configMu.Unlock()
}

func (h *Handler) retryConfig() config.RetryConfig {
	h.configMu.RLock()
	defer h.configMu.RUnlock()
	return h.retryCfg
}

func (h *Handler) failoverConfig() config.FailoverConfig {
	h.configMu.RLock()
	defer h.configMu.RUnlock()
	return h.failoverCfg
}

func normalizeCodexLogger(logger *slog.Logger) *slog.Logger {
	if logger == nil {
		logger = slog.Default()
	}
	return logger.With("provider", "codex")
}

// NewHandler 创建请求处理器
// 参数：
//   - manager:       端点管理器
//   - timeout:       单次请求总超时
//   - retryCfg:      同端点重试配置（max_attempts/base_delay/max_delay/multiplier）
//   - failoverCfg:   故障转移配置（loop_retry/max_loops 等）
//   - suspensionMgr: 请求挂起队列（nil 时不启用挂起）
func NewHandler(
	manager *EndpointManager,
	timeout time.Duration,
	retryCfg config.RetryConfig,
	failoverCfg config.FailoverConfig,
	suspensionMgr *SuspensionManager,
	logger *slog.Logger,
	usageTracker *tracking.UsageTracker,
	privacyService ...*privacy.Service,
) *Handler {
	return newHandler(manager, timeout, retryCfg, failoverCfg, suspensionMgr, logger, usageTracker, nil, privacyService...)
}

func NewHandlerWithTransport(
	manager *EndpointManager,
	timeout time.Duration,
	retryCfg config.RetryConfig,
	failoverCfg config.FailoverConfig,
	suspensionMgr *SuspensionManager,
	logger *slog.Logger,
	usageTracker *tracking.UsageTracker,
	transportProvider *transport.Provider,
	privacyService ...*privacy.Service,
) *Handler {
	return newHandler(manager, timeout, retryCfg, failoverCfg, suspensionMgr, logger, usageTracker, transportProvider, privacyService...)
}

func newHandler(
	manager *EndpointManager,
	timeout time.Duration,
	retryCfg config.RetryConfig,
	failoverCfg config.FailoverConfig,
	suspensionMgr *SuspensionManager,
	logger *slog.Logger,
	usageTracker *tracking.UsageTracker,
	transportProvider *transport.Provider,
	privacyService ...*privacy.Service,
) *Handler {
	// 所有 Codex 代理日志附加 provider=codex，用于文件分流
	logger = normalizeCodexLogger(logger)
	var redactor *privacy.Service
	if len(privacyService) > 0 {
		redactor = privacyService[0]
	}
	return &Handler{
		manager:        manager,
		forwarder:      NewForwarder(timeout, logger, transportProvider),
		streamFwd:      NewStreamingForwarder(logger, transportProvider),
		tokenParser:    NewTokenParser(),
		classifier:     NewErrorClassifier(),
		logger:         logger,
		usageTracker:   usageTracker,
		privacyService: redactor,
		states:         routing.NewStateTracker(),
		retryCfg:       retryCfg,
		failoverCfg:    failoverCfg,
		suspensionMgr:  suspensionMgr,
	}
}

// UpdateLogger updates the logger used by this handler and its forwarders.
func (h *Handler) UpdateLogger(logger *slog.Logger) {
	logger = normalizeCodexLogger(logger)
	h.logger = logger
	if h.forwarder != nil {
		h.forwarder.UpdateLogger(logger)
	}
	if h.streamFwd != nil {
		h.streamFwd.UpdateLogger(logger)
	}
}
