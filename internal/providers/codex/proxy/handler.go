// handler.go - Codex 代理请求处理器
// 实现 http.Handler，协调端点选择、转发、故障转移和 token 解析。

package codexproxy

import (
	"bufio"
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	"ai-relay-forwarder/config"
	"ai-relay-forwarder/internal/privacy"
	"ai-relay-forwarder/internal/tracking"
	"ai-relay-forwarder/internal/transport"
)

// chainNode 端点链路节点（结构化存储）
// 序列化为 JSON 数组写入 tracking.UpdateOptions.EndpointChain，与 Claude 端
// internal/proxy.ChainNode 保持兼容（{hop, channel, credential_group, endpoint}），供前端统一解析。
type chainNode struct {
	Hop             int    `json:"hop"`
	Channel         string `json:"channel"`
	CredentialGroup string `json:"credential_group,omitempty"`
	Endpoint        string `json:"endpoint"`
	StatusCode      int    `json:"status_code,omitempty"`
	Error           string `json:"error,omitempty"`
}

const maxPreOutputStreamBufferBytes = 256 * 1024
const failedCredentialGroupCacheIdle = 30 * time.Minute
const maxTurnFingerprintTextBytes = 8192

// Handler 处理 Codex 代理请求
type Handler struct {
	configMu       sync.RWMutex
	manager        *EndpointManager
	forwarder      *Forwarder
	streamFwd      *Forwarder
	tokenParser    *TokenParser
	classifier     *ErrorClassifier
	logger         *slog.Logger
	usageTracker   *tracking.UsageTracker
	privacyService *privacy.Service

	// retryCfg 同端点重试配置（来自设置页 retry.*）
	retryCfg config.RetryConfig
	// failoverCfg 故障转移与挂起配置（来自设置页 failover.*）
	failoverCfg config.FailoverConfig
	// suspensionMgr 请求挂起队列（loop_retry 全部耗尽后等待运维干预）
	// 为 nil 时不启用挂起，直接返回失败
	suspensionMgr *SuspensionManager

	turnFailureCacheMu       sync.Mutex
	turnFailureCacheKey      string
	turnFailureCacheTouched  time.Time
	turnFailedCredentialKeys map[credentialGroupRef]struct{}
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

// generateRequestID 生成请求 ID（统一 req- 前缀）
func generateRequestID() string {
	b := make([]byte, 4)
	rand.Read(b)
	return "req-" + hex.EncodeToString(b)
}

// strPtr 返回字符串指针（用于 UpdateOptions）
func strPtr(s string) *string { return &s }

func appendUniqueInt64(ids []int64, values ...int64) []int64 {
	if len(values) == 0 {
		return ids
	}
	seen := make(map[int64]struct{}, len(ids)+len(values))
	for _, id := range ids {
		seen[id] = struct{}{}
	}
	for _, id := range values {
		if id <= 0 {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		ids = append(ids, id)
	}
	return ids
}

func isCredentialGroupFailure(statusCode int, summary string) bool {
	if statusCode == http.StatusPaymentRequired {
		return true
	}
	if statusCode != http.StatusForbidden && statusCode != http.StatusTooManyRequests {
		return false
	}
	text := strings.ToLower(summary)
	return strings.Contains(text, "insufficient_quota") ||
		strings.Contains(text, "insufficient_user_quota") ||
		strings.Contains(text, "insufficient account balance") ||
		strings.Contains(text, "insufficient balance") ||
		strings.Contains(text, "insufficient_balance") ||
		strings.Contains(text, "quota exhausted") ||
		strings.Contains(text, "exceeded your current quota") ||
		strings.Contains(text, "billing hard limit") ||
		strings.Contains(text, "billing exhausted") ||
		strings.Contains(text, "billing_exhausted") ||
		strings.Contains(text, "credits exhausted") ||
		strings.Contains(text, "balance exhausted")
}

func credentialGroupRefFromEndpoint(endpoint *CodexEndpoint) credentialGroupRef {
	if endpoint == nil {
		return credentialGroupRef{}
	}
	return credentialGroupRef{ID: endpoint.CredentialGroupID, Name: endpoint.CredentialGroupName}
}

func (h *Handler) failedCredentialGroupsForTurn(turnKey string) []credentialGroupRef {
	if h == nil {
		return nil
	}
	now := time.Now()

	h.turnFailureCacheMu.Lock()
	defer h.turnFailureCacheMu.Unlock()

	if !h.prepareTurnFailureCacheLocked(turnKey, now) || len(h.turnFailedCredentialKeys) == 0 {
		return nil
	}
	groups := make([]credentialGroupRef, 0, len(h.turnFailedCredentialKeys))
	for group := range h.turnFailedCredentialKeys {
		groups = append(groups, group)
	}
	return groups
}

func (h *Handler) rememberFailedCredentialGroupForTurn(turnKey string, group credentialGroupRef) {
	if h == nil || !group.valid() {
		return
	}
	now := time.Now()

	h.turnFailureCacheMu.Lock()
	defer h.turnFailureCacheMu.Unlock()

	if !h.prepareTurnFailureCacheLocked(turnKey, now) {
		return
	}
	h.turnFailedCredentialKeys[group] = struct{}{}
}

func (h *Handler) prepareTurnFailureCacheLocked(turnKey string, now time.Time) bool {
	if h.turnFailedCredentialKeys == nil {
		h.turnFailedCredentialKeys = make(map[credentialGroupRef]struct{})
	}
	if turnKey != "" {
		if h.turnFailureCacheKey != turnKey {
			h.turnFailureCacheKey = turnKey
			h.turnFailedCredentialKeys = make(map[credentialGroupRef]struct{})
		}
		h.turnFailureCacheTouched = now
		return true
	}
	if h.turnFailureCacheKey == "" {
		return false
	}
	if !h.turnFailureCacheTouched.IsZero() && now.Sub(h.turnFailureCacheTouched) > failedCredentialGroupCacheIdle {
		h.turnFailureCacheKey = ""
		h.turnFailedCredentialKeys = make(map[credentialGroupRef]struct{})
		return false
	}
	h.turnFailureCacheTouched = now
	return true
}

func requestTurnFailureCacheKey(r *http.Request, body []byte) string {
	if r != nil {
		for _, header := range []string{
			"X-Codex-Turn-ID",
			"X-Codex-Prompt-ID",
			"X-Codex-User-Message-ID",
			"X-OpenAI-Turn-ID",
			"OpenAI-Turn-ID",
		} {
			if value := strings.TrimSpace(r.Header.Get(header)); value != "" {
				return hashTurnCacheKey(strings.ToLower(header), value)
			}
		}
	}

	var payload any
	if len(body) == 0 || json.Unmarshal(body, &payload) != nil {
		return ""
	}
	obj, ok := payload.(map[string]any)
	if !ok {
		return ""
	}
	if value := explicitTurnIDFromPayload(obj); value != "" {
		return hashTurnCacheKey("explicit", value)
	}
	if value := lastUserInputText(obj); value != "" {
		return hashTurnCacheKey("user", value)
	}
	return ""
}

func explicitTurnIDFromPayload(obj map[string]any) string {
	for _, key := range []string{"turn_id", "turnId", "prompt_id", "promptId", "user_message_id", "userMessageId"} {
		if value := stringValue(obj[key]); value != "" {
			return key + ":" + value
		}
	}
	if metadata, ok := obj["metadata"].(map[string]any); ok {
		for _, key := range []string{"turn_id", "turnId", "prompt_id", "promptId", "user_message_id", "userMessageId"} {
			if value := stringValue(metadata[key]); value != "" {
				return "metadata." + key + ":" + value
			}
		}
	}
	return ""
}

func lastUserInputText(obj map[string]any) string {
	for _, key := range []string{"input", "messages"} {
		if text := lastUserTextFromValue(obj[key]); text != "" {
			return text
		}
	}
	return ""
}

func lastUserTextFromValue(value any) string {
	switch typed := value.(type) {
	case string:
		return strings.TrimSpace(typed)
	case []any:
		for i := len(typed) - 1; i >= 0; i-- {
			item, ok := typed[i].(map[string]any)
			if !ok {
				continue
			}
			role := strings.ToLower(stringValue(item["role"]))
			if role != "user" {
				continue
			}
			if text := collectTurnContentText(item["content"]); text != "" {
				return text
			}
		}
	}
	return ""
}

func collectTurnContentText(value any) string {
	var builder strings.Builder
	appendTurnContentText(&builder, value)
	return strings.TrimSpace(builder.String())
}

func appendTurnContentText(builder *strings.Builder, value any) {
	if builder.Len() >= maxTurnFingerprintTextBytes {
		return
	}
	switch typed := value.(type) {
	case string:
		appendTurnText(builder, typed)
	case []any:
		for _, item := range typed {
			appendTurnContentText(builder, item)
			if builder.Len() >= maxTurnFingerprintTextBytes {
				return
			}
		}
	case map[string]any:
		if text := stringValue(typed["text"]); text != "" {
			appendTurnText(builder, text)
			return
		}
		if text := stringValue(typed["input_text"]); text != "" {
			appendTurnText(builder, text)
			return
		}
		if content, ok := typed["content"]; ok {
			appendTurnContentText(builder, content)
		}
	}
}

func appendTurnText(builder *strings.Builder, text string) {
	text = strings.TrimSpace(text)
	if text == "" || builder.Len() >= maxTurnFingerprintTextBytes {
		return
	}
	if builder.Len() > 0 {
		builder.WriteByte('\n')
	}
	remaining := maxTurnFingerprintTextBytes - builder.Len()
	if len(text) > remaining {
		text = text[:remaining]
	}
	builder.WriteString(text)
}

func stringValue(value any) string {
	text, ok := value.(string)
	if !ok {
		return ""
	}
	return strings.TrimSpace(text)
}

func hashTurnCacheKey(prefix, value string) string {
	sum := sha256.Sum256([]byte(prefix + "\x00" + value))
	return prefix + ":" + hex.EncodeToString(sum[:12])
}

// ServeHTTP 实现 http.Handler
// 三层重试结构：
//  1. 外层 loop_retry: 端点列表耗尽后是否回到首位再来一轮（受 failover.loop_retry/max_loops 控制）
//  2. 中层 endpoint cycle: 按优先级遍历当前轮可用端点
//  3. 内层 same-endpoint retry: 单端点的瞬时错误指数退避重试（受 retry.* 控制）
func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	startTime := time.Now()
	requestID := generateRequestID()

	// 1. 读取并缓存请求体（重试时需要重放）
	body, err := io.ReadAll(r.Body)
	if err != nil {
		h.writeError(w, http.StatusBadRequest, "读取请求体失败")
		return
	}
	defer r.Body.Close()
	originalBody := append([]byte(nil), body...)
	turnFailureKey := requestTurnFailureCacheKey(r, body)
	var privacySummary privacy.RedactionSummary
	privacyChanged := false
	if h.privacyService != nil {
		protectedBody, summary, changed, redactErr := h.privacyService.RedactRequestContext(
			r.Context(), originalBody, r.Header.Get("Content-Type"), r.Header.Get("Content-Encoding"),
		)
		if redactErr != nil {
			h.logger.Error("隐私防护预检失败，已阻止上游发送", "request_id", requestID, "error", redactErr)
			h.writeError(w, http.StatusInternalServerError, "隐私防护处理失败")
			return
		}
		originalBody = protectedBody
		privacySummary = summary
		privacyChanged = changed
	}

	// 渠道设置了专属 model 时，代理层直接改写请求体，避免依赖运行中的 Codex CLI 重读 config.toml。
	channelName := ""
	channelModel := ""
	var channelModelMapping map[string]string
	if h.manager != nil {
		if ch := h.manager.GetActiveChannel(); ch != nil {
			channelName = ch.Name
			channelModel = strings.TrimSpace(ch.Model)
			channelModelMapping = ch.ModelMapping
		}
	}
	modelRewrite := rewriteModelWithMappingDetails(originalBody, channelModelMapping, channelModel, h.logger, requestID)
	body = modelRewrite.Body

	// 2. 检测是否流式请求
	isStreaming := bytes.Contains(body, []byte(`"stream":true`)) ||
		bytes.Contains(body, []byte(`"stream": true`))

	// 3. 从请求体提取模型名（用于在响应未携带 model 时填充）
	requestModel := h.tokenParser.ParseModelFromBody(body)

	// 3b. 从请求体提取 reasoning 配置（Codex 请求体 reasoning: {effort, mode, summary}）
	reasoningJSON := extractReasoning(body)

	// 3c. 从请求体提取 service_tier（fast mode: auto/fast/priority）
	serviceTier := extractServiceTier(body)

	// 🆕 [Codex 生命周期] 请求开始日志
	h.logger.Info("[Codex 请求开始]",
		"provider", "codex",
		"request_id", requestID,
		"method", r.Method,
		"path", r.URL.Path,
		"remote", r.RemoteAddr,
		"streaming", isStreaming,
		"channel", channelName,
		"model", requestModel,
		"model_rewrite_source", modelRewrite.Source,
		"reasoning", reasoningJSON,
		"service_tier", serviceTier,
	)

	// 5. 记录请求开始（追踪：Provider=codex + 渠道 + 请求体模型）
	h.recordStart(requestID, r, isStreaming, channelName, requestModel, modelRewrite, reasoningJSON, serviceTier)
	if privacyChanged {
		h.recordPrivacyRedaction(
			requestID,
			privacy.SummaryToJSON(privacySummary),
			body,
			privacy.DebugMatchesToJSON(privacySummary),
		)
	}

	// 6. 决定外层循环轮数（loop_retry）
	maxLoops := 1
	if failoverCfg := h.failoverConfig(); failoverCfg.LoopRetry {
		maxLoops = failoverCfg.GetMaxLoops()
		if maxLoops <= 0 {
			maxLoops = 1
		}
	}

	// 7. 渠道内循环重试 + 挂起等待运维干预（最多挂起一次）
	var failedIDs []int64
	var endpointChain []chainNode
	attemptCount := 0
	var lastErr error
	var lastStatus int
	var lastSummary string
	suspendedOnce := false
	activeChannelID := int64(0)

outerLoop:
	for {
	rounds:
		for loopRound := 0; loopRound < maxLoops; loopRound++ {
			triedChannels := make(map[int64]struct{})
			if loopRound > 0 {
				h.manager.ResetUnhealthy()
				failedIDs = failedIDs[:0]
				if h.failoverConfig().Enabled {
					// 渠道间转移开启时，新一轮从最高优先级参与渠道开始。
					first, switchErr := h.manager.ActivateNextChannelForFailover(r.Context(), triedChannels, activeChannelID)
					if errors.Is(switchErr, errCodexActiveChannelChanged) {
						continue outerLoop
					}
					if switchErr != nil {
						lastErr = switchErr
						break rounds
					}
					triedChannels[first.ID] = struct{}{}
					activeChannelID = first.ID
					channelName = first.Name
					channelRewrite := rewriteModelWithMappingDetails(originalBody, first.ModelMapping, strings.TrimSpace(first.Model), h.logger, requestID)
					body = channelRewrite.Body
					requestModel = h.tokenParser.ParseModelFromBody(body)
				} else if active := h.manager.GetActiveChannel(); active != nil {
					// 渠道间转移关闭时，只重试当前活跃渠道，不写全局激活状态。
					triedChannels[active.ID] = struct{}{}
					activeChannelID = active.ID
					channelName = active.Name
					channelRewrite := rewriteModelWithMappingDetails(originalBody, active.ModelMapping, strings.TrimSpace(active.Model), h.logger, requestID)
					body = channelRewrite.Body
					requestModel = h.tokenParser.ParseModelFromBody(body)
				} else {
					lastErr = fmt.Errorf("无活跃 Codex 渠道")
					break rounds
				}
				h.logger.Info("[Codex 循环重试]",
					"provider", "codex",
					"request_id", requestID,
					"round", loopRound+1,
					"max_loops", maxLoops,
				)
			} else if active := h.manager.GetActiveChannel(); active != nil {
				// 首轮从当前接管渠道开始；挂起恢复后也重新建立本轮状态。
				triedChannels[active.ID] = struct{}{}
				activeChannelID = active.ID
				channelName = active.Name
				channelRewrite := rewriteModelWithMappingDetails(originalBody, active.ModelMapping, strings.TrimSpace(active.Model), h.logger, requestID)
				body = channelRewrite.Body
				requestModel = h.tokenParser.ParseModelFromBody(body)
			}

			for {
				result := h.tryEndpointCycle(
					w, r, body, requestID, startTime,
					isStreaming, channelName, requestModel,
					&failedIDs, &endpointChain, &attemptCount, turnFailureKey,
				)
				if result.routeChanged {
					failedIDs = failedIDs[:0]
					continue outerLoop
				}
				if result.completed {
					return
				}
				lastErr = result.lastErr
				lastStatus = result.lastStatus
				lastSummary = result.lastSummary
				if !h.failoverConfig().Enabled {
					break
				}
				next, switchErr := h.manager.ActivateNextChannelForFailover(r.Context(), triedChannels, activeChannelID)
				if errors.Is(switchErr, errCodexActiveChannelChanged) {
					failedIDs = failedIDs[:0]
					continue outerLoop
				}
				if switchErr != nil {
					break
				}
				triedChannels[next.ID] = struct{}{}
				activeChannelID = next.ID
				channelName = next.Name
				channelRewrite := rewriteModelWithMappingDetails(originalBody, next.ModelMapping, strings.TrimSpace(next.Model), h.logger, requestID)
				body = channelRewrite.Body
				requestModel = h.tokenParser.ParseModelFromBody(body)
				failedIDs = failedIDs[:0]
				h.logger.Info("[Codex 渠道间故障转移]", "request_id", requestID, "channel", channelName, "round", loopRound+1)
			}
			if errors.Is(lastErr, errCredentialGroupDailyCostLimitExceeded) {
				break rounds
			}
		}

		// loop_retry 全部耗尽 → 尝试挂起等待运维干预（最多挂起一次）
		failoverCfg := h.failoverConfig()
		canSuspend := !errors.Is(lastErr, errCredentialGroupDailyCostLimitExceeded) &&
			!suspendedOnce && h.suspensionMgr != nil &&
			failoverCfg.LoopRetrySuspendTimeout > 0
		if !canSuspend {
			break outerLoop
		}
		suspendedOnce = true

		suspendRes := h.suspensionMgr.Suspend(r.Context(), requestID, failoverCfg.LoopRetrySuspendTimeout)
		switch suspendRes {
		case SuspendRecovered:
			// 重置状态再来一轮 loop_retry
			failedIDs = failedIDs[:0]
			h.manager.ResetUnhealthy()
			h.logger.Info("[Codex 挂起恢复] 运维干预后重新进入端点循环",
				"provider", "codex",
				"request_id", requestID,
				"endpoints_tried", len(endpointChain),
			)
			continue outerLoop

		case SuspendCanceled:
			// 客户端在挂起期间取消
			h.logger.Info("[Codex 请求取消]",
				"provider", "codex",
				"request_id", requestID,
				"duration_ms", time.Since(startTime).Milliseconds(),
				"reason", "client_canceled_during_suspend",
			)
			h.recordFailure(requestID, startTime, "client_cancelled", "client_canceled_during_suspend", 499, nil, requestModel)
			return

		case SuspendTimedOut:
			// 超时未被唤醒，落入失败逻辑
			h.logger.Warn("[Codex 挂起超时] 运维未在超时前激活渠道/端点",
				"provider", "codex",
				"request_id", requestID,
				"timeout", failoverCfg.LoopRetrySuspendTimeout.String(),
			)
			break outerLoop

		case SuspendRejected:
			// 挂起队列已满，落入失败逻辑
			h.logger.Warn("[Codex 挂起拒绝] 已达最大挂起请求数",
				"provider", "codex",
				"request_id", requestID,
			)
			break outerLoop
		}
	}

	// 8. 所有循环耗尽 + 挂起也失败
	statusCode := http.StatusServiceUnavailable
	failureReason := "endpoint_exhausted"
	detail := h.buildExhaustedDetail(maxLoops, endpointChain, lastErr, lastStatus, lastSummary)
	if errors.Is(lastErr, errCredentialGroupDailyCostLimitExceeded) {
		statusCode = http.StatusTooManyRequests
		failureReason = "daily_cost_limit_exceeded"
		detail = "所有可用 Codex 凭证分组均已达到每日成本限额"
	} else if lastStatus >= 400 {
		statusCode = lastStatus
	}
	h.logger.Error("[Codex 请求失败]",
		"provider", "codex",
		"request_id", requestID,
		"failure_reason", failureReason,
		"detail", detail,
		"duration_ms", time.Since(startTime).Milliseconds(),
		"endpoints_tried", len(endpointChain),
		"loops_tried", maxLoops,
		"channel", channelName,
	)
	h.recordFailure(requestID, startTime, failureReason, detail, statusCode, nil, requestModel)
	h.writeError(w, statusCode, detail)
}

// cycleResult 端点循环结果
type cycleResult struct {
	completed    bool   // true: 响应已写给客户端；false: 端点耗尽，外层 loop 决定是否再来一轮
	routeChanged bool   // true: 共享活跃渠道已变化，外层应按新渠道重建请求路由
	lastErr      error  // 最后一次端点失败的错误
	lastStatus   int    // 最后一次端点失败的 HTTP 状态码（0 = 网络错误）
	lastSummary  string // 最后一次上游错误响应摘要
}

// tryEndpointCycle 单轮端点列表遍历（含每个端点的同端点重试）
// 返回 completed=true 表示已写响应给客户端；false 表示需要外层 loop 判断
func (h *Handler) tryEndpointCycle(
	w http.ResponseWriter,
	r *http.Request,
	body []byte,
	requestID string,
	startTime time.Time,
	isStreaming bool,
	channelName string,
	requestModel string,
	failedIDs *[]int64,
	endpointChain *[]chainNode,
	attemptCount *int,
	turnFailureKey string,
) cycleResult {
	if h.activeChannelChanged(channelName) {
		return cycleResult{routeChanged: true}
	}
	// 选择起始端点
	skippedGroups := h.failedCredentialGroupsForTurn(turnFailureKey)
	var quotaSkippedGroups []credentialGroupRef
	endpoint, quotaSkippedGroups, err := h.selectQuotaEligibleEndpoint(
		r.Context(), channelName, *failedIDs, skippedGroups, quotaSkippedGroups, true,
	)
	if err != nil && !errors.Is(err, errCredentialGroupDailyCostLimitExceeded) && len(*endpointChain) == 0 {
		// 新请求不能被上一请求留下的全局 unhealthy 状态直接挡在选路之前。
		// 仅在尚未产生真实链路时重置一次；凭证分组跳过状态仍然保留。
		h.manager.ResetUnhealthy()
		endpoint, quotaSkippedGroups, err = h.selectQuotaEligibleEndpoint(
			r.Context(), channelName, *failedIDs, skippedGroups, quotaSkippedGroups, true,
		)
	}
	if err != nil {
		h.logger.Warn("[Codex 当前轮无可用端点]",
			"provider", "codex",
			"request_id", requestID,
			"error", err,
			"endpoints_tried", len(*endpointChain),
			"channel", channelName,
		)
		// 交给外层 loop_retry；下一轮会重置历史 unhealthy 状态后重新选路。
		status := 0
		if errors.Is(err, errCredentialGroupDailyCostLimitExceeded) {
			status = http.StatusTooManyRequests
		}
		return cycleResult{completed: false, lastErr: err, lastStatus: status}
	}

	// 遍历当前轮可用端点
	for {
		if h.activeChannelChanged(channelName) {
			return cycleResult{routeChanged: true}
		}
		// 追加链路节点（按 channel+credential_group+endpoint 切换去重）
		n := len(*endpointChain)
		previousAttemptFailed := n > 0 && ((*endpointChain)[n-1].StatusCode != 0 || (*endpointChain)[n-1].Error != "")
		if n == 0 ||
			previousAttemptFailed ||
			(*endpointChain)[n-1].Channel != channelName ||
			(*endpointChain)[n-1].CredentialGroup != endpoint.CredentialGroupName ||
			(*endpointChain)[n-1].Endpoint != endpoint.Name {
			*endpointChain = append(*endpointChain, chainNode{
				Hop:             n + 1,
				Channel:         channelName,
				CredentialGroup: endpoint.CredentialGroupName,
				Endpoint:        endpoint.Name,
			})
		}
		h.recordForwarding(requestID, endpoint.Name, channelName, requestModel, *endpointChain)

		// 🆕 [Codex 生命周期] 端点选择日志
		h.logger.Info("[Codex 端点选择]",
			"provider", "codex",
			"request_id", requestID,
			"endpoint", endpoint.Name,
			"credential_group", endpoint.CredentialGroupName,
			"hop", len(*endpointChain),
			"channel", channelName,
		)

		h.logger.Debug("转发请求",
			"request_id", requestID,
			"endpoint", endpoint.Name,
			"url", endpoint.BaseURL,
			"streaming", isStreaming,
		)

		// 同端点重试转发
		resp, fwdErr, attempts := h.forwardWithRetry(r, body, endpoint, isStreaming, requestID)
		if attempts > 0 && attemptCount != nil {
			*attemptCount += attempts
			retryCount := *attemptCount - 1
			if h.usageTracker != nil {
				h.usageTracker.RecordRequestUpdate(requestID, tracking.UpdateOptions{RetryCount: &retryCount})
			}
		}

		// 客户端取消（context.Canceled）：直接结束
		if errors.Is(fwdErr, context.Canceled) {
			h.logger.Info("[Codex 请求取消]",
				"provider", "codex",
				"request_id", requestID,
				"endpoint", endpoint.Name,
				"duration_ms", time.Since(startTime).Milliseconds(),
				"reason", "client_canceled",
			)
			h.recordFailure(requestID, startTime, "client_cancelled", "client_canceled", 499, nil, requestModel)
			if resp != nil {
				resp.Body.Close()
			}
			return cycleResult{completed: true}
		}

		// 成功路径：无错误且 status < 400
		if fwdErr == nil && resp != nil && resp.StatusCode < 400 {
			if isStreaming {
				if streamErr := h.handleStreaming(w, resp, requestID, startTime, requestModel, endpoint.Name, endpoint.CredentialGroupName, channelName, len(*endpointChain)); streamErr != nil {
					fwdErr = streamErr
					resp = nil
				} else {
					h.manager.MarkHealthy(endpoint.ID)
					// 故障转移后将当前成功端点提升为活跃，刷新前端 UI
					if len(*failedIDs) > 0 && !h.activeChannelChanged(channelName) {
						if err := h.manager.PromoteActive(endpoint.ID); err != nil {
							h.logger.Warn("故障转移后提升活跃端点失败",
								"request_id", requestID,
								"endpoint_id", endpoint.ID,
								"endpoint", endpoint.Name,
								"error", err,
							)
						}
					}
					return cycleResult{completed: true}
				}
			} else {
				h.manager.MarkHealthy(endpoint.ID)
				// 故障转移后将当前成功端点提升为活跃，刷新前端 UI
				if len(*failedIDs) > 0 && !h.activeChannelChanged(channelName) {
					if err := h.manager.PromoteActive(endpoint.ID); err != nil {
						h.logger.Warn("故障转移后提升活跃端点失败",
							"request_id", requestID,
							"endpoint_id", endpoint.ID,
							"endpoint", endpoint.Name,
							"error", err,
						)
					}
				}
				h.handleNonStreaming(w, resp, requestID, startTime, requestModel, endpoint.Name, endpoint.CredentialGroupName, channelName, len(*endpointChain))
				return cycleResult{completed: true}
			}
		}

		// 失败路径：先读取上游响应摘要（仅当 resp 非空）
		var errSummary string
		var errBody []byte
		var status int
		if resp != nil {
			status = resp.StatusCode
			errBody, errSummary = h.readErrorBody(resp)
		} else if streamErr, ok := fwdErr.(*UpstreamStreamError); ok {
			status = streamErr.StatusCode
			errSummary = streamErr.Error()
		}

		// 三态分类
		action := h.classifier.Classify(resp, fwdErr)

		// FailFast：透传上游 4xx 错误响应，不再尝试任何重试或切换
		if action == ActionFailFast {
			if resp != nil {
				detail := fmt.Sprintf("上游 HTTP %d: %s", status, errSummary)
				annotateLastChainError(endpointChain, status, errSummary)
				h.recordForwarding(requestID, endpoint.Name, channelName, requestModel, *endpointChain)
				h.manager.SetFailureDetail(endpoint.ID, status, detail)
				h.logger.Error("[Codex 请求失败]",
					"provider", "codex",
					"request_id", requestID,
					"failure_reason", "server_error",
					"detail", detail,
					"duration_ms", time.Since(startTime).Milliseconds(),
					"endpoints_tried", len(*endpointChain),
					"channel", channelName,
				)
				h.recordFailure(requestID, startTime, "server_error", detail, resp.StatusCode, nil, requestModel)
				h.passthroughWithBody(w, resp, errBody)
				return cycleResult{completed: true}
			}
			// 兜底：网络错误归类为 ActionRetry，理论不应到这里
			detail := "请求失败"
			if fwdErr != nil {
				detail = fwdErr.Error()
			}
			annotateLastChainError(endpointChain, 0, detail)
			h.recordForwarding(requestID, endpoint.Name, channelName, requestModel, *endpointChain)
			h.manager.SetFailureDetail(endpoint.ID, 0, detail)
			h.logger.Error("[Codex 请求失败]",
				"provider", "codex",
				"request_id", requestID,
				"failure_reason", "network_error",
				"detail", detail,
				"duration_ms", time.Since(startTime).Milliseconds(),
				"endpoints_tried", len(*endpointChain),
				"channel", channelName,
			)
			h.recordFailure(requestID, startTime, "network_error", detail, http.StatusBadGateway, nil, requestModel)
			h.writeError(w, http.StatusBadGateway, detail)
			return cycleResult{completed: true}
		}

		// ActionRetry / ActionFailover：标记当前端点不健康并切换下一个
		if resp != nil {
			resp.Body.Close()
		}
		failedEndpointName := endpoint.Name
		*failedIDs = appendUniqueInt64(*failedIDs, endpoint.ID)

		// 失败原因日志
		var reason string
		if fwdErr != nil {
			reason = fwdErr.Error()
			h.logger.Warn(fmt.Sprintf("⚠️ [Codex故障转移] [%s] 失败端点: %s, 已尝试: %d 端点, 原因: %v",
				requestID, failedEndpointName, len(*endpointChain), fwdErr),
				"provider", "codex", "request_id", requestID)
		} else {
			reason = fmt.Sprintf("HTTP %d: %s", status, errSummary)
			h.logger.Warn(fmt.Sprintf("🔁 [Codex故障转移] [%s] 端点: %s, HTTP状态: %d, 上游响应: %s",
				requestID, failedEndpointName, status, errSummary),
				"provider", "codex", "request_id", requestID)
		}
		annotateLastChainError(endpointChain, status, chainErrorText(fwdErr, errSummary, reason))
		h.recordForwarding(requestID, endpoint.Name, channelName, requestModel, *endpointChain)
		if isCredentialGroupFailure(status, errSummary) {
			h.rememberFailedCredentialGroupForTurn(turnFailureKey, credentialGroupRefFromEndpoint(endpoint))
			groupEndpointIDs := h.manager.endpointIDsInCredentialGroup(endpoint.CredentialGroupID, endpoint.CredentialGroupName)
			*failedIDs = appendUniqueInt64(*failedIDs, groupEndpointIDs...)
			h.logger.Warn("[Codex 凭证分组故障转移]",
				"provider", "codex",
				"request_id", requestID,
				"credential_group", endpoint.CredentialGroupName,
				"skipped_endpoint_count", len(groupEndpointIDs),
				"status_code", status,
			)
		}
		h.manager.MarkUnhealthyWithDetail(endpoint.ID, status, reason)

		// 选下个端点
		skippedGroups = h.failedCredentialGroupsForTurn(turnFailureKey)
		next, updatedQuotaSkippedGroups, nextErr := h.selectQuotaEligibleEndpoint(
			r.Context(), channelName, *failedIDs, skippedGroups, quotaSkippedGroups, false,
		)
		quotaSkippedGroups = updatedQuotaSkippedGroups
		if nextErr != nil {
			// 当前轮端点列表耗尽，返回让外层 loop 决定是否再来一轮
			return cycleResult{
				completed:   false,
				lastErr:     endpointCycleLastError(nextErr, fwdErr, status),
				lastStatus:  quotaAwareStatus(nextErr, status),
				lastSummary: errSummary,
			}
		}

		h.logger.Warn("[Codex 故障转移]",
			"provider", "codex",
			"request_id", requestID,
			"failed_endpoint", failedEndpointName,
			"retry_count", len(*endpointChain),
			"next_endpoint", next.Name,
			"reason", reason,
		)
		endpoint = next
	}
}

// forwardWithRetry 单端点同端点重试（指数退避）
// 仅在 classifier 返回 ActionRetry 时重试（瞬时错误：网络错误、408、425）
// 其他情况（成功、ActionFailover、ActionFailFast）立即返回，由外层 cycle 处理
func (h *Handler) forwardWithRetry(
	r *http.Request,
	body []byte,
	endpoint *CodexEndpoint,
	isStreaming bool,
	requestID string,
) (*http.Response, error, int) {
	fwd := h.forwarder
	if isStreaming {
		fwd = h.streamFwd
	}

	retryCfg := h.retryConfig()
	maxAttempts := retryCfg.MaxAttempts
	if maxAttempts < 1 {
		maxAttempts = 1
	}

	var lastResp *http.Response
	var lastErr error
	attempts := 0

	for attempt := 0; attempt < maxAttempts; attempt++ {
		if attempt > 0 {
			// 关闭上一次的响应体（如果有）
			if lastResp != nil {
				lastResp.Body.Close()
				lastResp = nil
			}
			delay := computeBackoff(attempt-1, retryCfg.BaseDelay, retryCfg.MaxDelay, retryCfg.Multiplier)
			h.logger.Info("[Codex 同端点重试]",
				"provider", "codex",
				"request_id", requestID,
				"endpoint", endpoint.Name,
				"attempt", attempt+1,
				"max_attempts", maxAttempts,
				"delay_ms", delay.Milliseconds(),
			)
			select {
			case <-time.After(delay):
			case <-r.Context().Done():
				return nil, r.Context().Err(), attempts
			}
		}

		resp, err := fwd.Forward(r, body, endpoint)
		attempts++
		lastResp = resp
		lastErr = err

		// 客户端取消直接返回
		if errors.Is(err, context.Canceled) {
			return resp, err, attempts
		}

		// 只有 ActionRetry 才继续重试；其他动作（含成功、ActionFailover、ActionFailFast）立即返回
		action := h.classifier.Classify(resp, err)
		if action != ActionRetry {
			return resp, err, attempts
		}
		// ActionRetry 但已是最后一次：返回让外层 cycle 切换端点
		if attempt+1 >= maxAttempts {
			return resp, err, attempts
		}
	}

	return lastResp, lastErr, attempts
}

// buildExhaustedDetail 构造端点耗尽时的错误描述
func (h *Handler) buildExhaustedDetail(maxLoops int, chain []chainNode, lastErr error, lastStatus int, lastSummary string) string {
	base := fmt.Sprintf("endpoint_exhausted: 已尝试 %d 个端点", len(chain))
	if maxLoops > 1 {
		base += fmt.Sprintf("（共 %d 轮循环）", maxLoops)
	}
	if lastErr != nil {
		return base + fmt.Sprintf(", 最后错误: %v", lastErr)
	}
	if lastStatus >= 400 {
		return base + fmt.Sprintf(", 最后 HTTP %d: %s", lastStatus, lastSummary)
	}
	return base
}

// handleNonStreaming 处理非流式响应：读取完整 body，解析 token，写回客户端
func (h *Handler) handleNonStreaming(w http.ResponseWriter, resp *http.Response, requestID string, startTime time.Time, requestModel, endpointName, groupName, channelName string, hopsTried int) {
	defer resp.Body.Close()

	// 🆕 [详细日志] 非流式处理开始（emoji 风格，对齐 Claude 日志详细度）
	h.logger.Info(fmt.Sprintf("📥 [Codex非流式] [%s] 开始非流式处理，端点: %s", requestID, endpointName),
		"provider", "codex", "request_id", requestID)

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		h.logger.Error("读取上游响应失败", "request_id", requestID, "error", err)
		// 🆕 [Codex 生命周期] 请求最终失败日志
		h.logger.Error("[Codex 请求失败]",
			"provider", "codex",
			"request_id", requestID,
			"failure_reason", "network_error",
			"detail", "读取上游响应失败",
			"duration_ms", time.Since(startTime).Milliseconds(),
			"endpoints_tried", hopsTried,
			"channel", channelName,
		)
		h.recordFailure(requestID, startTime, "network_error", "读取上游响应失败", http.StatusBadGateway, nil, requestModel)
		h.writeError(w, http.StatusBadGateway, "读取上游响应失败")
		return
	}

	// 解析 token 用量并记录成功
	tokenResult := h.tokenParser.ParseNonStreaming(respBody)

	// 🆕 [详细日志] 模型 / Usage 提取（emoji 风格，对齐 Claude 日志详细度）
	if tokenResult != nil {
		if tokenResult.Model != "" {
			h.logger.Info(fmt.Sprintf("🎯 [Codex模型提取] [%s] 从response提取模型: %s",
				requestID, tokenResult.Model),
				"provider", "codex", "request_id", requestID)
		}
		if tokenResult.InputTokens > 0 || tokenResult.OutputTokens > 0 || tokenResult.CacheReadTokens > 0 {
			h.logger.Info(fmt.Sprintf("🎯 [Codex Usage] [%s] 提取token信息: input=%d, output=%d, cached=%d",
				requestID, tokenResult.InputTokens, tokenResult.OutputTokens, tokenResult.CacheReadTokens),
				"provider", "codex", "request_id", requestID)
		}
	}

	// 🆕 [Codex 生命周期] 增强的完成日志（含端点、渠道、token、状态码、耗时、跳数）
	durationMs := time.Since(startTime).Milliseconds()
	modelName := requestModel
	var inputTokens, outputTokens, cacheReadTokens, totalTokens int64
	var reasoningTokens int64
	if tokenResult != nil {
		modelName = pickModelName(tokenResult.Model, requestModel)
		inputTokens = tokenResult.InputTokens
		outputTokens = tokenResult.OutputTokens
		reasoningTokens = tokenResult.ReasoningTokens
		cacheReadTokens = tokenResult.CacheReadTokens
		totalTokens = tokenResult.TotalTokens
	}
	h.logger.Info("[Codex 请求完成]",
		"provider", "codex",
		"request_id", requestID,
		"endpoint", endpointName,
		"channel", channelName,
		"model", modelName,
		"duration_ms", durationMs,
		"hops_tried", hopsTried,
		"input_tokens", inputTokens,
		"output_tokens", outputTokens,
		"reasoning_tokens", reasoningTokens,
		"cache_read_tokens", cacheReadTokens,
		"total_tokens", totalTokens,
		"http_status", resp.StatusCode,
	)

	if tokenResult == nil {
		h.logger.Debug("Codex 非流式响应未携带 usage 字段",
			"request_id", requestID,
			"body_size", len(respBody),
		)
	}

	// 🆕 [详细日志] Token 最终统计 + 请求完成 + Token 汇总（emoji 风格，对齐 Claude 日志详细度）
	if tokenResult != nil {
		h.logger.Info(fmt.Sprintf("🪙 [Codex Token最终] [%s] 模型: %s, 输入: %d, 输出: %d, 缓存读取: %d",
			requestID, modelName, inputTokens, outputTokens, cacheReadTokens),
			"provider", "codex", "request_id", requestID)
	}
	h.logger.Info(fmt.Sprintf("✅ [Codex请求完成] [%s] 端点: %s (总尝试 %d 个端点)",
		requestID, endpointName, hopsTried),
		"provider", "codex", "request_id", requestID)
	h.logger.Info(fmt.Sprintf("📊 [Codex Token统计] [%s] 模型: %s, 输入[%d] 输出[%d] 总计[%d] 缓存[%d], 耗时: %dms",
		requestID, modelName, inputTokens, outputTokens, inputTokens+outputTokens, cacheReadTokens, durationMs),
		"provider", "codex", "request_id", requestID)

	h.recordOutputSent(requestID)
	h.recordSuccess(requestID, startTime, resp.StatusCode, tokenResult, requestModel, endpointName, groupName, channelName)

	// 复制响应头
	for key, values := range resp.Header {
		for _, v := range values {
			w.Header().Add(key, v)
		}
	}
	w.WriteHeader(resp.StatusCode)
	w.Write(respBody)
}

