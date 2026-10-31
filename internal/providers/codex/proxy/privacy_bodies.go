package codexproxy

import (
	"context"
	"net/http"
	"sync"

	"github.com/wsyangzy/Vane/internal/privacy"
	"github.com/wsyangzy/Vane/internal/providers/codex/store"
)

// codexPrivacyBodies 保存一次全局隐私处理结果，供全部端点尝试复用。
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

// outboundBody 在首次实际发送替身时记录摘要，渠道不再覆盖全局策略。
func (h *Handler) outboundBody(r *http.Request, requestID string, _ *store.CodexChannelRecord, fallback []byte) []byte {
	if r == nil {
		return fallback
	}
	bodies, ok := r.Context().Value(codexPrivacyKey{}).(*codexPrivacyBodies)
	if !ok || bodies == nil {
		return fallback
	}
	body, redacted := bodies.result.SelectBody(bodies.original)
	if redacted {
		bodies.recorded.Do(func() {
			summary := bodies.result
			summary.Changed = true
			h.recordPrivacyRedaction(requestID, privacy.SummaryToJSON(summary))
		})
	}
	return body
}
