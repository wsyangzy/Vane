package handlers

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
)

func withFinalStatusCode(ctx context.Context, statusCode int) context.Context {
	return context.WithValue(ctx, "final_status_code", statusCode)
}

// failoverTerminationStatusCode 故障转移终止时优先透传上游最后一次的真实状态码；
// 上游从未响应过时返回 Relay 自己的 502 网关兜底码。
func failoverTerminationStatusCode(lastErr error, lastResp *http.Response) int {
	if statusCode := GetStatusCodeFromError(lastErr, lastResp); statusCode != 0 {
		return statusCode
	}
	return http.StatusBadGateway
}

// localFailureBody 请求从未拿到提供商响应时给客户端的正文：只回 Relay 的失败分类标识，
// 不编造提供商响应；该标识不写入追踪（追踪已有 failure_reason 列，人类可读的说明由前端按分类渲染）。
func localFailureBody(reason, upstreamMsg string) string {
	if upstreamMsg != "" {
		return upstreamMsg
	}
	return reason
}

// failRequest 只有拿到真实上游状态时才把状态码写入追踪；本地失败不记状态码。
func failRequest(lifecycleManager RequestLifecycleManager, reason, detail string, lastErr error, lastResp *http.Response) {
	if upstreamStatus := GetStatusCodeFromError(lastErr, lastResp); upstreamStatus != 0 {
		lifecycleManager.FailRequestWithUpstreamStatus(reason, detail, upstreamStatus)
		return
	}
	lifecycleManager.FailRequest(reason, detail)
}

func (rh *RegularHandler) writeFailoverTermination(w http.ResponseWriter, r *http.Request, lifecycleManager RequestLifecycleManager, failedEndpointNames []string, lastErr error, errText string, isLoopLimit bool, connID string) {
	// 传递 nil 而不是 err，保留端点在 SetEndpointLastRequestFail 中设置的真实失败原因。
	rh.endpointManager.MarkEndpointsRequestFailed(failedEndpointNames, nil)
	statusCode := failoverTerminationStatusCode(lastErr, nil)
	failureReason := "all_channels_unavailable"
	if isLoopLimit {
		failureReason = "max_loops_exceeded"
	}
	errMsg := ExtractUpstreamErrorMessage(lastErr)
	*r = *r.WithContext(withFinalStatusCode(r.Context(), statusCode))
	failRequest(lifecycleManager, failureReason, errMsg, lastErr, nil)
	rh.endpointManager.ClearEndpointsRequestFailure(failedEndpointNames)
	slog.Warn(fmt.Sprintf("🛑 [故障转移终止] [%s] %s (立即返回错误)", connID, errText))
	WriteUpstreamErrorResponse(w, lastErr, localFailureBody(failureReason, errMsg), statusCode)
}

func (rh *RegularHandler) writeRegularEndpointExhausted(w http.ResponseWriter, r *http.Request, lifecycleManager RequestLifecycleManager, failedEndpointNames []string, lastErr error, connID string) {
	errMsg := ExtractUpstreamErrorMessage(lastErr)
	statusCode := GetStatusCodeFromError(lastErr, nil)
	if statusCode == 0 {
		statusCode = http.StatusBadGateway
	}
	*r = *r.WithContext(withFinalStatusCode(r.Context(), statusCode))
	failRequest(lifecycleManager, "endpoint_exhausted", errMsg, lastErr, nil)
	rh.endpointManager.ClearEndpointsRequestFailure(failedEndpointNames)
	slog.Warn(fmt.Sprintf("⚠️ [当前渠道端点耗尽] [%s] %s (立即返回错误)", connID, errMsg))
	WriteUpstreamErrorResponse(w, lastErr, localFailureBody("endpoint_exhausted", errMsg), statusCode)
}

func (sh *StreamingHandler) writeStreamingFailoverTermination(w http.ResponseWriter, r *http.Request, lifecycleManager RequestLifecycleManager, failedEndpointNames []string, lastErr error, errText string, isLoopLimit bool, connID string) {
	// 传递 nil 而不是 err 或 lastErr，保留端点在 SetEndpointLastRequestFail 中设置的真实失败原因。
	sh.endpointManager.MarkEndpointsRequestFailed(failedEndpointNames, nil)

	statusCode := failoverTerminationStatusCode(lastErr, nil)
	failureReason := "all_channels_unavailable"
	if isLoopLimit {
		failureReason = "max_loops_exceeded"
	}
	errMsg := ExtractUpstreamErrorMessage(lastErr)
	*r = *r.WithContext(withFinalStatusCode(r.Context(), statusCode))
	failRequest(lifecycleManager, failureReason, errMsg, lastErr, nil)
	sh.endpointManager.ClearEndpointsRequestFailure(failedEndpointNames)
	slog.Warn(fmt.Sprintf("🛑 [故障转移终止] [%s] %s (立即返回错误)", connID, errText))
	clearStreamingHeaders(w)
	WriteUpstreamErrorResponse(w, lastErr, localFailureBody(failureReason, errMsg), statusCode)
}

func (sh *StreamingHandler) writeStreamingEndpointExhausted(w http.ResponseWriter, r *http.Request, lifecycleManager RequestLifecycleManager, failedEndpointNames []string, lastErr error, connID string) {
	sh.writeStreamingTerminalFailure(w, r, lifecycleManager, failedEndpointNames, lastErr, "endpoint_exhausted", http.StatusBadGateway)
	slog.Warn(fmt.Sprintf("⚠️ [当前渠道端点耗尽] [%s] err=%s", connID, ExtractUpstreamErrorMessage(lastErr)))
}

func (sh *StreamingHandler) writeStreamingTerminalFailure(w http.ResponseWriter, r *http.Request, lifecycleManager RequestLifecycleManager, failedEndpointNames []string, lastErr error, failureReason string, fallbackStatusCode int) {
	errMsg := ExtractUpstreamErrorMessage(lastErr)
	statusCode := GetStatusCodeFromError(lastErr, nil)
	if statusCode == 0 {
		statusCode = fallbackStatusCode
	}
	if statusCode == 0 {
		statusCode = http.StatusBadGateway
	}
	*r = *r.WithContext(withFinalStatusCode(r.Context(), statusCode))
	failRequest(lifecycleManager, failureReason, errMsg, lastErr, nil)
	sh.endpointManager.ClearEndpointsRequestFailure(failedEndpointNames)
	clearStreamingHeaders(w)
	WriteUpstreamErrorResponse(w, lastErr, localFailureBody(failureReason, errMsg), statusCode)
}