// handleStreaming 处理流式响应：逐行透传 SSE 并提取 token。
// 如果上游在真正输出前通过 SSE 返回错误事件，则返回错误交给外层端点循环做故障转移。
func (h *Handler) handleStreaming(w http.ResponseWriter, resp *http.Response, requestID string, startTime time.Time, requestModel, endpointName, groupName, channelName string, hopsTried int) *UpstreamStreamError {
	defer resp.Body.Close()

	var headersWritten bool
	flusher, canFlush := w.(http.Flusher)
	writeHeaders := func() {
		if headersWritten {
			return
		}
		for key, values := range resp.Header {
			for _, v := range values {
				w.Header().Add(key, v)
			}
		}
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Connection", "keep-alive")
		w.WriteHeader(resp.StatusCode)
		headersWritten = true
	}

	// 🆕 [详细日志] 流式处理开始（emoji 风格，对齐 Claude 日志详细度）
	h.logger.Info(fmt.Sprintf("🌊 [Codex流式] [%s] 开始流式处理，端点: %s", requestID, endpointName),
		"provider", "codex", "request_id", requestID)

	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 64*1024), 256*1024)

	var lastResult *TokenResult
	var bytesProcessed int64
	var modelLogged, usageLogged bool
	var outputSentRecorded bool
	var downstreamStreamErr *UpstreamStreamError
	var streamCompleted bool
	var pendingLines [][]byte
	var pendingBytes int
	currentEvent := ""

	writeLine := func(line []byte) {
		writeHeaders()
		if !outputSentRecorded && len(bytes.TrimSpace(line)) > 0 {
			h.recordOutputSent(requestID)
			outputSentRecorded = true
		}
		w.Write(line)
		w.Write([]byte("\n"))
		if canFlush {
			flusher.Flush()
		}
	}
	flushPending := func() {
		if len(pendingLines) == 0 {
			return
		}
		for _, pending := range pendingLines {
			writeLine(pending)
		}
		pendingLines = nil
		pendingBytes = 0
	}
	bufferLine := func(line []byte) {
		copied := append([]byte(nil), line...)
		pendingLines = append(pendingLines, copied)
		pendingBytes += len(copied) + 1
		if pendingBytes >= maxPreOutputStreamBufferBytes {
			flushPending()
		}
	}

	for scanner.Scan() {
		line := scanner.Bytes()
		bytesProcessed += int64(len(line)) + 1 // +1 for trailing newline

		if eventName, ok := parseSSEEventName(line); ok {
			currentEvent = eventName
		}

		if streamErr := h.tokenParser.ParseStreamingErrorLine(line); streamErr != nil {
			if !headersWritten {
				h.logger.Warn("[Codex 流式上游错误] 输出前捕获到可转移错误",
					"provider", "codex",
					"request_id", requestID,
					"endpoint", endpointName,
					"status_code", streamErr.StatusCode,
					"error", streamErr.Error(),
				)
				return streamErr
			}
			downstreamStreamErr = streamErr
		}

		// 解析 token（流式最终 chunk 可能包含 usage）
		if result, isDone := h.tokenParser.ParseStreamingLine(line); isDone {
			streamCompleted = true
		} else if result != nil {
			// 🆕 [详细日志] 首次从 response 事件提取到模型时记录
			if !modelLogged && result.Model != "" {
				h.logger.Info(fmt.Sprintf("🎯 [Codex模型提取] [%s] 从response事件提取模型: %s",
					requestID, result.Model),
					"provider", "codex", "request_id", requestID)
				modelLogged = true
			}
			// 🆕 [详细日志] 首次提取到 token 用量时记录（input/output/cached 任一非零即视为获取到）
			if !usageLogged && (result.InputTokens > 0 || result.OutputTokens > 0 || result.CacheReadTokens > 0) {
				h.logger.Info(fmt.Sprintf("🎯 [Codex Usage] [%s] 提取token信息: input=%d, output=%d, cached=%d",
					requestID, result.InputTokens, result.OutputTokens, result.CacheReadTokens),
					"provider", "codex", "request_id", requestID)
				usageLogged = true
			}
			lastResult = result
		}
		if isCodexStreamCompletionLine(currentEvent, line) {
			streamCompleted = true
		}

		if !headersWritten && !isLikelyCodexOutputLine(currentEvent, line) {
			bufferLine(line)
			continue
		}
		flushPending()
		writeLine(line)
	}

	// 🆕 [详细日志] 扫描器错误检测：非 nil 视为流式中断；nil 视为正常完成
	if scanErr := scanner.Err(); scanErr != nil {
		h.logger.Error(fmt.Sprintf("❌ [Codex流式失败] [%s] 端点: %s, 已处理 %d 字节, 错误: %v",
			requestID, endpointName, bytesProcessed, scanErr),
			"provider", "codex", "request_id", requestID)
		if !headersWritten {
			return &UpstreamStreamError{
				Type:       "stream_read_error",
				Message:    scanErr.Error(),
				StatusCode: http.StatusBadGateway,
			}
		}
		if isContextCanceledError(scanErr) {
			if streamCompleted {
				h.logger.Warn("[Codex 流式收尾取消] 已收到完成事件，按成功记录",
					"provider", "codex",
					"request_id", requestID,
					"endpoint", endpointName,
					"bytes_processed", bytesProcessed,
					"error", scanErr,
				)
				h.recordSuccess(requestID, startTime, resp.StatusCode, lastResult, requestModel, endpointName, groupName, channelName)
				return nil
			}
			if outputSentRecorded {
				h.logger.Warn("[Codex 流式下游取消] 已输出内容后连接取消，按客户端取消记录",
					"provider", "codex",
					"request_id", requestID,
					"endpoint", endpointName,
					"bytes_processed", bytesProcessed,
					"error", scanErr,
				)
				h.recordCancelled(requestID, startTime, "client_canceled_after_output", 499, lastResult, requestModel, endpointName, groupName, channelName)
				return nil
			}
		}
		h.recordFailure(requestID, startTime, "network_error", scanErr.Error(), http.StatusBadGateway, nil, requestModel)
		return nil
	} else {
		h.logger.Info(fmt.Sprintf("✅ [Codex流式完成] [%s] 端点: %s, 已处理 %d 字节",
			requestID, endpointName, bytesProcessed),
			"provider", "codex", "request_id", requestID)
	}

	if downstreamStreamErr != nil {
		h.logger.Error("[Codex 流式上游错误] 已输出后收到错误事件，记录为失败",
			"provider", "codex",
			"request_id", requestID,
			"endpoint", endpointName,
			"status_code", downstreamStreamErr.StatusCode,
			"error", downstreamStreamErr.Error(),
		)
		h.recordFailure(requestID, startTime, "server_error", downstreamStreamErr.Error(), downstreamStreamErr.StatusCode, nil, requestModel)
		return nil
	}

	flushPending()

	// 🆕 [Codex 生命周期] 增强的完成日志（含端点、渠道、token、状态码、耗时、跳数）
	durationMs := time.Since(startTime).Milliseconds()
	modelName := requestModel
	var inputTokens, outputTokens, cacheReadTokens, totalTokens int64
	var reasoningTokens int64
	if lastResult != nil {
		modelName = pickModelName(lastResult.Model, requestModel)
		inputTokens = lastResult.InputTokens
		outputTokens = lastResult.OutputTokens
		reasoningTokens = lastResult.ReasoningTokens
		cacheReadTokens = lastResult.CacheReadTokens
		totalTokens = lastResult.TotalTokens
	}
	h.logger.Info("[Codex 请求完成]",
		"provider", "codex",
		"request_id", requestID,
		"endpoint", endpointName,
		"channel", channelName,
		"model", modelName,
		"duration_ms", durationMs,
		"hops_tried", hopsTried,
		"input_tokens", inputTokens,
		"output_tokens", outputTokens,
		"reasoning_tokens", reasoningTokens,
		"cache_read_tokens", cacheReadTokens,
		"total_tokens", totalTokens,
		"http_status", resp.StatusCode,
		"streaming", true,
	)

	if lastResult == nil {
		h.logger.Debug("Codex 流式响应未捕获 usage 信息",
			"request_id", requestID,
		)
	}

	// 🆕 [详细日志] Token 最终统计 + 请求完成 + Token 汇总（emoji 风格，对齐 Claude 日志详细度）
	if lastResult != nil {
		h.logger.Info(fmt.Sprintf("🪙 [Codex Token最终] [%s] 模型: %s, 输入: %d, 输出: %d, 缓存读取: %d",
			requestID, modelName, inputTokens, outputTokens, cacheReadTokens),
			"provider", "codex", "request_id", requestID)
	}
	h.logger.Info(fmt.Sprintf("✅ [Codex请求完成] [%s] 端点: %s (总尝试 %d 个端点)",
		requestID, endpointName, hopsTried),
		"provider", "codex", "request_id", requestID)
	h.logger.Info(fmt.Sprintf("📊 [Codex Token统计] [%s] 模型: %s, 输入[%d] 输出[%d] 总计[%d] 缓存[%d], 耗时: %dms",
		requestID, modelName, inputTokens, outputTokens, inputTokens+outputTokens, cacheReadTokens, durationMs),
		"provider", "codex", "request_id", requestID)

	h.recordSuccess(requestID, startTime, resp.StatusCode, lastResult, requestModel, endpointName, groupName, channelName)
	return nil
}

