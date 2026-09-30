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
	// recorded 全局模式未改写、但某次尝试因渠道级别强制脱敏时，只补记一次摘要
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
// fallback 是全局模式下的正文（ServeHTTP 已把它设为 result.Body），跟随全局的渠道直接用它。
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
	switch privacy.NormalizeChannelLevel(level) {
	case privacy.ChannelLevelDefault:
		return fallback
	case privacy.ChannelLevelTrusted:
		return bodies.original
	}
	body, redacted := bodies.result.SelectBody(level, bodies.original)
	// 全局已脱敏时入口处已记过摘要；这里只补记「渠道强制脱敏」这一种情况
	if redacted && !bodies.result.Changed {
		bodies.recorded.Do(func() {
			summary := bodies.result
			summary.Changed = true
			h.recordPrivacyRedaction(requestID, privacy.SummaryToJSON(summary))
		})
	}
	return body
}
