package codexproxy

import (
	"context"
	"net/http"
	"sync"

	"ai-relay-forwarder/internal/privacy"
	"ai-relay-forwarder/internal/providers/codex/store"
)

// codexPrivacyBodies 一次请求的隐私过滤结果：扫描只做一次（在 ServeHTTP 入口），
// 每次尝试选定端点后按目标渠道的隐私级别挑选实际发送的正文。
type codexPrivacyBodies struct {
	original []byte
	result   privacy.ApplyResult
	// recorded 脱敏摘要只在第一次真正发出改写正文时记录一次（与 Claude 侧 privacyBodies 一致）
	recorded sync.Once
}

type codexPrivacyKey struct{}

func contextWithCodexPrivacy(ctx context.Context, bodies *codexPrivacyBodies) context.Context {
	if bodies == nil {
		return ctx
	}
	return context.WithValue(ctx, codexPrivacyKey{}, bodies)
}

// outboundBody 选出发往该渠道的正文；请求未经隐私过滤时原样返回 fallback。
//
// 脱敏摘要在这里、而不是在入口处记录：入口处还不知道请求最终发往哪个渠道。
// 全局为「脱敏」时，发往可信渠道的仍是原文；若入口提前记录，请求详情会误标「已脱敏」。
// 只要本次请求有任一次尝试发出了改写正文就记录（同一请求可能先打可信渠道、再转移到三方中转）。
func (h *Handler) outboundBody(r *http.Request, requestID string, channel *store.CodexChannelRecord, fallback []byte) []byte {
	if r == nil {
		return fallback
	}
	bodies, ok := r.Context().Value(codexPrivacyKey{}).(*codexPrivacyBodies)
	if !ok || bodies == nil {
		return fallback
	}
	level := ""
	if channel != nil {
		level = channel.PrivacyLevel
	}
	body, redacted := bodies.result.SelectBody(level, bodies.original)
	if redacted {
		bodies.recorded.Do(func() {
			summary := bodies.result
			summary.Changed = true
			h.recordPrivacyRedaction(requestID, privacy.SummaryToJSON(summary))
		})
	}
	return body
}