func parseSSEEventName(line []byte) (string, bool) {
	trimmed := bytes.TrimSpace(line)
	const eventPrefix = "event:"
	if !bytes.HasPrefix(trimmed, []byte(eventPrefix)) {
		return "", false
	}
	return strings.TrimSpace(string(trimmed[len(eventPrefix):])), true
}

func isLikelyCodexOutputLine(eventName string, line []byte) bool {
	eventName = strings.ToLower(strings.TrimSpace(eventName))
	if strings.Contains(eventName, "delta") ||
		strings.Contains(eventName, "content") ||
		strings.Contains(eventName, "output_text") {
		return true
	}
	trimmed := bytes.TrimSpace(line)
	if !bytes.HasPrefix(trimmed, sseDataPrefix) {
		return false
	}
	payload := bytes.TrimSpace(trimmed[len(sseDataPrefix):])
	if bytes.Equal(payload, sseDoneMarker) {
		return true
	}
	payloadLower := bytes.ToLower(payload)
	return bytes.Contains(payloadLower, []byte(`"choices"`)) ||
		bytes.Contains(payloadLower, []byte(`"delta"`)) ||
		bytes.Contains(payloadLower, []byte(`"content"`)) ||
		bytes.Contains(payloadLower, []byte(`"output_text"`))
}

