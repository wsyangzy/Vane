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

func modelNameOrUnknown(lifecycleManager RequestLifecycleManager) string {
	modelName := lifecycleManager.GetModelName()
	if modelName == "" {
		return "unknown"
	}
	return modelName
}

// failoverTerminationStatusCode 故障转移终止时的状态码：优先透传上游最后一次的真实状态码，
// 上游从未响应过（纯本地失败）才兜底 502。反代不自造状态码——提供商没返回过的码不该出现在链路里。
func failoverTerminationStatusCode(lastErr error, lastResp *http.Response) int {
	if statusCode := GetStatusCodeFromError(lastErr, lastResp); statusCode != 0 {
		return statusCode
	}
	return http.StatusBadGateway
}

func (rh *RegularHandler) writeFailoverTermination(w http.ResponseWriter, r *http.Request, lifecycleManager RequestLifecycleManager, failedEndpointNames []string, lastErr error, errText string, isLoopLimit bool, connID string) {
	// 传递 nil 而不是 err，保留端点在 SetEndpointLastRequestFail 中设置的真实失败原因。
	rh.endpointManager.MarkEndpointsRequestFailed(failedEndpointNames, nil)
	statusCode := failoverTerminationStatusCode(lastErr, nil)
	failureReason := "all_channels_unavailable"
	if isLoopLimit {
		failureReason = "max_loops_exceeded"
	}
	*r = *r.WithContext(withFinalStatusCode(r.Context(), statusCode))
	lifecycleManager.FailRequest(failureReason, ExtractUpstreamErrorMessage(lastErr), statusCode)
	rh.endpointManager.ClearEndpointsRequestFailure(failedEndpointNames)
	slog.Warn(fmt.Sprintf("🛑 [故障转移终止] [%s] %s (立即返回错误)", connID, errText))
	WriteUpstreamErrorResponse(w, lastErr, "", statusCode)
}

func (rh *RegularHandler) writeRegularEndpointExhausted(w http.ResponseWriter, r *http.Request, lifecycleManager RequestLifecycleManager, failedEndpointNames []string, lastErr error, connID string) {
	errMsg := ExtractUpstreamErrorMessage(lastErr)
	statusCode := GetStatusCodeFromError(lastErr, nil)
	if statusCode == 0 {
		statusCode = http.StatusBadGateway
	}
	*r = *r.WithContext(withFinalStatusCode(r.Context(), statusCode))
	lifecycleManager.FailRequest("endpoint_exhausted", errMsg, statusCode)
	rh.endpointManager.ClearEndpointsRequestFailure(failedEndpointNames)
	slog.Warn(fmt.Sprintf("⚠️ [当前渠道端点耗尽] [%s] %s (立即返回错误)", connID, errMsg))
	WriteUpstreamErrorResponse(w, lastErr, errMsg, statusCode)
}

func (sh *StreamingHandler) writeStreamingFailoverTermination(w http.ResponseWriter, r *http.Request, lifecycleManager RequestLifecycleManager, failedEndpointNames []string, lastErr error, errText string, isLoopLimit bool, connID string) {
	// 传递 nil 而不是 err 或 lastErr，保留端点在 SetEndpointLastRequestFail 中设置的真实失败原因。
	sh.endpointManager.MarkEndpointsRequestFailed(failedEndpointNames, nil)

	statusCode := failoverTerminationStatusCode(lastErr, nil)
	failureReason := "all_channels_unavailable"
	if isLoopLimit {
		failureReason = "max_loops_exceeded"
	}
	*r = *r.WithContext(withFinalStatusCode(r.Context(), statusCode))
	lifecycleManager.FailRequest(failureReason, ExtractUpstreamErrorMessage(lastErr), statusCode)
	sh.endpointManager.ClearEndpointsRequestFailure(failedEndpointNames)
	slog.Warn(fmt.Sprintf("🛑 [故障转移终止] [%s] %s (立即返回错误)", connID, errText))
	clearStreamingHeaders(w)
	WriteUpstreamErrorResponse(w, lastErr, "", statusCode)
}

func (sh *StreamingHandler) writeStreamingEndpointExhausted(w http.ResponseWriter, r *http.Request, lifecycleManager RequestLifecycleManager, failedEndpointNames []string, lastErr error, connID string) {
	errMsg := ExtractUpstreamErrorMessage(lastErr)
	statusCode := GetStatusCodeFromError(lastErr, nil)
	if statusCode == 0 {
		statusCode = http.StatusBadGateway
	}
	*r = *r.WithContext(withFinalStatusCode(r.Context(), statusCode))
	lifecycleManager.FailRequest("endpoint_exhausted", errMsg, statusCode)
	sh.endpointManager.ClearEndpointsRequestFailure(failedEndpointNames)
	slog.Warn(fmt.Sprintf("⚠️ [当前渠道端点耗尽] [%s] status=%d err=%s", connID, statusCode, errMsg))
	clearStreamingHeaders(w)
	WriteUpstreamErrorResponse(w, lastErr, "", statusCode)
}
