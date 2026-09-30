package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"sync"

	"ai-relay-forwarder/internal/privacy"
	"ai-relay-forwarder/internal/providers/claude/proxy/endpoint"
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

// privacyBodies 一次请求的隐私过滤结果：扫描只做一次，每次尝试按目标渠道的隐私级别挑选正文。
//
// 扫描不含渠道维度，因此放在重试循环之外；渠道隐私级别（三方中转强制脱敏、可信渠道只检测）
// 由 forChannel 在选定端点后决定实际发送哪份正文——同一请求故障转移到不同渠道时，
// 发往三方中转的永远是脱敏后的正文。
type privacyBodies struct {
	original []byte
	result   privacy.ApplyResult
	levels   func(channel string) string
	recorder RequestLifecycleManager
	once     sync.Once
}

// forChannel 返回发往该渠道的正文，并在首次实际改写时记录脱敏摘要
func (p *privacyBodies) forChannel(channel string) []byte {
	if p == nil {
		return nil
	}
	level := ""
	if p.levels != nil {
		level = p.levels(channel)
	}
	body, redacted := p.result.SelectBody(level, p.original)
	if redacted && p.recorder != nil {
		p.once.Do(func() {
			summary := p.result
			summary.Changed = true
			p.recorder.RecordPrivacyRedaction(privacy.SummaryToJSON(summary))
		})
	}
	return body
}

// forEndpoint 同 forChannel，以端点所属渠道为准
func (p *privacyBodies) forEndpoint(ep *endpoint.Endpoint) []byte {
	return p.forChannel(endpointRouteName(ep))
}

type privacyBodiesKey struct{}

// contextWithPrivacyBodies 把本请求的隐私正文集合挂到 ctx 上：处理器实例跨请求共享，不能放字段
func contextWithPrivacyBodies(ctx context.Context, bodies *privacyBodies) context.Context {
	if bodies == nil {
		return ctx
	}
	return context.WithValue(ctx, privacyBodiesKey{}, bodies)
}

// outboundBody 选出发往该端点的正文：ctx 上有隐私正文集合时按渠道隐私级别挑选，否则原样使用 fallback。
// 所有外发路径（常规、流式、auto_wait 恢复、路由阶段）都必须经过这里，不能直接用 bodyBytes。
func outboundBody(ctx context.Context, ep *endpoint.Endpoint, fallback []byte) []byte {
	if bodies, ok := ctx.Value(privacyBodiesKey{}).(*privacyBodies); ok && bodies != nil {
		return bodies.forEndpoint(ep)
	}
	return fallback
}

// privacyLevelsFor 从端点管理器的路由快照读取渠道隐私级别
func privacyLevelsFor(em *endpoint.Manager) func(channel string) string {
	return func(channel string) string {
		if em == nil {
			return ""
		}
		return em.RouteConfig().PrivacyLevel(channel)
	}
}

// applyPrivacyFilter 在转发前过滤请求体，返回按渠道挑选正文的结果。
//
// 调用位置固定在重试循环之外、每个请求一次；渠道级差异只影响挑选哪份正文，不重复扫描。
// 全局模式为脱敏时，默认正文（跟随全局的渠道）就是脱敏后的正文。
func applyPrivacyFilter(
	ctx context.Context,
	filter PrivacyFilter,
	r *http.Request,
	providerType string,
	body []byte,
	lifecycleManager RequestLifecycleManager,
	levels func(channel string) string,
) (*privacyBodies, error) {
	bodies := &privacyBodies{
		original: body,
		result:   privacy.ApplyResult{Body: body, Action: privacy.ModeDisabled},
		levels:   levels,
		recorder: lifecycleManager,
	}
	if filter == nil {
		return bodies, nil
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
	if result.Body == nil {
		result.Body = body
	}
	bodies.result = result
	return bodies, nil
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