func isCodexStreamCompletionLine(eventName string, line []byte) bool {
	eventName = strings.ToLower(strings.TrimSpace(eventName))
	trimmed := bytes.TrimSpace(line)
	if !bytes.HasPrefix(trimmed, sseDataPrefix) {
		return false
	}
	if eventName == "response.completed" {
		return true
	}
	payload := bytes.ToLower(bytes.TrimSpace(trimmed[len(sseDataPrefix):]))
	return bytes.Contains(payload, []byte(`"response.completed"`))
}

func isContextCanceledError(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, context.Canceled) {
		return true
	}
	return strings.Contains(strings.ToLower(err.Error()), "context canceled")
}

// ============================================
// 追踪辅助方法
// ============================================

func (h *Handler) recordOutputSent(requestID string) {
	if h.usageTracker == nil {
		return
	}
	h.usageTracker.RecordOutputSent(requestID)
}

// activeChannelName 获取当前活跃 Codex 渠道名（无则返回空串）
func (h *Handler) activeChannelName() string {
	if h.manager == nil {
		return ""
	}
	ch := h.manager.GetActiveChannel()
	if ch == nil {
		return ""
	}
	return ch.Name
}

func (h *Handler) activeChannelChanged(expectedName string) bool {
	if h.manager == nil || expectedName == "" {
		return false
	}
	active := h.manager.GetActiveChannel()
	return active != nil && active.Name != expectedName
}

