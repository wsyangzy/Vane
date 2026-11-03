package codexproxy

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"time"

	"github.com/wsyangzy/Vane/internal/tracking"
)

func (h *Handler) recordOutputSent(requestID string) {
	if h.usageTracker == nil {
		return
	}
	h.usageTracker.RecordOutputSent(requestID)
}

// recordStart 记录请求开始
// 一次性写入 Provider/Channel/Model（来自请求体），减少与归档之间的字段缺失风险
func (h *Handler) recordStart(requestID string, r *http.Request, isStreaming bool, channelName, requestModel string, modelRewrite modelRewriteResult, reasoningJSON, serviceTier string) {
	if h.usageTracker == nil {
		return
	}
	h.usageTracker.RecordRequestStart(requestID, r.RemoteAddr, r.Header.Get("User-Agent"), r.Method, r.URL.Path, isStreaming)

	opts := tracking.UpdateOptions{
		Provider:             strPtr("codex"),
		RequestedServiceTier: &serviceTier,
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

// recordPrivacyRedaction 记录隐私脱敏摘要。
//
// summaryJSON 只含规则名与命中数。命中原文与请求正文一律不得进入日志或数据库：
// 日志会广播到前端日志查看器并落盘，正文进来就等于把整段客户对话又存了一份。
func (h *Handler) recordPrivacyRedaction(requestID string, summaryJSON string) {
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
	)
	if h.usageTracker != nil {
		h.usageTracker.RecordPrivacyRedaction(requestID, summaryJSON)
	}
}

// snapshotEndpointMultiplier 将 Codex 端点的倍率快照写入热池中的活跃请求
// 用于详情页计费过程展示
func (h *Handler) snapshotEndpointMultiplier(requestID, channelName, groupName, endpointName, modelName string) {
	if h.usageTracker == nil || requestID == "" || endpointName == "" {
		return
	}
	mult := h.usageTracker.GetEndpointMultiplierForEndpointAndModelWithGroup("codex", channelName, groupName, endpointName, modelName)
	_ = h.usageTracker.UpdateActiveRequestMultiplier(requestID, mult)
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
	mult := h.usageTracker.GetEndpointMultiplierForEndpointWithGroup("codex", channelName, groupName, endpointName)
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
		CacheWrite30mTokens:      tr.CacheWrite30mTokens,
		CacheWrite12hTokens:      tr.CacheWrite12hTokens,
		CacheReadTokens:          tr.CacheReadTokens,
		InputIncludesCacheRead:   true,
	}
}

// recordSuccess 记录请求成功
func (h *Handler) recordSuccess(requestID string, startTime time.Time, httpStatus int, tokenResult *TokenResult, requestModel, endpointName, groupName, channelName string, qualityReason ...string) {
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
			h.usageTracker.RecordRequestUpdate(requestID, tracking.UpdateOptions{ResponseServiceTier: &serviceTier})
		}
	} else {
		modelName = requestModel
	}
	h.snapshotEndpointMultiplier(requestID, channelName, groupName, endpointName, modelName)
	reason := ""
	if len(qualityReason) > 0 {
		reason = qualityReason[0]
	}
	h.usageTracker.RecordRequestSuccessWithHTTPStatusAndQuality(requestID, modelName, tokens, duration, httpStatus, reason)
}

// recordFailure 记录没有拿到上游 HTTP 响应的最终失败：不接受也不记录状态码，
// 给客户端的兜底码是协议必需，与追踪无关（对齐 Claude 侧 FailRequest）。
func (h *Handler) recordFailure(requestID string, startTime time.Time, reason string, detail string, tokenResult *TokenResult, requestModel string) {
	if h.usageTracker == nil {
		return
	}
	duration := time.Since(startTime)
	var tokens *tracking.TokenUsage
	var modelName string
	if tokenResult != nil {
		modelName = pickModelName(tokenResult.Model, requestModel)
		tokens = buildTokenUsage(tokenResult, tracking.BillingOutputTokenSourceOutput)
		if tokenResult.ServiceTier != "" {
			tier := tokenResult.ServiceTier
			h.usageTracker.RecordRequestUpdate(requestID, tracking.UpdateOptions{ResponseServiceTier: &tier})
		}
	} else {
		modelName = requestModel
	}
	h.usageTracker.RecordRequestFinalFailure(requestID, modelName, "failed", reason, detail, duration, 0, tokens)
}

func (h *Handler) recordUpstreamFailure(requestID string, startTime time.Time, reason string, detail string, httpStatus int, tokenResult *TokenResult, requestModel string) {
	if h.usageTracker == nil {
		return
	}
	duration := time.Since(startTime)
	var tokens *tracking.TokenUsage
	modelName := requestModel
	if tokenResult != nil {
		modelName = pickModelName(tokenResult.Model, requestModel)
		tokens = buildTokenUsage(tokenResult, tracking.BillingOutputTokenSourceOutput)
		if tokenResult.ServiceTier != "" {
			tier := tokenResult.ServiceTier
			h.usageTracker.RecordRequestUpdate(requestID, tracking.UpdateOptions{ResponseServiceTier: &tier})
		}
	}
	h.usageTracker.RecordRequestFinalFailureWithUpstreamStatus(requestID, modelName, "failed", reason, detail, duration, httpStatus, tokens)
}

// recordCancelled 客户端取消是 Relay 对下游连接的观察，不是上游响应，同样不记状态码。
func (h *Handler) recordCancelled(requestID string, startTime time.Time, reason string, tokenResult *TokenResult, requestModel, endpointName, groupName, channelName string) {
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
			h.usageTracker.RecordRequestUpdate(requestID, tracking.UpdateOptions{ResponseServiceTier: &serviceTier})
		}
	}
	h.snapshotEndpointMultiplier(requestID, channelName, groupName, endpointName, modelName)
	h.usageTracker.RecordRequestFinalFailure(requestID, modelName, "cancelled", reason, "", duration, 0, tokens)
}

// ============================================
// 响应辅助方法
// ============================================
