package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"

	"ai-relay-forwarder/internal/privacy"
)

// 隐私策略短路时写入 lifecycle 的失败原因
const (
	PrivacyFailureReasonBlocked    = "privacy_blocked"
	PrivacyFailureReasonScanFailed = "privacy_scan_failed"
)

// PrivacyFilter 出站请求隐私过滤接口（由 privacy.Service 实现）。
//
// Apply 返回的 error 只会是 *privacy.PolicyError，调用方必须用
// AsPrivacyPolicyError 识别并短路返回，不允许进入 retry / failover / 端点冷却。
type PrivacyFilter interface {
	Apply(ctx context.Context, req privacy.Request, body []byte) (privacy.ApplyResult, error)
}

// applyPrivacyFilter 在转发前过滤请求体。
//
// 调用位置固定在重试循环之外、每个请求一次：MVP 的规则不含端点/渠道维度，
// 因此无需 per-attempt 过滤，也就不需要请求级的 attempt cache。
// 若日后引入按端点差异化的作用域，必须重新评估这一点。
func applyPrivacyFilter(
	ctx context.Context,
	filter PrivacyFilter,
	r *http.Request,
	providerType string,
	body []byte,
	lifecycleManager RequestLifecycleManager,
) ([]byte, error) {
	if filter == nil {
		return body, nil
	}
	req := privacy.Request{
		Path:         requestPath(r),
		Method:       requestMethod(r),
		ProviderType: providerType,
		ContentType:  requestHeader(r, "Content-Type"),
	}
	if lifecycleManager != nil {
		req.RequestID = lifecycleManager.GetRequestID()
	}

	result, err := filter.Apply(ctx, req, body)
	logPrivacyApplyResult(req, result, err)
	if err != nil {
		return nil, err
	}
	if result.Changed && lifecycleManager != nil {
		lifecycleManager.RecordPrivacyRedaction(privacy.SummaryToJSON(result))
	}
	return result.Body, nil
}

func requestPath(r *http.Request) string {
	if r == nil || r.URL == nil {
		return ""
	}
	return r.URL.Path
}

func requestMethod(r *http.Request) string {
	if r == nil {
		return ""
	}
	return r.Method
}

func requestHeader(r *http.Request, key string) string {
	if r == nil {
		return ""
	}
	return r.Header.Get(key)
}

// AsPrivacyPolicyError 识别隐私策略短路错误
func AsPrivacyPolicyError(err error) *privacy.PolicyError {
	if err == nil {
		return nil
	}
	var policyErr *privacy.PolicyError
	if errors.As(err, &policyErr) {
		return policyErr
	}
	return nil
}

// PrivacyFailureReason 将策略错误映射为 lifecycle 失败原因
func PrivacyFailureReason(policyErr *privacy.PolicyError) string {
	if policyErr != nil && policyErr.Code == privacy.CodeScanFailed {
		return PrivacyFailureReasonScanFailed
	}
	return PrivacyFailureReasonBlocked
}

// WritePrivacyPolicyErrorResponse 向客户端写出 JSON 策略错误（不含命中原文）
func WritePrivacyPolicyErrorResponse(w http.ResponseWriter, policyErr *privacy.PolicyError) {
	if policyErr == nil || w == nil {
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(policyErr.StatusCode)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"error": map[string]any{
			"type":    policyErr.Code,
			"message": policyErr.Message,
		},
	})
}

// logPrivacyApplyResult 记录命中摘要。
//
// 只输出规则名与命中数，绝不输出命中原文，也绝不输出请求正文——
// 日志会被广播到前端日志查看器并写入日志文件，正文一旦进来就等于
// 把整段客户对话又落了一份盘。
func logPrivacyApplyResult(req privacy.Request, result privacy.ApplyResult, err error) {
	if err != nil {
		slog.Warn(fmt.Sprintf("🛡️ [隐私防护] [%s] 请求被策略拒绝: %v (path=%s provider=%s)",
			req.RequestID, err, req.Path, req.ProviderType))
		return
	}
	if result.Action == privacy.ModeDisabled {
		return
	}
	if result.HitCount == 0 && !result.Truncated && result.SkippedReason == "" {
		return
	}
	names := make([]string, 0, len(result.RuleHits))
	for _, hit := range result.RuleHits {
		names = append(names, fmt.Sprintf("%s(%d)", hit.RuleName, hit.Count))
	}
	slog.Info(fmt.Sprintf("🛡️ [隐私防护] [%s] action=%s hits=%d rules=[%s] changed=%t truncated=%t skipped=%q duration=%s path=%s provider=%s",
		req.RequestID, result.Action, result.HitCount, strings.Join(names, ","),
		result.Changed, result.Truncated, result.SkippedReason, result.ScanDuration,
		req.Path, req.ProviderType))
}