// recordStart 记录请求开始
// 一次性写入 Provider/Channel/Model（来自请求体），减少与归档之间的字段缺失风险
func (h *Handler) recordStart(requestID string, r *http.Request, isStreaming bool, channelName, requestModel string, modelRewrite modelRewriteResult, reasoningJSON, serviceTier string) {
	if h.usageTracker == nil {
		return
	}
	h.usageTracker.RecordRequestStart(requestID, r.RemoteAddr, r.Header.Get("User-Agent"), r.Method, r.URL.Path, isStreaming)

	opts := tracking.UpdateOptions{
		Provider: strPtr("codex"),
	}
	if channelName != "" {
		opts.Channel = strPtr(channelName)
	}
	if requestModel != "" {
		opts.ModelName = strPtr(requestModel)
	}
	if modelRewrite.Source != "" {
		opts.OriginalModelName = strPtr(modelRewrite.OriginalModel)
		opts.ModelRewriteSource = strPtr(modelRewrite.Source)
		opts.ModelRewriteTarget = strPtr(modelRewrite.NewModel)
	}
	if reasoningJSON != "" {
		opts.ReasoningEffort = &reasoningJSON
	}
	if serviceTier != "" {
		opts.ServiceTier = &serviceTier
	}
	h.usageTracker.RecordRequestUpdate(requestID, opts)
}

