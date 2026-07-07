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

func (rh *RegularHandler) writeFailoverTermination(w http.ResponseWriter, r *http.Request, lifecycleManager RequestLifecycleManager, failedEndpointNames []string, lastErr error, errText string, isLoopLimit bool, connID string) {
	// 传递 nil 而不是 err，保留端点在 SetEndpointLastRequestFail 中设置的真实失败原因。
	rh.endpointManager.MarkEndpointsRequestFailed(failedEndpointNames, nil)
	statusCode := http.StatusUnprocessableEntity
	failureReason := "all_channels_unavailable"
	if isLoopLimit {
		failureReason = "max_loops_exceeded"
	}
	*r = *r.WithContext(withFinalStatusCode(r.Context(), statusCode))
	lifecycleManager.FailRequest(failureReason, errText, statusCode)
	rh.endpointManager.ClearEndpointsRequestFailure(failedEndpointNames)
	slog.Warn(fmt.Sprintf("🛑 [故障转移终止] [%s] %s (立即返回错误)", connID, errText))
	WriteUpstreamErrorResponse(w, lastErr, errText, statusCode)
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

func (sh *StreamingHandler) writeStreamingFailoverTermination(w http.ResponseWriter, flusher http.Flusher, r *http.Request, lifecycleManager RequestLifecycleManager, failedEndpointNames []string, errText string, isLoopLimit bool, connID string) {
	// 传递 nil 而不是 err 或 lastErr，保留端点在 SetEndpointLastRequestFail 中设置的真实失败原因。
	sh.endpointManager.MarkEndpointsRequestFailed(failedEndpointNames, nil)

	statusCode := http.StatusUnprocessableEntity
	failureReason := "all_channels_unavailable"
	if isLoopLimit {
		failureReason = "max_loops_exceeded"
	}
	*r = *r.WithContext(withFinalStatusCode(r.Context(), statusCode))
	lifecycleManager.FailRequest(failureReason, errText, statusCode)
	sh.endpointManager.ClearEndpointsRequestFailure(failedEndpointNames)
	slog.Warn(fmt.Sprintf("🛑 [故障转移终止] [%s] %s (立即返回错误)", connID, errText))
	sendStreamInterruptedMessage(w, flusher, errText, modelNameOrUnknown(lifecycleManager))
}

func (sh *StreamingHandler) writeStreamingEndpointExhausted(w http.ResponseWriter, flusher http.Flusher, r *http.Request, lifecycleManager RequestLifecycleManager, failedEndpointNames []string, lastErr error, lastResp *http.Response, connID string) {
	errMsg := ExtractUpstreamErrorMessage(lastErr)
	statusCode := GetStatusCodeFromError(lastErr, lastResp)
	if statusCode == 0 {
		statusCode = http.StatusBadGateway
	}
	*r = *r.WithContext(withFinalStatusCode(r.Context(), statusCode))
	lifecycleManager.FailRequest("endpoint_exhausted", errMsg, statusCode)
	sh.endpointManager.ClearEndpointsRequestFailure(failedEndpointNames)
	slog.Warn(fmt.Sprintf("⚠️ [当前渠道端点耗尽] [%s] status=%d err=%s", connID, statusCode, errMsg))
	sendStreamInterruptedMessage(w, flusher, errMsg, modelNameOrUnknown(lifecycleManager))
}
