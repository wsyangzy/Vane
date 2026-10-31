package tracking

import (
	"time"

	_ "modernc.org/sqlite"
)

// UsageStatsDetailed 详细的使用统计
type UsageStatsDetailed struct {
	TotalRequests   int64                   `json:"total_requests"`
	SuccessRequests int64                   `json:"success_requests"`
	ErrorRequests   int64                   `json:"error_requests"`
	TotalTokens     int64                   `json:"total_tokens"`
	TotalCost       float64                 `json:"total_cost"`
	ModelStats      map[string]ModelStat    `json:"model_stats"`
	EndpointStats   map[string]EndpointStat `json:"endpoint_stats"`
	GroupStats      map[string]GroupStat    `json:"group_stats"`
}

// ModelStat 模型统计
type ModelStat struct {
	RequestCount int64   `json:"request_count"`
	TotalCost    float64 `json:"total_cost"`
}

// EndpointStat 端点统计
type EndpointStat struct {
	RequestCount int64   `json:"request_count"`
	TotalCost    float64 `json:"total_cost"`
}

// GroupStat 组统计
type GroupStat struct {
	RequestCount int64   `json:"request_count"`
	TotalCost    float64 `json:"total_cost"`
}

// RequestEvent 表示请求事件
type RequestEvent struct {
	Type      string      `json:"type"` // "start", "flexible_update", "success", "final_failure", "complete", "failed_request_tokens", "token_recovery"
	RequestID string      `json:"request_id"`
	Timestamp time.Time   `json:"timestamp"`
	Data      interface{} `json:"data"` // 根据Type不同而变化
}

// RequestStartData 请求开始事件数据
type RequestStartData struct {
	ClientIP    string `json:"client_ip"`
	UserAgent   string `json:"user_agent"`
	Method      string `json:"method"`
	Path        string `json:"path"`
	IsStreaming bool   `json:"is_streaming"` // 是否为流式请求
}

// RequestUpdateData 请求更新事件数据
type RequestUpdateData struct {
	Channel              string `json:"channel"`
	EndpointName         string `json:"endpoint_name"`
	GroupName            string `json:"group_name"`
	Status               string `json:"status"`
	RetryCount           int    `json:"retry_count"`
	HTTPStatus           int    `json:"http_status"`
	HTTPStatusIsUpstream bool   `json:"http_status_is_upstream"`
	// 🚀 [状态机重构] Phase 2: 新增失败原因和取消原因字段
	FailureReason string `json:"failure_reason,omitempty"`
	CancelReason  string `json:"cancel_reason,omitempty"`
}

func normalizeUpstreamHTTPStatus(code int, isUpstream bool) (int, bool) {
	if !isUpstream || code <= 0 {
		return 0, false
	}
	return code, true
}

// RequestCompleteData 请求完成事件数据
type RequestCompleteData struct {
	ModelName                string        `json:"model_name"`
	InputTokens              int64         `json:"input_tokens"`
	OutputTokens             int64         `json:"output_tokens"`
	ReasoningTokens          int64         `json:"reasoning_tokens"`
	BillableOutputTokens     int64         `json:"billable_output_tokens"`
	BillingOutputTokenSource string        `json:"billing_output_token_source"`
	CacheCreationTokens      int64         `json:"cache_creation_tokens"`
	CacheCreation5mTokens    int64         `json:"cache_creation_5m_tokens"`
	CacheCreation1hTokens    int64         `json:"cache_creation_1h_tokens"`
	CacheReadTokens          int64         `json:"cache_read_tokens"`
	Duration                 time.Duration `json:"duration"`
	FailureReason            string        `json:"failure_reason,omitempty"` // 可选：失败原因
	InputIncludesCacheRead   bool          `json:"-"`                        // OpenAI/Codex input_tokens 包含缓存读取和写入，仅计费时扣除
}

// TokenUsage token使用统计
type TokenUsage struct {
	CacheWrite30mTokens      int64
	CacheWrite12hTokens      int64
	InputTokens              int64
	OutputTokens             int64
	ReasoningTokens          int64
	BillableOutputTokens     int64
	BillingOutputTokenSource string
	CacheCreationTokens      int64 // 总缓存创建 tokens（向后兼容，= 5m + 1h）
	CacheReadTokens          int64
	CacheCreation5mTokens    int64 // 5分钟缓存创建 tokens (1.25x 定价)
	CacheCreation1hTokens    int64 // 1小时缓存创建 tokens (2x 定价)
	InputIncludesCacheRead   bool  // OpenAI/Codex input_tokens 包含缓存读取和写入，仅计费时扣除
}

// UpdateOptions 统一的请求更新选项
// 支持可选字段更新，只更新非nil的字段
type UpdateOptions struct {
	RequestedServiceTier *string
	ResponseServiceTier  *string
	EndpointName         *string        // 端点名称
	EndpointChain        *string        // 端点链路（形如 "A -> B -> C"）
	Channel              *string        // 渠道标签
	GroupName            *string        // 组名称
	Status               *string        // 状态
	RetryCount           *int           // 重试次数
	HttpStatus           *int           // HTTP状态码
	HttpStatusIsUpstream *bool          // HTTP状态码是否来自真实上游响应
	ModelName            *string        // 模型名称
	OriginalModelName    *string        // 改写前的原始请求模型
	ModelRewriteSource   *string        // 模型改写来源：mapping/channel_model/route
	ModelRewriteTarget   *string        // 改写后的目标模型
	RouteSnapshot        *string        // 请求级路由快照 JSON（只含规则标识与阶段序号）
	PrivacyRedacted      *bool          // 是否命中隐私脱敏
	PrivacySummary       *string        // 隐私脱敏摘要（仅规则名与命中数，绝不含原文）
	EndTime              *time.Time     // 结束时间
	Duration             *time.Duration // 持续时间
	FailureReason        *string        // 失败原因（用于中间过程记录）
	Provider             *string        // 提供商标识: claude/codex
	ReasoningEffort      *string        // reasoning 配置 JSON；effort 常见为 minimal/low/medium/high/xhigh
	ServiceTier          *string        // 服务档位: auto/default/batch/flex/priority/fast
}