// recordForwarding 记录转发状态（含端点链路 + 渠道）
// chain 以 JSON 数组形式写入（形如 [{"hop":1,"channel":"xxx","credential_group":"group","endpoint":"yyy"}]），
// 与 Claude 端 internal/proxy 写入的结构化格式一致，供前端统一解析与展示。
func (h *Handler) recordForwarding(requestID string, endpointName, channelName, modelName string, chain []chainNode) {
	if h.usageTracker == nil {
		return
	}
	groupName := latestCredentialGroupName(chain)
	chainBytes, err := json.Marshal(chain)
	if err != nil {
		// 序列化失败仅记录日志，跳过 EndpointChain 字段更新以避免写入脏数据
		h.logger.Warn("序列化 Codex 端点链路失败",
			"request_id", requestID,
			"error", err,
		)
		opts := tracking.UpdateOptions{
			Status:       strPtr("forwarding"),
			EndpointName: strPtr(endpointName),
		}
		if channelName != "" {
			opts.Channel = strPtr(channelName)
		}
		if groupName != "" {
			opts.GroupName = strPtr(groupName)
		}
		h.usageTracker.RecordRequestUpdate(requestID, opts)
		h.snapshotEndpointMultiplier(requestID, channelName, groupName, endpointName, modelName)
		return
	}
	chainStr := string(chainBytes)
	opts := tracking.UpdateOptions{
		Status:        strPtr("forwarding"),
		EndpointName:  strPtr(endpointName),
		EndpointChain: &chainStr,
	}
	// 转发阶段补写渠道，避免在 recordStart 之后渠道发生切换（虽然概率低）
	if channelName != "" {
		opts.Channel = strPtr(channelName)
	}
	if groupName != "" {
		opts.GroupName = strPtr(groupName)
	}
	h.usageTracker.RecordRequestUpdate(requestID, opts)
	h.snapshotEndpointMultiplier(requestID, channelName, groupName, endpointName, modelName)
}

