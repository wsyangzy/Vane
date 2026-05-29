// handler.go - Codex 代理请求处理器
// 实现 http.Handler，协调端点选择、转发、故障转移和 token 解析。

package codexproxy

import (
	"bufio"
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"ai-relay-forwarder/config"
	"ai-relay-forwarder/internal/tracking"
)

// chainNode 端点链路节点（结构化存储）
// 序列化为 JSON 数组写入 tracking.UpdateOptions.EndpointChain，与 Claude 端
// internal/proxy.ChainNode 保持同形（{hop, channel, endpoint}），供前端统一解析。
type chainNode struct {
	Hop      int    `json:"hop"`
	Channel  string `json:"channel"`
	Endpoint string `json:"endpoint"`
}

// Handler 处理 Codex 代理请求
type Handler struct {
	manager      *EndpointManager
	forwarder    *Forwarder
	streamFwd    *Forwarder
	tokenParser  *TokenParser
	classifier   *ErrorClassifier
	logger       *slog.Logger
	usageTracker *tracking.UsageTracker

	// retryCfg 同端点重试配置（来自设置页 retry.*）
	retryCfg config.RetryConfig
	// failoverCfg 故障转移与挂起配置（来自设置页 failover.*）
	failoverCfg config.FailoverConfig
	// suspensionMgr 请求挂起队列（loop_retry 全部耗尽后等待运维干预）
	// 为 nil 时不启用挂起，直接返回失败
	suspensionMgr *SuspensionManager
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
) *Handler {
	if logger == nil {
		logger = slog.Default()
	}
	// 所有 Codex 代理日志附加 provider=codex，用于文件分流
	logger = logger.With("provider", "codex")
	return &Handler{
		manager:       manager,
		forwarder:     NewForwarder(timeout, logger),
		streamFwd:     NewStreamingForwarder(logger),
		tokenParser:   NewTokenParser(),
		classifier:    NewErrorClassifier(),
		logger:        logger,
		usageTracker:  usageTracker,
		retryCfg:      retryCfg,
		failoverCfg:   failoverCfg,
		suspensionMgr: suspensionMgr,
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

	// 2. 检测是否流式请求
	isStreaming := bytes.Contains(body, []byte(`"stream":true`)) ||
		bytes.Contains(body, []byte(`"stream": true`))

	// 3. 从请求体提取模型名（用于在响应未携带 model 时填充）
	requestModel := h.tokenParser.ParseModelFromBody(body)

	// 4. 获取活跃渠道名（用于追踪展示）
	channelName := h.activeChannelName()

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
	)

	// 5. 记录请求开始（追踪：Provider=codex + 渠道 + 请求体模型）
	h.recordStart(requestID, r, isStreaming, channelName, requestModel)

	// 6. 决定外层循环轮数（loop_retry）
	maxLoops := 1
	if h.failoverCfg.LoopRetry {
		maxLoops = h.failoverCfg.GetMaxLoops()
		if maxLoops <= 0 {
			maxLoops = 1
		}
	}

	// 7. 渠道内循环重试 + 挂起等待运维干预（最多挂起一次）
	var failedNames []string
	var endpointChain []chainNode
	var lastErr error
	var lastStatus int
	var lastSummary string
	suspendedOnce := false

outerLoop:
	for {
		for loopRound := 0; loopRound < maxLoops; loopRound++ {
			if loopRound > 0 {
				// 新一轮：重置端点不健康状态 + 清空已失败名单，让所有端点重新可选
				h.manager.ResetUnhealthy()
				failedNames = failedNames[:0]
				h.logger.Info("[Codex 循环重试]",
					"provider", "codex",
					"request_id", requestID,
					"round", loopRound+1,
					"max_loops", maxLoops,
				)
			}

			result := h.tryEndpointCycle(
				w, r, body, requestID, startTime,
				isStreaming, channelName, requestModel,
				&failedNames, &endpointChain,
			)
			if result.completed {
				// 响应已写给客户端（成功 / 客户端取消 / FailFast）
				return
			}

			// 端点列表耗尽，保存最后一次失败信息，决定是否再来一轮
			lastErr = result.lastErr
			lastStatus = result.lastStatus
			lastSummary = result.lastSummary
		}

		// loop_retry 全部耗尽 → 尝试挂起等待运维干预（最多挂起一次）
		canSuspend := !suspendedOnce && h.suspensionMgr != nil &&
			h.failoverCfg.LoopRetrySuspendTimeout > 0
		if !canSuspend {
			break outerLoop
		}
		suspendedOnce = true

		suspendRes := h.suspensionMgr.Suspend(r.Context(), requestID, h.failoverCfg.LoopRetrySuspendTimeout)
		switch suspendRes {
		case SuspendRecovered:
			// 重置状态再来一轮 loop_retry
			failedNames = failedNames[:0]
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
				"timeout", h.failoverCfg.LoopRetrySuspendTimeout.String(),
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
	if lastStatus >= 400 {
		statusCode = lastStatus
	}
	detail := h.buildExhaustedDetail(maxLoops, endpointChain, lastErr, lastStatus, lastSummary)
	h.logger.Error("[Codex 请求失败]",
		"provider", "codex",
		"request_id", requestID,
		"failure_reason", "endpoint_exhausted",
		"detail", detail,
		"duration_ms", time.Since(startTime).Milliseconds(),
		"endpoints_tried", len(endpointChain),
		"loops_tried", maxLoops,
		"channel", channelName,
	)
	h.recordFailure(requestID, startTime, "endpoint_exhausted", detail, statusCode, nil, requestModel)
	h.writeError(w, statusCode, detail)
}

// cycleResult 端点循环结果
type cycleResult struct {
	completed   bool   // true: 响应已写给客户端；false: 端点耗尽，外层 loop 决定是否再来一轮
	lastErr     error  // 最后一次端点失败的错误
	lastStatus  int    // 最后一次端点失败的 HTTP 状态码（0 = 网络错误）
	lastSummary string // 最后一次上游错误响应摘要
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
	failedNames *[]string,
	endpointChain *[]chainNode,
) cycleResult {
	// 选择起始端点
	endpoint, err := h.manager.GetPrimaryEndpoint()
	if err != nil {
		// 首次就无端点：写错误并完成
		if len(*endpointChain) == 0 {
			h.logger.Error("无可用端点", "error", err)
			h.logger.Error("[Codex 请求失败]",
				"provider", "codex",
				"request_id", requestID,
				"failure_reason", "network_error",
				"detail", "无可用 Codex 端点",
				"duration_ms", time.Since(startTime).Milliseconds(),
				"endpoints_tried", 0,
				"channel", channelName,
			)
			h.recordFailure(requestID, startTime, "network_error", "无可用 Codex 端点", http.StatusServiceUnavailable, nil, requestModel)
			h.writeError(w, http.StatusServiceUnavailable, "无可用 Codex 端点")
			return cycleResult{completed: true}
		}
		// 非首轮：返回让外层决定
		return cycleResult{completed: false, lastErr: err}
	}

	// 遍历当前轮可用端点
	for {
		// 追加链路节点（按 channel+endpoint 切换去重）
		n := len(*endpointChain)
		if n == 0 || (*endpointChain)[n-1].Channel != channelName || (*endpointChain)[n-1].Endpoint != endpoint.Name {
			*endpointChain = append(*endpointChain, chainNode{
				Hop:      n + 1,
				Channel:  channelName,
				Endpoint: endpoint.Name,
			})
		}
		h.recordForwarding(requestID, endpoint.Name, channelName, *endpointChain)

		// 🆕 [Codex 生命周期] 端点选择日志
		h.logger.Info("[Codex 端点选择]",
			"provider", "codex",
			"request_id", requestID,
			"endpoint", endpoint.Name,
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
		resp, fwdErr := h.forwardWithRetry(r, body, endpoint, isStreaming, requestID)

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
			// 故障转移后将当前成功端点提升为活跃，刷新前端 UI
			if len(*failedNames) > 0 {
				if err := h.manager.PromoteActive(endpoint.Name); err != nil {
					h.logger.Warn("故障转移后提升活跃端点失败",
						"request_id", requestID,
						"endpoint", endpoint.Name,
						"error", err,
					)
				}
			}
			if isStreaming {
				h.handleStreaming(w, resp, requestID, startTime, requestModel, endpoint.Name, channelName, len(*endpointChain))
			} else {
				h.handleNonStreaming(w, resp, requestID, startTime, requestModel, endpoint.Name, channelName, len(*endpointChain))
			}
			return cycleResult{completed: true}
		}

		// 失败路径：先读取上游响应摘要（仅当 resp 非空）
		var errSummary string
		var errBody []byte
		var status int
		if resp != nil {
			status = resp.StatusCode
			errBody, errSummary = h.readErrorBody(resp)
		}

		// 三态分类
		action := h.classifier.Classify(resp, fwdErr)

		// FailFast：透传上游 4xx 错误响应，不再尝试任何重试或切换
		if action == ActionFailFast {
			if resp != nil {
				detail := fmt.Sprintf("上游 HTTP %d: %s", status, errSummary)
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
		h.manager.MarkUnhealthy(endpoint.Name)
		*failedNames = append(*failedNames, endpoint.Name)

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

		// 选下个端点
		next, nextErr := h.manager.GetNextEndpoint(*failedNames)
		if nextErr != nil {
			// 当前轮端点列表耗尽，返回让外层 loop 决定是否再来一轮
			return cycleResult{
				completed:   false,
				lastErr:     fwdErr,
				lastStatus:  status,
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
) (*http.Response, error) {
	fwd := h.forwarder
	if isStreaming {
		fwd = h.streamFwd
	}

	maxAttempts := h.retryCfg.MaxAttempts
	if maxAttempts < 1 {
		maxAttempts = 1
	}

	var lastResp *http.Response
	var lastErr error

	for attempt := 0; attempt < maxAttempts; attempt++ {
		if attempt > 0 {
			// 关闭上一次的响应体（如果有）
			if lastResp != nil {
				lastResp.Body.Close()
				lastResp = nil
			}
			delay := computeBackoff(attempt-1, h.retryCfg.BaseDelay, h.retryCfg.MaxDelay, h.retryCfg.Multiplier)
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
				return nil, r.Context().Err()
			}
		}

		resp, err := fwd.Forward(r, body, endpoint)
		lastResp = resp
		lastErr = err

		// 客户端取消直接返回
		if errors.Is(err, context.Canceled) {
			return resp, err
		}

		// 只有 ActionRetry 才继续重试；其他动作（含成功、ActionFailover、ActionFailFast）立即返回
		action := h.classifier.Classify(resp, err)
		if action != ActionRetry {
			return resp, err
		}
		// ActionRetry 但已是最后一次：返回让外层 cycle 切换端点
		if attempt+1 >= maxAttempts {
			return resp, err
		}
	}

	return lastResp, lastErr
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
func (h *Handler) handleNonStreaming(w http.ResponseWriter, resp *http.Response, requestID string, startTime time.Time, requestModel, endpointName, channelName string, hopsTried int) {
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
	if tokenResult != nil {
		modelName = pickModelName(tokenResult.Model, requestModel)
		inputTokens = tokenResult.InputTokens
		outputTokens = tokenResult.OutputTokens
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

	h.recordSuccess(requestID, startTime, resp.StatusCode, tokenResult, requestModel)

	// 复制响应头
	for key, values := range resp.Header {
		for _, v := range values {
			w.Header().Add(key, v)
		}
	}
	w.WriteHeader(resp.StatusCode)
	w.Write(respBody)
}

// handleStreaming 处理流式响应：逐行透传 SSE 并提取 token
func (h *Handler) handleStreaming(w http.ResponseWriter, resp *http.Response, requestID string, startTime time.Time, requestModel, endpointName, channelName string, hopsTried int) {
	defer resp.Body.Close()

	// 设置流式响应头
	for key, values := range resp.Header {
		for _, v := range values {
			w.Header().Add(key, v)
		}
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.WriteHeader(resp.StatusCode)

	flusher, canFlush := w.(http.Flusher)

	// 🆕 [详细日志] 流式处理开始（emoji 风格，对齐 Claude 日志详细度）
	h.logger.Info(fmt.Sprintf("🌊 [Codex流式] [%s] 开始流式处理，端点: %s", requestID, endpointName),
		"provider", "codex", "request_id", requestID)

	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 64*1024), 256*1024)

	var lastResult *TokenResult
	var bytesProcessed int64
	var modelLogged, usageLogged bool
	for scanner.Scan() {
		line := scanner.Bytes()
		bytesProcessed += int64(len(line)) + 1 // +1 for trailing newline

		// 解析 token（流式最终 chunk 可能包含 usage）
		if result, _ := h.tokenParser.ParseStreamingLine(line); result != nil {
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

		// 透传给客户端
		w.Write(line)
		w.Write([]byte("\n"))
		if canFlush {
			flusher.Flush()
		}
	}

	// 🆕 [详细日志] 扫描器错误检测：非 nil 视为流式中断；nil 视为正常完成
	if scanErr := scanner.Err(); scanErr != nil {
		h.logger.Error(fmt.Sprintf("❌ [Codex流式失败] [%s] 端点: %s, 已处理 %d 字节, 错误: %v",
			requestID, endpointName, bytesProcessed, scanErr),
			"provider", "codex", "request_id", requestID)
	} else {
		h.logger.Info(fmt.Sprintf("✅ [Codex流式完成] [%s] 端点: %s, 已处理 %d 字节",
			requestID, endpointName, bytesProcessed),
			"provider", "codex", "request_id", requestID)
	}

	// 🆕 [Codex 生命周期] 增强的完成日志（含端点、渠道、token、状态码、耗时、跳数）
	durationMs := time.Since(startTime).Milliseconds()
	modelName := requestModel
	var inputTokens, outputTokens, cacheReadTokens, totalTokens int64
	if lastResult != nil {
		modelName = pickModelName(lastResult.Model, requestModel)
		inputTokens = lastResult.InputTokens
		outputTokens = lastResult.OutputTokens
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

	h.recordSuccess(requestID, startTime, resp.StatusCode, lastResult, requestModel)
}

// ============================================
// 追踪辅助方法
// ============================================

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

// recordStart 记录请求开始
// 一次性写入 Provider/Channel/Model（来自请求体），减少与归档之间的字段缺失风险
func (h *Handler) recordStart(requestID string, r *http.Request, isStreaming bool, channelName, requestModel string) {
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
	h.usageTracker.RecordRequestUpdate(requestID, opts)
}

// recordForwarding 记录转发状态（含端点链路 + 渠道）
// chain 以 JSON 数组形式写入（形如 [{"hop":1,"channel":"xxx","endpoint":"yyy"}]），
// 与 Claude 端 internal/proxy 写入的结构化格式一致，供前端统一解析与展示。
func (h *Handler) recordForwarding(requestID string, endpointName, channelName string, chain []chainNode) {
	if h.usageTracker == nil {
		return
	}
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
		h.usageTracker.RecordRequestUpdate(requestID, opts)
		h.snapshotEndpointMultiplier(requestID, channelName, endpointName)
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
	h.usageTracker.RecordRequestUpdate(requestID, opts)
	h.snapshotEndpointMultiplier(requestID, channelName, endpointName)
}

// snapshotEndpointMultiplier 将 Codex 端点的倍率快照写入热池中的活跃请求
// 用于详情页计费过程展示
func (h *Handler) snapshotEndpointMultiplier(requestID, channelName, endpointName string) {
	if h.usageTracker == nil || requestID == "" || endpointName == "" {
		return
	}
	key := tracking.EndpointMultiplierKey(channelName, endpointName)
	mult := h.usageTracker.GetEndpointMultiplier(key)
	_ = h.usageTracker.UpdateActiveRequestMultiplier(requestID, mult)
}

// pickModelName 在响应未携带 model 时回退到请求体 model
func pickModelName(responseModel, requestModel string) string {
	if responseModel != "" {
		return responseModel
	}
	return requestModel
}

// buildTokenUsage 从 TokenResult 构造 tracking.TokenUsage（包含缓存读取）
func buildTokenUsage(tr *TokenResult) *tracking.TokenUsage {
	if tr == nil {
		return nil
	}
	return &tracking.TokenUsage{
		InputTokens:     tr.InputTokens,
		OutputTokens:    tr.OutputTokens,
		CacheReadTokens: tr.CacheReadTokens,
	}
}

// recordSuccess 记录请求成功
func (h *Handler) recordSuccess(requestID string, startTime time.Time, httpStatus int, tokenResult *TokenResult, requestModel string) {
	if h.usageTracker == nil {
		return
	}
	duration := time.Since(startTime)
	var tokens *tracking.TokenUsage
	var modelName string
	if tokenResult != nil {
		modelName = pickModelName(tokenResult.Model, requestModel)
		tokens = buildTokenUsage(tokenResult)
	} else {
		modelName = requestModel
	}
	h.usageTracker.RecordRequestSuccess(requestID, modelName, tokens, duration)
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
		tokens = buildTokenUsage(tokenResult)
	} else {
		modelName = requestModel
	}
	h.usageTracker.RecordRequestFinalFailure(requestID, modelName, "failed", reason, detail, duration, httpStatus, tokens)
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

// writeError 写入 JSON 错误响应
func (h *Handler) writeError(w http.ResponseWriter, status int, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	fmt.Fprintf(w, `{"error":{"message":"%s","type":"proxy_error"}}`, message)
}