func (h *Handler) recordPrivacyRedaction(requestID string, summaryJSON string, redactedBody []byte, matchDetailsJSON string) {
	if requestID == "" || summaryJSON == "" {
		return
	}
	logger := h.logger
	if logger == nil {
		logger = slog.Default().With("provider", "codex")
	}
	logger.Debug("[隐私防护] 请求正文已脱敏",
		"request_id", requestID,
		"privacy_redaction_summary", summaryJSON,
		"privacy_redaction_matches", matchDetailsJSON,
		"privacy_redacted_body_bytes", len(redactedBody),
		"privacy_redacted_body", string(redactedBody),
	)
	if h.usageTracker != nil {
		h.usageTracker.RecordPrivacyRedaction(requestID, summaryJSON, matchDetailsJSON)
	}
}

// snapshotEndpointMultiplier 将 Codex 端点的倍率快照写入热池中的活跃请求
// 用于详情页计费过程展示
func (h *Handler) snapshotEndpointMultiplier(requestID, channelName, groupName, endpointName, modelName string) {
	if h.usageTracker == nil || requestID == "" || endpointName == "" {
		return
	}
	mult := h.usageTracker.GetEndpointMultiplierForEndpointAndModelWithGroup(channelName, groupName, endpointName, modelName)
	_ = h.usageTracker.UpdateActiveRequestMultiplier(requestID, mult)
}

func latestCredentialGroupName(chain []chainNode) string {
	if len(chain) == 0 {
		return ""
	}
	return chain[len(chain)-1].CredentialGroup
}

func annotateLastChainError(chain *[]chainNode, statusCode int, errorText string) {
	if chain == nil || len(*chain) == 0 {
		return
	}
	errorText = strings.TrimSpace(errorText)
	if errorText == "" {
		return
	}
	last := &(*chain)[len(*chain)-1]
	last.StatusCode = statusCode
	last.Error = errorText
}

func chainErrorText(err error, errSummary, fallback string) string {
	if err != nil {
		return err.Error()
	}
	if strings.TrimSpace(errSummary) != "" {
		return errSummary
	}
	return fallback
}

// pickModelName 在响应未携带 model 时回退到请求体 model
func pickModelName(responseModel, requestModel string) string {
	if responseModel != "" {
		return responseModel
	}
	return requestModel
}

func (h *Handler) endpointBillingOutputTokenSource(channelName, groupName, endpointName string) string {
	if h.usageTracker == nil || endpointName == "" {
		return tracking.BillingOutputTokenSourceOutput
	}
	mult := h.usageTracker.GetEndpointMultiplierForEndpointWithGroup(channelName, groupName, endpointName)
	return tracking.NormalizeBillingOutputTokenSource(mult.BillingOutputTokenSource)
}

// buildTokenUsage 从 TokenResult 构造 tracking.TokenUsage（包含缓存读取和写入）
func buildTokenUsage(tr *TokenResult, billingOutputTokenSource string) *tracking.TokenUsage {
	if tr == nil {
		return nil
	}
	return &tracking.TokenUsage{
		InputTokens:              tr.InputTokens,
		OutputTokens:             tr.OutputTokens,
		ReasoningTokens:          tr.ReasoningTokens,
		BillingOutputTokenSource: tracking.NormalizeBillingOutputTokenSource(billingOutputTokenSource),
		CacheCreationTokens:      tr.CacheWriteTokens,
		CacheReadTokens:          tr.CacheReadTokens,
		InputIncludesCacheRead:   true,
	}
}

// recordSuccess 记录请求成功
func (h *Handler) recordSuccess(requestID string, startTime time.Time, httpStatus int, tokenResult *TokenResult, requestModel, endpointName, groupName, channelName string) {
	if h.usageTracker == nil {
		return
	}
	duration := time.Since(startTime)
	var tokens *tracking.TokenUsage
	var modelName string
	if tokenResult != nil {
		modelName = pickModelName(tokenResult.Model, requestModel)
		tokens = buildTokenUsage(tokenResult, h.endpointBillingOutputTokenSource(channelName, groupName, endpointName))
		if tokenResult.ServiceTier != "" {
			serviceTier := tokenResult.ServiceTier
			h.usageTracker.RecordRequestUpdate(requestID, tracking.UpdateOptions{ServiceTier: &serviceTier})
		}
	} else {
		modelName = requestModel
	}
	h.snapshotEndpointMultiplier(requestID, channelName, groupName, endpointName, modelName)
	h.usageTracker.RecordRequestSuccessWithHTTPStatus(requestID, modelName, tokens, duration, httpStatus)
}

// recordFailure 记录请求失败
func (h *Handler) recordFailure(requestID string, startTime time.Time, reason string, detail string, httpStatus int, tokenResult *TokenResult, requestModel string) {
	if h.usageTracker == nil {
		return
	}
	duration := time.Since(startTime)
	var tokens *tracking.TokenUsage
	var modelName string
	if tokenResult != nil {
		modelName = pickModelName(tokenResult.Model, requestModel)
		tokens = buildTokenUsage(tokenResult, tracking.BillingOutputTokenSourceOutput)
	} else {
		modelName = requestModel
	}
	h.usageTracker.RecordRequestFinalFailure(requestID, modelName, "failed", reason, detail, duration, httpStatus, tokens)
}

func (h *Handler) recordCancelled(requestID string, startTime time.Time, reason string, httpStatus int, tokenResult *TokenResult, requestModel, endpointName, groupName, channelName string) {
	if h.usageTracker == nil {
		return
	}
	duration := time.Since(startTime)
	var tokens *tracking.TokenUsage
	modelName := requestModel
	if tokenResult != nil {
		modelName = pickModelName(tokenResult.Model, requestModel)
		tokens = buildTokenUsage(tokenResult, h.endpointBillingOutputTokenSource(channelName, groupName, endpointName))
		if tokenResult.ServiceTier != "" {
			serviceTier := tokenResult.ServiceTier
			h.usageTracker.RecordRequestUpdate(requestID, tracking.UpdateOptions{ServiceTier: &serviceTier})
		}
	}
	h.snapshotEndpointMultiplier(requestID, channelName, groupName, endpointName, modelName)
	h.usageTracker.RecordRequestFinalFailure(requestID, modelName, "cancelled", reason, "", duration, httpStatus, tokens)
}

// ============================================
// 响应辅助方法
// ============================================

// passthrough 透传上游响应（含错误响应）
func (h *Handler) passthrough(w http.ResponseWriter, resp *http.Response) {
	defer resp.Body.Close()

	for key, values := range resp.Header {
		for _, v := range values {
			w.Header().Add(key, v)
		}
	}
	w.WriteHeader(resp.StatusCode)
	io.Copy(w, resp.Body)
}

// passthroughWithBody 直接透传给定 body（用于已经读取了 body 用于错误日志的场景）
func (h *Handler) passthroughWithBody(w http.ResponseWriter, resp *http.Response, body []byte) {
	defer resp.Body.Close()
	for key, values := range resp.Header {
		for _, v := range values {
			w.Header().Add(key, v)
		}
	}
	// 重写 Content-Length（如果有）
	w.Header().Set("Content-Length", fmt.Sprintf("%d", len(body)))
	w.WriteHeader(resp.StatusCode)
	_, _ = w.Write(body)
}

// readErrorBody 读取上游错误响应体（最大 4KB），用于错误日志和追踪
// 返回完整 body 字节和截断后的可读字符串
func (h *Handler) readErrorBody(resp *http.Response) (body []byte, summary string) {
	if resp == nil || resp.Body == nil {
		return nil, ""
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 4096))
	if err != nil {
		return nil, fmt.Sprintf("(读取响应体失败: %v)", err)
	}
	summary = strings.TrimSpace(string(body))
	// 单行化，避免日志多行
	summary = strings.ReplaceAll(summary, "\n", " ")
	summary = strings.ReplaceAll(summary, "\r", "")
	if len(summary) > 500 {
		summary = summary[:500] + "..."
	}
	return body, summary
}

// extractReasoning 从 Codex 请求体 JSON 中提取完整的 reasoning 配置并序列化为 JSON
// 请求体格式: {"reasoning":{"effort":"xhigh","mode":"auto","summary":"concise"},...}
// 返回 "" 表示请求体未设置 reasoning（将使用 Codex CLI 默认值）
func extractReasoning(body []byte) string {
	if len(body) == 0 {
		return ""
	}
	var partial struct {
		Reasoning json.RawMessage `json:"reasoning"`
	}
	if err := json.Unmarshal(body, &partial); err != nil || len(partial.Reasoning) == 0 || bytes.Equal(bytes.TrimSpace(partial.Reasoning), []byte("null")) {
		return ""
	}
	return string(partial.Reasoning)
}

// extractServiceTier 从 Codex 请求体 JSON 中提取 service_tier
// 常见取值: auto/default/batch/flex/priority；部分兼容服务商使用 fast。
func extractServiceTier(body []byte) string {
	if len(body) == 0 {
		return ""
	}
	var partial struct {
		ServiceTier string `json:"service_tier"`
	}
	if err := json.Unmarshal(body, &partial); err != nil {
		return ""
	}
	return strings.ToLower(strings.TrimSpace(partial.ServiceTier))
}

// writeError 写入 JSON 错误响应
func (h *Handler) writeError(w http.ResponseWriter, status int, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	fmt.Fprintf(w, `{"error":{"message":"%s","type":"proxy_error"}}`, message)
}
